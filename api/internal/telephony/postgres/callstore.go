// Package postgres implements telephony-core's ports.CallStore against
// the atsapbx database (internal/postgres.Pool), tenant-scoped via
// WithTenant so PostgreSQL Row-Level Security enforces isolation on
// every read and write (docs/hld/01-architecture.md §4).
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/shared/event"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
)

// CallStore implements ports.CallStore.
type CallStore struct {
	pool *corepostgres.Pool
}

var _ ports.CallStore = (*CallStore)(nil)

// NewCallStore returns a CallStore backed by pool.
func NewCallStore(pool *corepostgres.Pool) *CallStore {
	return &CallStore{pool: pool}
}

// SaveCall upserts call, every one of its participants, and one outbox
// row per event — all in the same transaction, tenant-scoped. This IS
// the transactional outbox pattern (docs/hld/01-architecture.md §3.2,
// task 7.1): the domain write and the outbox write commit or roll back
// together, so an event can never be silently lost between them. The
// outbox worker (task 7.2) reads these rows and publishes to NATS
// asynchronously, entirely independent of this method.
func (s *CallStore) SaveCall(ctx context.Context, call *domain.Call, events []event.DomainEvent) error {
	return s.pool.WithTenant(ctx, call.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO calls (id, tenant_id, direction, state, source_number, dest_number, started_at, answered_at, ended_at, termination_reason)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO UPDATE SET
				state              = EXCLUDED.state,
				answered_at        = EXCLUDED.answered_at,
				ended_at           = EXCLUDED.ended_at,
				termination_reason = EXCLUDED.termination_reason
		`,
			call.ID.String(), call.TenantID.String(), string(call.Direction), string(call.State),
			call.SourceNumber, call.DestNumber, call.StartedAt, call.AnsweredAt, call.EndedAt,
			nullableString(call.TerminationReason),
		)
		if err != nil {
			return fmt.Errorf("upsert call: %w", err)
		}

		for _, p := range call.Participants {
			_, err := tx.Exec(ctx, `
				INSERT INTO call_participants (id, tenant_id, call_id, role, endpoint_uri, state, joined_at, answered_at, left_at, billable_seconds)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (id) DO UPDATE SET
					state            = EXCLUDED.state,
					answered_at      = EXCLUDED.answered_at,
					left_at          = EXCLUDED.left_at,
					billable_seconds = EXCLUDED.billable_seconds
			`,
				p.ID.String(), p.TenantID.String(), p.CallID.String(), string(p.Role), p.EndpointURI,
				string(p.State), p.JoinedAt, p.AnsweredAt, p.LeftAt, p.BillableSeconds,
			)
			if err != nil {
				return fmt.Errorf("upsert participant %s: %w", p.ID, err)
			}
		}

		for _, ev := range events {
			payload, err := json.Marshal(ev)
			if err != nil {
				return fmt.Errorf("marshal event %s: %w", ev.EventType(), err)
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO outbox (id, tenant_id, event_type, aggregate_id, payload)
				VALUES (gen_random_uuid(), $1, $2, $3, $4)
			`, call.TenantID.String(), ev.EventType(), call.ID.String(), payload)
			if err != nil {
				return fmt.Errorf("insert outbox row for %s: %w", ev.EventType(), err)
			}
		}
		return nil
	})
}

