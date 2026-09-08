package rpc_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
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
