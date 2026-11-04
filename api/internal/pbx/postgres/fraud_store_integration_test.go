//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbxdomain "atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpostgres "atsap-api/internal/pbx/postgres"
)

func TestFraudStore_PolicySpendAndAttempts(t *testing.T) {
	h := newStore(t)
	ctx := context.Background()
	policies := pbxpostgres.NewOutboundPolicyStore()
	spend := pbxpostgres.NewSpendStore()
	attempts := pbxpostgres.NewAttemptStore()
	now := time.Now().UTC()

	var got pbxdomain.OutboundPolicy
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		got, err = policies.Get(ctx, tx, h.tenantA)
		return err
	}))
	assert.Equal(t, 20, got.AnomalyCallThreshold, "the default applies when no row is stored")

	policy := pbxdomain.OutboundPolicy{
		DailySpendCapMicros: 100, BlockedPrefixes: []string{"+1900"},
		AnomalyCallThreshold: 5, AnomalyWindow: 10 * time.Minute,
		BusinessStartMinute: 480, BusinessEndMinute: 1080,
	}
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return policies.Set(ctx, tx, h.tenantA, policy)
	}))
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		got, _ = policies.Get(ctx, tx, h.tenantA)
		return nil
	}))
	assert.Equal(t, int64(100), got.DailySpendCapMicros)
	assert.Equal(t, []string{"+1900"}, got.BlockedPrefixes)

	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return spend.Add(ctx, tx, h.tenantA, now, 30)
	}))
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return spend.Add(ctx, tx, h.tenantA, now, 20)
	}))
	var gotSpend ports.Spend
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		gotSpend, err = spend.Read(ctx, tx, h.tenantA, now)
		return err
	}))
	assert.Equal(t, int64(50), gotSpend.DayMicros)
	assert.Equal(t, int64(50), gotSpend.MonthMicros)

	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		if err := attempts.LockTenant(ctx, tx, h.tenantA); err != nil {
			return err
		}
		if err := attempts.Record(ctx, tx, h.tenantA, "+1900555", true, now); err != nil {
			return err
		}
		n, err := attempts.CountInternationalSince(ctx, tx, h.tenantA, now.Add(-time.Hour))
		assert.Equal(t, 1, n)
		return err
	}))

	var suspended bool
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		suspended, err = policies.Suspend(ctx, tx, h.tenantA, "test", now)
		return err
	}))
	assert.True(t, suspended)
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		suspended, err = policies.Resume(ctx, tx, h.tenantA)
		return err
	}))
	assert.True(t, suspended)

	// Another tenant sees neither the policy nor the spend.
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		v, err := policies.Get(ctx, tx, h.tenantB)
		assert.Equal(t, int64(0), v.DailySpendCapMicros)
		return err
	}))
}
