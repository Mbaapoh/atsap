//go:build integration

package postgres_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
	telephonypostgres "atsap-api/internal/telephony/postgres"
	"atsap-api/internal/telephony/rpc"
)

// seedCalls writes, for tenant: an answered-then-ended call, a call
// Screening refused (never answered), and a live inbound call — started
// one second apart so "newest first" is observable.
func seedCalls(t *testing.T, store *telephonypostgres.CallStore, tenant shareddomain.TenantID, base time.Time) (ended, refused, live *domain.Call) {
	t.Helper()
	ctx := context.Background()
	mk := func(dir domain.Direction, at time.Time) (*domain.Call, *domain.CallParticipant, *domain.CallParticipant) {
		c := domain.NewCall(shareddomain.NewCallID(), tenant, dir, "1000", "+442071234567", at)
		a := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: c.ID, TenantID: tenant, Role: domain.RoleCaller, EndpointURI: "PJSIP/e_0123456789abcdef0123456789abcdef"}
		b := &domain.CallParticipant{ID: shareddomain.NewParticipantID(), CallID: c.ID, TenantID: tenant, Role: domain.RoleAgent, EndpointURI: "PJSIP/442071234567@t_0123456789abcdef0123456789abcdef"}
		require.NoError(t, c.AddParticipant(a))
		require.NoError(t, c.AddParticipant(b))
		require.NoError(t, c.TransitionToScreening())
		return c, a, b
	}

	ended, a, b := mk(domain.Outbound, base)
	_, err := ended.ApplyScreeningVerdict(true, true, base)
	require.NoError(t, err)
	require.NoError(t, ended.TransitionToPresenting())
	_, err = ended.ConnectParticipant(a.ID, base.Add(2*time.Second))
	require.NoError(t, err)
	_, err = ended.ConnectParticipant(b.ID, base.Add(2*time.Second))
	require.NoError(t, err)
	_, err = ended.DisconnectParticipant(a.ID, base.Add(32*time.Second))
	require.NoError(t, err)
	_, err = ended.Terminate("normal clearing", base.Add(32*time.Second))
	require.NoError(t, err)
	require.NoError(t, store.SaveCall(ctx, ended, nil))

	refused, _, _ = mk(domain.Outbound, base.Add(time.Second))
	_, err = refused.ApplyScreeningVerdict(false, true, base.Add(time.Second))
	require.NoError(t, err)
	require.NoError(t, store.SaveCall(ctx, refused, nil))

	live, a, b = mk(domain.Inbound, base.Add(2*time.Second))
	_, err = live.ApplyScreeningVerdict(true, true, base.Add(2*time.Second))
	require.NoError(t, err)
	require.NoError(t, live.TransitionToPresenting())
	_, err = live.ConnectParticipant(a.ID, base.Add(3*time.Second))
	require.NoError(t, err)
	_, err = live.ConnectParticipant(b.ID, base.Add(3*time.Second))
	require.NoError(t, err)
	require.NoError(t, store.SaveCall(ctx, live, nil))
	return ended, refused, live
}

func TestListCalls_IsolationFiltersPaging(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)
	ctx := context.Background()
	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()

	tenantA, tenantB := shareddomain.NewTenantID(), shareddomain.NewTenantID()
	seedTenant(t, pool, tenantA)
	seedTenant(t, pool, tenantB)
	store := telephonypostgres.NewCallStore(pool)
	base := time.Now().UTC().Truncate(time.Second)
	ended, refused, live := seedCalls(t, store, tenantA, base)
	seedCalls(t, store, tenantB, base)

	all, err := store.ListCalls(ctx, tenantA, ports.CallFilter{}, 50, 0)
	require.NoError(t, err)
	require.Len(t, all, 3, "tenant B's calls never appear")
	assert.Equal(t, []shareddomain.CallID{live.ID, refused.ID, ended.ID},
		[]shareddomain.CallID{all[0].ID, all[1].ID, all[2].ID}, "newest first")

	term, err := store.ListCalls(ctx, tenantA, ports.CallFilter{State: domain.CallTerminated}, 50, 0)
	require.NoError(t, err)
	assert.Len(t, term, 2)
	in, err := store.ListCalls(ctx, tenantA, ports.CallFilter{Direction: domain.Inbound}, 50, 0)
	require.NoError(t, err)
	require.Len(t, in, 1)
	assert.Equal(t, live.ID, in[0].ID)
	window, err := store.ListCalls(ctx, tenantA, ports.CallFilter{From: base.Add(time.Second), To: base.Add(2 * time.Second)}, 50, 0)
	require.NoError(t, err)
	require.Len(t, window, 1, "[from, to): includes from, excludes to")
	assert.Equal(t, refused.ID, window[0].ID)

	page, err := store.ListCalls(ctx, tenantA, ports.CallFilter{}, 1, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, refused.ID, page[0].ID)
}

// Through the real handler: durations, reasons, and — task 3.2 — no
// engine identifier anywhere, although every participant row in the
// database holds one.
func TestListCalls_WireCarriesNoEngineIdentifier(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetSchema(t, databaseURL)
	ctx := context.Background()
	pool, err := corepostgres.Open(ctx, databaseURL)
	require.NoError(t, err)
	defer pool.Close()
	tenant := shareddomain.NewTenantID()
	seedTenant(t, pool, tenant)
	store := telephonypostgres.NewCallStore(pool)
	base := time.Now().UTC().Truncate(time.Second)
	ended, refused, _ := seedCalls(t, store, tenant, base)

	h := rpc.NewTelephonyHandler(store, store)
	res, err := h.ListCalls(ctx, connect.NewRequest(&atsapbxv1.ListCallsRequest{TenantId: tenant.String()}))
	require.NoError(t, err)
	byID := map[string]*atsapbxv1.CallSummary{}
	for _, c := range res.Msg.GetCalls() {
		byID[c.GetCallId()] = c
	}
	assert.Equal(t, int32(30), byID[ended.ID.String()].GetDurationSeconds(), "answer to end")
	assert.Equal(t, int32(0), byID[refused.ID.String()].GetDurationSeconds(), "never answered")
	assert.NotEmpty(t, byID[refused.ID.String()].GetTerminationReason(), "a setup failure says why")

	b, err := protojson.Marshal(res.Msg)
	require.NoError(t, err)
	text := string(b)
	for _, f := range []string{"PJSIP/", "atsa-part-", "bridge"} {
		assert.NotContains(t, text, f)
	}
	assert.False(t, regexp.MustCompile(`[et]_[0-9a-f]{32}`).MatchString(text), "no engine identifier in %s", text)

	_, err = h.ListCalls(ctx, connect.NewRequest(&atsapbxv1.ListCallsRequest{TenantId: tenant.String(), State: "Bogus"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = h.ListCalls(ctx, connect.NewRequest(&atsapbxv1.ListCallsRequest{TenantId: tenant.String(),
		StartedFrom: timestamppb.New(base.Add(time.Hour))}))
	require.NoError(t, err)
}
