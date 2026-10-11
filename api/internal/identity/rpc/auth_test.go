package rpc_test

import (
	"context"
	"crypto/ed25519"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/memstore"
	"atsap-api/internal/identity/rpc"
)

// Wire tests for AuthService and the identity revision-2 additions
// (identity-accounts-and-sessions tasks 5.2, 5.3).

type authEnv struct {
	*env
	auth *rpc.AuthHandler
	mail *memstore.Outbox
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	st := memstore.New()
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	issuer, err := application.NewTokenIssuer(priv, time.Hour)
	require.NoError(t, err)
	logger := slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	mail := &memstore.Outbox{}
	svc := application.NewService(application.StoresOf(st), st, application.NewPasswordHasher(), issuer, logger).
		WithMailer(mail, "https://console.example.test")
	return &authEnv{
		env:  &env{svc: svc, h: rpc.NewIdentityHandler(svc), st: st},
		auth: rpc.NewAuthHandler(svc, rpc.SourceResolver{}, false),
		mail: mail,
	}
}

func cookieFrom(t *testing.T, h http.Header) *http.Cookie {
	t.Helper()
	cookies := (&http.Response{Header: h}).Cookies()
	require.Len(t, cookies, 1, "exactly one Set-Cookie")
	return cookies[0]
}

func withCookie[T any](msg *T, c *http.Cookie) *connect.Request[T] {
	req := connect.NewRequest(msg)
	if c != nil {
		req.Header().Set("Cookie", c.Name+"="+c.Value)
	}
	return req
}

func TestSignIn_CookieCarriesTheSecretAndNothingElseDoes(t *testing.T) {
	e := newAuthEnv(t)
	tenant := e.seedTenant(t, "Acme")
	e.seedUser(t, tenant, "ana", "correct horse battery", "AGENT", "")

	resp, err := e.auth.SignIn(context.Background(), connect.NewRequest(&atsapbxv1.SignInRequest{
		Email: "ana@example.test", Password: "correct horse battery"}))
	require.NoError(t, err)
	c := cookieFrom(t, resp.Header())
	assert.Equal(t, rpc.RefreshCookie, c.Name)
	assert.True(t, c.HttpOnly, "page scripts cannot read it")
	assert.True(t, c.Secure)
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
	assert.Equal(t, "/"+atsapbxv1connect.AuthServiceName+"/", c.Path, "sent to AuthService only")
	assert.Greater(t, c.MaxAge, 11*3600, "lives as long as the session's absolute expiry")

	body, err := protojson.Marshal(resp.Msg)
	require.NoError(t, err)
	assert.NotContains(t, string(body), c.Value, "the refresh secret never appears in a body")
	require.NotNil(t, resp.Msg.GetAccess())
	assert.Equal(t, tenant.ID.String(), resp.Msg.GetAccess().GetTenantId())
	require.Len(t, resp.Msg.GetMemberships(), 1)

	insecure := rpc.NewAuthHandler(e.svc, rpc.SourceResolver{}, true)
	resp, err = insecure.SignIn(context.Background(), connect.NewRequest(&atsapbxv1.SignInRequest{
		Email: "ana@example.test", Password: "correct horse battery"}))
	require.NoError(t, err)
	assert.False(t, cookieFrom(t, resp.Header()).Secure, "only the dev flag drops Secure")
}

