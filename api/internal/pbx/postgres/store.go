// Package postgres implements pbx-core's outbound storage port
// (ports.ExtensionStore) against the atsapbx database.
//
// Unlike identity's store, which opens its own tenant-scoped transaction
// via internal/postgres.Pool.WithTenant, every method here runs inside a
// transaction its CALLER supplies: the extension's domain row and its
// engine projection in ps_endpoints/ps_auths/ps_aors must commit or fail
// together, which is only expressible if both writes share one tx
// (ports.ExtensionStore's doc comment, design D1). The caller is
// therefore responsible for scoping that transaction to a tenant with
// WithTenant before calling in — the same app.tenant_id setting that
// makes PostgreSQL Row-Level Security enforce isolation on every read
// and write (docs/hld/01-architecture.md §4). This store never opens a
// transaction of its own and never holds a pool.
//
// Every statement here uses parameter placeholders. No SQL in this
// package is built by string concatenation with caller input
// (LLD-03 §10.4, OWASP A03).
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// Store implements ports.ExtensionStore. It is stateless: it holds no
// pool and no connection, because every operation is executed against
// the transaction handed to it by the caller.
type Store struct{}

var _ ports.ExtensionStore = (*Store)(nil)

// NewStore returns a Store. No configuration is needed — the store has
// no connection state of its own.
func NewStore() *Store {
	return &Store{}
}

// --- extensions ----------------------------------------------------
//
// The schema's department_id column (Phase B) is deliberately absent
// from domain.Extension, so no statement here reads or writes it.
// Created_at and updated_at are persisted from the domain value rather
// than defaulted: the row must read back exactly as the aggregate that
// produced it says it is, the same way identity persists a tenant's or
// a principal's created_at.

// Insert persists a new extension inside tx.
//
// The (tenant_id, extension_number) uniqueness is what makes numbers
// tenant-local: a duplicate number within one tenant is refused as
// ports.ErrNumberTaken, while the same number in a different tenant is
// an unrelated extension and succeeds (spec: "Extension numbers are
// unique per tenant, not globally").
func (s *Store) Insert(ctx context.Context, tx pgx.Tx, ext domain.Extension) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO extensions
			(id, tenant_id, extension_number, display_name, auth_username, secret_digest, device_type, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, ext.ID.String(), ext.TenantID.String(), ext.Number, ext.DisplayName,
		ext.AuthUsername, ext.SecretDigest, string(ext.DeviceType), ext.CreatedAt, ext.UpdatedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("extension number %q: %w", ext.Number, ports.ErrNumberTaken)
	}
	if err != nil {
		return fmt.Errorf("insert extension: %w", err)
	}
	return nil
}

