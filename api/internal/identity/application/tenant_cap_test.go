package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
)

// Tenant cap (LLD-02 §4a.1, D-51; spec identity/tenant-provisioning).

func provision(h *harness, name string) (domain.Tenant, error) {
	return h.svc.ProvisionTenant(context.Background(), application.SystemActor(), name, "EU")
}

func TestProvisionTenant_RefusesAtTheCap(t *testing.T) {
	for _, maxTenants := range []int{1, 3} {
		h := newHarness(t)
		h.stores.TenantCap = maxTenants

		for i := 0; i < maxTenants; i++ {
			_, err := provision(h, "within-cap")
			require.NoError(t, err, "tenant %d of %d is within the cap", i+1, maxTenants)
		}
		auditBefore := len(h.stores.Audit)

		_, err := provision(h, "over-cap")
		require.ErrorIs(t, err, ports.ErrEntitlementExceeded)
		assert.NotErrorIs(t, err, ports.ErrPermissionDenied,
			"a licence refusal is not an authorization failure (AC-06.14)")
		assert.Equal(t, maxTenants, h.stores.TenantCount(), "the refused tenant must not exist")
		assert.Len(t, h.stores.Audit, auditBefore, "a refusal is not a mutation and records no tenant.create")
	}
}

func TestProvisionTenant_ZeroCapIsUnlimited(t *testing.T) {
	h := newHarness(t)
	h.stores.TenantCap = 0
	for i := 0; i < 25; i++ {
		_, err := provision(h, "tenant")
		require.NoError(t, err)
	}
	assert.Equal(t, 25, h.stores.TenantCount())
}

// An installation that cannot read its licence does not get to guess:
// defaulting to zero here would mean "unlimited".
func TestProvisionTenant_CapReadFailureFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.stores.TenantCapErr = errors.New("entitlement unreadable")

	_, err := provision(h, "tenant")
	require.Error(t, err)
	assert.Zero(t, h.stores.TenantCount())
}

// A cap below the existing count removes nothing, hides nothing, and
// leaves every existing tenant fully usable; only the next provisioning
// is refused (AC-06.13).
func TestProvisionTenant_LoweredCapGrandfathersExistingTenants(t *testing.T) {
	h := newHarness(t)
	var existing []domain.Tenant
	for i := 0; i < 3; i++ {
		tenant, err := provision(h, "existing")
		require.NoError(t, err)
		existing = append(existing, tenant)
	}

	h.stores.TenantCap = 1 // the licence in force now allows fewer than exist

	for _, tenant := range existing {
		got, err := h.svc.LoadTenant(context.Background(), tenant.ID)
		require.NoError(t, err, "reads are never gated")
		assert.Equal(t, domain.TenantActive, got.Status)
		require.NoError(t, h.svc.SetTenantStatus(context.Background(), application.SystemActor(),
			tenant.ID, domain.TenantSuspended), "existing tenants stay administrable")
	}

	_, err := provision(h, "new")
	require.ErrorIs(t, err, ports.ErrEntitlementExceeded)
	assert.Equal(t, 3, h.stores.TenantCount())
}
