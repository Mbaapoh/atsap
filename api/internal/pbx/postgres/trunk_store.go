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

// TrunkStore implements ports.TrunkStore on carrier_trunks.
//
// The same shape as Store: the caller's transaction, parameterized SQL
// only, and tenancy enforced by the transaction's RLS context rather than
// by a WHERE clause — so another tenant's trunk reads, updates and
// deletes exactly like one that does not exist (INV-10).
type TrunkStore struct{}

var _ ports.TrunkStore = TrunkStore{}

// NewTrunkStore returns a TrunkStore.
func NewTrunkStore() TrunkStore { return TrunkStore{} }

const trunkColumns = `id, tenant_id, name, host, port, auth_username, credentials_ref,
	channel_limit, priority, health_status, health_reason, health_observed_at, created_at, updated_at`

// Insert writes a new trunk. Health is written as given, which on the
// configuration path is always Unknown with no observation.
func (TrunkStore) Insert(ctx context.Context, tx pgx.Tx, t domain.Trunk) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO carrier_trunks (`+trunkColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, t.ID.String(), t.TenantID.String(), t.Name, t.Host, t.Port, nullable(t.AuthUsername),
		nullable(t.CredentialRef), t.ChannelLimit, t.Priority, string(t.Health), nullable(string(t.HealthReason)),
		t.HealthObservedAt, t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert trunk: %w", err)
	}
	return nil
}

// Update rewrites a trunk's configuration. It does NOT write health:
// configuration and observation have separate writers, so an update can
// never smuggle a health value in (spec: "Health cannot be set by
// configuration"). A new host or port does make the old observation
// stale — it was of another address — so the trunk returns to Unknown
// until the new address is checked (pbx-trunk-health-probing).
func (TrunkStore) Update(ctx context.Context, tx pgx.Tx, t domain.Trunk) error {
	tag, err := tx.Exec(ctx, `
		UPDATE carrier_trunks SET
			health_status      = CASE WHEN host = $3 AND port = $4 THEN health_status      ELSE 'UNKNOWN' END,
			health_reason      = CASE WHEN host = $3 AND port = $4 THEN health_reason      ELSE NULL END,
			health_observed_at = CASE WHEN host = $3 AND port = $4 THEN health_observed_at ELSE NULL END,
			name = $2, host = $3, port = $4, auth_username = $5, credentials_ref = $6,
			channel_limit = $7, priority = $8, updated_at = $9
		WHERE id = $1
	`, t.ID.String(), t.Name, t.Host, t.Port, nullable(t.AuthUsername), nullable(t.CredentialRef),
		t.ChannelLimit, t.Priority, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update trunk: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("trunk %s: %w", t.ID, ports.ErrNotFound)
	}
	return nil
}

// Delete removes a trunk and fails closed on a row it did not remove —
// the gate in front of RemoveTrunk, whose tables carry no RLS.
func (TrunkStore) Delete(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM carrier_trunks WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("delete trunk: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("trunk %s: %w", id, ports.ErrNotFound)
	}
	return nil
}

// Get reads one trunk.
func (TrunkStore) Get(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID) (domain.Trunk, error) {
	t, err := scanTrunk(tx.QueryRow(ctx, `SELECT `+trunkColumns+` FROM carrier_trunks WHERE id = $1`, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Trunk{}, fmt.Errorf("trunk %s: %w", id, ports.ErrNotFound)
	}
	return t, err
}

// List returns one page of the tenant's trunks, by priority then name.
func (TrunkStore) List(ctx context.Context, tx pgx.Tx, page ports.Page) ([]domain.Trunk, error) {
	rows, err := tx.Query(ctx, `SELECT `+trunkColumns+` FROM carrier_trunks
		ORDER BY priority, name, id LIMIT $1 OFFSET $2`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("list trunks: %w", err)
	}
	defer rows.Close()

	var trunks []domain.Trunk
	for rows.Next() {
		t, err := scanTrunk(rows)
		if err != nil {
			return nil, err
		}
		trunks = append(trunks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list trunks: %w", err)
	}
	return trunks, nil
}

// SetHealth records an observation from a call through the trunk. With
// SetCheckedHealth it is the only writer of health.
func (TrunkStore) SetHealth(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID, obs domain.HealthObservation) error {
	if err := validObservation(obs); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE carrier_trunks SET health_status = $2, health_reason = $3, health_observed_at = $4 WHERE id = $1
	`, id.String(), string(obs.Health), string(obs.Reason), obs.At)
	if err != nil {
		return fmt.Errorf("set trunk health: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("trunk %s: %w", id, ports.ErrNotFound)
	}
	return nil
}

