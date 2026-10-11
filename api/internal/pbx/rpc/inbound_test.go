package rpc_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	identityports "atsap-api/internal/identity/ports"
	"atsap-api/internal/pbx/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// Every new method refuses an unauthenticated caller, like every old
// one: the interceptor is not the enforcement point, the handler is.
func TestInboundMethods_RequireAuthentication(t *testing.T) {
	e := newEnv(t)
	anon := context.Background()

	extID := shareddomain.NewExtensionID().String()
	calls := map[string]func() error{
		"SetEmergencyProfile": func() error {
			_, err := e.h.SetEmergencyProfile(anon, connect.NewRequest(&atsapbxv1.SetEmergencyProfileRequest{}))
			return err
		},
		"GetEmergencyProfile": func() error {
			_, err := e.h.GetEmergencyProfile(anon, connect.NewRequest(&atsapbxv1.GetEmergencyProfileRequest{}))
			return err
		},
		"CreateEmergencyNumber": func() error {
			_, err := e.h.CreateEmergencyNumber(anon, connect.NewRequest(&atsapbxv1.CreateEmergencyNumberRequest{}))
			return err
		},
		"ListEmergencyNumbers": func() error {
			_, err := e.h.ListEmergencyNumbers(anon, connect.NewRequest(&atsapbxv1.ListEmergencyNumbersRequest{}))
			return err
		},
		"DeleteEmergencyNumber": func() error {
			_, err := e.h.DeleteEmergencyNumber(anon, connect.NewRequest(&atsapbxv1.DeleteEmergencyNumberRequest{}))
			return err
		},
		"CreateInboundNumber": func() error {
			_, err := e.h.CreateInboundNumber(anon, connect.NewRequest(&atsapbxv1.CreateInboundNumberRequest{ExtensionId: extID}))
			return err
		},
		"ListInboundNumbers": func() error {
			_, err := e.h.ListInboundNumbers(anon, connect.NewRequest(&atsapbxv1.ListInboundNumbersRequest{}))
			return err
		},
		"DeleteInboundNumber": func() error {
			_, err := e.h.DeleteInboundNumber(anon, connect.NewRequest(&atsapbxv1.DeleteInboundNumberRequest{}))
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

// TestInboundErrorMapping pins the wire contract (task 4.1): each
// failure mode gets its fixed code, with the field named where the
// caller can fix it.
func TestInboundErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		call    func(e *env) error
		want    connect.Code
		wantMsg string
	}{
		{
			"invalid number names the field",
			fmt.Errorf("%w: number: %v", ports.ErrInvalidInput, errors.New(`destination must be an optional + followed by 1-32 digits: "abc"`)),
			func(e *env) error {
				_, err := e.h.CreateInboundNumber(e.ctx, connect.NewRequest(&atsapbxv1.CreateInboundNumberRequest{
					TenantId: e.tenant.String(), Number: "abc", ExtensionId: shareddomain.NewExtensionID().String(),
				}))
				return err
			},
			connect.CodeInvalidArgument, "number",
		},
		{
			"invalid country names the field",
			fmt.Errorf("%w: country_code: %v", ports.ErrInvalidInput, errors.New(`country code must be ISO 3166-1 alpha-2 upper case: "cm"`)),
			func(e *env) error {
				_, err := e.h.SetEmergencyProfile(e.ctx, connect.NewRequest(&atsapbxv1.SetEmergencyProfileRequest{
					TenantId: e.tenant.String(), CountryCode: "cm",
				}))
				return err
			},
			connect.CodeInvalidArgument, "country_code",
		},
		{
			"deleting a built-in emergency number is failed precondition",
			fmt.Errorf(`emergency number "112" is built-in and cannot be removed: %w`, ports.ErrEmergencyNumberProtected),
			func(e *env) error {
				_, err := e.h.DeleteEmergencyNumber(e.ctx, connect.NewRequest(&atsapbxv1.DeleteEmergencyNumberRequest{
					TenantId: e.tenant.String(), Number: "112",
				}))
				return err
			},
			connect.CodeFailedPrecondition, "built-in",
		},
		{
			"duplicate DID is already exists",
			fmt.Errorf(`inbound number "442071234567": %w`, ports.ErrInboundNumberTaken),
			func(e *env) error {
				_, err := e.h.CreateInboundNumber(e.ctx, connect.NewRequest(&atsapbxv1.CreateInboundNumberRequest{
					TenantId: e.tenant.String(), Number: "442071234567", ExtensionId: shareddomain.NewExtensionID().String(),
				}))
				return err
			},
			connect.CodeAlreadyExists, "already assigned",
		},
		{
			"foreign extension is not found",
			ports.ErrNotFound,
			func(e *env) error {
				_, err := e.h.CreateInboundNumber(e.ctx, connect.NewRequest(&atsapbxv1.CreateInboundNumberRequest{
					TenantId: e.tenant.String(), Number: "442071234567", ExtensionId: shareddomain.NewExtensionID().String(),
				}))
				return err
			},
			connect.CodeNotFound, "not found",
		},
		{
			"unauthorized is permission denied",
			identityports.ErrPermissionDenied,
			func(e *env) error {
				_, err := e.h.ListInboundNumbers(e.ctx, connect.NewRequest(&atsapbxv1.ListInboundNumbersRequest{
					TenantId: e.tenant.String(),
				}))
				return err
			},
			connect.CodePermissionDenied, "not permitted",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.svc.err = tc.err
			err := tc.call(e)
			require.Error(t, err)
			assert.Equal(t, tc.want, connect.CodeOf(err))
			assert.Contains(t, err.Error(), tc.wantMsg)
		})
	}
}

