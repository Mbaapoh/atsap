// Package postgres reads terminated calls for reporting under RLS.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	corepostgres "atsap-api/internal/postgres"
	"atsap-api/internal/reporting/domain"
	"atsap-api/internal/reporting/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// Store reads calls, participants and usage rows under tenant RLS.
type Store struct{ pool *corepostgres.Pool }

// NewStore returns a reporting store.
func NewStore(pool *corepostgres.Pool) *Store { return &Store{pool: pool} }

var _ ports.Store = (*Store)(nil)

// List returns one page of terminated calls, newest first.
func (s *Store) List(ctx context.Context, tenantID shareddomain.TenantID, filter domain.Filter, limit, offset int) ([]domain.CallRecord, error) {
	return s.read(ctx, tenantID, filter, limit, offset, "DESC")
}

// Export returns matching records oldest first.
func (s *Store) Export(ctx context.Context, tenantID shareddomain.TenantID, filter domain.Filter, maxRows int) ([]domain.CallRecord, error) {
	return s.read(ctx, tenantID, filter, maxRows, 0, "ASC")
}

func (s *Store) read(ctx context.Context, tenantID shareddomain.TenantID, filter domain.Filter, limit, offset int, order string) ([]domain.CallRecord, error) {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	var records []domain.CallRecord
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		query, args := callQuery(filter, limit, offset, order)
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("list call records: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var r domain.CallRecord
			var rawID, direction, source, dest, reason string
			var started time.Time
			var answered, ended *time.Time
			if err := rows.Scan(&rawID, &direction, &source, &dest, &started, &answered, &ended, &reason); err != nil {
				return err
			}
			if r.CallID, err = shareddomain.ParseCallID(rawID); err != nil {
				return err
			}
			r.Direction, r.SourceNumber, r.DestNumber = direction, source, dest
			r.StartedAt, r.AnsweredAt, r.EndedAt, r.TerminationReason = started, answered, ended, reason
			records = append(records, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return loadParticipants(ctx, tx, records)
	})
	return records, err
}

func callQuery(filter domain.Filter, limit, offset int, order string) (string, []any) {
	where := []string{"state = 'Terminated'"}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if !filter.From.IsZero() {
		where = append(where, "started_at >= "+arg(filter.From))
	}
	if !filter.To.IsZero() {
		where = append(where, "started_at < "+arg(filter.To))
	}
	if filter.Direction != "" {
		where = append(where, "direction = "+arg(filter.Direction))
	}
	if filter.Number != "" {
		p := arg("%" + filter.Number + "%")
		where = append(where, "(source_number ILIKE "+p+" OR dest_number ILIKE "+p+")")
	}
	if filter.ExtensionID != nil {
		where = append(where, "EXISTS (SELECT 1 FROM call_participants cp WHERE cp.call_id = calls.id AND cp.extension_id = "+arg(filter.ExtensionID.String())+")")
	}
	if filter.PrincipalID != nil {
		where = append(where, "EXISTS (SELECT 1 FROM call_participants cp WHERE cp.call_id = calls.id AND cp.principal_id = "+arg(filter.PrincipalID.String())+")")
	}
	query := `SELECT id, direction, source_number, dest_number, started_at, answered_at, ended_at, termination_reason
		FROM calls WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY started_at ` + order + ` LIMIT ` + arg(limit) + ` OFFSET ` + arg(offset)
	return query, args
}

func loadParticipants(ctx context.Context, tx pgx.Tx, records []domain.CallRecord) error {
	if len(records) == 0 {
		return nil
	}
	ids := make([]string, len(records))
	for i := range records {
		ids[i] = records[i].CallID.String()
	}
	rows, err := tx.Query(ctx, `
		SELECT p.call_id, p.id, p.role, p.extension_id, p.principal_id, p.billable_seconds,
		       (SELECT COUNT(*) FROM usage_seconds u WHERE u.participant_id = p.id)
		FROM call_participants p WHERE p.call_id = ANY($1)
		ORDER BY p.joined_at`, ids)
	if err != nil {
		return fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()
	byCall := map[string]int{}
	for i := range records {
		byCall[records[i].CallID.String()] = i
	}
	for rows.Next() {
		var rawCall, rawID, role string
		var extensionID, principalID *string
		var billable, usage int
		if err := rows.Scan(&rawCall, &rawID, &role, &extensionID, &principalID, &billable, &usage); err != nil {
			return err
		}
		idx, ok := byCall[rawCall]
		if !ok {
			continue
		}
		p, err := participantOf(rawID, role, extensionID, principalID, billable, usage)
		if err != nil {
			return err
		}
		records[idx].Participants = append(records[idx].Participants, p)
	}
	return rows.Err()
}

func participantOf(rawID, role string, extensionID, principalID *string, billable, usage int) (domain.Participant, error) {
	p := domain.Participant{Role: role, BillableSeconds: billable, UsageSeconds: usage}
	id, err := shareddomain.ParseParticipantID(rawID)
	if err != nil {
		return p, err
	}
	p.ID = id
	if extensionID != nil {
		v, err := shareddomain.ParseExtensionID(*extensionID)
		if err != nil {
			return p, err
		}
		p.ExtensionID = &v
	}
	if principalID != nil {
		v, err := shareddomain.ParsePrincipalID(*principalID)
		if err != nil {
			return p, err
		}
		p.PrincipalID = &v
	}
	return p, nil
}