// Update rewrites an extension's mutable columns inside tx, keyed by id
// alone. Tenancy is not part of the WHERE clause: the caller's
// transaction already carries the app.tenant_id context, and RLS is what
// stops an update from touching another tenant's row — the same shape as
// identity's tenant-scoped updates. A row invisible to the current
// context (another tenant's, or one that does not exist) updates zero
// rows and is reported as ports.ErrNotFound, so the two are
// indistinguishable (INV-10).
//
// A renumber that would collide with another extension in the same
// tenant trips the same unique constraint as Insert and is reported as
// ports.ErrNumberTaken.
func (s *Store) Update(ctx context.Context, tx pgx.Tx, ext domain.Extension) error {
	tag, err := tx.Exec(ctx, `
		UPDATE extensions SET
			extension_number = $2,
			display_name     = $3,
			auth_username    = $4,
			secret_digest    = $5,
			device_type      = $6,
			updated_at       = $7
		WHERE id = $1
	`, ext.ID.String(), ext.Number, ext.DisplayName, ext.AuthUsername,
		ext.SecretDigest, string(ext.DeviceType), ext.UpdatedAt)
	if isUniqueViolation(err) {
		return fmt.Errorf("extension number %q: %w", ext.Number, ports.ErrNumberTaken)
	}
	if err != nil {
		return fmt.Errorf("update extension: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("extension %s: %w", ext.ID, ports.ErrNotFound)
	}
	return nil
}

// Delete removes the extension with id inside tx, and reports
// ports.ErrNotFound when it removed nothing.
//
// Failing closed here is a security requirement, not tidiness (design
// D9). The projection tables have no RLS, so the projector's
// RemoveExtension is unguarded by the database. If this method treated a
// blocked delete as success, the caller would go on to deprovision the
// projection anyway:
//
//	tenant B → DeleteExtension(tenant A's id)
//	  RLS blocks the row, 0 rows affected, "success"
//	  projector.RemoveExtension → no RLS → tenant A's ps_* rows deleted
//	  → tenant A's phone stops working; the console still shows it
//
// Returning an error stops the caller before the projection. INV-10 is
// unaffected: this is the identical answer a genuinely nonexistent id
// receives, so nothing is revealed about another tenant's data.
func (s *Store) Delete(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM extensions WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("delete extension: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("extension %s: %w", id, ports.ErrNotFound)
	}
	return nil
}

// Get reads one extension inside tx.
//
// Only id is matched; tenancy is enforced by the caller's RLS context.
// An extension that belongs to another tenant therefore reads exactly
// like one that exists nowhere — both are ports.ErrNotFound — because
// distinguishing them would confirm the existence of another tenant's
// resource (spec: "An extension belonging to another tenant is
// indistinguishable from one that does not exist").
func (s *Store) Get(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, id shareddomain.ExtensionID) (domain.Extension, error) {
	var (
		ext        domain.Extension
		rawID      string
		rawTenant  string
		deviceType string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, tenant_id, extension_number, display_name, auth_username,
		       secret_digest, device_type, created_at, updated_at
		FROM extensions WHERE id = $1
	`, id.String()).Scan(&rawID, &rawTenant, &ext.Number, &ext.DisplayName,
		&ext.AuthUsername, &ext.SecretDigest, &deviceType, &ext.CreatedAt, &ext.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Extension{}, fmt.Errorf("extension %s: %w", id, ports.ErrNotFound)
	}
	if err != nil {
		return domain.Extension{}, fmt.Errorf("query extension: %w", err)
	}

	if ext.ID, err = shareddomain.ParseExtensionID(rawID); err != nil {
		return domain.Extension{}, fmt.Errorf("decode extension id: %w", err)
	}
	if ext.TenantID, err = shareddomain.ParseTenantID(rawTenant); err != nil {
		return domain.Extension{}, fmt.Errorf("decode tenant id: %w", err)
	}
	ext.DeviceType = domain.DeviceType(deviceType)
	return ext, nil
}

// List returns one page of the current tenant's extensions inside tx,
// ordered by number so the console shows a directory, not an insertion
// history. Like Get, it relies on the caller's RLS context for tenancy,
// so a page never leaks a row from another tenant.
func (s *Store) List(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, page ports.Page) ([]domain.Extension, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, tenant_id, extension_number, display_name, auth_username,
		       secret_digest, device_type, created_at, updated_at
		FROM extensions
		ORDER BY extension_number, id
		LIMIT $1 OFFSET $2
	`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list extensions: %w", err)
	}
	defer rows.Close()

	var extensions []domain.Extension
	for rows.Next() {
		var (
			ext        domain.Extension
			rawID      string
			rawTenant  string
			deviceType string
		)
		if err := rows.Scan(&rawID, &rawTenant, &ext.Number, &ext.DisplayName,
			&ext.AuthUsername, &ext.SecretDigest, &deviceType, &ext.CreatedAt, &ext.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan extension: %w", err)
		}
		if ext.ID, err = shareddomain.ParseExtensionID(rawID); err != nil {
			return nil, fmt.Errorf("decode extension id: %w", err)
		}
		if ext.TenantID, err = shareddomain.ParseTenantID(rawTenant); err != nil {
			return nil, fmt.Errorf("decode tenant id: %w", err)
		}
		ext.DeviceType = domain.DeviceType(deviceType)
		extensions = append(extensions, ext)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list extensions: %w", err)
	}
	return extensions, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23505"
}
