//go:build integration

package application_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/acl/asterisk"
	"atsap-api/internal/pbx/application"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	pbxpg "atsap-api/internal/pbx/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	telephonydomain "atsap-api/internal/telephony/domain"
	telephonyports "atsap-api/internal/telephony/ports"
)

// inboundHarness wires the emergency/inbound stores plus the router and
// placement fakes onto the shared harness.
type inboundHarness struct {
	*harness
	svc    *application.Service
	calls  *fakeCalls
	status *stubStatus
	mu     sync.Mutex
	health []shareddomain.TrunkID
}

type stubStatus struct {
	ended    bool
	answered *time.Time
}

func (s *stubStatus) Status(context.Context, shareddomain.TenantID, shareddomain.CallID) (ports.CallOutcome, error) {
	return ports.CallOutcome{StartedAt: time.Now().UTC(), AnsweredAt: s.answered, Ended: s.ended}, nil
}

func newInboundHarness(t *testing.T) *inboundHarness {
	t.Helper()
	h := newHarness(t)
	ih := &inboundHarness{harness: h, calls: newFakeCalls(), status: &stubStatus{}}
	svc := h.svc.WithEmergencyStores(pbxpg.NewInboundNumberStore(), pbxpg.NewEmergencyStore())
	svc = svc.WithPlacement(application.Placement{
		Calls: ih.calls, Status: ih.calls, Addresser: asterisk.Addresser{},
		Legs: pbxpg.NewCallLegStore(), WatchInterval: 10 * time.Millisecond, WatchTimeout: 3 * time.Second,
	})
	ih.svc = svc.WithInbound(application.Inbound{
		Addresser: asterisk.Addresser{},
		Numbers:   pbxpg.NewInboundNumberStore(),
		Emergency: pbxpg.NewEmergencyStore(),
		Legs:      pbxpg.NewCallLegStore(),
		Status:    ih.status,
		ReportHealth: func(_ context.Context, trunkID shareddomain.TrunkID, _ int, _ time.Duration) error {
			ih.mu.Lock()
			defer ih.mu.Unlock()
			ih.health = append(ih.health, trunkID)
			return nil
		},
		WatchInterval: 10 * time.Millisecond,
		WatchTimeout:  2 * time.Second,
	})
	return ih
}

func (h *inboundHarness) createExtension(t *testing.T, number string) ports.CreatedExtension {
	t.Helper()
	got, err := h.svc.CreateExtension(h.ctx, ports.CreateExtensionCommand{
		TenantID: h.tenant, Number: number, DisplayName: "Ext " + number, DeviceType: domain.DeviceSIP,
	})
	require.NoError(t, err)
	return got
}

// register stands in for a device registering: Asterisk writes the
// contact row itself in production.
func (h *inboundHarness) register(t *testing.T, id shareddomain.ExtensionID) {
	t.Helper()
	_, err := h.pool.Unwrap().Exec(context.Background(),
		`INSERT INTO ps_contacts (id, endpoint, uri, expiration_time) VALUES ($1, $2, $3, $4)`,
		"c-"+id.String(), domain.EndpointIdentifier(id), "sip:x@10.0.0.1:5060", time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
}

func (h *inboundHarness) createTrunk(t *testing.T, name, host string, limit int) ports.TrunkView {
	t.Helper()
	v, err := h.svc.CreateTrunk(h.ctx, ports.CreateTrunkCommand{
		TenantID: h.tenant, Name: name, Host: host, Port: 5061, ChannelLimit: limit, Priority: 1,
	})
	require.NoError(t, err)
	return v
}

func (h *inboundHarness) createRoute(t *testing.T, prefix string, trunkID shareddomain.TrunkID, priority int) {
	t.Helper()
	_, err := h.svc.CreateRoute(h.ctx, ports.CreateRouteCommand{
		TenantID: h.tenant, PrefixPattern: prefix, TrunkID: trunkID, Priority: priority,
	})
	require.NoError(t, err)
}

// --- 3.4: emergency profile ------------------------------------------

func TestEmergencyProfile_CountryCatalogueAndDefaults(t *testing.T) {
	h := newInboundHarness(t)

	// No country set: the safe default, flagged.
	profile, err := h.svc.GetEmergencyProfile(h.ctx, h.tenant)
	require.NoError(t, err)
	assert.Empty(t, profile.CountryCode)
	assert.False(t, profile.CountrySet)
	assert.False(t, profile.InCatalogue)
	numbers := map[string]bool{}
	for _, n := range profile.Numbers {
		numbers[n.Number] = n.BuiltIn
	}
	for _, n := range []string{"112", "911", "999"} {
		builtIn, ok := numbers[n]
		require.True(t, ok, "default set holds %s", n)
		assert.True(t, builtIn)
	}

	// Cameroon: 112 plus the catalogue, all built-in; 911 ordinary.
	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "CM"))
	profile, err = h.svc.GetEmergencyProfile(h.ctx, h.tenant)
	require.NoError(t, err)
	assert.Equal(t, "CM", profile.CountryCode)
	assert.True(t, profile.CountrySet)
	assert.True(t, profile.InCatalogue)
	numbers = map[string]bool{}
	for _, n := range profile.Numbers {
		numbers[n.Number] = n.BuiltIn
	}
	for _, n := range []string{"112", "113", "117", "118", "119"} {
		builtIn, ok := numbers[n]
		require.True(t, ok, "CM set holds %s", n)
		assert.True(t, builtIn, "%s is built-in and cannot be removed", n)
	}
	_, ok := numbers["911"]
	assert.False(t, ok, "a foreign emergency number is ordinary in CM")

	// An uncatalogued country: 112 plus additions, flagged.
	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "XX"))
	profile, err = h.svc.GetEmergencyProfile(h.ctx, h.tenant)
	require.NoError(t, err)
	assert.True(t, profile.CountrySet)
	assert.False(t, profile.InCatalogue, "an uncatalogued country must prompt for numbers")

	// Invalid codes refused.
	assert.ErrorIs(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "C"), ports.ErrInvalidInput)
	assert.ErrorIs(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "cm"), ports.ErrInvalidInput)
	assert.ErrorIs(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, ""), ports.ErrInvalidInput)
}

