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
	pbxpostgres "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

func TestFlowStore_CreatePublishRollbackHolidays(t *testing.T) {
	h := newStore(t)
	ctx := context.Background()
	store := pbxpostgres.NewFlowStore()
	open, closed := shareddomain.NewExtensionID(), shareddomain.NewExtensionID()
	principal := shareddomain.NewPrincipalID()
	now := time.Now().UTC()
	flow := pbxdomain.Flow{ID: shareddomain.NewFlowID(), TenantID: h.tenantA, Number: "4420", Name: "Main", ActiveVersion: 1, CreatedAt: now, UpdatedAt: now}
	v1 := pbxdomain.FlowVersion{FlowID: flow.ID, Version: 1, Config: pbxdomain.FlowConfig{OpenExtensionID: &open, BusinessStartMinute: 480, BusinessEndMinute: 1080}, PublishedAt: now, PublishedBy: principal}

	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return store.Create(ctx, tx, h.tenantA, flow, v1)
	}))

	var got pbxdomain.Flow
	var gotV pbxdomain.FlowVersion
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		got, gotV, err = store.FindByNumber(ctx, tx, h.tenantA, "4420")
		return err
	}))
	assert.Equal(t, "Main", got.Name)
	assert.Equal(t, open, *gotV.Config.OpenExtensionID)

	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		version, err := store.Publish(ctx, tx, h.tenantA, flow.ID, pbxdomain.FlowConfig{OpenExtensionID: &open, ClosedExtensionID: &closed, BusinessStartMinute: 480, BusinessEndMinute: 1080}, principal, now)
		assert.Equal(t, 2, version)
		return err
	}))
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return store.Rollback(ctx, tx, h.tenantA, flow.ID, 1)
	}))
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		f, v, err := store.Get(ctx, tx, h.tenantA, flow.ID)
		assert.Equal(t, 1, f.ActiveVersion)
		assert.Nil(t, v.Config.ClosedExtensionID)
		return err
	}))

	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		return store.AddHoliday(ctx, tx, h.tenantA, "2026-12-25", "Christmas")
	}))
	require.NoError(t, h.pool.WithTenant(ctx, h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		holidays, err := store.ListHolidays(ctx, tx, h.tenantA)
		assert.Equal(t, "Christmas", holidays["2026-12-25"])
		return err
	}))

}
