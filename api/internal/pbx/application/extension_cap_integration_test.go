//go:build integration

package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	identityports "atsap-api/internal/identity/ports"
	identitypg "atsap-api/internal/identity/postgres"
	"atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpg "atsap-api/internal/pbx/postgres"
	"atsap-api/internal/pbx/rpc"
	shareddomain "atsap-api/internal/shared/domain"
)

// Extension cap (D-51 mirrored; spec pbx-core/extension-management).

func (h *harness) tryCreate(number string) (ports.CreatedExtension, error) {
	return h.svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: number, DisplayName: "Ext " + number, DeviceType: domain.DeviceSIP,
	})
}

func TestCreateExtension_EleventhIsRefusedOnATenExtensionLicence(t *testing.T) {
	h := newHarness(t)
	h.extCap.Store(10)
	for i := range 10 {
		h.create(t, fmt.Sprintf("10%02d", i))
	}
	endpointsBefore := h.countRow(t, `SELECT count(*) FROM ps_endpoints`)
	auditBefore := h.countTenantRow(t, `SELECT count(*) FROM audit_logs`)

	_, err := h.tryCreate("1010")
	require.ErrorIs(t, err, ports.ErrEntitlementExceeded)
	assert.NotErrorIs(t, err, identityports.ErrPermissionDenied,
		"a licence refusal is not an authorization failure (AC-06.14)")

	assert.Equal(t, 10, h.countTenantRow(t, `SELECT count(*) FROM extensions`), "the refused extension must not exist")
	assert.Equal(t, endpointsBefore, h.countRow(t, `SELECT count(*) FROM ps_endpoints`), "nor be projected")
	assert.Equal(t, auditBefore, h.countTenantRow(t, `SELECT count(*) FROM audit_logs`), "nor be audited as created")
}

func TestCreateExtension_ZeroCapIsUnlimited(t *testing.T) {
	h := newHarness(t)
	h.extCap.Store(0)
	for i := range 15 {
		h.create(t, fmt.Sprintf("20%02d", i))
	}
	assert.Equal(t, 15, h.countTenantRow(t, `SELECT count(*) FROM extensions`))
}