func TestEmergencyNumbers_AdditionsAddAndRemove(t *testing.T) {
	h := newInboundHarness(t)
	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "CM"))

	require.NoError(t, h.svc.CreateEmergencyNumber(h.ctx, h.tenant, "+1500"))
	listed, err := h.svc.ListEmergencyNumbers(h.ctx, h.tenant)
	require.NoError(t, err)
	byNumber := map[string]bool{}
	for _, n := range listed {
		byNumber[n.Number] = n.BuiltIn
	}
	builtIn, ok := byNumber["1500"]
	require.True(t, ok, "the addition is effective")
	assert.False(t, builtIn, "additions are the tenant's own")

	require.NoError(t, h.svc.DeleteEmergencyNumber(h.ctx, h.tenant, "1500"))
	listed, err = h.svc.ListEmergencyNumbers(h.ctx, h.tenant)
	require.NoError(t, err)
	for _, n := range listed {
		assert.NotEqual(t, "1500", n.Number)
	}
}

func TestEmergencyNumbers_BuiltInCannotBeDeleted(t *testing.T) {
	h := newInboundHarness(t)
	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "CM"))

	for _, n := range []string{"112", "117", "118", "119", "113"} {
		err := h.svc.DeleteEmergencyNumber(h.ctx, h.tenant, n)
		assert.ErrorIs(t, err, ports.ErrEmergencyNumberProtected, "built-in %s must survive", n)
	}
	assert.Error(t, h.svc.CreateEmergencyNumber(h.ctx, h.tenant, "not-a-number"))
}

func TestEmergencyProfile_OtherTenantUnaffected(t *testing.T) {
	h := newInboundHarness(t)
	otherID, otherCtx := h.otherTenant(t)

	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "CM"))
	require.NoError(t, h.svc.CreateEmergencyNumber(h.ctx, h.tenant, "1500"))

	profile, err := h.svc.GetEmergencyProfile(otherCtx, otherID)
	require.NoError(t, err)
	assert.False(t, profile.CountrySet, "countries are per tenant")
}

// TestPlaceCall_UsesEffectiveEmergencySet covers the 3.4 switch: a CM
// national emergency number bypasses Screening from the API path, an
// ordinary short code does not, and a country change applies to the
// next call.
func TestPlaceCall_UsesEffectiveEmergencySet(t *testing.T) {
	h := newInboundHarness(t)
	from := h.createExtension(t, "1001")
	h.register(t, from.Extension.ID)
	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "CM"))
	trunk := h.createTrunk(t, "carrier", "203.0.113.10", 30)
	h.createRoute(t, "11", trunk.ID, 1)
	h.createRoute(t, "911", trunk.ID, 1)

	place := func(dest string) telephonyports.InitiateCallCommand {
		_, err := h.svc.PlaceCall(h.ctx, ports.PlaceCallCommand{TenantID: h.tenant, ExtensionID: from.Extension.ID, Destination: dest})
		require.NoError(t, err)
		return h.calls.last()
	}

	assert.True(t, place("117").BypassScreening, "a CM national emergency number bypasses Screening")
	assert.False(t, place("911").BypassScreening, "911 is ordinary in CM")

	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "US"))
	assert.True(t, place("911").BypassScreening, "the country change applies to the next call")
	assert.False(t, place("117").BypassScreening)
}

