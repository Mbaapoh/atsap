package postgres

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// IngressStore implements ports.IngressMatcher on carrier_ingress_match
// (migration 0016, D-77): the platform's one cross-tenant read, run in its
// own transaction with no tenant set.
type IngressStore struct{ pool *corepostgres.Pool }

var _ ports.IngressMatcher = IngressStore{}

// NewIngressStore returns an IngressStore over pool.
func NewIngressStore(pool *corepostgres.Pool) IngressStore { return IngressStore{pool: pool} }

// Match returns the (tenant, trunk) pairs whose trunk recognises source
// and whose tenant has the (normalised) number.
func (s IngressStore) Match(ctx context.Context, source netip.Addr, number string) ([]domain.IngressMatch, error) {
	var out []domain.IngressMatch
	err := pgx.BeginFunc(ctx, s.pool.Unwrap(), func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id::text, trunk_id::text FROM carrier_ingress_match($1, $2)`, source, number)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var tenant, trunk string
			if err := rows.Scan(&tenant, &trunk); err != nil {
				return err
			}
			m := domain.IngressMatch{}
			if m.TenantID, err = shareddomain.ParseTenantID(tenant); err != nil {
				return err
			}
			if m.TrunkID, err = shareddomain.ParseTrunkID(trunk); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("match carrier ingress: %w", err)
	}
	return out, nil
}
