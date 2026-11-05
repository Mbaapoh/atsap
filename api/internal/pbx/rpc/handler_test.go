package rpc_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	identityapp "atsap-api/internal/identity/application"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
	"atsap-api/internal/pbx/rpc"
	shareddomain "atsap-api/internal/shared/domain"
)

// fakeService stands in for the application layer. The handler's job is
// tenant matching, translation and error mapping, so the service is
// faked to isolate exactly that.
type fakeService struct {
	err       error
	created   ports.CreatedExtension
	view      ports.ExtensionView
	views     []ports.ExtensionView
	lastPage  ports.Page
	lastTenID shareddomain.TenantID
	// Inbound-and-emergency surface (Group 4 stubs return these).
	createdInbound ports.InboundNumberView
	inboundViews   []ports.InboundNumberView
	profile        ports.EmergencyProfile
	emergencyViews []ports.EmergencyNumberView
	registrar      ports.SipRegistrar
	// Trunks (pbx-carrier-addresses wire tests).
	trunkView       ports.TrunkView
	lastCreateTrunk ports.CreateTrunkCommand
	lastUpdateTrunk ports.UpdateTrunkCommand
	trunkViews      []ports.TrunkView
	lastTrunkID     shareddomain.TrunkID
	routeView       ports.RouteView
	routeViews      []ports.RouteView
	lastCreateRoute ports.CreateRouteCommand
	lastRouteID     shareddomain.RouteID
	placed          ports.PlacedCall
	lastPlaceCall   ports.PlaceCallCommand
	lastCallID      shareddomain.CallID
	outboundPolicy  ports.OutboundPolicyView
	lastOutbound    ports.SetOutboundPolicyCommand
	lastAssign      ports.AssignExtensionCommand
	flowView        ports.FlowView
	flowViews       []ports.FlowView
	flowVersions    []ports.FlowVersionView
	holidays        map[string]string
	lastFlow        ports.CreateFlowCommand
}

func (f *fakeService) CreateFlow(_ context.Context, cmd ports.CreateFlowCommand) (ports.FlowView, error) {
	f.lastFlow = cmd
	return f.flowView, f.err
}
func (f *fakeService) GetFlow(context.Context, shareddomain.TenantID, shareddomain.FlowID) (ports.FlowView, error) {
	return f.flowView, f.err
}
func (f *fakeService) ListFlows(context.Context, shareddomain.TenantID) ([]ports.FlowView, error) {
	return f.flowViews, f.err
}
func (f *fakeService) PublishFlow(context.Context, shareddomain.TenantID, shareddomain.FlowID, domain.FlowConfig) (ports.FlowView, error) {
	return f.flowView, f.err
}
func (f *fakeService) RollbackFlow(context.Context, shareddomain.TenantID, shareddomain.FlowID, int) (ports.FlowView, error) {
	return f.flowView, f.err
}
func (f *fakeService) DeleteFlow(context.Context, shareddomain.TenantID, shareddomain.FlowID) error {
	return f.err
}
func (f *fakeService) ListFlowVersions(context.Context, shareddomain.TenantID, shareddomain.FlowID) ([]ports.FlowVersionView, error) {
	return f.flowVersions, f.err
}
func (f *fakeService) ListHolidays(context.Context, shareddomain.TenantID) (map[string]string, error) {
	return f.holidays, f.err
}
func (f *fakeService) AddHoliday(context.Context, shareddomain.TenantID, string, string) error {
	return f.err
}
func (f *fakeService) DeleteHoliday(context.Context, shareddomain.TenantID, string) error {
	return f.err
}

func (f *fakeService) AssignExtension(_ context.Context, cmd ports.AssignExtensionCommand) error {
	f.lastTenID, f.lastAssign = cmd.TenantID, cmd
	return f.err
}
func (f *fakeService) UnassignExtension(_ context.Context, tenantID shareddomain.TenantID, _ shareddomain.ExtensionID) error {
	f.lastTenID = tenantID
	return f.err
}

func (f *fakeService) GetOutboundPolicy(context.Context, shareddomain.TenantID) (ports.OutboundPolicyView, error) {
	return f.outboundPolicy, f.err
}
func (f *fakeService) SetOutboundPolicy(_ context.Context, cmd ports.SetOutboundPolicyCommand) error {
	f.lastOutbound = cmd
	return f.err
}
func (f *fakeService) ResumeOutbound(context.Context, shareddomain.TenantID) error { return f.err }

