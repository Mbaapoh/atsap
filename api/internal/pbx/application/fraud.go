package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

const (
	actionReadOutboundPolicy = "outbound.policy.read"
	actionSetOutboundPolicy  = "outbound.policy.set"
	actionResumeOutbound     = "outbound.resume"
)

// usageWatchTimeout caps the usage observer for a call that never reports
// its end. It is deliberately much longer than the health observer.
const usageWatchTimeout = 6 * time.Hour

// checkFraudGuard applies the tenant's blocked prefixes, suspension, spend
// caps and anomaly rule to a trunk-bound, non-emergency destination. It
// records the attempt in the same transaction as the anomaly count, so
// concurrent attempts cannot all pass under the threshold.
func (s *Service) checkFraudGuard(ctx context.Context, tenantID shareddomain.TenantID, res domain.Resolution, destination string) error {
	if s.policies == nil || res.Destination.Kind != domain.DestinationTrunk || res.Emergency {
		return nil
	}
	now := s.now().UTC()
	international := strings.HasPrefix(destination, "+")
	var (
		fraudAlert *ports.FraudAlert
		capAlert   bool
		capReason  string
	)
	err := s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		fraudAlert, capAlert, capReason, err = s.evaluateFraud(ctx, tx, tenantID, destination, international, now)
		return err
	})
	if fraudAlert != nil {
		s.notifyFraud(ctx, *fraudAlert)
	}
	if capAlert {
		s.notifySpend(ctx, tenantID, capReason, now)
	}
	return err
}

func (s *Service) evaluateFraud(ctx context.Context, tx pgx.Tx, tenantID shareddomain.TenantID,
	destination string, international bool, now time.Time) (*ports.FraudAlert, bool, string, error) {
	if err := s.attempts.LockTenant(ctx, tx, tenantID); err != nil {
		return nil, false, "", err
	}
	policy, err := s.policies.Get(ctx, tx, tenantID)
	if err != nil {
		return nil, false, "", err
	}
	if policy.Suspended {
		return nil, false, "", fmt.Errorf("%w: %s", ports.ErrOutboundSuspended, policy.SuspendedReason)
	}
	if policy.Blocks(destination) {
		return nil, false, "", ports.ErrOutboundBlocked
	}
	spend, err := s.spend.Read(ctx, tx, tenantID, now)
	if err != nil {
		return nil, false, "", err
	}
	if exceeded, reason := policy.SpendExceeded(spend.DayMicros, spend.MonthMicros); exceeded {
		alert := false
		if policy.LastCapAlertAt == nil || now.Sub(*policy.LastCapAlertAt) >= time.Hour {
			alert = true
			policy.LastCapAlertAt = &now
			if err := s.policies.Set(ctx, tx, tenantID, policy); err != nil {
				return nil, false, "", err
			}
			if err := s.policies.MarkCapAlerted(ctx, tx, tenantID, now); err != nil {
				return nil, false, "", err
			}
		}
		return nil, alert, reason, ports.ErrSpendCapReached
	}
	recent, err := s.attempts.CountInternationalSince(ctx, tx, tenantID, now.Add(-policy.AnomalyWindow))
	if err != nil {
		return nil, false, "", err
	}
	if policy.Anomaly(recent, now, international) {
		policy.Suspended, policy.SuspendedAt, policy.SuspendedReason = true, &now, "anomaly: high-volume international calling outside business hours"
		if err := s.policies.Set(ctx, tx, tenantID, policy); err != nil {
			return nil, false, "", err
		}
		return &ports.FraudAlert{
			TenantID: tenantID, Destination: destination, RecentCalls: recent + 1,
			Threshold: policy.AnomalyCallThreshold, Window: policy.AnomalyWindow, At: now,
		}, false, "", ports.ErrOutboundSuspended
	}
	return nil, false, "", s.attempts.Record(ctx, tx, tenantID, destination, international, now)
}

func (s *Service) notifyFraud(parent context.Context, alert ports.FraudAlert) {
	if s.fraud == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 20*time.Second)
	go func() {
		defer cancel()
		if err := s.fraud.OutboundSuspended(ctx, alert); err != nil {
			slog.Default().Warn("pbx: fraud alert failed", "tenant_id", alert.TenantID, "error", err)
		}
	}()
}

func (s *Service) notifySpend(parent context.Context, tenantID shareddomain.TenantID, reason string, at time.Time) {
	if s.fraud == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 20*time.Second)
	go func() {
		defer cancel()
		if err := s.fraud.SpendCapReached(ctx, tenantID, reason, at); err != nil {
			slog.Default().Warn("pbx: spend alert failed", "tenant_id", tenantID, "error", err)
		}
	}()
}

