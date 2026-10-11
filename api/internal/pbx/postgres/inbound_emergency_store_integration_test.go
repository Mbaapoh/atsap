//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pbxdomain "atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpostgres "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

// TestInboundNumberStore covers design D7 at the store level: DIDs
// assigned, listed, resolved and deleted within a tenant; invisible
// across tenants; duplicates refused; another tenant's extension
// refused by the composite foreign key.
func TestInboundNumberStore(t *testing.T) {
	h := newStore(t)
	numbers := pbxpostgres.NewInboundNumberStore()

	extA := newExtension(h.tenantA, "1000", "Alice", pbxdomain.DeviceSIP)
	require.NoError(t, h.insert(t, extA))
	extB := newExtension(h.tenantB, "1000", "Bob", pbxdomain.DeviceSIP)
	require.NoError(t, h.insert(t, extB))

	insert := func(tenant shareddomain.TenantID, n pbxdomain.InboundNumber) error {
		return h.run(tenant, func(ctx context.Context, tx pgx.Tx) error {
			return numbers.Insert(ctx, tx, n)
		})
	}

	t.Run("round trip", func(t *testing.T) {
		n := pbxdomain.InboundNumber{ID: uuid.New(), TenantID: h.tenantA, Number: "442071234567", ExtensionID: extA.ID}
		require.NoError(t, insert(h.tenantA, n))

		var listed []pbxdomain.InboundNumber
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			listed, err = numbers.List(ctx, tx)
			return err
		}))
		require.Len(t, listed, 1)
		assert.Equal(t, n.ID, listed[0].ID)
		assert.Equal(t, "442071234567", listed[0].Number)

		var found pbxdomain.InboundNumber
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			found, err = numbers.FindByNumber(ctx, tx, "442071234567")
			return err
		}))
		assert.Equal(t, extA.ID, found.ExtensionID)

		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			return numbers.Delete(ctx, tx, n.ID)
		}))
	})

	t.Run("cross-tenant invisibility", func(t *testing.T) {
		n := pbxdomain.InboundNumber{ID: uuid.New(), TenantID: h.tenantA, Number: "442071234568", ExtensionID: extA.ID}
		require.NoError(t, insert(h.tenantA, n))

		require.NoError(t, h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error {
			listed, err := numbers.List(ctx, tx)
			require.NoError(t, err)
			assert.Empty(t, listed, "another tenant's DIDs are invisible")
			_, err = numbers.FindByNumber(ctx, tx, "442071234568")
			assert.ErrorIs(t, err, ports.ErrNotFound)
			assert.ErrorIs(t, numbers.Delete(ctx, tx, n.ID), ports.ErrNotFound,
				"deleting another tenant's DID is not found, not success")
			return nil
		}))
	})

	t.Run("duplicate refused, cross-tenant same number allowed", func(t *testing.T) {
		dup := pbxdomain.InboundNumber{ID: uuid.New(), TenantID: h.tenantA, Number: "442071234568", ExtensionID: extA.ID}
		assert.ErrorIs(t, insert(h.tenantA, dup), ports.ErrInboundNumberTaken)

		other := pbxdomain.InboundNumber{ID: uuid.New(), TenantID: h.tenantB, Number: "442071234568", ExtensionID: extB.ID}
		require.NoError(t, insert(h.tenantB, other), "DIDs are tenant-local, like extension numbers")
	})

	t.Run("another tenant's extension refused", func(t *testing.T) {
		xTenant := pbxdomain.InboundNumber{ID: uuid.New(), TenantID: h.tenantA, Number: "442071234569", ExtensionID: extB.ID}
		assert.ErrorIs(t, insert(h.tenantA, xTenant), ports.ErrNotFound)
	})

	t.Run("delete by extension", func(t *testing.T) {
		removed := []pbxdomain.InboundNumber{}
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			removed, err = numbers.DeleteByExtension(ctx, tx, extA.ID)
			return err
		}))
		assert.NotEmpty(t, removed)
		for _, n := range removed {
			assert.Equal(t, extA.ID, n.ExtensionID)
		}
		require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
			listed, err := numbers.List(ctx, tx)
			require.NoError(t, err)
			assert.Empty(t, listed)
			return nil
		}))
	})
}

// TestEmergencyStore covers design D6 at the store level: country get
// and set, additions added (idempotently), listed and deleted.
func TestEmergencyStore(t *testing.T) {
	h := newStore(t)
	emg := pbxpostgres.NewEmergencyStore()

	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		country, set, err := emg.GetCountry(ctx, tx)
		require.NoError(t, err)
		assert.Empty(t, country)
		assert.False(t, set, "no country until one is set")
		return emg.SetCountry(ctx, tx, h.tenantA, "CM")
	}))
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		country, set, err := emg.GetCountry(ctx, tx)
		require.NoError(t, err)
		assert.Equal(t, "CM", country)
		assert.True(t, set)
		return nil
	}))

	add := func(tenant shareddomain.TenantID, number string) error {
		return h.run(tenant, func(ctx context.Context, tx pgx.Tx) error {
			return emg.AddAddition(ctx, tx, tenant, number)
		})
	}
	require.NoError(t, add(h.tenantA, "1500"))
	require.NoError(t, add(h.tenantA, "1500"), "adding twice is idempotent")

	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		listed, err := emg.ListAdditions(ctx, tx)
		require.NoError(t, err)
		assert.Equal(t, []string{"1500"}, listed)
		return nil
	}))
	require.NoError(t, h.run(h.tenantB, func(ctx context.Context, tx pgx.Tx) error {
		listed, err := emg.ListAdditions(ctx, tx)
		require.NoError(t, err)
		assert.Empty(t, listed, "another tenant's additions are invisible")
		return nil
	}))
	require.NoError(t, h.run(h.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		assert.ErrorIs(t, emg.DeleteAddition(ctx, tx, "1999"), ports.ErrNotFound)
		require.NoError(t, emg.DeleteAddition(ctx, tx, "1500"))
		listed, err := emg.ListAdditions(ctx, tx)
		require.NoError(t, err)
		assert.Empty(t, listed)
		return nil
	}))
}
