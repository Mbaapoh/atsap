package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// EmergencyStore implements ports.EmergencyStore. Stateless like the
// other pbx stores: every method runs inside the caller's transaction,
// and tenancy is that transaction's RLS context.
type EmergencyStore struct{}

var _ ports.EmergencyStore = (*EmergencyStore)(nil)

// NewEmergencyStore returns an EmergencyStore.
func NewEmergencyStore() *EmergencyStore {
	return &EmergencyStore{}
}

// GetCountry returns the tenant's emergency country and whether one was
// ever set.
func (s *EmergencyStore) GetCountry(ctx context.Context, tx pgx.Tx) (string, bool, error) {
	var code string
	err := tx.QueryRow(ctx, `SELECT country_code FROM emergency_settings`).Scan(&code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read emergency settings: %w", err)
	}
	return code, true, nil
}

// SetCountry replaces the tenant's emergency country.
func (s *EmergencyStore) SetCountry(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, countryCode string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO emergency_settings (tenant_id, country_code)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET
			country_code = EXCLUDED.country_code,
			updated_at   = NOW()
	`, tenantID.String(), countryCode); err != nil {
		return fmt.Errorf("set emergency country: %w", err)
	}
	return nil
}

// ListAdditions returns the tenant's own numbers, ascending.
func (s *EmergencyStore) ListAdditions(ctx context.Context, tx pgx.Tx) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT number FROM emergency_numbers ORDER BY number`)
	if err != nil {
		return nil, fmt.Errorf("list emergency numbers: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan emergency number: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list emergency numbers: %w", err)
	}
	return out, nil
}

// AddAddition records one of the tenant's own numbers. Adding what is
// already there succeeds idempotently: the set, not the row, is what
// matters.
func (s *EmergencyStore) AddAddition(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, number string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO emergency_numbers (tenant_id, number)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, tenantID.String(), number); err != nil {
		return fmt.Errorf("add emergency number: %w", err)
	}
	return nil
}

// DeleteAddition removes one of the tenant's own numbers, failing
// closed when it removed nothing.
func (s *EmergencyStore) DeleteAddition(ctx context.Context, tx pgx.Tx, number string) error {
	cmd, err := tx.Exec(ctx, `DELETE FROM emergency_numbers WHERE number = $1`, number)
	if err != nil {
		return fmt.Errorf("delete emergency number: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("emergency number %q: %w", number, ports.ErrNotFound)
	}
	return nil
}