func TestRefreshAndSignOut_OverTheWire(t *testing.T) {
	e := newAuthEnv(t)
	tenant := e.seedTenant(t, "Acme")
	e.seedUser(t, tenant, "ana", "correct horse battery", "AGENT", "")
	ctx := context.Background()

	signed, err := e.auth.SignIn(ctx, connect.NewRequest(&atsapbxv1.SignInRequest{Email: "ana@example.test", Password: "correct horse battery"}))
	require.NoError(t, err)
	first := cookieFrom(t, signed.Header())

	refreshed, err := e.auth.RefreshSession(ctx, withCookie(&atsapbxv1.RefreshSessionRequest{}, first))
	require.NoError(t, err)
	second := cookieFrom(t, refreshed.Header())
	assert.NotEqual(t, first.Value, second.Value, "rotated")

	_, err = e.auth.RefreshSession(ctx, withCookie(&atsapbxv1.RefreshSessionRequest{}, nil))
	requireCode(t, err, connect.CodeUnauthenticated)

	_, err = e.auth.RefreshSession(ctx, withCookie(&atsapbxv1.RefreshSessionRequest{}, first))
	requireCode(t, err, connect.CodeUnauthenticated)
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	cleared := (&http.Response{Header: cerr.Meta()}).Cookies()
	require.Len(t, cleared, 1)
	assert.Equal(t, -1, cleared[0].MaxAge, "a refused refresh clears the cookie")

	// The replay revoked the session: the rotated cookie is dead too.
	_, err = e.auth.RefreshSession(ctx, withCookie(&atsapbxv1.RefreshSessionRequest{}, second))
	requireCode(t, err, connect.CodeUnauthenticated)

	// Sign-out ends the session and clears the cookie.
	again, err := e.auth.SignIn(ctx, connect.NewRequest(&atsapbxv1.SignInRequest{Email: "ana@example.test", Password: "correct horse battery"}))
	require.NoError(t, err)
	tc, err := e.svc.ValidateToken(ctx, again.Msg.GetAccess().GetToken())
	require.NoError(t, err)
	out, err := e.auth.SignOut(application.WithTenantContext(ctx, tc), withCookie(&atsapbxv1.SignOutRequest{}, cookieFrom(t, again.Header())))
	require.NoError(t, err)
	assert.Equal(t, -1, cookieFrom(t, out.Header()).MaxAge)
	_, err = e.svc.ValidateToken(ctx, again.Msg.GetAccess().GetToken())
	assert.Error(t, err, "the access token stops working at once")
}

func TestSignIn_ThrottledIsResourceExhaustedWithDelay(t *testing.T) {
	e := newAuthEnv(t)
	tenant := e.seedTenant(t, "Acme")
	e.seedUser(t, tenant, "ana", "correct horse battery", "AGENT", "")
	try := func(email, pw string) error {
		_, err := e.auth.SignIn(context.Background(), connect.NewRequest(&atsapbxv1.SignInRequest{Email: email, Password: pw}))
		return err
	}
	for i := 0; i < 5; i++ {
		requireCode(t, try("ana@example.test", "nope-nope-nope"), connect.CodeUnauthenticated)
	}
	err := try("ana@example.test", "correct horse battery")
	requireCode(t, err, connect.CodeResourceExhausted)
	var cerr *connect.Error
	require.ErrorAs(t, err, &cerr)
	assert.Equal(t, "30", cerr.Meta().Get("Retry-After"))
	assert.Contains(t, cerr.Message(), "retry after 30 seconds")
}

func TestPasswordReset_OverTheWireAnswersAlike(t *testing.T) {
	e := newAuthEnv(t)
	tenant := e.seedTenant(t, "Acme")
	e.seedUser(t, tenant, "ana", "correct horse battery", "AGENT", "")
	ctx := context.Background()

	known, err := e.auth.RequestPasswordReset(ctx, connect.NewRequest(&atsapbxv1.RequestPasswordResetRequest{Email: "ana@example.test"}))
	require.NoError(t, err)
	unknown, err := e.auth.RequestPasswordReset(ctx, connect.NewRequest(&atsapbxv1.RequestPasswordResetRequest{Email: "ghost@example.test"}))
	require.NoError(t, err)
	assert.True(t, proto.Equal(known.Msg, unknown.Msg))

	m, ok := e.mail.Last("ana@example.test")
	require.True(t, ok)
	secret := regexp.MustCompile(`#([A-Za-z0-9_-]+)`).FindStringSubmatch(m.Body)[1]
	_, err = e.auth.CompletePasswordReset(ctx, connect.NewRequest(&atsapbxv1.CompletePasswordResetRequest{Secret: secret, NewPassword: "short"}))
	requireCode(t, err, connect.CodeInvalidArgument)
	_, err = e.auth.CompletePasswordReset(ctx, connect.NewRequest(&atsapbxv1.CompletePasswordResetRequest{Secret: secret, NewPassword: "a-good-new-password"}))
	require.NoError(t, err)
	_, err = e.auth.CompletePasswordReset(ctx, connect.NewRequest(&atsapbxv1.CompletePasswordResetRequest{Secret: secret, NewPassword: "a-good-new-password"}))
	requireCode(t, err, connect.CodeInvalidArgument)
	assert.Contains(t, err.Error(), "invalid or has expired")
}

func TestDiscoverSignIn_Wire(t *testing.T) {
	e := newAuthEnv(t)
	resp, err := e.auth.DiscoverSignIn(context.Background(), connect.NewRequest(&atsapbxv1.DiscoverSignInRequest{Email: "anyone@example.test"}))
	require.NoError(t, err)
	assert.Equal(t, atsapbxv1.SignInMethod_SIGN_IN_METHOD_PASSWORD, resp.Msg.GetMethod())
}