// --- 3.5: inbound numbers ----------------------------------------------

func TestInboundNumbers_CRUD(t *testing.T) {
	h := newInboundHarness(t)
	ext := h.createExtension(t, "1001")

	created, err := h.svc.CreateInboundNumber(h.ctx, ports.CreateInboundNumberCommand{
		TenantID: h.tenant, Number: "+442071234567", ExtensionID: ext.Extension.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, "442071234567", created.Number, "the + is not significant")

	listed, err := h.svc.ListInboundNumbers(h.ctx, h.tenant)
	require.NoError(t, err)
	require.Len(t, listed, 1)

	require.NoError(t, h.svc.DeleteInboundNumber(h.ctx, h.tenant, created.ID))
	listed, err = h.svc.ListInboundNumbers(h.ctx, h.tenant)
	require.NoError(t, err)
	assert.Empty(t, listed)
}

func TestInboundNumbers_ValidationAndIsolation(t *testing.T) {
	h := newInboundHarness(t)
	ext := h.createExtension(t, "1001")
	otherID, otherCtx := h.otherTenant(t)

	_, err := h.svc.CreateInboundNumber(h.ctx, ports.CreateInboundNumberCommand{
		TenantID: h.tenant, Number: "not-a-number", ExtensionID: ext.Extension.ID,
	})
	assert.ErrorIs(t, err, ports.ErrInvalidInput, "invalid number names the field")

	foreignExt := shareddomain.NewExtensionID()
	_, err = h.svc.CreateInboundNumber(h.ctx, ports.CreateInboundNumberCommand{
		TenantID: h.tenant, Number: "442071234567", ExtensionID: foreignExt,
	})
	assert.ErrorIs(t, err, ports.ErrNotFound, "another tenant's extension is not found")

	_, err = h.svc.CreateInboundNumber(h.ctx, ports.CreateInboundNumberCommand{
		TenantID: h.tenant, Number: "442071234567", ExtensionID: ext.Extension.ID,
	})
	require.NoError(t, err)
	_, err = h.svc.CreateInboundNumber(h.ctx, ports.CreateInboundNumberCommand{
		TenantID: h.tenant, Number: "+442071234567", ExtensionID: ext.Extension.ID,
	})
	assert.ErrorIs(t, err, ports.ErrInboundNumberTaken, "a duplicate number in the tenant is refused with or without +")

	listed, err := h.svc.ListInboundNumbers(otherCtx, otherID)
	require.NoError(t, err)
	assert.Empty(t, listed, "cross-tenant invisibility")
	assert.ErrorIs(t, h.svc.DeleteInboundNumber(otherCtx, otherID, uuid.New()), ports.ErrNotFound)
}

func TestDeleteExtension_CascadesInboundNumbers(t *testing.T) {
	h := newInboundHarness(t)
	ext := h.createExtension(t, "1001")
	_, err := h.svc.CreateInboundNumber(h.ctx, ports.CreateInboundNumberCommand{
		TenantID: h.tenant, Number: "442071234567", ExtensionID: ext.Extension.ID,
	})
	require.NoError(t, err)

	require.NoError(t, h.svc.DeleteExtension(h.ctx, h.tenant, ext.Extension.ID))
	listed, err := h.svc.ListInboundNumbers(h.ctx, h.tenant)
	require.NoError(t, err)
	assert.Empty(t, listed, "the extension's numbers leave with it")
	assert.Equal(t, 1, h.countTenantRow(t,
		`SELECT count(*) FROM audit_logs WHERE action = 'inbound_number.deleted'`),
		"each cascaded number gets its audit row")
}

// --- 3.6: the router -----------------------------------------------------

func trunkSource(trunkID shareddomain.TrunkID) string {
	return domain.TrunkEndpointIdentifier(trunkID)
}

func extSource(extID shareddomain.ExtensionID) string {
	return domain.EndpointIdentifier(extID)
}

func (h *inboundHarness) route(t *testing.T, source, dialled, caller string) telephonyports.InboundDecision {
	t.Helper()
	decision, err := h.svc.ResolveInbound(context.Background(), telephonyports.InboundRequest{
		TenantID: h.tenant, CallID: shareddomain.NewCallID(),
		SourceEndpoint: source, Dialled: dialled, CallerNumber: caller,
	})
	require.NoError(t, err)
	return decision
}

func TestRouter_TrunkCallToAssignedDIDRingsExtension(t *testing.T) {
	h := newInboundHarness(t)
	ext := h.createExtension(t, "1001")
	_, err := h.svc.CreateInboundNumber(h.ctx, ports.CreateInboundNumberCommand{
		TenantID: h.tenant, Number: "442071234567", ExtensionID: ext.Extension.ID,
	})
	require.NoError(t, err)
	trunk := h.createTrunk(t, "carrier", "203.0.113.10", 30)

	decision := h.route(t, trunkSource(trunk.ID), "+442071234567", "+1555000111")
	assert.Equal(t, telephonyports.RefusalNone, decision.Refuse)
	assert.Equal(t, "PJSIP/"+extSource(ext.Extension.ID), decision.DestinationURI)
	assert.Equal(t, telephonydomain.Inbound, decision.Direction)
	assert.False(t, decision.BypassScreening)
	assert.Empty(t, decision.CallerNumber, "a carrier's caller number stands")
	assert.Empty(t, decision.CallerName)
}

func TestRouter_UnassignedDIDAndUnknownSourceAreNotFound(t *testing.T) {
	h := newInboundHarness(t)
	trunk := h.createTrunk(t, "carrier", "203.0.113.10", 30)

	decision := h.route(t, trunkSource(trunk.ID), "442071234599", "+1555000111")
	assert.Equal(t, telephonyports.RefusalNotFound, decision.Refuse)

	decision = h.route(t, "1000", "442071234599", "+1555000111")
	assert.Equal(t, telephonyports.RefusalNotFound, decision.Refuse, "an unknown source is refused")

	// A phone whose extension was deleted is an unknown source too.
	gone := shareddomain.NewExtensionID()
	decision = h.route(t, extSource(gone), "1001", extSource(gone))
	assert.Equal(t, telephonyports.RefusalNotFound, decision.Refuse, "a deleted extension's phone is refused")
}

func TestRouter_DeskPhoneDialPlan(t *testing.T) {
	h := newInboundHarness(t)
	alice := h.createExtension(t, "1001")
	bob := h.createExtension(t, "1002")
	trunk := h.createTrunk(t, "carrier", "203.0.113.10", 30)
	h.createRoute(t, "44", trunk.ID, 1)

	// A real phone presents its SIP username — the engine endpoint id —
	// as its caller number; the router attributes the call to the
	// extension behind it (phase-a-hardening task 1.2).
	phone := extSource(alice.Extension.ID)

	internal := h.route(t, phone, "1002", phone)
	assert.Equal(t, telephonyports.RefusalNone, internal.Refuse)
	assert.Equal(t, "PJSIP/"+extSource(bob.Extension.ID), internal.DestinationURI)
	assert.Equal(t, telephonydomain.Internal, internal.Direction)
	assert.Equal(t, "1001", internal.CallerNumber)
	assert.Equal(t, "Ext 1001", internal.CallerName)

	external := h.route(t, phone, "+442071234567", phone)
	assert.Equal(t, telephonyports.RefusalNone, external.Refuse)
	assert.Equal(t, telephonydomain.Outbound, external.Direction)
	assert.Contains(t, external.DestinationURI, domain.TrunkEndpointIdentifier(trunk.ID))
	assert.Equal(t, "1001", external.CallerNumber)
	assert.Equal(t, "Ext 1001", external.CallerName)

	unroutable := h.route(t, phone, "999000111222", phone)
	assert.Equal(t, telephonyports.RefusalNotFound, unroutable.Refuse)
}

func TestRouter_FullTrunkIsPassedOver(t *testing.T) {
	h := newInboundHarness(t)
	alice := h.createExtension(t, "1001")
	full := h.createTrunk(t, "full", "203.0.113.11", 1)
	spare := h.createTrunk(t, "spare", "203.0.113.12", 30)
	h.createRoute(t, "44", full.ID, 1)
	h.createRoute(t, "44", spare.ID, 2)

	// Occupy the only channel on the first trunk with a live leg: no
	// call id yet and fresh, so it counts as mid-origination, not
	// orphaned.
	legs := pbxpg.NewCallLegStore()
	require.NoError(t, h.pool.WithTenant(context.Background(), h.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return legs.Insert(ctx, tx, domain.PlacedCallLeg{
			ID: uuid.New(), TenantID: h.tenant, ExtensionID: alice.Extension.ID,
			TrunkID: &full.ID, Destination: "442071234567", CreatedAt: time.Now().UTC(),
		})
	}))

	decision := h.route(t, extSource(alice.Extension.ID), "+442071234567", "1001")
	assert.Equal(t, telephonyports.RefusalNone, decision.Refuse)
	assert.Contains(t, decision.DestinationURI, domain.TrunkEndpointIdentifier(spare.ID),
		"a full trunk is passed over for the next candidate")
}