func (f *fakeService) SipRegistrar(_ context.Context, tenantID shareddomain.TenantID) (ports.SipRegistrar, error) {
	f.lastTenID = tenantID
	return f.registrar, f.err
}

func (f *fakeService) CreateExtension(_ context.Context, cmd ports.CreateExtensionCommand) (ports.CreatedExtension, error) {
	f.lastTenID = cmd.TenantID
	return f.created, f.err
}

func (f *fakeService) UpdateExtension(_ context.Context, cmd ports.UpdateExtensionCommand) error {
	f.lastTenID = cmd.TenantID
	return f.err
}

func (f *fakeService) DeleteExtension(_ context.Context, tenantID shareddomain.TenantID, _ shareddomain.ExtensionID) error {
	f.lastTenID = tenantID
	return f.err
}

func (f *fakeService) ListExtensions(_ context.Context, tenantID shareddomain.TenantID, page ports.Page) ([]ports.ExtensionView, error) {
	f.lastTenID, f.lastPage = tenantID, page
	return f.views, f.err
}

func (f *fakeService) GetExtension(_ context.Context, tenantID shareddomain.TenantID, _ shareddomain.ExtensionID) (ports.ExtensionView, error) {
	f.lastTenID = tenantID
	return f.view, f.err
}

func (f *fakeService) RegenerateSecret(_ context.Context, tenantID shareddomain.TenantID, _ shareddomain.ExtensionID) (ports.CreatedExtension, error) {
	f.lastTenID = tenantID
	return f.created, f.err
}

// Trunk and route operations are exercised in trunks_test.go through
// their own fake; these satisfy the interface for the extension tests.
func (f *fakeService) CreateTrunk(_ context.Context, cmd ports.CreateTrunkCommand) (ports.TrunkView, error) {
	f.lastCreateTrunk = cmd
	return f.trunkView, f.err
}

func (f *fakeService) UpdateTrunk(_ context.Context, cmd ports.UpdateTrunkCommand) (ports.TrunkView, error) {
	f.lastUpdateTrunk = cmd
	return f.trunkView, f.err
}

func (f *fakeService) DeleteTrunk(_ context.Context, _ shareddomain.TenantID, id shareddomain.TrunkID) error {
	f.lastTrunkID = id
	return f.err
}

func (f *fakeService) ListTrunks(_ context.Context, _ shareddomain.TenantID, page ports.Page) ([]ports.TrunkView, error) {
	f.lastPage = page
	return f.trunkViews, f.err
}

func (f *fakeService) CreateRoute(_ context.Context, cmd ports.CreateRouteCommand) (ports.RouteView, error) {
	f.lastCreateRoute = cmd
	return f.routeView, f.err
}

func (f *fakeService) DeleteRoute(_ context.Context, _ shareddomain.TenantID, id shareddomain.RouteID) error {
	f.lastRouteID = id
	return f.err
}

func (f *fakeService) PlaceCall(_ context.Context, cmd ports.PlaceCallCommand) (ports.PlacedCall, error) {
	f.lastPlaceCall = cmd
	return f.placed, f.err
}

func (f *fakeService) HangupCall(_ context.Context, _ shareddomain.TenantID, id shareddomain.CallID) error {
	f.lastCallID = id
	return f.err
}

func (f *fakeService) ListRoutes(_ context.Context, _ shareddomain.TenantID, page ports.Page) ([]ports.RouteView, error) {
	f.lastPage = page
	return f.routeViews, f.err
}

// Stubs for the inbound-and-emergency surface: the handlers land in
// Group 4, but the interface already carries them so the fake must too.
func (f *fakeService) CreateInboundNumber(context.Context, ports.CreateInboundNumberCommand) (ports.InboundNumberView, error) {
	return f.createdInbound, f.err
}

func (f *fakeService) ListInboundNumbers(context.Context, shareddomain.TenantID) ([]ports.InboundNumberView, error) {
	return f.inboundViews, f.err
}

