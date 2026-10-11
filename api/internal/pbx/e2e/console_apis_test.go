//go:build e2e

// console-apis-phase-a task 5.1: the APIs console slice A is built on,
// exercised through the running, licensed app — licence status and
// activation, user listing and role revocation, and call history.
package e2e_test

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	licensingdomain "atsap-api/internal/licensing/domain"
	shareddomain "atsap-api/internal/shared/domain"
)

// repoRoot is two directories above api/.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

// devSigner signs licence tokens with the rig's development key — the one
// `make dev-licence` created and the dev app was built to trust.
func devSigner(t *testing.T) func(channels int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".dev-licence", "ed25519.seed"))
	if err != nil {
		t.Skip("no .dev-licence key on this machine — run `make dev-licence` first")
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	priv := ed25519.NewKeyFromSeed(seed)
	return func(channels int) string {
		payload, err := json.Marshal(licensingdomain.LicenseToken{
			Edition: "development", Capacity: channels,
			InstanceID: "00000000-0000-0000-0000-0000000000de",
		})
		require.NoError(t, err)
		return licensingdomain.EncodeToken(payload, ed25519.Sign(priv, payload))
	}
}

// envToken is the token the app booted with, re-applied after the test.
func envToken(t *testing.T) string {
	t.Helper()
	f, err := os.Open(filepath.Join(repoRoot(t), ".env"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "ATSAPBX_LICENSE_TOKEN="); ok {
			return v
		}
	}
	t.Skip("no ATSAPBX_LICENSE_TOKEN in .env — run `make dev-licence` first")
	return ""
}

// platformOperator seeds a PLATFORM_ADMIN with a system-scope binding in
// the rig's tenant and returns its token: installation-wide authority.
func platformOperator(t *testing.T, r *rig) string {
	t.Helper()
	ctx := context.Background()
	id := shareddomain.NewPrincipalID()
	username := "e2e-platform-" + id.String()[:8]
	require.NoError(t, r.pool.WithTenant(ctx, r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, tenant_id, username, email, password_hash, role, status)
			VALUES ($1, $2, $3, $4, $5, 'PLATFORM_ADMIN', 'ACTIVE')`,
			id.String(), r.tenant.String(), username, username+"@e2e.invalid", fixtureHash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
			VALUES ($1, $2, 'PLATFORM_ADMIN', 'system')`, id.String(), r.tenant.String())
		return err
	}))
	return r.authenticate(t, username)
}

type licenceStatus struct {
	State    string `json:"state"`
	Edition  string `json:"edition"`
	Channels int    `json:"channels"`
}

func (r *rig) licence(t *testing.T, token string) licenceStatus {
	t.Helper()
	var out struct {
		Status licenceStatus `json:"status"`
	}
	r.call(t, "LicensingService/GetLicenseStatus", token, map[string]any{}, &out, http.StatusOK)
	return out.Status
}

