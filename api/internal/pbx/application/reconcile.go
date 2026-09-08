package application

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// Reconciler compares what the engine holds against what the platform
// says it should hold, and repairs the difference on demand.
//
// It is operator-invoked and never scheduled (design D6). A transaction
// already prevents drift caused by our own code, so any drift that
// appears is either a bug or a human editing the database directly.
// Repairing that silently on a timer hides both; reporting loudly and
// repairing when asked keeps the signal.
//
// The platform's records are the sole source of truth. Repair only ever
// makes the engine match them — it never writes a domain row to match a
// projection, because a row someone typed into the engine is not a
// configuration decision the platform made.
type Reconciler struct {
	pool      *corepostgres.Pool
	store     ports.ExtensionStore
	projector ports.EndpointProjector
	inspector ports.ProjectionReconciler
}

// NewReconciler wires the reconciler. The inspector is a separate port
// from the projector because interrogating engine state is a diagnostic
// capability only this command needs — keeping them apart stops an
// ordinary handler acquiring it.
func NewReconciler(
	pool *corepostgres.Pool,
	store ports.ExtensionStore,
	projector ports.EndpointProjector,
	inspector ports.ProjectionReconciler,
) *Reconciler {
	return &Reconciler{pool: pool, store: store, projector: projector, inspector: inspector}
}

// Divergence is one extension whose engine state disagrees with the
// platform's.
type Divergence struct {
	ExtensionID shareddomain.ExtensionID
	TenantID    shareddomain.TenantID
	Number      string
	// Fields names what differs — "missing" when the engine holds nothing
	// for this extension at all.
	Fields []string
}

// Report is the outcome of a comparison.
type Report struct {
	// Diverged are extensions the platform knows about whose projection is
	// absent or wrong.
	Diverged []Divergence
	// Orphaned are projected endpoints attributable to an extension that
	// no longer exists. A device could still register against one, so an
	// orphan is a live endpoint nobody is administering.
	Orphaned []shareddomain.ExtensionID
	// Unattributable are engine rows this ACL did not write and cannot
	// explain. Reported, never repaired: deleting a row nobody can account
	// for is how a reconciler turns a mystery into an outage.
	Unattributable []string
	// Checked is how many extensions were compared, so a clean report is
	// distinguishable from one that examined nothing.
	Checked int
}

// Clean reports whether the engine and the platform agree.
func (r Report) Clean() bool {
	return len(r.Diverged) == 0 && len(r.Orphaned) == 0 && len(r.Unattributable) == 0
}

// Reconcile compares every tenant's extensions against the engine.
//
// It enumerates tenants rather than requiring one, because drift is not a
// tenant-scoped question: an operator asking "is the engine consistent"
// needs the whole answer. `tenants` carries no RLS — it is the root of
// tenancy — so listing it needs no privileged role, and each tenant's
// extensions are then read under that tenant's own context.
func (r *Reconciler) Reconcile(ctx context.Context) (Report, error) {
	var report Report

	inventory, err := r.inspector.ListProjected(ctx)
	if err != nil {
		return Report{}, err
	}
	report.Unattributable = inventory.Unattributable

	projected := make(map[shareddomain.ExtensionID]struct{}, len(inventory.Extensions))
	for _, id := range inventory.Extensions {
		projected[id] = struct{}{}
	}

	tenants, err := r.listTenants(ctx)
	if err != nil {
		return Report{}, err
	}

	known := make(map[shareddomain.ExtensionID]struct{}, len(projected))
	for _, tenantID := range tenants {
		var extensions []domain.Extension
		if err := r.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			extensions, err = r.store.List(ctx, tx, tenantID, ports.Page{Limit: allExtensions})
			return err
		}); err != nil {
			return Report{}, fmt.Errorf("list extensions for tenant %s: %w", tenantID, err)
		}

		for _, ext := range extensions {
			known[ext.ID] = struct{}{}
			report.Checked++

			fields, err := r.inspector.DiffExtension(ctx, ext)
			if err != nil {
				return Report{}, err
			}
			if len(fields) > 0 {
				report.Diverged = append(report.Diverged, Divergence{
					ExtensionID: ext.ID,
					TenantID:    ext.TenantID,
					Number:      ext.Number,
					Fields:      fields,
				})
			}
		}
	}

	for id := range projected {
		if _, ok := known[id]; !ok {
			report.Orphaned = append(report.Orphaned, id)
		}
	}
	sort.Slice(report.Orphaned, func(i, j int) bool {
		return report.Orphaned[i].String() < report.Orphaned[j].String()
	})

	return report, nil
}

// allExtensions is the page size reconciliation reads with. Drift is
// whole-estate by nature, so paging through it would only add a way to
// miss half of it.
const allExtensions = 100000

// Repair makes the engine match the platform for everything the report
// found, and returns what it changed.
//
// Unattributable rows are deliberately left alone — see Report.
func (r *Reconciler) Repair(ctx context.Context, report Report) (repaired int, err error) {
	for _, d := range report.Diverged {
		if err := r.pool.WithTenant(ctx, d.TenantID, func(ctx context.Context, tx pgx.Tx) error {
			ext, err := r.store.Get(ctx, tx, d.TenantID, d.ExtensionID)
			if err != nil {
				return err
			}
			return r.projector.ProjectExtension(ctx, tx, ext)
		}); err != nil {
			return repaired, fmt.Errorf("repair extension %s: %w", d.ExtensionID, err)
		}
		repaired++
	}

	// An orphan has no domain row and no tenant, so its removal cannot be
	// tenant-scoped. That is safe precisely because the projection carries
	// no tenancy: the identifier is globally unique, and it was matched
	// against every tenant's extensions before being called an orphan.
	for _, id := range report.Orphaned {
		tx, err := r.pool.Unwrap().Begin(ctx)
		if err != nil {
			return repaired, fmt.Errorf("begin orphan removal: %w", err)
		}
		if err := r.projector.RemoveExtension(ctx, tx, id); err != nil {
			_ = tx.Rollback(ctx)
			return repaired, fmt.Errorf("remove orphan %s: %w", id, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return repaired, fmt.Errorf("commit orphan removal: %w", err)
		}
		repaired++
	}

	return repaired, nil
}

// listTenants reads every tenant id. `tenants` is the root of tenancy: it
// has no tenant_id and no RLS policy, so this needs no tenant context and
// no privileged role.
func (r *Reconciler) listTenants(ctx context.Context) ([]shareddomain.TenantID, error) {
	rows, err := r.pool.Unwrap().Query(ctx, `SELECT id FROM tenants ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var ids []shareddomain.TenantID
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan tenant id: %w", err)
		}
		id, err := shareddomain.ParseTenantID(raw)
		if err != nil {
			return nil, fmt.Errorf("decode tenant id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
