package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// InboundNumberStore implements ports.InboundNumberStore. Like Store it
// is stateless: every method runs inside the caller's transaction, and
// tenancy is that transaction's RLS context.
type InboundNumberStore struct{}

var _ ports.InboundNumberStore = (*InboundNumberStore)(nil)

// NewInboundNumberStore returns an InboundNumberStore.
func NewInboundNumberStore() *InboundNumberStore {
	return &InboundNumberStore{}
}

// Insert persists a DID. number.Number must already be normalized to
// digits (domain.NormalizeInboundNumber): the UNIQUE (tenant_id, number)
// then matches "+442…" and "442…" as the same number, both ways.
func (s *InboundNumberStore) Insert(ctx context.Context, tx pgx.Tx, number domain.InboundNumber) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO inbound_numbers (id, tenant_id, number, extension_id)
		VALUES ($1, $2, $3, $4)
	`, number.ID, number.TenantID.String(), number.Number, number.ExtensionID.String())
	if isUniqueViolation(err) {
		return fmt.Errorf("inbound number %q: %w", number.Number, ports.ErrInboundNumberTaken)
	}
	if isForeignKeyViolation(err) {
		// The composite key refused another tenant's extension — which
		// is exactly what that extension looks like to this tenant:
		// not found (INV-10).
		return fmt.Errorf("inbound number %q: %w", number.Number, ports.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("insert inbound number: %w", err)
	}
	return nil
}

// List returns the tenant's DIDs in assignment order.
func (s *InboundNumberStore) List(ctx context.Context, tx pgx.Tx) ([]domain.InboundNumber, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, tenant_id, number, extension_id
		FROM inbound_numbers
		ORDER BY created_at, number
	`)
	if err != nil {
		return nil, fmt.Errorf("list inbound numbers: %w", err)
	}
	defer rows.Close()

	var out []domain.InboundNumber
	for rows.Next() {
		var (
			n            domain.InboundNumber
			id, tenantID string
			extID        string
		)
		if err := rows.Scan(&id, &tenantID, &n.Number, &extID); err != nil {
			return nil, fmt.Errorf("scan inbound number: %w", err)
		}
		n.ID, err = uuid.Parse(id)
		if err != nil {
			return nil, fmt.Errorf("parse inbound number id: %w", err)
		}
		if n.TenantID, err = shareddomain.ParseTenantID(tenantID); err != nil {
			return nil, fmt.Errorf("parse inbound number tenant: %w", err)
		}
		if n.ExtensionID, err = shareddomain.ParseExtensionID(extID); err != nil {
			return nil, fmt.Errorf("parse inbound number extension: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list inbound numbers: %w", err)
	}
	return out, nil
}

// Delete removes one DID, failing closed when it removed nothing —
// which is also what another tenant's DID looks like under RLS.
func (s *InboundNumberStore) Delete(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	cmd, err := tx.Exec(ctx, `DELETE FROM inbound_numbers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete inbound number: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("inbound number %s: %w", id, ports.ErrNotFound)
	}
	return nil
}

// FindByNumber resolves a normalized DID for the arriving-call path.
func (s *InboundNumberStore) FindByNumber(ctx context.Context, tx pgx.Tx, number string) (domain.InboundNumber, error) {
	var (
		n            domain.InboundNumber
		id, tenantID string
		extID        string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, tenant_id, number, extension_id
		FROM inbound_numbers
		WHERE number = $1
	`, number).Scan(&id, &tenantID, &n.Number, &extID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.InboundNumber{}, fmt.Errorf("inbound number %q: %w", number, ports.ErrNotFound)
	}
	if err != nil {
		return domain.InboundNumber{}, fmt.Errorf("find inbound number: %w", err)
	}
	var err2 error
	if n.ID, err2 = uuid.Parse(id); err2 != nil {
		return domain.InboundNumber{}, fmt.Errorf("parse inbound number id: %w", err2)
	}
	if n.TenantID, err2 = shareddomain.ParseTenantID(tenantID); err2 != nil {
		return domain.InboundNumber{}, fmt.Errorf("parse inbound number tenant: %w", err2)
	}
	if n.ExtensionID, err2 = shareddomain.ParseExtensionID(extID); err2 != nil {
		return domain.InboundNumber{}, fmt.Errorf("parse inbound number extension: %w", err2)
	}
	return n, nil
}

// DeleteByExtension removes every DID pointing at extensionID and
// returns them for the audit trail. An extension deletion takes its
// numbers with it in the same transaction (design D7).
func (s *InboundNumberStore) DeleteByExtension(ctx context.Context, tx pgx.Tx, extensionID shareddomain.ExtensionID) ([]domain.InboundNumber, error) {
	rows, err := tx.Query(ctx, `
		DELETE FROM inbound_numbers
		WHERE extension_id = $1
		RETURNING id, tenant_id, number, extension_id
	`, extensionID.String())
	if err != nil {
		return nil, fmt.Errorf("delete inbound numbers by extension: %w", err)
	}
	defer rows.Close()

	var out []domain.InboundNumber
	for rows.Next() {
		var (
			n            domain.InboundNumber
			id, tenantID string
			extID        string
		)
		if err := rows.Scan(&id, &tenantID, &n.Number, &extID); err != nil {
			return nil, fmt.Errorf("scan deleted inbound number: %w", err)
		}
		var err2 error
		if n.ID, err2 = uuid.Parse(id); err2 != nil {
			return nil, fmt.Errorf("parse inbound number id: %w", err2)
		}
		if n.TenantID, err2 = shareddomain.ParseTenantID(tenantID); err2 != nil {
			return nil, fmt.Errorf("parse inbound number tenant: %w", err2)
		}
		if n.ExtensionID, err2 = shareddomain.ParseExtensionID(extID); err2 != nil {
			return nil, fmt.Errorf("parse inbound number extension: %w", err2)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delete inbound numbers by extension: %w", err)
	}
	return out, nil
}