// SetCheckedHealth records the platform's check of the carrier at host
// and port, only while the trunk still has that address: a result for an
// address since changed, or for a trunk since deleted, is dropped and
// reported false (pbx-trunk-health-probing design D3).
func (TrunkStore) SetCheckedHealth(ctx context.Context, tx pgx.Tx, id shareddomain.TrunkID, host string, port int, obs domain.HealthObservation) (bool, error) {
	if err := validObservation(obs); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE carrier_trunks SET health_status = $4, health_reason = $5, health_observed_at = $6
		WHERE id = $1 AND host = $2 AND port = $3
	`, id.String(), host, port, string(obs.Health), string(obs.Reason), obs.At)
	if err != nil {
		return false, fmt.Errorf("set checked trunk health: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// validObservation refuses an observation that is not one: Unknown is
// where a trunk starts, never something observed.
func validObservation(obs domain.HealthObservation) error {
	if !obs.Health.Valid() || obs.Health == domain.HealthUnknown || !obs.Reason.Valid() || obs.At.IsZero() {
		return fmt.Errorf("%w: health observation %+v", ports.ErrInvalidInput, obs)
	}
	return nil
}

func scanTrunk(row pgx.Row) (domain.Trunk, error) {
	var (
		t                      domain.Trunk
		rawID, rawTenant, hlth string
		username, ref, reason  *string
		observedAt             *time.Time
	)
	if err := row.Scan(&rawID, &rawTenant, &t.Name, &t.Host, &t.Port, &username, &ref,
		&t.ChannelLimit, &t.Priority, &hlth, &reason, &observedAt, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Trunk{}, err
		}
		return domain.Trunk{}, fmt.Errorf("scan trunk: %w", err)
	}
	var err error
	if t.ID, err = shareddomain.ParseTrunkID(rawID); err != nil {
		return domain.Trunk{}, fmt.Errorf("decode trunk id: %w", err)
	}
	if t.TenantID, err = shareddomain.ParseTenantID(rawTenant); err != nil {
		return domain.Trunk{}, fmt.Errorf("decode tenant id: %w", err)
	}
	t.CreatedAt, t.UpdatedAt = t.CreatedAt.UTC(), t.UpdatedAt.UTC()
	t.Health = domain.TrunkHealth(hlth)
	t.HealthReason = domain.HealthReason(deref(reason))
	if observedAt != nil {
		at := observedAt.UTC()
		t.HealthObservedAt = &at
	}
	t.AuthUsername = deref(username)
	t.CredentialRef = deref(ref)
	return t, nil
}

// RouteStore implements ports.RouteStore on carrier_routes.
type RouteStore struct{}

var _ ports.RouteStore = RouteStore{}

// NewRouteStore returns a RouteStore.
func NewRouteStore() RouteStore { return RouteStore{} }

const routeColumns = `id, tenant_id, prefix_pattern, trunk_id, cost_rate_per_min, priority, created_at`

// Insert writes a route. A trunk id outside the transaction's tenant is
// refused by the composite foreign key (SQLSTATE 23503) and reported as
// ports.ErrNotFound — the same answer a nonexistent trunk gets (INV-10).
func (RouteStore) Insert(ctx context.Context, tx pgx.Tx, r domain.CarrierRoute) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO carrier_routes (`+routeColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, r.ID.String(), r.TenantID.String(), r.PrefixPattern, r.TrunkID.String(), r.CostPerMinute, r.Priority, r.CreatedAt)
	if isForeignKeyViolation(err) {
		return fmt.Errorf("trunk %s: %w", r.TrunkID, ports.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("insert route: %w", err)
	}
	return nil
}

// Delete removes a route, failing closed on a row it did not remove.
func (RouteStore) Delete(ctx context.Context, tx pgx.Tx, id shareddomain.RouteID) error {
	tag, err := tx.Exec(ctx, `DELETE FROM carrier_routes WHERE id = $1`, id.String())
	if err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("route %s: %w", id, ports.ErrNotFound)
	}
	return nil
}

// Get reads one route.
func (RouteStore) Get(ctx context.Context, tx pgx.Tx, id shareddomain.RouteID) (domain.CarrierRoute, error) {
	r, err := scanRoute(tx.QueryRow(ctx, `SELECT `+routeColumns+` FROM carrier_routes WHERE id = $1`, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CarrierRoute{}, fmt.Errorf("route %s: %w", id, ports.ErrNotFound)
	}
	return r, err
}

// List returns one page of the tenant's routes, in pattern order.
func (RouteStore) List(ctx context.Context, tx pgx.Tx, page ports.Page) ([]domain.CarrierRoute, error) {
	return queryRoutes(ctx, tx, `SELECT `+routeColumns+` FROM carrier_routes
		ORDER BY prefix_pattern, priority, id LIMIT $1 OFFSET $2`, page.Limit, page.Offset)
}

// DeleteByTrunk removes and returns every route sending to trunkID.
func (RouteStore) DeleteByTrunk(ctx context.Context, tx pgx.Tx, trunkID shareddomain.TrunkID) ([]domain.CarrierRoute, error) {
	return queryRoutes(ctx, tx, `DELETE FROM carrier_routes WHERE trunk_id = $1 RETURNING `+routeColumns, trunkID.String())
}

func queryRoutes(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]domain.CarrierRoute, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query routes: %w", err)
	}
	defer rows.Close()

	var routes []domain.CarrierRoute
	for rows.Next() {
		r, err := scanRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query routes: %w", err)
	}
	return routes, nil
}

func scanRoute(row pgx.Row) (domain.CarrierRoute, error) {
	var (
		r                          domain.CarrierRoute
		rawID, rawTenant, rawTrunk string
	)
	if err := row.Scan(&rawID, &rawTenant, &r.PrefixPattern, &rawTrunk, &r.CostPerMinute, &r.Priority, &r.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.CarrierRoute{}, err
		}
		return domain.CarrierRoute{}, fmt.Errorf("scan route: %w", err)
	}
	var err error
	if r.ID, err = shareddomain.ParseRouteID(rawID); err != nil {
		return domain.CarrierRoute{}, fmt.Errorf("decode route id: %w", err)
	}
	if r.TenantID, err = shareddomain.ParseTenantID(rawTenant); err != nil {
		return domain.CarrierRoute{}, fmt.Errorf("decode tenant id: %w", err)
	}
	if r.TrunkID, err = shareddomain.ParseTrunkID(rawTrunk); err != nil {
		return domain.CarrierRoute{}, fmt.Errorf("decode trunk id: %w", err)
	}
	r.CreatedAt = r.CreatedAt.UTC()
	return r, nil
}

// isForeignKeyViolation reports SQLSTATE 23503.
func isForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == "23503"
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