// GetCall reads call and its participants back into a *domain.Call.
func (s *CallStore) GetCall(ctx context.Context, tenantID shareddomain.TenantID, id shareddomain.CallID) (*domain.Call, error) {
	var call *domain.Call
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var direction, state, sourceNumber, destNumber string
		var startedAt time.Time
		var answeredAt, endedAt *time.Time
		var terminationReason *string

		err := tx.QueryRow(ctx, `
			SELECT direction, state, source_number, dest_number, started_at, answered_at, ended_at, termination_reason
			FROM calls WHERE id = $1
		`, id.String()).Scan(&direction, &state, &sourceNumber, &destNumber, &startedAt, &answeredAt, &endedAt, &terminationReason)
		if err != nil {
			return fmt.Errorf("query call: %w", err)
		}

		call = &domain.Call{
			ID: id, TenantID: tenantID, Direction: domain.Direction(direction), State: domain.CallState(state),
			SourceNumber: sourceNumber, DestNumber: destNumber, StartedAt: startedAt,
			AnsweredAt: answeredAt, EndedAt: endedAt,
		}
		if terminationReason != nil {
			call.TerminationReason = *terminationReason
		}

		rows, err := tx.Query(ctx, `
			SELECT id, role, endpoint_uri, state, joined_at, answered_at, left_at, billable_seconds
			FROM call_participants WHERE call_id = $1
		`, id.String())
		if err != nil {
			return fmt.Errorf("query participants: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var idStr, role, endpointURI, pState string
			var joinedAt time.Time
			var pAnsweredAt, leftAt *time.Time
			var billableSeconds int
			if err := rows.Scan(&idStr, &role, &endpointURI, &pState, &joinedAt, &pAnsweredAt, &leftAt, &billableSeconds); err != nil {
				return fmt.Errorf("scan participant: %w", err)
			}
			pID, err := shareddomain.ParseParticipantID(idStr)
			if err != nil {
				return fmt.Errorf("parse participant id: %w", err)
			}
			call.Participants = append(call.Participants, &domain.CallParticipant{
				ID: pID, CallID: id, TenantID: tenantID, Role: domain.ParticipantRole(role),
				EndpointURI: endpointURI, State: domain.ParticipantState(pState), JoinedAt: joinedAt,
				AnsweredAt: pAnsweredAt, LeftAt: leftAt, BillableSeconds: billableSeconds,
			})
		}
		return rows.Err()
	})
	return call, err
}

// AddChannelHistory appends one ACL correlation-audit row. nodeID is
// left empty in this single-node walking skeleton (reserved for a
// future multi-node deployment identifying which process handled the
// channel).
func (s *CallStore) AddChannelHistory(ctx context.Context, tenantID shareddomain.TenantID, participantID shareddomain.ParticipantID, channelRef ports.ChannelRef, bridgeID ports.BridgeID, eventType string) error {
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO channel_history (tenant_id, participant_id, channel_ref, bridge_id, node_id, event_type)
			VALUES ($1, $2, $3, $4, '', $5)
		`, tenantID.String(), participantID.String(), []byte(channelRef), string(bridgeID), eventType)
		return err
	})
}

// RecordUsageTicks inserts usage_seconds rows, deduplicated at the
// database level by the (participant_id, second_ts) primary key —
// defense in depth alongside domain.GenerateUsageTicks' own no-duplicate
// guarantee. Ticks are grouped by tenant so each batch runs under the
// correct RLS context; in practice this walking skeleton always passes
// ticks for a single tenant at a time (recordUsageForParticipant).
func (s *CallStore) RecordUsageTicks(ctx context.Context, ticks []domain.UsageTick) error {
	byTenant := make(map[string][]domain.UsageTick)
	for _, t := range ticks {
		key := t.TenantID.String()
		byTenant[key] = append(byTenant[key], t)
	}

	for tenantIDStr, tenantTicks := range byTenant {
		tenantID, err := shareddomain.ParseTenantID(tenantIDStr)
		if err != nil {
			return fmt.Errorf("parse tenant id: %w", err)
		}
		err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			for _, t := range tenantTicks {
				_, err := tx.Exec(ctx, `
					INSERT INTO usage_seconds (tenant_id, participant_id, call_id, second_ts, direction)
					VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (participant_id, second_ts) DO NOTHING
				`, t.TenantID.String(), t.ParticipantID.String(), t.CallID.String(), t.SecondTS, string(t.Direction))
				if err != nil {
					return fmt.Errorf("insert usage tick: %w", err)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