// watchUsage records the rated cost of an answered trunk call when it
// ends. It never touches a call in progress; a missing end after the cap
// simply stops observing.
func (s *Service) watchUsage(ctx context.Context, tenantID shareddomain.TenantID, callID shareddomain.CallID, costPerMinute int64) {
	if s.spend == nil || costPerMinute <= 0 {
		return
	}
	p := s.placement
	deadline := time.Now().Add(usageWatchTimeout)
	for time.Now().Before(deadline) {
		out, err := p.Status.Status(ctx, tenantID, callID)
		if err == nil && out.Ended {
			if out.AnsweredAt == nil {
				return
			}
			ended := s.now()
			if out.EndedAt != nil {
				ended = *out.EndedAt
			}
			seconds := int64(ended.Sub(*out.AnsweredAt) / time.Second)
			if seconds < 1 {
				seconds = 1
			}
			cost := seconds * costPerMinute / 60
			if cost > 0 {
				_ = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
					if err := s.spend.Add(ctx, tx, tenantID, ended, cost); err != nil {
						slog.Default().Warn("pbx: record spend failed", "tenant_id", tenantID, "error", err)
					}
					return nil
				})
			}
			return
		}
		time.Sleep(p.WatchInterval)
	}
}

// GetOutboundPolicy returns the tenant's fraud guard configuration.
func (s *Service) GetOutboundPolicy(ctx context.Context, tenantID shareddomain.TenantID) (ports.OutboundPolicyView, error) {
	actor, err := actorFrom(ctx)
	if err != nil {
		return ports.OutboundPolicyView{}, err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionReadOutboundPolicy, tenantID.String()); err != nil {
		return ports.OutboundPolicyView{}, err
	}
	if s.policies == nil {
		return outboundPolicyView(domain.DefaultOutboundPolicy()), nil
	}
	var policy domain.OutboundPolicy
	err = s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		policy, err = s.policies.Get(ctx, tx, tenantID)
		return err
	})
	if err != nil {
		return ports.OutboundPolicyView{}, err
	}
	return outboundPolicyView(policy), nil
}

// SetOutboundPolicy updates the configuration but never silently resumes
// a suspended tenant: suspension is cleared only by ResumeOutbound.
func (s *Service) SetOutboundPolicy(ctx context.Context, cmd ports.SetOutboundPolicyCommand) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionSetOutboundPolicy, cmd.TenantID.String()); err != nil {
		return err
	}
	next := cmd.Policy
	if err := next.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ports.ErrInvalidInput, err)
	}
	if s.policies == nil {
		return errors.New("outbound policy is not configured")
	}
	return s.pool.WithTenant(ctx, cmd.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		current, err := s.policies.Get(ctx, tx, cmd.TenantID)
		if err != nil {
			return err
		}
		next.Suspended, next.SuspendedAt, next.SuspendedReason, next.LastCapAlertAt =
			current.Suspended, current.SuspendedAt, current.SuspendedReason, current.LastCapAlertAt
		if err := s.policies.Set(ctx, tx, cmd.TenantID, next); err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, cmd.TenantID, "outbound.policy.set",
			"outbound_policy", cmd.TenantID.String(), outboundPolicyAudit(&current), outboundPolicyAudit(&next))
	})
}

// ResumeOutbound clears an automatic or manual suspension.
func (s *Service) ResumeOutbound(ctx context.Context, tenantID shareddomain.TenantID) error {
	actor, err := actorFrom(ctx)
	if err != nil {
		return err
	}
	if err := s.authz.AuthorizeAction(ctx, actor.PrincipalID, actionResumeOutbound, tenantID.String()); err != nil {
		return err
	}
	if s.policies == nil {
		return nil
	}
	return s.pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := s.policies.Resume(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		return s.appendAuditEntry(ctx, tx, actor, tenantID, "outbound.resumed",
			"outbound_policy", tenantID.String(), nil, nil)
	})
}

func outboundPolicyView(p domain.OutboundPolicy) ports.OutboundPolicyView {
	return ports.OutboundPolicyView{
		DailySpendCapMicros: p.DailySpendCapMicros, MonthlySpendCapMicros: p.MonthlySpendCapMicros,
		BlockedPrefixes:      append([]string(nil), p.BlockedPrefixes...),
		AnomalyCallThreshold: p.AnomalyCallThreshold, AnomalyWindow: p.AnomalyWindow,
		BusinessStartMinute: p.BusinessStartMinute, BusinessEndMinute: p.BusinessEndMinute,
		Suspended: p.Suspended, SuspendedAt: p.SuspendedAt, SuspendedReason: p.SuspendedReason,
	}
}

func outboundPolicyAudit(p *domain.OutboundPolicy) map[string]any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"daily_spend_cap_micros": p.DailySpendCapMicros, "monthly_spend_cap_micros": p.MonthlySpendCapMicros,
		"blocked_prefixes": p.BlockedPrefixes, "anomaly_call_threshold": p.AnomalyCallThreshold,
		"anomaly_window_seconds": int(p.AnomalyWindow.Seconds()),
		"business_start_minute":  p.BusinessStartMinute, "business_end_minute": p.BusinessEndMinute,
		"suspended": p.Suspended, "suspended_reason": p.SuspendedReason,
	}
}