// An unreadable licence fails closed: defaulting to zero would mean
// "unlimited".
func TestCreateExtension_CapReadFailureFailsClosed(t *testing.T) {
	h := newHarness(t)
	failing := ports.ExtensionCapFunc(func(context.Context) (int, error) {
		return 0, errors.New("entitlement unreadable")
	})
	svc := application.NewService(h.pool, pbxpg.NewStore(), pbxpg.NewTrunkStore(), pbxpg.NewRouteStore(), failing, h.projector, h.authz,
		identitypg.NewStore(h.pool), testRealm)

	_, err := svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: "3000", DisplayName: "Ext", DeviceType: domain.DeviceSIP,
	})
	require.Error(t, err)
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM extensions`))
}

// Two creations racing for the last slot must not both commit.
//
// Proved deterministically, not by launching goroutines and hoping they
// interleave between the count and the insert (identity's equivalent
// passed with its lock removed). One creation takes the last slot
// through the store's own CountForCreate and holds its transaction open;
// the service's creation must wait, then count afresh and refuse.
func TestCreateExtension_RacingCreationsCannotBothTakeTheLastSlot(t *testing.T) {
	h := newHarness(t)
	h.extCap.Store(2)
	h.create(t, "4000") // one slot left

	store := pbxpg.NewStore()
	locked, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- h.pool.WithTenant(context.Background(), h.tenant, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := store.CountForCreate(ctx, tx, h.tenant); err != nil {
				return err
			}
			now := time.Now().UTC()
			ext := domain.Extension{
				ID: shareddomain.NewExtensionID(), TenantID: h.tenant, Number: "4001",
				DisplayName: "slot taker", DeviceType: domain.DeviceSIP, CreatedAt: now, UpdatedAt: now,
			}
			ext.AuthUsername = domain.EndpointIdentifier(ext.ID)
			ext.SecretDigest = "0123456789abcdef0123456789abcdef"
			if err := store.Insert(ctx, tx, ext); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked

	done := make(chan error, 1)
	go func() { _, err := h.tryCreate("4002"); done <- err }()

	select {
	case err := <-done:
		close(release)
		t.Fatalf("CreateExtension finished (%v) while another creation held the tenant's lock: "+
			"it counted without serializing, so two racers could both take the last slot", err)
	case <-time.After(300 * time.Millisecond):
	}

	close(release)
	require.NoError(t, <-holderDone)
	select {
	case err := <-done:
		require.ErrorIs(t, err, ports.ErrEntitlementExceeded,
			"the waiter must count after the slot taker commits and find no room")
	case <-time.After(5 * time.Second):
		t.Fatal("CreateExtension never resumed after the lock was released")
	}
	assert.Equal(t, 2, h.countTenantRow(t, `SELECT count(*) FROM extensions`))
}

// A licence allowing fewer extensions than exist removes, hides and
// deprovisions nothing. Every extension stays listed, readable,
// updatable, rotatable, deletable and projected; only creation is
// refused (AC-06.13).
func TestCreateExtension_LoweredCapLeavesTheEstateUntouched(t *testing.T) {
	h := newHarness(t)
	var existing []ports.CreatedExtension
	for i := range 3 {
		existing = append(existing, h.create(t, fmt.Sprintf("50%02d", i)))
	}

	h.extCap.Store(1) // the licence in force now allows fewer than exist

	listed, err := h.svc.ListExtensions(h.ctx, h.tenant, ports.Page{Limit: 50})
	require.NoError(t, err)
	assert.Len(t, listed, 3, "nothing hidden")
	for _, v := range listed {
		assert.Equal(t, domain.StatusNotRegistered, v.Registration, "registration state still attached")
	}

	for _, e := range existing {
		id := e.Extension.ID
		_, err := h.svc.GetExtension(h.ctx, h.tenant, id)
		require.NoError(t, err)
		require.NoError(t, h.svc.UpdateExtension(h.ctx, ports.UpdateExtensionCommand{
			TenantID: h.tenant, ID: id, DisplayName: "renamed", DeviceType: domain.DeviceSIP,
		}))
		_, err = h.svc.RegenerateSecret(h.ctx, h.tenant, id)
		require.NoError(t, err)
		assert.Equal(t, 1, h.countRow(t, `SELECT count(*) FROM ps_endpoints WHERE id = $1`,
			domain.EndpointIdentifier(id)), "still projected: a phone on it keeps registering")
	}

	_, err = h.tryCreate("5099")
	require.ErrorIs(t, err, ports.ErrEntitlementExceeded)

	// Deleting below the cap frees room again: the cap counts what
	// exists, not what was ever created.
	for _, e := range existing {
		require.NoError(t, h.svc.DeleteExtension(h.ctx, h.tenant, e.Extension.ID))
	}
	_, err = h.tryCreate("5099")
	require.NoError(t, err)
}

// On the wire the refusal is FAILED_PRECONDITION naming the licence,
// through the real service rather than a fake, so the sentinel's
// survival through the transaction's error wrapping is proven too
// (design D5, AC-06.14).
func TestCreateExtension_AtCapIsFailedPreconditionOnTheWire(t *testing.T) {
	h := newHarness(t)
	h.extCap.Store(1)
	h.create(t, "6000")

	_, err := rpc.NewPbxHandler(h.svc).CreateExtension(h.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
		TenantId: h.tenant.String(), Number: "6001", DisplayName: "over cap",
		DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_SIP,
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "never PERMISSION_DENIED")
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Message(), "licence entitlement", "the reason identifies the licence, not the caller's roles")
}

// Before a licence is applied, nothing is provisioned: the refusal is
// FAILED_PRECONDITION saying so, and neither the extension nor its engine
// projection is written (phase-a-hardening task 3.1, D-72).
func TestCreateExtension_NotLicensedIsRefusedOnTheWire(t *testing.T) {
	h := newHarness(t)
	notLicensed := ports.ExtensionCapFunc(func(context.Context) (int, error) {
		return 0, fmt.Errorf("licence state Setup: %w", ports.ErrNotLicensed)
	})
	svc := application.NewService(h.pool, pbxpg.NewStore(), pbxpg.NewTrunkStore(), pbxpg.NewRouteStore(), notLicensed, h.projector, h.authz,
		identitypg.NewStore(h.pool), testRealm)

	_, err := rpc.NewPbxHandler(svc).CreateExtension(h.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
		TenantId: h.tenant.String(), Number: "7000", DisplayName: "before activation",
		DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_SIP,
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err), "never PERMISSION_DENIED")
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Contains(t, cerr.Message(), "no licence is applied")
	assert.Zero(t, h.countTenantRow(t, `SELECT count(*) FROM extensions`))
	var projected int
	require.NoError(t, h.pool.Unwrap().QueryRow(context.Background(), `SELECT count(*) FROM ps_endpoints`).Scan(&projected))
	assert.Zero(t, projected, "nothing is projected to the engine")
}
