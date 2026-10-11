package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// CallLegStore implements ports.CallLegStore on placed_call_legs: the
// caller's transaction, parameterized SQL, tenancy by RLS.
type CallLegStore struct{}

var _ ports.CallLegStore = CallLegStore{}

// NewCallLegStore returns a CallLegStore.
func NewCallLegStore() CallLegStore { return CallLegStore{} }

const legColumns = `id, call_id, tenant_id, extension_id, trunk_id, route_id, destination, emergency, created_at, released_at`

// LockTrunk takes a transaction-scoped advisory lock per trunk. Every
// placement through the trunk takes it before counting, so two racing
// placements cannot both see a free channel (spec: "Racing placements
// cannot exceed the limit").
func (CallLegStore) LockTrunk(ctx context.Context, tx pgx.Tx, trunkID shareddomain.TrunkID) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pbx.trunk.place:' || $1, 0))`,
		trunkID.String()); err != nil {
		return fmt.Errorf("lock trunk placement: %w", err)
	}
	return nil
}

// Insert records a placed call.
func (CallLegStore) Insert(ctx context.Context, tx pgx.Tx, l domain.PlacedCallLeg) error {
	_, err := tx.Exec(ctx, `INSERT INTO placed_call_legs (`+legColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		l.ID.String(), optionalCall(l.CallID), l.TenantID.String(), l.ExtensionID.String(),
		optionalTrunk(l.TrunkID), optionalRoute(l.RouteID), l.Destination, l.Emergency, l.CreatedAt, l.ReleasedAt)
	if err != nil {
		return fmt.Errorf("insert placed call: %w", err)
	}
	return nil
}

// AttachCall records the call id origination returned.
func (CallLegStore) AttachCall(ctx context.Context, tx pgx.Tx, legID uuid.UUID, callID shareddomain.CallID) error {
	tag, err := tx.Exec(ctx, `UPDATE placed_call_legs SET call_id = $2 WHERE id = $1`, legID.String(), callID.String())
	if err != nil {
		return fmt.Errorf("attach call: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("leg %s: %w", legID, ports.ErrNotFound)
	}
	return nil
}

// LiveOnTrunk returns the unreleased legs on trunkID.
func (CallLegStore) LiveOnTrunk(ctx context.Context, tx pgx.Tx, trunkID shareddomain.TrunkID) ([]domain.PlacedCallLeg, error) {
	rows, err := tx.Query(ctx, `SELECT `+legColumns+` FROM placed_call_legs
		WHERE trunk_id = $1 AND released_at IS NULL ORDER BY created_at, call_id`, trunkID.String())
	if err != nil {
		return nil, fmt.Errorf("list live legs: %w", err)
	}
	defer rows.Close()
	var legs []domain.PlacedCallLeg
	for rows.Next() {
		l, err := scanLeg(rows)
		if err != nil {
			return nil, err
		}
		legs = append(legs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list live legs: %w", err)
	}
	return legs, nil
}

// Release marks a leg released, once: a second release keeps the first
// timestamp and is not an error.
func (CallLegStore) Release(ctx context.Context, tx pgx.Tx, legID uuid.UUID, at time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE placed_call_legs SET released_at = $2
		WHERE id = $1 AND released_at IS NULL`, legID.String(), at); err != nil {
		return fmt.Errorf("release placed call: %w", err)
	}
	return nil
}

// Get reads one placed call; another tenant's reads as nonexistent.
func (CallLegStore) Get(ctx context.Context, tx pgx.Tx, callID shareddomain.CallID) (domain.PlacedCallLeg, error) {
	l, err := scanLeg(tx.QueryRow(ctx, `SELECT `+legColumns+` FROM placed_call_legs WHERE call_id = $1`,
		callID.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PlacedCallLeg{}, fmt.Errorf("call %s: %w", callID, ports.ErrNotFound)
	}
	return l, err
}

func scanLeg(row pgx.Row) (domain.PlacedCallLeg, error) {
	var (
		l                     domain.PlacedCallLeg
		id, tenant, extension string
		callID, trunk, route  *string
	)
	if err := row.Scan(&id, &callID, &tenant, &extension, &trunk, &route, &l.Destination, &l.Emergency,
		&l.CreatedAt, &l.ReleasedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.PlacedCallLeg{}, err
		}
		return domain.PlacedCallLeg{}, fmt.Errorf("scan placed call: %w", err)
	}
	var err error
	if l.ID, err = uuid.Parse(id); err != nil {
		return domain.PlacedCallLeg{}, fmt.Errorf("decode leg id: %w", err)
	}
	if callID != nil {
		u, err := uuid.Parse(*callID)
		if err != nil {
			return domain.PlacedCallLeg{}, fmt.Errorf("decode call id: %w", err)
		}
		c := shareddomain.CallID(u)
		l.CallID = &c
	}
	if l.TenantID, err = shareddomain.ParseTenantID(tenant); err != nil {
		return domain.PlacedCallLeg{}, fmt.Errorf("decode tenant id: %w", err)
	}
	if l.ExtensionID, err = shareddomain.ParseExtensionID(extension); err != nil {
		return domain.PlacedCallLeg{}, fmt.Errorf("decode extension id: %w", err)
	}
	if trunk != nil {
		id, err := shareddomain.ParseTrunkID(*trunk)
		if err != nil {
			return domain.PlacedCallLeg{}, fmt.Errorf("decode trunk id: %w", err)
		}
		l.TrunkID = &id
	}
	if route != nil {
		id, err := shareddomain.ParseRouteID(*route)
		if err != nil {
			return domain.PlacedCallLeg{}, fmt.Errorf("decode route id: %w", err)
		}
		l.RouteID = &id
	}
	l.CreatedAt = l.CreatedAt.UTC()
	if l.ReleasedAt != nil {
		r := l.ReleasedAt.UTC()
		l.ReleasedAt = &r
	}
	return l, nil
}

func optionalCall(id *shareddomain.CallID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func optionalTrunk(id *shareddomain.TrunkID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func optionalRoute(id *shareddomain.RouteID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}