func TestConsoleAPIs_Licence(t *testing.T) {
	r := newRig(t)
	sign := devSigner(t)
	original := envToken(t)
	op := platformOperator(t, r)
	t.Cleanup(func() {
		// Put the rig back exactly as the app booted, even on failure.
		r.call(t, "LicensingService/ApplyLicenseKey", op, map[string]any{"token": original}, nil, http.StatusOK)
	})

	st := r.licence(t, r.token)
	assert.Equal(t, "LICENSE_STATE_VALID", st.State, "any authenticated user reads the status")
	assert.Equal(t, 16, st.Channels)

	// A tenant administrator's authority does not reach the installation.
	body := r.call(t, "LicensingService/ApplyLicenseKey", r.token, map[string]any{"token": sign(8)}, nil, http.StatusForbidden)
	assert.Contains(t, string(body), "permission_denied")
	assert.Equal(t, 16, r.licence(t, r.token).Channels, "nothing applied")

	// A forged token is refused and changes nothing.
	forged := sign(64)
	forged = forged[:len(forged)-4] + "AAAA"
	body = r.call(t, "LicensingService/ApplyLicenseKey", op, map[string]any{"token": forged}, nil, http.StatusBadRequest)
	assert.Contains(t, string(body), "does not verify")
	assert.NotContains(t, string(body), forged, "the token is never echoed")
	assert.Equal(t, 16, r.licence(t, r.token).Channels)

	// A valid token takes effect at once, with no restart.
	var applied struct {
		Status licenceStatus `json:"status"`
	}
	r.call(t, "LicensingService/ApplyLicenseKey", op, map[string]any{"token": sign(8)}, &applied, http.StatusOK)
	assert.Equal(t, 8, applied.Status.Channels)
	assert.Equal(t, 8, r.licence(t, r.token).Channels, "every caller sees the new licence immediately")

	var audited int
	require.NoError(t, r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'licence.apply'
			AND audit_logs::text NOT LIKE '%' || $1 || '%'`, sign(8)).Scan(&audited)
	}))
	assert.Equal(t, 1, audited, "applying is audited, without the token")
}

func TestConsoleAPIs_UsersAndRoles(t *testing.T) {
	r := newRig(t)
	const pw = "e2e-supervisor-password-ok"
	var created struct {
		Principal struct {
			PrincipalID string `json:"principalId"`
		} `json:"principal"`
	}
	r.call(t, "IdentityService/ProvisionPrincipal", r.token, map[string]any{
		"tenantId": r.tenant.String(), "username": "e2e-sup", "email": "sup@e2e.invalid", "password": pw, "role": "SUPERVISOR",
	}, &created, http.StatusOK)

	var listed struct {
		Principals []struct {
			Principal struct {
				Username string `json:"username"`
			} `json:"principal"`
			RoleBindings []struct {
				Role string `json:"role"`
			} `json:"roleBindings"`
		} `json:"principals"`
	}
	raw := r.call(t, "IdentityService/ListPrincipals", r.token, map[string]any{"tenantId": r.tenant.String()}, &listed, http.StatusOK)
	require.Len(t, listed.Principals, 2, "the operator and the new supervisor, nobody else")
	assert.NotContains(t, string(raw), "argon2", "no credential material is listed")

	// The supervisor can list users (principal.read)...
	var sup struct {
		Token string `json:"token"`
	}
	r.call(t, "IdentityService/AuthenticateUser", "", map[string]any{
		"tenantId": r.tenant.String(), "username": "e2e-sup", "password": pw}, &sup, http.StatusOK)
	r.call(t, "IdentityService/ListPrincipals", sup.Token, map[string]any{"tenantId": r.tenant.String()}, nil, http.StatusOK)

	// ...until the role is revoked; the very next request feels it.
	r.call(t, "IdentityService/RevokeRole", r.token, map[string]any{
		"tenantId": r.tenant.String(), "principalId": created.Principal.PrincipalID, "role": "SUPERVISOR"}, nil, http.StatusOK)
	r.call(t, "IdentityService/ListPrincipals", sup.Token, map[string]any{"tenantId": r.tenant.String()}, nil, http.StatusForbidden)

	var audited int
	require.NoError(t, r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'principal.role.revoke'
			AND tenant_id = $1`, r.tenant.String()).Scan(&audited)
	}))
	assert.Equal(t, 1, audited, "the revocation is audited")
}

func TestConsoleAPIs_CallHistory(t *testing.T) {
	p := newPlacementRig(t, true, 4)
	callID, _ := p.place(t, p.ext, "+442071234567", http.StatusOK)
	require.Eventually(t, func() bool { return p.state(t, callID) == "Active" }, 10*time.Second, 200*time.Millisecond)
	p.hangup(t, callID)

	var out struct {
		Calls []struct {
			CallID     string `json:"callId"`
			Direction  string `json:"direction"`
			DestNumber string `json:"destNumber"`
		} `json:"calls"`
	}
	require.Eventually(t, func() bool {
		raw := p.call(t, "TelephonyService/ListCalls", p.token, map[string]any{"tenantId": p.tenant.String()}, &out, http.StatusOK)
		assert.NotContains(t, string(raw), "PJSIP/", "no engine identifier is listed")
		return len(out.Calls) > 0 && out.Calls[0].CallID == callID
	}, 5*time.Second, 200*time.Millisecond, "the placed call is first in the history")
	assert.Equal(t, "OUTBOUND", out.Calls[0].Direction)
	assert.Equal(t, "+442071234567", out.Calls[0].DestNumber)

	// Naming another tenant is refused by the interceptor's tenant match,
	// with the same INVALID_ARGUMENT GetCall gets.
	body := p.call(t, "TelephonyService/ListCalls", p.token, map[string]any{"tenantId": shareddomain.NewTenantID().String()},
		nil, http.StatusBadRequest)
	assert.Contains(t, string(body), "does not match the authenticated tenant")
}
