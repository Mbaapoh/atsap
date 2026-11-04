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

// OutboundPolicyStore persists the fraud guard's configuration.
type OutboundPolicyStore struct{}

// NewOutboundPolicyStore returns the policy store.
func NewOutboundPolicyStore() *OutboundPolicyStore { return &OutboundPolicyStore{} }

var _ ports.OutboundPolicyStore = (*OutboundPolicyStore)(nil)

// Get returns the tenant's stored policy, or the safe default when none
// has been saved.
func (s *OutboundPolicyStore) Get(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID) (domain.OutboundPolicy, error) {
	var (
		p       = domain.DefaultOutboundPolicy()
		prefix  []string
		seconds int
	)
	err := tx.QueryRow(ctx, `
		SELECT daily_spend_cap_micros, monthly_spend_cap_micros, blocked_prefixes,
		       anomaly_call_threshold, anomaly_window_seconds,
		       business_start_minute, business_end_minute,
		       suspended, suspended_at, suspended_reason, last_cap_alert_at
		FROM tenant_outbound_policies WHERE tenant_id = $1
	`, tenantID.String()).Scan(&p.DailySpendCapMicros, &p.MonthlySpendCapMicros, &prefix,
		&p.AnomalyCallThreshold, &seconds, &p.BusinessStartMinute, &p.BusinessEndMinute,
		&p.Suspended, &p.SuspendedAt, &p.SuspendedReason, &p.LastCapAlertAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return domain.OutboundPolicy{}, fmt.Errorf("read outbound policy: %w", err)
	}
	p.BlockedPrefixes = prefix
	p.AnomalyWindow = time.Duration(seconds) * time.Second
	return p, nil
}

// Set stores a policy, replacing any existing row.
func (s *OutboundPolicyStore) Set(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, p domain.OutboundPolicy) error {
	prefixes := p.BlockedPrefixes
	if prefixes == nil {
		prefixes = []string{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO tenant_outbound_policies
			(tenant_id, daily_spend_cap_micros, monthly_spend_cap_micros, blocked_prefixes,
			 anomaly_call_threshold, anomaly_window_seconds, business_start_minute,
			 business_end_minute, suspended, suspended_at, suspended_reason, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET
			daily_spend_cap_micros = EXCLUDED.daily_spend_cap_micros,
			monthly_spend_cap_micros = EXCLUDED.monthly_spend_cap_micros,
			blocked_prefixes = EXCLUDED.blocked_prefixes,
			anomaly_call_threshold = EXCLUDED.anomaly_call_threshold,
			anomaly_window_seconds = EXCLUDED.anomaly_window_seconds,
			business_start_minute = EXCLUDED.business_start_minute,
			business_end_minute = EXCLUDED.business_end_minute,
			suspended = EXCLUDED.suspended,
			suspended_at = EXCLUDED.suspended_at,
			suspended_reason = EXCLUDED.suspended_reason,
			updated_at = NOW()
	`, tenantID.String(), p.DailySpendCapMicros, p.MonthlySpendCapMicros, prefixes,
		p.AnomalyCallThreshold, int(p.AnomalyWindow.Seconds()), p.BusinessStartMinute,
		p.BusinessEndMinute, p.Suspended, p.SuspendedAt, p.SuspendedReason)
	if err != nil {
		return fmt.Errorf("write outbound policy: %w", err)
	}
	return nil
}

// Suspend atomically marks outbound suspended, unless already suspended.
func (s *OutboundPolicyStore) Suspend(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, reason string, at time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE tenant_outbound_policies SET suspended = TRUE, suspended_at = $2,
			suspended_reason = $3, updated_at = NOW()
		WHERE tenant_id = $1 AND suspended = FALSE
	`, tenantID.String(), at, reason)
	if err != nil {
		return false, fmt.Errorf("suspend outbound: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Resume clears a suspension.
func (s *OutboundPolicyStore) Resume(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID) (bool, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE tenant_outbound_policies SET suspended = FALSE, suspended_at = NULL,
			suspended_reason = '', updated_at = NOW()
		WHERE tenant_id = $1 AND suspended = TRUE
	`, tenantID.String())
	if err != nil {
		return false, fmt.Errorf("resume outbound: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// SpendStore persists the rated usage ledger.
type SpendStore struct{}

// NewSpendStore returns the ledger store.
func NewSpendStore() *SpendStore { return &SpendStore{} }

var _ ports.SpendStore = (*SpendStore)(nil)

// Read returns the tenant's spend for the UTC day and month containing at.
func (s *SpendStore) Read(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, at time.Time) (ports.Spend, error) {
	var out ports.Spend
	err := tx.QueryRow(ctx, `
		SELECT COALESCE((SELECT cost_micros FROM tenant_spend_ledger WHERE tenant_id = $1 AND day = $2::date), 0),
		       COALESCE((SELECT SUM(cost_micros) FROM tenant_spend_ledger
		                 WHERE tenant_id = $1 AND day >= date_trunc('month', $2::date)::date
		                   AND day < (date_trunc('month', $2::date) + interval '1 month')::date), 0)
	`, tenantID.String(), at.UTC()).Scan(&out.DayMicros, &out.MonthMicros)
	if err != nil {
		return ports.Spend{}, fmt.Errorf("read spend: %w", err)
	}
	return out, nil
}

// Add records usage from one answered call.
func (s *SpendStore) Add(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, at time.Time, costMicros int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tenant_spend_ledger (tenant_id, day, cost_micros, call_count)
		VALUES ($1, $2::date, $3, 1)
		ON CONFLICT (tenant_id, day) DO UPDATE SET
			cost_micros = tenant_spend_ledger.cost_micros + EXCLUDED.cost_micros,
			call_count = tenant_spend_ledger.call_count + 1
	`, tenantID.String(), at.UTC(), costMicros)
	if err != nil {
		return fmt.Errorf("record spend: %w", err)
	}
	return nil
}

// AttemptStore records and counts trunk-bound attempts for anomaly
// detection.
type AttemptStore struct{}

// NewAttemptStore returns the attempt store.
func NewAttemptStore() *AttemptStore { return &AttemptStore{} }

var _ ports.AttemptStore = (*AttemptStore)(nil)

// CountInternationalSince counts international attempts in the window.
func (s *AttemptStore) CountInternationalSince(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, since time.Time) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbound_attempts
		WHERE tenant_id = $1 AND international = TRUE AND at > $2
	`, tenantID.String(), since).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count outbound attempts: %w", err)
	}
	return n, nil
}

// Record inserts one attempt under the caller's transaction.
func (s *AttemptStore) Record(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, destination string, international bool, at time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outbound_attempts (tenant_id, destination, international, at)
		VALUES ($1, $2, $3, $4)
	`, tenantID.String(), destination, international, at)
	if err != nil {
		return fmt.Errorf("record outbound attempt: %w", err)
	}
	return nil
}

// LockTenant serializes fraud checks for one tenant inside tx.
func (s *AttemptStore) LockTenant(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pbx.fraud:' || $1, 0))`, tenantID.String())
	if err != nil {
		return fmt.Errorf("lock fraud check: %w", err)
	}
	return nil
}

// MarkCapAlerted records that a cap alert was sent, so the operator is
// not mailed on every blocked call.
func (s *OutboundPolicyStore) MarkCapAlerted(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID, at time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE tenant_outbound_policies SET last_cap_alert_at = $2 WHERE tenant_id = $1`, tenantID.String(), at)
	if err != nil {
		return fmt.Errorf("mark cap alert: %w", err)
	}
	return nil
}