// --- the interceptor confines a must-change token, in every service ---------

func TestInterceptor_MustChangeTokenOnlyChangesItsPassword(t *testing.T) {
	e := newAuthEnv(t)
	tenant := e.seedTenant(t, "Acme")
	admin := e.seedUser(t, tenant, "admin", "admin-password-1", "TENANT_ADMIN", domain.ScopeTenant)
	member := e.seedUser(t, tenant, "solo", "solo-password-1", "AGENT", domain.ScopeTenant)
	_, err := e.h.ResetPrincipalPassword(admin.ctx, connect.NewRequest(&atsapbxv1.ResetPrincipalPasswordRequest{
		TenantId: tenant.ID.String(), PrincipalId: member.principal.ID.String(), TemporaryPassword: "temporary-pass-1"}))
	require.NoError(t, err)

	signed, err := e.auth.SignIn(context.Background(), connect.NewRequest(&atsapbxv1.SignInRequest{Email: "solo@example.test", Password: "temporary-pass-1"}))
	require.NoError(t, err)
	assert.True(t, signed.Msg.GetPasswordChangeRequired())
	token := signed.Msg.GetAccess().GetToken()

	i := application.NewAuthInterceptor(e.svc, rpc.TenantID).
		ExemptProcedure(application.AuthenticateUserProcedure).
		AllowWhilePasswordChange(rpc.PasswordChangeProcedures...)
	call := func(procedure string) error {
		next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&atsapbxv1.SignOutResponse{}), nil
		}
		req := &specRequest{Request: connect.NewRequest(&atsapbxv1.SignOutRequest{}), procedure: procedure}
		req.Header().Set("Authorization", "Bearer "+token)
		_, err := i.WrapUnary(next)(context.Background(), req)
		return err
	}
	requireCode(t, call(atsapbxv1connect.IdentityServiceListPrincipalsProcedure), connect.CodePermissionDenied)
	requireCode(t, call(atsapbxv1connect.TelephonyServiceListCallsProcedure), connect.CodePermissionDenied)
	assert.NoError(t, call(atsapbxv1connect.AuthServiceChangePasswordProcedure))
	assert.NoError(t, call(atsapbxv1connect.AuthServiceSignOutProcedure))
}

// specRequest overrides a request's procedure, so the interceptor can be
// driven as any service without a server.
type specRequest struct {
	*connect.Request[atsapbxv1.SignOutRequest]
	procedure string
}

func (r *specRequest) Spec() connect.Spec { return connect.Spec{Procedure: r.procedure} }

// --- identity additions -----------------------------------------------------

func TestSetUserStatus_PlatformOnly(t *testing.T) {
	e := newAuthEnv(t)
	tenant := e.seedTenant(t, "Acme")
	admin := e.seedUser(t, tenant, "admin", "admin-password-1", "TENANT_ADMIN", domain.ScopeTenant)
	op := e.seedUser(t, tenant, "op", "op-password-123", "PLATFORM_ADMIN", domain.ScopeSystem)
	target := e.seedUser(t, tenant, "solo", "solo-password-1", "AGENT", domain.ScopeTenant)
	req := func() *connect.Request[atsapbxv1.SetUserStatusRequest] {
		return connect.NewRequest(&atsapbxv1.SetUserStatusRequest{
			UserId: target.principal.UserID.String(), Status: atsapbxv1.UserStatus_USER_STATUS_DISABLED})
	}
	_, err := e.h.SetUserStatus(admin.ctx, req())
	requireCode(t, err, connect.CodePermissionDenied)
	_, err = e.h.SetUserStatus(op.ctx, req())
	require.NoError(t, err)
	_, err = e.svc.AuthenticateUser(context.Background(), tenant.ID, "solo@example.test", "solo-password-1")
	assert.Error(t, err, "the person is disabled everywhere")
}