// TestInboundSurface_DoesNotLeakEngineVocabulary is DoD 13 for the new
// surface: every response renders clean, and a fault-injected internal
// failure naming engine objects comes back sanitized. It fails when
// internal error text is passed through — run it against any mapper
// change.
func TestInboundSurface_DoesNotLeakEngineVocabulary(t *testing.T) {
	e := newEnv(t)
	extID := shareddomain.NewExtensionID()
	e.svc.createdInbound = ports.InboundNumberView{ID: uuid.New(), Number: "442071234567", ExtensionID: extID}
	e.svc.inboundViews = []ports.InboundNumberView{e.svc.createdInbound}
	e.svc.profile = ports.EmergencyProfile{
		CountryCode: "CM", CountrySet: true, InCatalogue: true,
		Numbers: []ports.EmergencyNumberView{{Number: "112", BuiltIn: true}, {Number: "117", BuiltIn: true}},
	}
	e.svc.emergencyViews = e.svc.profile.Numbers

	createResp, err := e.h.CreateInboundNumber(e.ctx, connect.NewRequest(&atsapbxv1.CreateInboundNumberRequest{
		TenantId: e.tenant.String(), Number: "+442071234567", ExtensionId: extID.String(),
	}))
	require.NoError(t, err)
	listResp, err := e.h.ListInboundNumbers(e.ctx, connect.NewRequest(&atsapbxv1.ListInboundNumbersRequest{
		TenantId: e.tenant.String(),
	}))
	require.NoError(t, err)
	profileResp, err := e.h.GetEmergencyProfile(e.ctx, connect.NewRequest(&atsapbxv1.GetEmergencyProfileRequest{
		TenantId: e.tenant.String(),
	}))
	require.NoError(t, err)

	checked := 0
	sweep := func(name string, m interface {
		String() string
	}) {
		s := strings.ToLower(m.String())
		checked++
		for _, marker := range engineMarkers {
			assert.NotContains(t, s, marker, "%s leaks engine vocabulary %q: %q", name, marker, s)
		}
		assert.NotContains(t, s, "atsa_", "%s leaks channel state: %q", name, s)
	}
	sweep("CreateInboundNumber", createResp.Msg)
	sweep("ListInboundNumbers", listResp.Msg)
	sweep("GetEmergencyProfile", profileResp.Msg)
	assert.Positive(t, checked)

	// Fault injection: an internal failure naming engine objects must
	// come back as a bare internal, on every new method.
	e.svc.err = errors.New(`insert ps_endpoint_id_ips: duplicate key value violates constraint "ps_endpoint_id_ips_pkey"`)
	failing := map[string]func() error{
		"SetEmergencyProfile": func() error {
			_, err := e.h.SetEmergencyProfile(e.ctx, connect.NewRequest(&atsapbxv1.SetEmergencyProfileRequest{TenantId: e.tenant.String(), CountryCode: "CM"}))
			return err
		},
		"GetEmergencyProfile": func() error {
			_, err := e.h.GetEmergencyProfile(e.ctx, connect.NewRequest(&atsapbxv1.GetEmergencyProfileRequest{TenantId: e.tenant.String()}))
			return err
		},
		"CreateEmergencyNumber": func() error {
			_, err := e.h.CreateEmergencyNumber(e.ctx, connect.NewRequest(&atsapbxv1.CreateEmergencyNumberRequest{TenantId: e.tenant.String(), Number: "1500"}))
			return err
		},
		"ListEmergencyNumbers": func() error {
			_, err := e.h.ListEmergencyNumbers(e.ctx, connect.NewRequest(&atsapbxv1.ListEmergencyNumbersRequest{TenantId: e.tenant.String()}))
			return err
		},
		"DeleteEmergencyNumber": func() error {
			_, err := e.h.DeleteEmergencyNumber(e.ctx, connect.NewRequest(&atsapbxv1.DeleteEmergencyNumberRequest{TenantId: e.tenant.String(), Number: "1500"}))
			return err
		},
		"CreateInboundNumber": func() error {
			_, err := e.h.CreateInboundNumber(e.ctx, connect.NewRequest(&atsapbxv1.CreateInboundNumberRequest{TenantId: e.tenant.String(), Number: "442071234567", ExtensionId: extID.String()}))
			return err
		},
		"ListInboundNumbers": func() error {
			_, err := e.h.ListInboundNumbers(e.ctx, connect.NewRequest(&atsapbxv1.ListInboundNumbersRequest{TenantId: e.tenant.String()}))
			return err
		},
		"DeleteInboundNumber": func() error {
			_, err := e.h.DeleteInboundNumber(e.ctx, connect.NewRequest(&atsapbxv1.DeleteInboundNumberRequest{TenantId: e.tenant.String(), InboundNumberId: uuid.New().String()}))
			return err
		},
	}
	for name, call := range failing {
		t.Run("internal/"+name, func(t *testing.T) {
			err := call()
			require.Error(t, err)
			assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
			assert.NotContains(t, err.Error(), "ps_endpoint_id_ips",
				"%s passes internal error text through", name)
		})
	}
}
