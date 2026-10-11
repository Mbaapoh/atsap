package rpc

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/genproto/atsapbx/v1/atsapbxv1connect"
	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
	shareddomain "atsap-api/internal/shared/domain"
)

// RefreshCookie is the cookie that carries a session's refresh secret
// (D-62). It is the only place the secret ever travels.
const RefreshCookie = "atsa_refresh"

// refreshCookiePath scopes the cookie to AuthService, so the browser never
// sends the refresh secret to any other RPC.
const refreshCookiePath = "/" + atsapbxv1connect.AuthServiceName + "/"

// AuthHandler implements atsapbxv1connect.AuthServiceHandler: console
// sign-in, sessions and password recovery (design D5).
type AuthHandler struct {
	svc      *application.Service
	source   SourceResolver
	insecure bool
	now      func() time.Time

	atsapbxv1connect.UnimplementedAuthServiceHandler
}

var _ atsapbxv1connect.AuthServiceHandler = (*AuthHandler)(nil)

// NewAuthHandler returns an AuthHandler. insecureCookie drops the Secure
// attribute and exists only for the plain-HTTP dev rig
// (ATSAPBX_COOKIE_INSECURE); production cookies are always Secure.
func NewAuthHandler(svc *application.Service, source SourceResolver, insecureCookie bool) *AuthHandler {
	return &AuthHandler{svc: svc, source: source, insecure: insecureCookie, now: time.Now}
}

func (h *AuthHandler) withSource(ctx context.Context, req connect.AnyRequest) context.Context {
	return application.WithSource(ctx, h.source.Resolve(req.Peer(), req.Header()))
}

// --- discovery and sign-in --------------------------------------------------

func (h *AuthHandler) DiscoverSignIn(ctx context.Context, req *connect.Request[atsapbxv1.DiscoverSignInRequest]) (*connect.Response[atsapbxv1.DiscoverSignInResponse], error) {
	m, err := h.svc.DiscoverSignIn(ctx, req.Msg.GetEmail())
	if err != nil {
		return nil, mapErr(err)
	}
	out := &atsapbxv1.DiscoverSignInResponse{Method: atsapbxv1.SignInMethod_SIGN_IN_METHOD_PASSWORD}
	if m.Provider != nil {
		out.Method = atsapbxv1.SignInMethod_SIGN_IN_METHOD_SSO
		out.ProviderId = m.Provider.ID.String()
		out.ProviderDisplayName = m.Provider.DisplayName
	}
	return connect.NewResponse(out), nil
}

func (h *AuthHandler) SignIn(ctx context.Context, req *connect.Request[atsapbxv1.SignInRequest]) (*connect.Response[atsapbxv1.SignInResponse], error) {
	res, err := h.svc.SignIn(h.withSource(ctx, req), req.Msg.GetEmail(), req.Msg.GetPassword())
	if err != nil {
		return nil, mapErr(err)
	}
	resp := connect.NewResponse(&atsapbxv1.SignInResponse{
		Access:                 toProtoAccess(res.Access),
		Memberships:            toProtoMemberships(res.Memberships),
		PasswordChangeRequired: res.PasswordChangeOnly,
		SessionExpiresAt:       timestamppb.New(res.ExpiresAt),
	})
	h.setCookie(resp.Header(), res.RefreshSecret, res.ExpiresAt)
	return resp, nil
}

// RefreshSession is authenticated by its cookie alone. Any secret that
// cannot be used is refused identically and the cookie is cleared.
func (h *AuthHandler) RefreshSession(ctx context.Context, req *connect.Request[atsapbxv1.RefreshSessionRequest]) (*connect.Response[atsapbxv1.RefreshSessionResponse], error) {
	secret := refreshSecret(req.Header())
	var tenant *shareddomain.TenantID
	if raw := req.Msg.GetTenantId(); raw != "" {
		t, err := shareddomain.ParseTenantID(raw)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid tenant_id"))
		}
		tenant = &t
	}
	if secret == "" {
		return nil, h.unusable()
	}
	res, err := h.svc.RefreshSession(ctx, secret, tenant)
	if errors.Is(err, ports.ErrSessionNotUsable) {
		return nil, h.unusable()
	}
	if err != nil {
		return nil, mapErr(err)
	}
	resp := connect.NewResponse(&atsapbxv1.RefreshSessionResponse{
		Access:                 toProtoAccess(res.Access),
		Memberships:            toProtoMemberships(res.Memberships),
		PasswordChangeRequired: res.PasswordChangeOnly,
		SessionExpiresAt:       timestamppb.New(res.ExpiresAt),
	})
	h.setCookie(resp.Header(), res.RefreshSecret, res.ExpiresAt)
	return resp, nil
}

func (h *AuthHandler) unusable() error {
	cerr := connect.NewError(connect.CodeUnauthenticated, errors.New("session expired; sign in again"))
	h.clearCookie(cerr.Meta())
	return cerr
}

