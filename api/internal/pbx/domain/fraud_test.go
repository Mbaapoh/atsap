package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
)

func TestOutboundPolicyValidation(t *testing.T) {
	p := domain.DefaultOutboundPolicy()
	require.NoError(t, p.Validate())
	for _, prefix := range []string{"+1900", "239", "+1"} {
		assert.NoError(t, domain.ValidateBlockedPrefix(prefix), prefix)
	}
	for _, prefix := range []string{"", "+", "19a0", "+1900#"} {
		assert.ErrorIs(t, domain.ValidateBlockedPrefix(prefix), domain.ErrBlockedPrefixInvalid, prefix)
	}
	bad := p
	bad.DailySpendCapMicros = -1
	assert.Error(t, bad.Validate())
}

func TestOutboundPolicyBlocks(t *testing.T) {
	p := domain.OutboundPolicy{BlockedPrefixes: []string{"+1900", "239"}}
	assert.True(t, p.Blocks("+19005551212"))
	assert.True(t, p.Blocks("2391234"))
	assert.False(t, p.Blocks("+442071234567"))
}

func TestOutboundPolicyBusinessHours(t *testing.T) {
	p := domain.OutboundPolicy{BusinessStartMinute: 8 * 60, BusinessEndMinute: 18 * 60}
	at := func(h, m int) time.Time { return time.Date(2026, 11, 4, h, m, 0, 0, time.UTC) }
	assert.False(t, p.OutsideBusinessHours(at(8, 0)))
	assert.False(t, p.OutsideBusinessHours(at(17, 59)))
	assert.True(t, p.OutsideBusinessHours(at(18, 0)))
	assert.True(t, p.OutsideBusinessHours(at(7, 59)))

	overnight := domain.OutboundPolicy{BusinessStartMinute: 22 * 60, BusinessEndMinute: 6 * 60}
	assert.False(t, overnight.OutsideBusinessHours(at(23, 0)))
	assert.False(t, overnight.OutsideBusinessHours(at(5, 0)))
	assert.True(t, overnight.OutsideBusinessHours(at(12, 0)))

	allDay := domain.OutboundPolicy{BusinessStartMinute: 0, BusinessEndMinute: 0}
	assert.False(t, allDay.OutsideBusinessHours(at(3, 0)))
}

func TestOutboundPolicySpendAndAnomaly(t *testing.T) {
	p := domain.OutboundPolicy{DailySpendCapMicros: 100, MonthlySpendCapMicros: 500}
	exceeded, reason := p.SpendExceeded(100, 0)
	assert.True(t, exceeded)
	assert.Contains(t, reason, "daily")
	exceeded, reason = p.SpendExceeded(1, 500)
	assert.True(t, exceeded)
	assert.Contains(t, reason, "monthly")
	exceeded, _ = p.SpendExceeded(99, 499)
	assert.False(t, exceeded)

	p = domain.DefaultOutboundPolicy()
	outside := time.Date(2026, 11, 4, 23, 0, 0, 0, time.UTC)
	assert.False(t, p.Anomaly(0, outside, true), "one call is below the threshold")
	assert.True(t, p.Anomaly(p.AnomalyCallThreshold-1, outside, true))
	assert.False(t, p.Anomaly(100, outside, false), "national calls do not count")
	inside := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	assert.False(t, p.Anomaly(100, inside, true), "business hours are not the pattern")
}

func TestOutboundPolicyRejectsBadValues(t *testing.T) {
	base := domain.DefaultOutboundPolicy()
	cases := []domain.OutboundPolicy{
		{DailySpendCapMicros: -1},
		{MonthlySpendCapMicros: -1},
		{AnomalyCallThreshold: -1},
		{AnomalyWindow: -time.Second},
		{BusinessStartMinute: -1},
		{BusinessEndMinute: 1440},
		{BlockedPrefixes: []string{"12a"}},
	}
	for _, c := range cases {
		assert.Error(t, c.Validate(), "%+v", c)
	}
	assert.NoError(t, base.Validate())
}

func TestOutboundPolicyDisabledRuleDoesNothing(t *testing.T) {
	p := domain.OutboundPolicy{AnomalyCallThreshold: 0}
	outside := time.Date(2026, 11, 4, 23, 0, 0, 0, time.UTC)
	assert.False(t, p.Anomaly(100, outside, true))
	assert.False(t, p.Blocks("+4420"))
	assert.False(t, p.OutsideBusinessHours(outside), "zero start and end mean all day")
	exceeded, reason := p.SpendExceeded(1<<62, 1<<62)
	assert.False(t, exceeded)
	assert.Empty(t, reason)
}
