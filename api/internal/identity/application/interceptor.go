package application

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"

	"atsap-api/internal/identity/domain"
	"atsap-api/internal/identity/ports"
)

// authorizationHeader is where the bearer token arrives.
const authorizationHeader = "Authorization"

const bearerPrefix = "Bearer "

// AuthenticateUserProcedure is the full procedure name of the one
// IdentityService method a caller may invoke before holding a token. It
// is the exemption wired via AuthInterceptor.ExemptProcedure, matched
// verbatim.
const AuthenticateUserProcedure = "/atsapbx.v1.IdentityService/AuthenticateUser"

// tenantContextKey types the context value so no other package can
// collide with it — or forge one, since the key type is unexported.
type tenantContextKey struct{}

// TenantContextFrom returns the authenticated identity attached by the
// auth interceptor, if any.
//
// Absence means the request was not authenticated: handlers must treat
// that as "no permission", never as "skip the check".
func TenantContextFrom(ctx context.Context) (*ports.TenantContext, bool) {
	tc, ok := ctx.Value(tenantContextKey{}).(*ports.TenantContext)
	return tc, ok
}

// withTenantContext attaches an authenticated identity to ctx.
func withTenantContext(ctx context.Context, tc *ports.TenantContext) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tc)
}

// WithTenantContext attaches an already-authenticated identity to ctx.
//
// It does NOT validate anything — that is the interceptor's job, and this
// helper exists so handlers and tests can carry a context the
// interceptor produced into code paths that need one. Treat it as the
// interceptor's output channel, never as a way to skip authentication.
func WithTenantContext(ctx context.Context, tc *ports.TenantContext) context.Context {
	return withTenantContext(ctx, tc)
}

// TenantMatcher reports the tenant a request body names, if it names one
// at all. It exists so the interceptor can enforce the body-tenant vs
// token-tenant rule without importing generated protobuf types — which
// would point identity at a specific API surface it should not know
// about.
type TenantMatcher func(request any) (tenantID string, ok bool)

// AuthInterceptor authenticates every RPC and rejects a request whose
// body names a tenant other than the caller's own.
//
// In this change it is installed for IdentityService only: that service
// carries mutating operations, so it is authenticated from the moment it
// exists (identity-api design, "Auth is enforced on this service now").
// TelephonyService stays mounted without it until auth-cutover-connectrpc,
// whose risk is breaking callers that exist today (the e2e suite, the UAT
// rig). Exactly one procedure — AuthenticateUser — is exempt, matched by
// its full name via ExemptProcedure.
type AuthInterceptor struct {
	identity ports.IdentityService
	matcher  TenantMatcher
	// exemptProcedure, when set, names exactly one full procedure name
	// (e.g. "/atsapbx.v1.IdentityService/AuthenticateUser") that passes
	// without a token. Matched verbatim, never by prefix or pattern: an
	// exemption that could widen is worse than none (identity-api design,
	// AuthenticateUser "exempt by necessity").
	exemptProcedure string
}

// NewAuthInterceptor returns an interceptor validating tokens through
// identity. matcher may be nil, in which case no body-tenant check runs.
func NewAuthInterceptor(identity ports.IdentityService, matcher TenantMatcher) *AuthInterceptor {
	return &AuthInterceptor{identity: identity, matcher: matcher}
}

// ExemptProcedure marks exactly one procedure as reachable without a
// token. Subsequent calls replace, never accumulate — the field holds a
// single exact name by design.
func (i *AuthInterceptor) ExemptProcedure(procedure string) *AuthInterceptor {
	i.exemptProcedure = procedure
	return i
}

// Exempt returns the currently exempt procedure name ("" when none).
func (i *AuthInterceptor) Exempt() string {
	return i.exemptProcedure
}

// isExempt reports whether procedure is the one exempt method. Exact
// string equality only: a prefix or pattern match could accidentally
// exempt a sibling method, and an exemption that can widen is worse than
// none.
func (i *AuthInterceptor) isExempt(procedure string) bool {
	return i.exemptProcedure != "" && i.exemptProcedure == procedure
}

// WrapUnary implements connect.Interceptor for unary RPCs.
func (i *AuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if i.isExempt(req.Spec().Procedure) {
			return next(ctx, req)
		}
		tc, err := i.authenticate(ctx, req.Header().Get(authorizationHeader))
		if err != nil {
			return nil, err
		}
		if err := i.checkBodyTenant(tc, req.Any()); err != nil {
			return nil, err
		}
		return next(withTenantContext(ctx, tc), req)
	}
}

// WrapStreamingClient implements connect.Interceptor. Outbound client
// streams are not authenticated here — this process is the server.
func (i *AuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

// WrapStreamingHandler authenticates inbound streaming RPCs.
//
// The body-tenant check does not apply: a stream's messages arrive after
// the handler starts, so the tenant match is the handler's own
// responsibility for each message it reads.
func (i *AuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		if i.isExempt(conn.Spec().Procedure) {
			return next(ctx, conn)
		}
		tc, err := i.authenticate(ctx, conn.RequestHeader().Get(authorizationHeader))
		if err != nil {
			return err
		}
		return next(withTenantContext(ctx, tc), conn)
	}
}

// authenticate resolves a bearer token to an identity.
//
// Every failure returns the same Unauthenticated code with the same
// message. The reason is already logged by the identity service; putting
// it in the response would tell a caller whether a token was expired,
// forged, or belonged to a disabled account (INV-10).
func (i *AuthInterceptor) authenticate(ctx context.Context, header string) (*ports.TenantContext, error) {
	token, ok := strings.CutPrefix(header, bearerPrefix)
	if !ok || token == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}

	tc, err := i.identity.ValidateToken(ctx, token)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("authentication required"))
	}
	return tc, nil
}

// checkBodyTenant enforces that a request naming a tenant names the
// caller's own.
//
// Unlike the failures above, this one is reported as InvalidArgument and
// says what happened: the caller supplied that tenant_id themselves, so
// the response reveals nothing they did not already assert. It is not a
// substitute for resource-level isolation — a request for another
// tenant's resource returns not-found via RLS regardless.
func (i *AuthInterceptor) checkBodyTenant(tc *ports.TenantContext, request any) error {
	if i.matcher == nil {
		return nil
	}
	bodyTenant, ok := i.matcher(request)
	if !ok || bodyTenant == "" {
		return nil
	}
	if bodyTenant != tc.TenantID.String() {
		// A system-scoped caller acts on tenants other than their own —
		// that is the definition of platform authority. domain.IsAuthorized
		// treats system scope as reaching every tenant, so the interceptor
		// must agree or a platform operator could create a tenant but never
		// provision its first administrator (identity-api task 3.1a).
		if tc.HasScope(domain.ScopeSystem) {
			return nil
		}
		return connect.NewError(connect.CodeInvalidArgument,
			errors.New("tenant_id does not match the authenticated tenant"))
	}
	return nil
}