func (f *fakeService) DeleteInboundNumber(context.Context, shareddomain.TenantID, uuid.UUID) error {
	return f.err
}

func (f *fakeService) SetEmergencyProfile(context.Context, shareddomain.TenantID, string) error {
	return f.err
}

func (f *fakeService) GetEmergencyProfile(context.Context, shareddomain.TenantID) (ports.EmergencyProfile, error) {
	return f.profile, f.err
}

func (f *fakeService) CreateEmergencyNumber(context.Context, shareddomain.TenantID, string) error {
	return f.err
}

func (f *fakeService) ListEmergencyNumbers(context.Context, shareddomain.TenantID) ([]ports.EmergencyNumberView, error) {
	return f.emergencyViews, f.err
}

func (f *fakeService) DeleteEmergencyNumber(context.Context, shareddomain.TenantID, string) error {
	return f.err
}

func sampleView() ports.ExtensionView {
	id := shareddomain.NewExtensionID()
	return ports.ExtensionView{
		ID:           id,
		Number:       "1000",
		DisplayName:  "Reception",
		AuthUsername: domain.EndpointIdentifier(id),
		DeviceType:   domain.DeviceSIP,
		Registration: domain.StatusNotRegistered,
	}
}

type env struct {
	h      *rpc.PbxHandler
	svc    *fakeService
	tenant shareddomain.TenantID
	ctx    context.Context
}

func newEnv(t *testing.T) *env {
	t.Helper()
	svc := &fakeService{view: sampleView()}
	svc.created = ports.CreatedExtension{Extension: svc.view, Secret: "GENERATEDSECRETVALUE0000000000AB"}
	tenant := shareddomain.NewTenantID()
	ctx := identityapp.WithTenantContext(context.Background(), &identityports.TenantContext{
		TenantID:    tenant,
		PrincipalID: shareddomain.NewPrincipalID(),
	})
	return &env{h: rpc.NewPbxHandler(svc), svc: svc, tenant: tenant, ctx: ctx}
}

// --- 6.2: authorization and tenancy ----------------------------------

func TestCreateExtension_Success(t *testing.T) {
	e := newEnv(t)
	resp, err := e.h.CreateExtension(e.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
		TenantId: e.tenant.String(), Number: "1000", DisplayName: "Reception",
		DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_SIP,
	}))
	require.NoError(t, err)
	assert.Equal(t, "1000", resp.Msg.GetExtension().GetNumber())
	assert.NotEmpty(t, resp.Msg.GetSecret(), "the generated secret is returned exactly once, here")
	assert.Equal(t, e.tenant, e.svc.lastTenID)
}

func TestEveryMethod_RequiresAuthentication(t *testing.T) {
	e := newEnv(t)
	anon := context.Background() // no TenantContext attached

	calls := map[string]func() error{
		"CreateExtension": func() error {
			_, err := e.h.CreateExtension(anon, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{}))
			return err
		},
		"GetExtension": func() error {
			_, err := e.h.GetExtension(anon, connect.NewRequest(&atsapbxv1.GetExtensionRequest{}))
			return err
		},
		"ListExtensions": func() error {
			_, err := e.h.ListExtensions(anon, connect.NewRequest(&atsapbxv1.ListExtensionsRequest{}))
			return err
		},
		"UpdateExtension": func() error {
			_, err := e.h.UpdateExtension(anon, connect.NewRequest(&atsapbxv1.UpdateExtensionRequest{}))
			return err
		},
		"DeleteExtension": func() error {
			_, err := e.h.DeleteExtension(anon, connect.NewRequest(&atsapbxv1.DeleteExtensionRequest{}))
			return err
		},
		"RegenerateSecret": func() error {
			_, err := e.h.RegenerateSecret(anon, connect.NewRequest(&atsapbxv1.RegenerateSecretRequest{}))
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			require.Error(t, err)
			assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err),
				"%s must refuse an unauthenticated caller", name)
		})
	}
}