// SignOut ends the bearer token's session, and the cookie's session if a
// cookie came too, then clears the cookie.
func (h *AuthHandler) SignOut(ctx context.Context, req *connect.Request[atsapbxv1.SignOutRequest]) (*connect.Response[atsapbxv1.SignOutResponse], error) {
	tc, err := requireContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.SignOut(ctx, tc); err != nil {
		return nil, mapErr(err)
	}
	if err := h.svc.SignOutSecret(ctx, refreshSecret(req.Header())); err != nil {
		return nil, mapErr(err)
	}
	resp := connect.NewResponse(&atsapbxv1.SignOutResponse{})
	h.clearCookie(resp.Header())
	return resp, nil
}

// --- passwords ------------------------------------------------------------------

func (h *AuthHandler) ChangePassword(ctx context.Context, req *connect.Request[atsapbxv1.ChangePasswordRequest]) (*connect.Response[atsapbxv1.ChangePasswordResponse], error) {
	tc, err := requireContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.svc.ChangePassword(h.withSource(ctx, req), tc, req.Msg.GetCurrentPassword(), req.Msg.GetNewPassword()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.ChangePasswordResponse{}), nil
}

// RequestPasswordReset answers identically for every email (D-64); only a
// throttle or an internal fault changes the answer, and neither depends on
// the email.
func (h *AuthHandler) RequestPasswordReset(ctx context.Context, req *connect.Request[atsapbxv1.RequestPasswordResetRequest]) (*connect.Response[atsapbxv1.RequestPasswordResetResponse], error) {
	if err := h.svc.RequestPasswordReset(h.withSource(ctx, req), req.Msg.GetEmail()); err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.RequestPasswordResetResponse{}), nil
}

func (h *AuthHandler) CompletePasswordReset(ctx context.Context, req *connect.Request[atsapbxv1.CompletePasswordResetRequest]) (*connect.Response[atsapbxv1.CompletePasswordResetResponse], error) {
	err := h.svc.CompletePasswordReset(h.withSource(ctx, req), req.Msg.GetSecret(), req.Msg.GetNewPassword())
	if errors.Is(err, ports.ErrInvalidCredentials) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("this link is invalid or has expired"))
	}
	if err != nil {
		return nil, mapErr(err)
	}
	return connect.NewResponse(&atsapbxv1.CompletePasswordResetResponse{}), nil
}

// --- cookie and conversion helpers -------------------------------------------------

func (h *AuthHandler) setCookie(header http.Header, secret string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	c := &http.Cookie{
		Name: RefreshCookie, Value: secret, Path: refreshCookiePath,
		MaxAge: maxAge, HttpOnly: true, Secure: !h.insecure, SameSite: http.SameSiteStrictMode,
	}
	header.Add("Set-Cookie", c.String())
}

func (h *AuthHandler) clearCookie(header http.Header) {
	c := &http.Cookie{
		Name: RefreshCookie, Value: "", Path: refreshCookiePath,
		MaxAge: -1, HttpOnly: true, Secure: !h.insecure, SameSite: http.SameSiteStrictMode,
	}
	header.Add("Set-Cookie", c.String())
}

func refreshSecret(header http.Header) string {
	c, err := (&http.Request{Header: header}).Cookie(RefreshCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func toProtoAccess(t *ports.AuthToken) *atsapbxv1.AccessToken {
	if t == nil {
		return nil
	}
	return &atsapbxv1.AccessToken{
		Token: t.Token, ExpiresAt: timestamppb.New(t.ExpiresAt),
		TenantId: t.TenantID.String(), PrincipalId: t.PrincipalID.String(),
	}
}

func toProtoMemberships(ms []domain.Membership) []*atsapbxv1.Membership {
	out := make([]*atsapbxv1.Membership, len(ms))
	for i, m := range ms {
		out[i] = &atsapbxv1.Membership{
			TenantId: m.TenantID.String(), TenantName: m.TenantName,
			PrincipalId: m.PrincipalID.String(), Roles: m.Roles,
		}
	}
	return out
}

// AuthPublicProcedures are the AuthService procedures reachable without a
// bearer token: a caller needs them before it holds one, or after losing
// it. SignOut and ChangePassword are deliberately absent.
var AuthPublicProcedures = []string{
	atsapbxv1connect.AuthServiceDiscoverSignInProcedure,
	atsapbxv1connect.AuthServiceSignInProcedure,
	atsapbxv1connect.AuthServiceRefreshSessionProcedure,
	atsapbxv1connect.AuthServiceRequestPasswordResetProcedure,
	atsapbxv1connect.AuthServiceCompletePasswordResetProcedure,
}

// PasswordChangeProcedures are the only procedures a must-change token may
// call (D-64).
var PasswordChangeProcedures = []string{
	atsapbxv1connect.AuthServiceChangePasswordProcedure,
	atsapbxv1connect.AuthServiceSignOutProcedure,
}
