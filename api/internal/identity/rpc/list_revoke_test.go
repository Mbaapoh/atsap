package rpc_test

import (
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	atsapbxv1 "atsap-api/internal/genproto/atsapbx/v1"
	"atsap-api/internal/identity/domain"
)

func TestListPrincipals(t *testing.T) {
	e := newEnv(t)
	acme := e.seedTenant(t, "Acme")
	other := e.seedTenant(t, "Other")
	admin := e.seedUser(t, acme, "admin", "correct-pw-a-battery", "TENANT_ADMIN", domain.ScopeTenant)
	e.seedUser(t, acme, "agent", "correct-pw-b-battery", "AGENT", domain.ScopeTenant)
	e.seedUser(t, other, "stranger", "correct-pw-c-battery", "AGENT", domain.ScopeTenant)

	res, err := e.h.ListPrincipals(admin.ctx, connect.NewRequest(&atsapbxv1.ListPrincipalsRequest{TenantId: acme.ID.String()}))
	require.NoError(t, err)
	var names []string
	for _, p := range res.Msg.GetPrincipals() {
		names = append(names, p.GetPrincipal().GetUsername())
		require.NotEmpty(t, p.GetRoleBindings(), "%s is listed with its roles", p.GetPrincipal().GetUsername())
	}
	assert.Equal(t, []string{"admin", "agent"}, names, "the tenant's users, by username, and no other tenant's")

	b, _ := protojson.Marshal(res.Msg)
	assert.NotContains(t, string(b), "argon2", "no password hash in any field")
	assert.NotContains(t, strings.ToLower(string(b)), "password")

	agent := e.seedUser(t, acme, "agent2", "correct-pw-d-battery", "AGENT", domain.ScopeTenant)
	_, err = e.h.ListPrincipals(agent.ctx, connect.NewRequest(&atsapbxv1.ListPrincipalsRequest{TenantId: acme.ID.String()}))
	requireCode(t, err, connect.CodePermissionDenied)
}

func TestRevokeRole(t *testing.T) {
	e := newEnv(t)
	acme := e.seedTenant(t, "Acme")
	admin := e.seedUser(t, acme, "admin", "correct-pw-a-battery", "TENANT_ADMIN", domain.ScopeTenant)
	deputy := e.seedUser(t, acme, "deputy", "correct-pw-b-battery", "TENANT_ADMIN", domain.ScopeTenant)
	revoke := func(u user, target user, role, scope string) error {
		_, err := e.h.RevokeRole(u.ctx, connect.NewRequest(&atsapbxv1.RevokeRoleRequest{
			TenantId: acme.ID.String(), PrincipalId: target.principal.ID.String(), Role: role, Scope: scope}))
		return err
	}

	// A revoked role stops authorizing: the deputy can list users before,
	// not after.
	_, err := e.h.ListPrincipals(deputy.ctx, connect.NewRequest(&atsapbxv1.ListPrincipalsRequest{TenantId: acme.ID.String()}))
	require.NoError(t, err)
	require.NoError(t, revoke(admin, deputy, "TENANT_ADMIN", ""))
	_, err = e.h.ListPrincipals(deputy.ctx, connect.NewRequest(&atsapbxv1.ListPrincipalsRequest{TenantId: acme.ID.String()}))
	requireCode(t, err, connect.CodePermissionDenied)

	// The last tenant administrator cannot be revoked — by anyone, itself
	// included.
	requireCode(t, revoke(admin, admin, "TENANT_ADMIN", ""), connect.CodeFailedPrecondition)
	_, err = e.h.ListPrincipals(admin.ctx, connect.NewRequest(&atsapbxv1.ListPrincipalsRequest{TenantId: acme.ID.String()}))
	require.NoError(t, err, "the refused revocation changed nothing")

	// Beyond a tenant admin's authority: platform role and system scope.
	op := e.seedUser(t, acme, "op", "correct-pw-op-battery", "PLATFORM_ADMIN", domain.ScopeSystem)
	requireCode(t, revoke(admin, op, "PLATFORM_ADMIN", domain.ScopeSystem), connect.CodePermissionDenied)

	// A binding that does not exist reads as not found.
	requireCode(t, revoke(admin, deputy, "SUPERVISOR", ""), connect.CodeNotFound)
}