// A caller naming a tenant that is not their own is refused before the
// application layer is reached — proven by the service never being
// called.
func TestCrossTenantRequest_IsRefusedBeforeReachingTheService(t *testing.T) {
	e := newEnv(t)
	other := shareddomain.NewTenantID()

	_, err := e.h.CreateExtension(e.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
		TenantId: other.String(), Number: "1000", DisplayName: "Theirs",
		DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_SIP,
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	assert.True(t, e.svc.lastTenID.IsZero(),
		"the service must never see a request for another tenant")
}

// An extension in another tenant and one that exists nowhere must be
// indistinguishable to the caller (INV-10) — same code, same message.
func TestNotFound_IsIdenticalForForeignAndNonexistent(t *testing.T) {
	e := newEnv(t)
	e.svc.err = ports.ErrNotFound

	_, err1 := e.h.GetExtension(e.ctx, connect.NewRequest(&atsapbxv1.GetExtensionRequest{
		TenantId: e.tenant.String(), ExtensionId: shareddomain.NewExtensionID().String(),
	}))
	_, err2 := e.h.GetExtension(e.ctx, connect.NewRequest(&atsapbxv1.GetExtensionRequest{
		TenantId: e.tenant.String(), ExtensionId: shareddomain.NewExtensionID().String(),
	}))

	require.Error(t, err1)
	require.Error(t, err2)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err1))
	assert.Equal(t, err1.Error(), err2.Error(),
		"a foreign extension and a nonexistent one must produce byte-identical errors")
}

func TestErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want connect.Code
	}{
		{"permission denied", identityports.ErrPermissionDenied, connect.CodePermissionDenied},
		{"not found", ports.ErrNotFound, connect.CodeNotFound},
		{"duplicate number", ports.ErrNumberTaken, connect.CodeAlreadyExists},
		{"invalid input", ports.ErrInvalidInput, connect.CodeInvalidArgument},
		{"entitlement exceeded", fmt.Errorf("extension cap 10 reached: %w", ports.ErrEntitlementExceeded), connect.CodeFailedPrecondition},
		{"not licensed", fmt.Errorf("read extension cap: %w", ports.ErrNotLicensed), connect.CodeFailedPrecondition},
		{"unexpected", errors.New("connection reset by peer"), connect.CodeInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.svc.err = tc.err
			_, err := e.h.CreateExtension(e.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
				TenantId: e.tenant.String(), Number: "1000", DisplayName: "X",
				DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_SIP,
			}))
			require.Error(t, err)
			assert.Equal(t, tc.want, connect.CodeOf(err))
		})
	}
}

// An internal failure must not leak the underlying error text, which can
// name a table, a constraint, or an engine object.
func TestInternalError_DoesNotLeakTheUnderlyingMessage(t *testing.T) {
	e := newEnv(t)
	e.svc.err = errors.New(`insert ps_endpoints: duplicate key value violates constraint "ps_endpoints_pkey"`)

	_, err := e.h.CreateExtension(e.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
		TenantId: e.tenant.String(), Number: "1000", DisplayName: "X",
		DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_SIP,
	}))
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "ps_endpoints")
	assert.NotContains(t, err.Error(), "constraint")
}

func TestListExtensions_BoundsThePage(t *testing.T) {
	e := newEnv(t)

	_, err := e.h.ListExtensions(e.ctx, connect.NewRequest(&atsapbxv1.ListExtensionsRequest{
		TenantId: e.tenant.String(), Limit: 0,
	}))
	require.NoError(t, err)
	assert.Equal(t, 50, e.svc.lastPage.Limit, "an unset limit takes the default")

	_, err = e.h.ListExtensions(e.ctx, connect.NewRequest(&atsapbxv1.ListExtensionsRequest{
		TenantId: e.tenant.String(), Limit: 100000,
	}))
	require.NoError(t, err)
	assert.Equal(t, 200, e.svc.lastPage.Limit, "one call must not be able to demand every extension")

	_, err = e.h.ListExtensions(e.ctx, connect.NewRequest(&atsapbxv1.ListExtensionsRequest{
		TenantId: e.tenant.String(), Offset: -5,
	}))
	require.NoError(t, err)
	assert.Zero(t, e.svc.lastPage.Offset, "a negative offset is clamped, not passed to SQL")
}

// --- 6.3: no engine identifier crosses the wire ----------------------

// engineMarkers are the things that must never appear in a response: the
// projected identifier prefix, the engine's table names, its dialplan
// context, and its transport names.
var engineMarkers = []string{
	"e_", "ps_endpoints", "ps_auths", "ps_aors", "ps_contacts",
	"stasis-in", "transport-udp", "transport-wss", "pjsip",
}

