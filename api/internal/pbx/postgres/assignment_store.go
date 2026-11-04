package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// AssignmentStore persists extension ownership (D-65).
type AssignmentStore struct{}

// NewAssignmentStore returns the store.
func NewAssignmentStore() *AssignmentStore { return &AssignmentStore{} }

var _ ports.AssignmentStore = (*AssignmentStore)(nil)

// Get reads one extension's assignment.
func (s *AssignmentStore) Get(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, extensionID shareddomain.ExtensionID) (domain.ExtensionAssignment, error) {
	var rawPrincipal, rawExtension string
	var at time.Time
	err := tx.QueryRow(ctx, `SELECT extension_id, principal_id, assigned_at
		FROM extension_assignments WHERE tenant_id = $1 AND extension_id = $2`,
		tenantID.String(), extensionID.String()).Scan(&rawExtension, &rawPrincipal, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExtensionAssignment{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.ExtensionAssignment{}, fmt.Errorf("read assignment: %w", err)
	}
	a, err := assignmentOf(rawExtension, rawPrincipal, at)
	return a, err
}

// Put replaces the extension's owner.
func (s *AssignmentStore) Put(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, a domain.ExtensionAssignment) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO extension_assignments (tenant_id, extension_id, principal_id, assigned_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (tenant_id, extension_id) DO UPDATE SET
			principal_id = EXCLUDED.principal_id, assigned_at = EXCLUDED.assigned_at
	`, tenantID.String(), a.ExtensionID.String(), a.PrincipalID.String(), a.AssignedAt)
	if err != nil {
		return fmt.Errorf("assign extension: %w", err)
	}
	return nil
}

// Delete removes the extension's owner; ErrNotFound if none.
func (s *AssignmentStore) Delete(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, extensionID shareddomain.ExtensionID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM extension_assignments WHERE tenant_id = $1 AND extension_id = $2`,
		tenantID.String(), extensionID.String())
	if err != nil {
		return fmt.Errorf("unassign extension: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// List reads assignments for a page of extensions.
func (s *AssignmentStore) List(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, extensionIDs []shareddomain.ExtensionID) (map[shareddomain.ExtensionID]domain.ExtensionAssignment, error) {
	out := map[shareddomain.ExtensionID]domain.ExtensionAssignment{}
	if len(extensionIDs) == 0 {
		return out, nil
	}
	ids := make([]string, len(extensionIDs))
	for i, id := range extensionIDs {
		ids[i] = id.String()
	}
	rows, err := tx.Query(ctx, `SELECT extension_id, principal_id, assigned_at
		FROM extension_assignments WHERE tenant_id = $1 AND extension_id = ANY($2)`,
		tenantID.String(), ids)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var rawExtension, rawPrincipal string
		var at time.Time
		if err := rows.Scan(&rawExtension, &rawPrincipal, &at); err != nil {
			return nil, err
		}
		a, err := assignmentOf(rawExtension, rawPrincipal, at)
		if err != nil {
			return nil, err
		}
		out[a.ExtensionID] = a
	}
	return out, rows.Err()
}

func assignmentOf(rawExtension, rawPrincipal string, at time.Time) (domain.ExtensionAssignment, error) {
	ext, err := shareddomain.ParseExtensionID(rawExtension)
	if err != nil {
		return domain.ExtensionAssignment{}, err
	}
	principal, err := shareddomain.ParsePrincipalID(rawPrincipal)
	if err != nil {
		return domain.ExtensionAssignment{}, err
	}
	return domain.ExtensionAssignment{ExtensionID: ext, PrincipalID: principal, AssignedAt: at}, nil
}
