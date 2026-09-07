package application

import (
	"context"
	"errors"
	"strings"

	"connectrpc.com/connect"

	"atsap-api/internal/identity/ports"
)

// authorizationHeader is where the bearer token arrives.
const authorizationHeader = "Authorization"

const bearerPrefix = "Bearer "

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

// TenantMatcher reports the tenant a request body names, if it names one
// at all. It exists so the interceptor can enforce the body-tenant vs
// token-tenant rule without importing generated protobuf types — which
// would point identity at a specific API surface it should not know
// about.
type TenantMatcher func(request any) (tenantID string, ok bool)

// AuthInterceptor authenticates every RPC and rejects a request whose
// body names a tenant other than the caller's own.
//
// NOTE: this is deliberately NOT installed in cmd/atsap-api by this
// change. LLD-02 §9 sequences the enforcement cutover last, so providers
// exist before consumers are forced to use them; wiring it here would
// break the e2e suite and the UAT rig in the same commit that
// introduces auth, making any failure ambiguous between "auth is broken"
// and "callers have no tokens yet". The cutover change installs it.
type AuthInterceptor struct {
	identity ports.IdentityService
	matcher  TenantMatcher
}

// NewAuthInterceptor returns an interceptor validating tokens through
// identity. matcher may be nil, in which case no body-tenant check runs.
func NewAuthInterceptor(identity ports.IdentityService, matcher TenantMatcher) *AuthInterceptor {
	return &AuthInterceptor{identity: identity, matcher: matcher}
}

// WrapUnary implements connect.Interceptor for unary RPCs.
func (i *AuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
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
		return connect.NewError(connect.CodeInvalidArgument,
			errors.New("tenant_id does not match the authenticated tenant"))
	}
	return nil
}