func TestRouter_AllTrunksFullIsUnavailable(t *testing.T) {
	h := newInboundHarness(t)
	alice := h.createExtension(t, "1001")
	full := h.createTrunk(t, "full", "203.0.113.11", 1)
	h.createRoute(t, "44", full.ID, 1)

	legs := pbxpg.NewCallLegStore()
	require.NoError(t, h.pool.WithTenant(context.Background(), h.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return legs.Insert(ctx, tx, domain.PlacedCallLeg{
			ID: uuid.New(), TenantID: h.tenant, ExtensionID: alice.Extension.ID,
			TrunkID: &full.ID, Destination: "442071234567", CreatedAt: time.Now().UTC(),
		})
	}))

	decision := h.route(t, extSource(alice.Extension.ID), "+442071234567", "1001")
	assert.Equal(t, telephonyports.RefusalUnavailable, decision.Refuse,
		"an ordinary call with no free channel is a 503, never silence")
}

// TestRouter_EmergencyIgnoresTheLimit covers INV-01 on the desk-phone
// path: a CM national emergency number connects with the trunk full,
// bypasses Screening, and still reserves its channel where others see
// it. A near-emergency number is ordinary.
func TestRouter_EmergencyIgnoresTheLimit(t *testing.T) {
	h := newInboundHarness(t)
	alice := h.createExtension(t, "1001")
	full := h.createTrunk(t, "full", "203.0.113.11", 1)
	h.createRoute(t, "11", full.ID, 1)
	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "CM"))

	legs := pbxpg.NewCallLegStore()
	require.NoError(t, h.pool.WithTenant(context.Background(), h.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return legs.Insert(ctx, tx, domain.PlacedCallLeg{
			ID: uuid.New(), TenantID: h.tenant, ExtensionID: alice.Extension.ID,
			TrunkID: &full.ID, Destination: "442071234500", CreatedAt: time.Now().UTC(),
		})
	}))

	decision := h.route(t, extSource(alice.Extension.ID), "117", "1001")
	assert.Equal(t, telephonyports.RefusalNone, decision.Refuse, "emergency ignores the trunk limit")
	assert.True(t, decision.BypassScreening, "only an emergency match sets the bypass")
	assert.Equal(t, telephonydomain.Outbound, decision.Direction)
}