func TestProvisionPrincipal_WireResponseRevealsNoExistingAccount(t *testing.T) {
	e := newAuthEnv(t)
	acme := e.seedTenant(t, "Acme")
	other := e.seedTenant(t, "Other")
	admin := e.seedUser(t, acme, "admin", "admin-password-1", "TENANT_ADMIN", domain.ScopeTenant)
	e.seedUser(t, other, "ana", "anas-own-password", "AGENT", domain.ScopeTenant)

	provision := func(email string) *atsapbxv1.Principal {
		resp, err := e.h.ProvisionPrincipal(admin.ctx, connect.NewRequest(&atsapbxv1.ProvisionPrincipalRequest{
			TenantId: acme.ID.String(), Email: email, DisplayName: "Someone", Password: "chosen-by-admin", Role: "AGENT"}))
		require.NoError(t, err)
		return resp.Msg.GetPrincipal()
	}
	existing, fresh := provision("ana@example.test"), provision("bob@example.test")
	norm := func(p *atsapbxv1.Principal) string {
		c := proto.Clone(p).(*atsapbxv1.Principal)
		c.PrincipalId, c.UserId, c.Email, c.Username, c.CreatedAt = "", "", "", "", nil //nolint:staticcheck // deprecated field cleared for comparison
		b, err := protojson.Marshal(c)
		require.NoError(t, err)
		return string(b)
	}
	assert.Equal(t, norm(fresh), norm(existing), "byte-identical apart from identifiers")
	assert.Equal(t, "ana@example.test", existing.GetUsername(), "the deprecated username carries the email") //nolint:staticcheck
}

func TestResponses_AuthServiceCarryNoCredentialFields(t *testing.T) {
	for _, m := range []protoreflect.Message{
		(&atsapbxv1.DiscoverSignInResponse{}).ProtoReflect(),
		(&atsapbxv1.SignInResponse{}).ProtoReflect(),
		(&atsapbxv1.RefreshSessionResponse{}).ProtoReflect(),
		(&atsapbxv1.SignOutResponse{}).ProtoReflect(),
		(&atsapbxv1.ChangePasswordResponse{}).ProtoReflect(),
		(&atsapbxv1.RequestPasswordResetResponse{}).ProtoReflect(),
		(&atsapbxv1.CompletePasswordResetResponse{}).ProtoReflect(),
		(&atsapbxv1.ResetPrincipalPasswordResponse{}).ProtoReflect(),
		(&atsapbxv1.SetUserStatusResponse{}).ProtoReflect(),
		(&atsapbxv1.ListPrincipalsResponse{}).ProtoReflect(),
	} {
		walkNoCredentialFieldsExcept(t, m.Descriptor(), "password_change_required")
	}
}

func walkNoCredentialFieldsExcept(t *testing.T, md protoreflect.MessageDescriptor, allowed string) {
	t.Helper()
	for i := 0; i < md.Fields().Len(); i++ {
		f := md.Fields().Get(i)
		name := strings.ToLower(string(f.Name()))
		if name == allowed && f.Kind() == protoreflect.BoolKind {
			continue // a flag, not material
		}
		for _, bad := range []string{"password", "secret", "key_hash", "hash", "refresh"} {
			assert.NotContains(t, name, bad, "%s.%s must not reference credential material", md.FullName(), f.Name())
		}
		if f.Kind() == protoreflect.MessageKind && !f.IsMap() {
			walkNoCredentialFieldsExcept(t, f.Message(), allowed)
		}
	}
}

func TestTenantID_CoversResetPrincipalPassword(t *testing.T) {
	id, ok := rpc.TenantID(&atsapbxv1.ResetPrincipalPasswordRequest{TenantId: "t-1"})
	assert.True(t, ok)
	assert.Equal(t, "t-1", id)
	_, ok = rpc.TenantID(&atsapbxv1.RefreshSessionRequest{TenantId: "t-2"})
	assert.False(t, ok, "a refresh is authenticated by its cookie and never matched against a token")
}

func TestSourceResolver(t *testing.T) {
	h := http.Header{}
	h.Set("X-Forwarded-For", "198.51.100.1, 10.0.0.5")
	peer := connect.Peer{Addr: "10.0.0.9:40000"}

	assert.Equal(t, "10.0.0.9", rpc.SourceResolver{}.Resolve(peer, h), "no trusted proxy: the header is ignored")

	trusted, err := rpc.ParseTrustedProxies("10.0.0.0/8")
	require.NoError(t, err)
	assert.Equal(t, "198.51.100.1", rpc.NewSourceResolver(trusted).Resolve(peer, h),
		"behind a trusted proxy: the right-most untrusted hop")

	one, err := rpc.ParseTrustedProxies("192.0.2.1")
	require.NoError(t, err)
	assert.Equal(t, []netip.Prefix{netip.MustParsePrefix("192.0.2.1/32")}, one)
	_, err = rpc.ParseTrustedProxies("not-an-address")
	assert.Error(t, err)
}