// walkStrings visits every string field of a proto message, however
// nested, so the assertion covers the whole response rather than the
// fields someone remembered to check.
func walkStrings(m proto.Message, visit func(path, value string)) {
	var walk func(prefix string, msg protoreflect.Message)
	walk = func(prefix string, msg protoreflect.Message) {
		msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			name := prefix + string(fd.Name())
			switch {
			case fd.IsList():
				list := v.List()
				for i := range list.Len() {
					if fd.Kind() == protoreflect.MessageKind {
						walk(name+".", list.Get(i).Message())
					} else if fd.Kind() == protoreflect.StringKind {
						visit(name, list.Get(i).String())
					}
				}
			case fd.Kind() == protoreflect.MessageKind:
				walk(name+".", v.Message())
			case fd.Kind() == protoreflect.StringKind:
				visit(name, v.String())
			}
			return true
		})
	}
	walk("", m.ProtoReflect())
}

// TestNoEngineIdentifierCrossesTheWire is task 6.3, and the reason
// auth_username is the one field that needs thinking about: it IS the
// projected identifier, deliberately (design D2), because Asterisk
// matches a REGISTER by From-user against the endpoint id. It is
// provisioning material returned like the secret, not a resource
// identifier — so it is excluded from this sweep by name, and every
// other field must be clean.
func TestNoEngineIdentifierCrossesTheWire(t *testing.T) {
	e := newEnv(t)

	resp, err := e.h.CreateExtension(e.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
		TenantId: e.tenant.String(), Number: "1000", DisplayName: "Reception",
		DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_SIP,
	}))
	require.NoError(t, err)

	checked := 0
	walkStrings(resp.Msg, func(path, value string) {
		if strings.HasSuffix(path, "auth_username") {
			return // the SIP credential, by design — see the doc comment
		}
		checked++
		for _, marker := range engineMarkers {
			assert.NotContains(t, strings.ToLower(value), marker,
				"field %q leaks engine vocabulary %q: %q", path, marker, value)
		}
	})
	assert.Positive(t, checked, "the sweep must actually have inspected fields")
}

func TestListResponse_CarriesNoEngineVocabulary(t *testing.T) {
	e := newEnv(t)
	e.svc.views = []ports.ExtensionView{sampleView(), sampleView()}

	resp, err := e.h.ListExtensions(e.ctx, connect.NewRequest(&atsapbxv1.ListExtensionsRequest{
		TenantId: e.tenant.String(),
	}))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetExtensions(), 2)

	walkStrings(resp.Msg, func(path, value string) {
		if strings.HasSuffix(path, "auth_username") {
			return
		}
		for _, marker := range engineMarkers {
			assert.NotContains(t, strings.ToLower(value), marker,
				"field %q leaks engine vocabulary %q", path, marker)
		}
	})
}