// TestRouter_NearEmergencyIsOrdinary covers exact matching: "1170" is
// not an emergency number in CM — it routes (or fails) as an ordinary
// destination, never with the bypass.
func TestRouter_NearEmergencyIsOrdinary(t *testing.T) {
	h := newInboundHarness(t)
	alice := h.createExtension(t, "1001")
	spare := h.createTrunk(t, "spare", "203.0.113.12", 30)
	h.createRoute(t, "11", spare.ID, 1)
	require.NoError(t, h.svc.SetEmergencyProfile(h.ctx, h.tenant, "CM"))

	ordinary := h.route(t, extSource(alice.Extension.ID), "1170", "1001")
	assert.Equal(t, telephonyports.RefusalNone, ordinary.Refuse, "1170 routes as an ordinary number")
	assert.False(t, ordinary.BypassScreening, "a near-emergency number never bypasses Screening")
}

// TestRouter_HealthWatcherOnConnect covers that a connected inbound leg
// reports trunk health like a placed call's watcher does.
func TestRouter_HealthWatcherOnConnect(t *testing.T) {
	h := newInboundHarness(t)
	alice := h.createExtension(t, "1001")
	trunk := h.createTrunk(t, "carrier", "203.0.113.10", 30)
	h.createRoute(t, "44", trunk.ID, 1)

	now := time.Now().UTC()
	h.status.answered = &now
	decision := h.route(t, extSource(alice.Extension.ID), "+442071234567", "1001")
	assert.Equal(t, telephonyports.RefusalNone, decision.Refuse)

	require.Eventually(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.health) == 1 && h.health[0] == trunk.ID
	}, 5*time.Second, 20*time.Millisecond, "an answered inbound leg reports its trunk healthy")
}
