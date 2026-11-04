package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// OutboundPolicy is a tenant's fraud and spend configuration. Zero caps
// mean unlimited; a zero anomaly threshold disables anomaly detection.
// Business hours are UTC minutes from midnight, [start, end); an overnight
// window is supported.
type OutboundPolicy struct {
	DailySpendCapMicros   int64
	MonthlySpendCapMicros int64
	BlockedPrefixes       []string
	AnomalyCallThreshold  int
	AnomalyWindow         time.Duration
	BusinessStartMinute   int
	BusinessEndMinute     int
	Suspended             bool
	SuspendedAt           *time.Time
	SuspendedReason       string
	LastCapAlertAt        *time.Time
}

// DefaultOutboundPolicy is the safe configuration for a tenant with no
// stored row: no spend cap (Phase A has no rating currency configured),
// no blocked prefixes, and anomaly detection at 20 international calls
// in ten minutes outside 08:00-18:00 UTC.
func DefaultOutboundPolicy() OutboundPolicy {
	return OutboundPolicy{
		AnomalyCallThreshold: 20,
		AnomalyWindow:        10 * time.Minute,
		BusinessStartMinute:  8 * 60,
		BusinessEndMinute:    18 * 60,
	}
}

// Fraud errors.
var (
	ErrBlockedPrefix        = errors.New("destination matches a blocked prefix")
	ErrOutboundSuspended    = errors.New("outbound calling is suspended for this tenant")
	ErrSpendCapReached      = errors.New("the tenant's outbound spend cap is reached")
	ErrBlockedPrefixInvalid = errors.New("blocked prefix must be an optional + followed by 1-32 digits")
)

// Validate checks a caller-supplied policy. Caps are non-negative; a
// negative one is refused rather than treated as unlimited.
func (p OutboundPolicy) Validate() error {
	if p.DailySpendCapMicros < 0 || p.MonthlySpendCapMicros < 0 {
		return errors.New("spend caps must not be negative")
	}
	if p.AnomalyCallThreshold < 0 {
		return errors.New("anomaly threshold must not be negative")
	}
	if p.AnomalyWindow < 0 {
		return errors.New("anomaly window must not be negative")
	}
	if p.BusinessStartMinute < 0 || p.BusinessStartMinute >= 24*60 ||
		p.BusinessEndMinute < 0 || p.BusinessEndMinute >= 24*60 {
		return errors.New("business hours must be minutes between 0 and 1439")
	}
	for _, prefix := range p.BlockedPrefixes {
		if err := ValidateBlockedPrefix(prefix); err != nil {
			return err
		}
	}
	return nil
}

// ValidateBlockedPrefix accepts digits with an optional leading "+".
func ValidateBlockedPrefix(prefix string) error {
	body := strings.TrimPrefix(prefix, "+")
	if body == "" || len(body) > 32 {
		return fmt.Errorf("%w: %q", ErrBlockedPrefixInvalid, prefix)
	}
	for _, r := range body {
		if r < '0' || r > '9' {
			return fmt.Errorf("%w: %q", ErrBlockedPrefixInvalid, prefix)
		}
	}
	return nil
}

// Blocks reports whether destination matches any blocked prefix.
func (p OutboundPolicy) Blocks(destination string) bool {
	for _, prefix := range p.BlockedPrefixes {
		if strings.HasPrefix(destination, prefix) {
			return true
		}
	}
	return false
}

// OutsideBusinessHours reports whether at is outside the configured
// window, in UTC. A start equal to end means business all day.
func (p OutboundPolicy) OutsideBusinessHours(at time.Time) bool {
	if p.BusinessStartMinute == p.BusinessEndMinute {
		return false
	}
	minute := at.UTC().Hour()*60 + at.UTC().Minute()
	if p.BusinessStartMinute < p.BusinessEndMinute {
		return minute < p.BusinessStartMinute || minute >= p.BusinessEndMinute
	}
	// Overnight: business from start through midnight into end.
	return minute >= p.BusinessEndMinute && minute < p.BusinessStartMinute
}

// SpendExceeded reports whether either cap has been reached.
func (p OutboundPolicy) SpendExceeded(day, month int64) (bool, string) {
	if p.DailySpendCapMicros > 0 && day >= p.DailySpendCapMicros {
		return true, "daily spend cap reached"
	}
	if p.MonthlySpendCapMicros > 0 && month >= p.MonthlySpendCapMicros {
		return true, "monthly spend cap reached"
	}
	return false, ""
}

// Anomaly reports whether one more international call after recent would
// cross the threshold outside business hours. recent excludes the call
// currently being placed.
func (p OutboundPolicy) Anomaly(recent int, at time.Time, international bool) bool {
	return international && p.AnomalyCallThreshold > 0 &&
		p.OutsideBusinessHours(at) && recent+1 >= p.AnomalyCallThreshold
}

// FraudAlert is what the operator is told when a tenant is auto-suspended.
type FraudAlert struct {
	TenantID       string
	Destination    string
	RecentCalls    int
	Threshold      int
	Window         time.Duration
	At             time.Time
	SuspendedUntil string // empty: manual resume
}

// SpendAlert tells the operator that a tenant reached a cap.
type SpendAlert struct {
	TenantID string
	Reason   string
	At       time.Time
}