// An unspecified device type must reach the domain as invalid rather
// than being silently defaulted here — a caller who omits it should be
// told, not quietly given WebRTC.
func TestUnspecifiedDeviceType_IsNotDefaultedSilently(t *testing.T) {
	e := newEnv(t)
	e.svc.err = ports.ErrInvalidInput

	_, err := e.h.CreateExtension(e.ctx, connect.NewRequest(&atsapbxv1.CreateExtensionRequest{
		TenantId: e.tenant.String(), Number: "1000", DisplayName: "X",
		DeviceType: atsapbxv1.DeviceType_DEVICE_TYPE_UNSPECIFIED,
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

// --- structural guarantees, asserted rather than assumed --------------
//
// Three spec scenarios are satisfied by the shape of the API rather than
// by logic: a caller cannot supply a secret, cannot write registration
// state, and cannot read a secret back. "Impossible by construction" is
// only true while the construction holds, so these assert the shape.

func fieldNames(m proto.Message) []string {
	fields := m.ProtoReflect().Descriptor().Fields()
	names := make([]string, 0, fields.Len())
	for i := range fields.Len() {
		names = append(names, string(fields.Get(i).Name()))
	}
	return names
}

// "A caller-supplied secret is not honoured": the request has nowhere to
// put one. The platform generates it, because a chosen secret would be
// the weak link in a digest that is offline-crackable.
func TestRequests_CannotCarryASecret(t *testing.T) {
	for _, m := range []proto.Message{
		&atsapbxv1.CreateExtensionRequest{},
		&atsapbxv1.UpdateExtensionRequest{},
		&atsapbxv1.RegenerateSecretRequest{},
	} {
		names := fieldNames(m)
		for _, n := range names {
			assert.NotContains(t, strings.ToLower(n), "secret",
				"%T must have no secret field — the platform generates credentials, callers never supply them",
				m)
			assert.NotContains(t, strings.ToLower(n), "password", "%T must have no password field", m)
			assert.NotContains(t, strings.ToLower(n), "digest", "%T must have no digest field", m)
		}
	}
}

// "Registration state cannot be written": no request carries it. It
// describes what a device is doing, and is observed, never set.
func TestRequests_CannotSetRegistrationState(t *testing.T) {
	for _, m := range []proto.Message{
		&atsapbxv1.CreateExtensionRequest{},
		&atsapbxv1.UpdateExtensionRequest{},
	} {
		for _, n := range fieldNames(m) {
			assert.NotContains(t, strings.ToLower(n), "registration",
				"%T must not let a caller assert registration state", m)
		}
	}
}

// "Secret is returned once at creation": only the two responses that mint
// one carry it. A read path that could return a secret would make "shown
// once" a convention rather than a guarantee.
func TestOnlyMintingResponsesCarryTheSecret(t *testing.T) {
	carries := func(m proto.Message) bool {
		for _, n := range fieldNames(m) {
			if strings.Contains(strings.ToLower(n), "secret") {
				return true
			}
		}
		return false
	}

	assert.True(t, carries(&atsapbxv1.CreateExtensionResponse{}), "creation returns the generated secret")
	assert.True(t, carries(&atsapbxv1.RegenerateSecretResponse{}), "rotation returns the new secret")

	for _, m := range []proto.Message{
		&atsapbxv1.GetExtensionResponse{},
		&atsapbxv1.ListExtensionsResponse{},
		&atsapbxv1.UpdateExtensionResponse{},
		&atsapbxv1.Extension{},
	} {
		assert.False(t, carries(m), "%T must not carry a secret: it is shown once, at minting, and never read back", m)
	}
}

// --- pbx-sip-registrar-settings 2.2 -----------------------------------

func TestGetSipRegistrar_MapsEveryField(t *testing.T) {
	e := newEnv(t)
	e.svc.registrar = ports.SipRegistrar{Configured: true, Host: "pbx.example.com", Port: 5080, Transport: "tls", WebRTCURL: "wss://pbx.example.com/ws"}
	resp, err := e.h.GetSipRegistrar(e.ctx, connect.NewRequest(&atsapbxv1.GetSipRegistrarRequest{TenantId: e.tenant.String()}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetConfigured())
	assert.Equal(t, "pbx.example.com", resp.Msg.GetHost())
	assert.EqualValues(t, 5080, resp.Msg.GetPort())
	assert.Equal(t, "tls", resp.Msg.GetTransport())
	assert.Equal(t, "wss://pbx.example.com/ws", resp.Msg.GetWebrtcUrl())
	assert.Equal(t, e.tenant, e.svc.lastTenID)
}

func TestGetSipRegistrar_NotConfigured(t *testing.T) {
	e := newEnv(t)
	resp, err := e.h.GetSipRegistrar(e.ctx, connect.NewRequest(&atsapbxv1.GetSipRegistrarRequest{}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetConfigured())
	assert.Empty(t, resp.Msg.GetHost())
	assert.Equal(t, e.tenant, e.svc.lastTenID, "an omitted tenant is the caller's own")
}

func TestGetSipRegistrar_RefusesAnotherTenantAndUnauthenticated(t *testing.T) {
	e := newEnv(t)
	_, err := e.h.GetSipRegistrar(e.ctx, connect.NewRequest(&atsapbxv1.GetSipRegistrarRequest{TenantId: shareddomain.NewTenantID().String()}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	_, err = e.h.GetSipRegistrar(context.Background(), connect.NewRequest(&atsapbxv1.GetSipRegistrarRequest{}))
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
}

func TestGetSipRegistrar_PermissionRefusalMaps(t *testing.T) {
	e := newEnv(t)
	e.svc.err = identityports.ErrPermissionDenied
	_, err := e.h.GetSipRegistrar(e.ctx, connect.NewRequest(&atsapbxv1.GetSipRegistrarRequest{}))
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
}

func TestGetSipRegistrar_ReportsICEServers(t *testing.T) {
	e := newEnv(t)
	e.svc.registrar = ports.SipRegistrar{
		Configured: true, Host: "pbx.example.test", Port: 5060, Transport: "udp",
		ICEServers: []ports.ICEServer{{URLs: []string{"turn:turn.example.test:3478"}, Username: "browser", Credential: "secret"}},
	}
	got, err := e.h.GetSipRegistrar(e.ctx, connect.NewRequest(&atsapbxv1.GetSipRegistrarRequest{TenantId: e.tenant.String()}))
	require.NoError(t, err)
	require.Len(t, got.Msg.GetIceServers(), 1)
	assert.Equal(t, []string{"turn:turn.example.test:3478"}, got.Msg.GetIceServers()[0].GetUrls())
	assert.Equal(t, "browser", got.Msg.GetIceServers()[0].GetUsername())
}

func TestOutboundPolicy_RoundTrip(t *testing.T) {
	e := newEnv(t)
	e.svc.outboundPolicy = ports.OutboundPolicyView{
		DailySpendCapMicros: 500, BlockedPrefixes: []string{"+1900"},
		AnomalyCallThreshold: 20, AnomalyWindow: 10 * time.Minute,
		BusinessStartMinute: 480, BusinessEndMinute: 1080,
	}
	got, err := e.h.GetOutboundPolicy(e.ctx, connect.NewRequest(&atsapbxv1.GetOutboundPolicyRequest{TenantId: e.tenant.String()}))
	require.NoError(t, err)
	assert.Equal(t, int64(500), got.Msg.GetPolicy().GetDailySpendCapMicros())
	assert.Equal(t, []string{"+1900"}, got.Msg.GetPolicy().GetBlockedPrefixes())

	_, err = e.h.UpdateOutboundPolicy(e.ctx, connect.NewRequest(&atsapbxv1.UpdateOutboundPolicyRequest{
		TenantId: e.tenant.String(), DailySpendCapMicros: 9000, MonthlySpendCapMicros: 10000,
		BlockedPrefixes: []string{"+239"}, AnomalyCallThreshold: 5, AnomalyWindowSeconds: 300,
		BusinessStartMinute: 60, BusinessEndMinute: 120,
	}))
	require.NoError(t, err)
	assert.Equal(t, int64(9000), e.svc.lastOutbound.Policy.DailySpendCapMicros)
	assert.Equal(t, 5*time.Minute, e.svc.lastOutbound.Policy.AnomalyWindow)

	_, err = e.h.ResumeOutbound(e.ctx, connect.NewRequest(&atsapbxv1.ResumeOutboundRequest{TenantId: e.tenant.String()}))
	require.NoError(t, err)
}

func TestAssignExtension_Wire(t *testing.T) {
	e := newEnv(t)
	extID, principalID := shareddomain.NewExtensionID(), shareddomain.NewPrincipalID()
	e.svc.view = ports.ExtensionView{ID: extID, Number: "1001"}
	_, err := e.h.AssignExtension(e.ctx, connect.NewRequest(&atsapbxv1.AssignExtensionRequest{
		TenantId: e.tenant.String(), ExtensionId: extID.String(), PrincipalId: principalID.String(),
	}))
	require.NoError(t, err)
	assert.Equal(t, principalID, e.svc.lastAssign.PrincipalID)

	_, err = e.h.UnassignExtension(e.ctx, connect.NewRequest(&atsapbxv1.UnassignExtensionRequest{
		TenantId: e.tenant.String(), ExtensionId: extID.String(),
	}))
	require.NoError(t, err)

	_, err = e.h.AssignExtension(e.ctx, connect.NewRequest(&atsapbxv1.AssignExtensionRequest{
		TenantId: e.tenant.String(), ExtensionId: extID.String(), PrincipalId: "not-a-uuid",
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestFlows_Wire(t *testing.T) {
	e := newEnv(t)
	flowID, extID := shareddomain.NewFlowID(), shareddomain.NewExtensionID()
	e.svc.flowView = ports.FlowView{ID: flowID, Number: "4420", Name: "Main", ActiveVersion: 2,
		Config: domain.FlowConfig{OpenExtensionID: &extID, BusinessStartMinute: 480, BusinessEndMinute: 1080}}
	e.svc.flowViews = []ports.FlowView{e.svc.flowView}
	e.svc.flowVersions = []ports.FlowVersionView{{Version: 2, Config: e.svc.flowView.Config, PublishedAt: time.Now(), PublishedBy: shareddomain.NewPrincipalID()}}
	e.svc.holidays = map[string]string{"2026-12-25": "Christmas"}

	created, err := e.h.CreateFlow(e.ctx, connect.NewRequest(&atsapbxv1.CreateFlowRequest{
		TenantId: e.tenant.String(), Number: "4420", Name: "Main",
		Config: &atsapbxv1.FlowConfig{OpenExtensionId: extID.String(), BusinessStartMinute: 480, BusinessEndMinute: 1080},
	}))
	require.NoError(t, err)
	assert.Equal(t, "4420", created.Msg.GetFlow().GetNumber())
	assert.Equal(t, extID.String(), e.svc.lastFlow.Config.OpenExtensionID.String())

	got, err := e.h.GetFlow(e.ctx, connect.NewRequest(&atsapbxv1.GetFlowRequest{TenantId: e.tenant.String(), FlowId: flowID.String()}))
	require.NoError(t, err)
	assert.Equal(t, int32(2), got.Msg.GetFlow().GetActiveVersion())

	listed, err := e.h.ListFlows(e.ctx, connect.NewRequest(&atsapbxv1.ListFlowsRequest{TenantId: e.tenant.String()}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetFlows(), 1)

	_, err = e.h.PublishFlow(e.ctx, connect.NewRequest(&atsapbxv1.PublishFlowRequest{
		TenantId: e.tenant.String(), FlowId: flowID.String(), Config: &atsapbxv1.FlowConfig{OpenExtensionId: extID.String()},
	}))
	require.NoError(t, err)

	_, err = e.h.RollbackFlow(e.ctx, connect.NewRequest(&atsapbxv1.RollbackFlowRequest{TenantId: e.tenant.String(), FlowId: flowID.String(), Version: 1}))
	require.NoError(t, err)

	versions, err := e.h.ListFlowVersions(e.ctx, connect.NewRequest(&atsapbxv1.ListFlowVersionsRequest{TenantId: e.tenant.String(), FlowId: flowID.String()}))
	require.NoError(t, err)
	require.Len(t, versions.Msg.GetVersions(), 1)

	holidays, err := e.h.ListHolidays(e.ctx, connect.NewRequest(&atsapbxv1.ListHolidaysRequest{TenantId: e.tenant.String()}))
	require.NoError(t, err)
	require.Len(t, holidays.Msg.GetHolidays(), 1)
	_, err = e.h.AddHoliday(e.ctx, connect.NewRequest(&atsapbxv1.AddHolidayRequest{TenantId: e.tenant.String(), Day: "2026-12-25", Name: "Christmas"}))
	require.NoError(t, err)
	_, err = e.h.DeleteHoliday(e.ctx, connect.NewRequest(&atsapbxv1.DeleteHolidayRequest{TenantId: e.tenant.String(), Day: "2026-12-25"}))
	require.NoError(t, err)

	_, err = e.h.DeleteFlow(e.ctx, connect.NewRequest(&atsapbxv1.DeleteFlowRequest{TenantId: e.tenant.String(), FlowId: flowID.String()}))
	require.NoError(t, err)

	_, err = e.h.GetFlow(e.ctx, connect.NewRequest(&atsapbxv1.GetFlowRequest{TenantId: e.tenant.String(), FlowId: "bad"}))
	require.Error(t, err)
	_, err = e.h.CreateFlow(e.ctx, connect.NewRequest(&atsapbxv1.CreateFlowRequest{TenantId: e.tenant.String(), Config: &atsapbxv1.FlowConfig{OpenExtensionId: "bad"}}))
	require.Error(t, err)
}
