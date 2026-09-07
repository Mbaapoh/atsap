package application_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
)

// bodyWithTenant stands in for a generated request message carrying a
// tenant_id, without importing the real protobuf types — the same
// reason the interceptor takes a TenantMatcher rather than knowing about
// a specific API surface.
type bodyWithTenant struct{ TenantId string }

func tenantMatcher(request any) (string, bool) {
	if b, ok := request.(*bodyWithTenant); ok {
		return b.TenantId, true
	}
	return "", false
}

// callUnary drives the interceptor with a fake downstream handler,
// reporting whether that handler was reached and what context it saw.
func callUnary[T any](t *testing.T, i *application.AuthInterceptor, header string, body *T) (reached bool, ctxSeen context.Context, err error) {
	t.Helper()

	next := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		reached, ctxSeen = true, ctx
		return connect.NewResponse(&bodyWithTenant{}), nil
	}

	req := connect.NewRequest(body)
	if header != "" {
		req.Header().Set("Authorization", header)
	}

	_, err = i.WrapUnary(next)(context.Background(), req)
	return reached, ctxSeen, err
}

func TestAuthInterceptor_RejectsMissingToken(t *testing.T) {
	h := newHarness(t)
	i := application.NewAuthInterceptor(h.svc, tenantMatcher)

	for _, header := range []string{"", "Bearer ", "Basic abc", "token-without-prefix"} {
		reached, _, err := callUnary(t, i, header, &bodyWithTenant{})
		require.Error(t, err)
		assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		assert.False(t, reached, "the handler must never run for an unauthenticated request")
	}
}

func TestAuthInterceptor_RejectsInvalidToken(t *testing.T) {
	h := newHarness(t)
	i := application.NewAuthInterceptor(h.svc, tenantMatcher)

	reached, _, err := callUnary(t, i, "Bearer not-a-real-token", &bodyWithTenant{})
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	assert.False(t, reached)
}

// TestAuthInterceptor_FailuresAreIndistinguishable: an expired token, a
// forged one, and a disabled account must all look the same to a caller
// (INV-10). Only the logs distinguish them.
func TestAuthInterceptor_FailuresAreIndistinguishable(t *testing.T) {
	h := newHarness(t)
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
	i := application.NewAuthInterceptor(h.svc, tenantMatcher)

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)

	_, _, forgedErr := callUnary(t, i, "Bearer not-a-real-token", &bodyWithTenant{})

	require.NoError(t, h.svc.SetPrincipalStatus(context.Background(), application.SystemActor(),
		tenant.ID, principal.ID, domain.PrincipalDisabled))
	_, _, disabledErr := callUnary(t, i, "Bearer "+tok.Token, &bodyWithTenant{})

	require.Error(t, forgedErr)
	require.Error(t, disabledErr)
	assert.Equal(t, connect.CodeOf(forgedErr), connect.CodeOf(disabledErr))
	assert.Equal(t, forgedErr.Error(), disabledErr.Error(),
		"a caller must not be able to tell a forged token from a disabled account")
}

func TestAuthInterceptor_PassesValidTokenThrough(t *testing.T) {
	h := newHarness(t)
	tenant, principal := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
	i := application.NewAuthInterceptor(h.svc, tenantMatcher)

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)

	reached, ctxSeen, err := callUnary(t, i, "Bearer "+tok.Token,
		&bodyWithTenant{TenantId: tenant.ID.String()})
	require.NoError(t, err)
	assert.True(t, reached)

	tc, ok := application.TenantContextFrom(ctxSeen)
	require.True(t, ok, "the handler must receive the authenticated identity")
	assert.Equal(t, tenant.ID, tc.TenantID)
	assert.Equal(t, principal.ID, tc.PrincipalID)
}

// TestAuthInterceptor_RejectsTenantMismatch is the body-tenant rule.
func TestAuthInterceptor_RejectsTenantMismatch(t *testing.T) {
	h := newHarness(t)
	tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
	i := application.NewAuthInterceptor(h.svc, tenantMatcher)

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)

	otherTenant, err := h.svc.ProvisionTenant(context.Background(), application.SystemActor(), "Other", "EU")
	require.NoError(t, err)

	reached, _, err := callUnary(t, i, "Bearer "+tok.Token,
		&bodyWithTenant{TenantId: otherTenant.ID.String()})
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	assert.False(t, reached, "the handler must not run for a cross-tenant request")
}

func TestAuthInterceptor_AllowsBodyWithoutTenantField(t *testing.T) {
	h := newHarness(t)
	tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
	i := application.NewAuthInterceptor(h.svc, tenantMatcher)

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)

	// A body the matcher does not recognise, and one with an empty
	// tenant, both pass the tenant check — there is nothing to compare.
	reached, _, err := callUnary(t, i, "Bearer "+tok.Token, &struct{ Other string }{})
	require.NoError(t, err)
	assert.True(t, reached)

	reached, _, err = callUnary(t, i, "Bearer "+tok.Token, &bodyWithTenant{})
	require.NoError(t, err)
	assert.True(t, reached)
}

func TestAuthInterceptor_NilMatcherSkipsTenantCheck(t *testing.T) {
	h := newHarness(t)
	tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")
	i := application.NewAuthInterceptor(h.svc, nil)

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)

	reached, _, err := callUnary(t, i, "Bearer "+tok.Token,
		&bodyWithTenant{TenantId: "some-other-tenant"})
	require.NoError(t, err, "with no matcher there is no body-tenant rule to enforce")
	assert.True(t, reached)
}

// TestTenantContextFrom_AbsentMeansUnauthenticated: a handler that finds
// no identity must treat it as no permission, not as a check to skip.
func TestTenantContextFrom_AbsentMeansUnauthenticated(t *testing.T) {
	_, ok := application.TenantContextFrom(context.Background())
	assert.False(t, ok)
}

func TestAuthInterceptor_ExpiredTokenIsRejected(t *testing.T) {
	h := newHarness(t)
	tenant, _ := h.seedTenantAndPrincipal(t, "alice", "correct horse battery")

	tok, err := h.svc.AuthenticateUser(context.Background(), tenant.ID, "alice", "correct horse battery")
	require.NoError(t, err)

	later := h.svc.WithClock(func() time.Time { return svcNow.Add(2 * application.DefaultTokenLifetime) })
	i := application.NewAuthInterceptor(later, tenantMatcher)

	reached, _, err := callUnary(t, i, "Bearer "+tok.Token, &bodyWithTenant{})
	require.Error(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
	assert.False(t, reached)
}
