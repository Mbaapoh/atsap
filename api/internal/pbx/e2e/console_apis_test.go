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
	"fmt"
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
	sign := devTokenSigner(t)
	return func(channels int) string {
		return sign(licensingdomain.LicenseToken{Edition: "development", Capacity: channels})
	}
}

// devTokenSigner signs any licence token with the rig's development key,
// for tests that need a particular edition or caps.
func devTokenSigner(t *testing.T) func(licensingdomain.LicenseToken) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".dev-licence", "ed25519.seed"))
	if err != nil {
		t.Skip("no .dev-licence key on this machine — run `make dev-licence` first")
	}
	seed, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	priv := ed25519.NewKeyFromSeed(seed)
	return func(tok licensingdomain.LicenseToken) string {
		tok.InstanceID = "00000000-0000-0000-0000-0000000000de"
		payload, err := json.Marshal(tok)
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
	userID := seedFixtureUser(t, r.pool, username+"@e2e.invalid", fixtureHash)
	t.Cleanup(func() { forgetFixtureUser(r.pool, r.tenant, username+"@e2e.invalid") })
	require.NoError(t, r.pool.WithTenant(ctx, r.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, tenant_id, user_id, role, status)
			VALUES ($1, $2, $3, 'PLATFORM_ADMIN', 'ACTIVE')`,
			id.String(), r.tenant.String(), userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
			VALUES ($1, $2, 'PLATFORM_ADMIN', 'system')`, id.String(), r.tenant.String())
		return err
	}))
	return r.authenticate(t, username)
}

type licenceStatus struct {
	State         string `json:"state"`
	Edition       string `json:"edition"`
	Channels      int    `json:"channels"`
	ChannelsInUse int    `json:"channelsInUse"`
	Condition     string `json:"condition"`
	Tier          string `json:"tier"`
	Capacity      string `json:"capacity"`
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
	supEmail := fmt.Sprintf("sup-%d@e2e.invalid", time.Now().UnixNano())
	t.Cleanup(func() { forgetFixtureUser(r.pool, r.tenant, supEmail) })
	var created struct {
		Principal struct {
			PrincipalID string `json:"principalId"`
		} `json:"principal"`
	}
	r.call(t, "IdentityService/ProvisionPrincipal", r.token, map[string]any{
		"tenantId": r.tenant.String(), "email": supEmail, "password": pw, "role": "SUPERVISOR",
	}, &created, http.StatusOK)

	var listed struct {
		Principals []struct {
			Principal struct {
				Email string `json:"email"`
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
		"tenantId": r.tenant.String(), "username": supEmail, "password": pw}, &sup, http.StatusOK)
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

// TestConsoleAPIs_LicenceConditions is licensing-status-conditions task
// 4.1: the server reports the PRD condition, tier and capacity level of
// the licence in force, against the running app. A Free Community
// licence says so; on a 5-channel licence, holding 3, 4 and 5 ringing
// calls reads Normal, Warning and Reached. The original licence is put
// back in cleanup, even on failure.
func TestConsoleAPIs_LicenceConditions(t *testing.T) {
	sign := devTokenSigner(t)
	original := envToken(t)
	p := newPlacementRig(t, true, 64)
	op := platformOperator(t, p.rig)
	apply := func(token string) {
		t.Helper()
		p.call(t, "LicensingService/ApplyLicenseKey", op, map[string]any{"token": token}, nil, http.StatusOK)
	}
	t.Cleanup(func() { apply(original) })

	t.Run("a Free Community licence says so", func(t *testing.T) {
		apply(sign(licensingdomain.LicenseToken{Edition: licensingdomain.FreeEdition, Capacity: licensingdomain.FreeChannels,
			MaxExtensions: licensingdomain.FreeMaxExtensions, MaxTenants: licensingdomain.FreeMaxTenants}))
		st := p.licence(t, p.token)
		assert.Equal(t, "LICENSE_STATE_VALID", st.State, "the entitlement state is unchanged")
		assert.Equal(t, "LICENSE_CONDITION_FREE_COMMUNITY", st.Condition)
		assert.Equal(t, "LICENSE_TIER_FREE_COMMUNITY", st.Tier)
		assert.Equal(t, "CAPACITY_LEVEL_NORMAL", st.Capacity)
	})

	t.Run("usage crosses 80% then 100% on a 5-channel licence", func(t *testing.T) {
		apply(sign(licensingdomain.LicenseToken{Edition: "development", Capacity: 5}))
		require.Zero(t, p.licence(t, p.token).ChannelsInUse, "the rig starts with no call held")

		p.phone.ringOnly.Store(true)
		p.carrier.ringOnly.Store(true)
		var held []string
		t.Cleanup(func() {
			for _, id := range held {
				p.call(t, "PbxService/HangupCall", p.token, map[string]any{"tenantId": p.tenant.String(), "callId": id}, nil, http.StatusOK)
			}
			p.phone.ringOnly.Store(false)
			p.carrier.ringOnly.Store(false)
		})
		want := map[int][2]string{
			3: {"LICENSE_CONDITION_VALID", "CAPACITY_LEVEL_NORMAL"},
			4: {"LICENSE_CONDITION_CAPACITY_WARNING", "CAPACITY_LEVEL_WARNING"},
			5: {"LICENSE_CONDITION_CAPACITY_REACHED", "CAPACITY_LEVEL_REACHED"},
		}
		for n := 1; n <= 5; n++ {
			id, _ := p.place(t, p.ext, fmt.Sprintf("+4420700001%02d", n), http.StatusOK)
			held = append(held, id)
			var st licenceStatus
			require.Eventually(t, func() bool {
				st = p.licence(t, p.token)
				return st.ChannelsInUse == n
			}, 10*time.Second, 200*time.Millisecond, "call %d holds a channel", n)
			if w, ok := want[n]; ok {
				assert.Equal(t, w[0], st.Condition, "%d of 5 channels held", n)
				assert.Equal(t, w[1], st.Capacity, "%d of 5 channels held", n)
				assert.Equal(t, "LICENSE_TIER_COMMERCIAL", st.Tier)
			}
		}
	})
}

// pbx-sip-registrar-settings task 3.1: the running app reports the rig's
// configured registrar address to an operator, and refuses a role that
// may not read extensions.
func TestConsoleAPIs_SipRegistrar(t *testing.T) {
	r := newRig(t)
	var reg struct {
		Configured bool   `json:"configured"`
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Transport  string `json:"transport"`
	}
	r.call(t, "PbxService/GetSipRegistrar", r.token, map[string]any{"tenantId": r.tenant.String()}, &reg, http.StatusOK)
	assert.True(t, reg.Configured)
	assert.Equal(t, "localhost", reg.Host, "deploy/docker-compose.yml sets SIP_PUBLIC_HOST for the rig")
	assert.Equal(t, 5060, reg.Port)
	assert.Equal(t, "udp", reg.Transport)

	const pw = "e2e-supervisor-password-ok"
	supEmail := fmt.Sprintf("sup-reg-%d@e2e.invalid", time.Now().UnixNano())
	t.Cleanup(func() { forgetFixtureUser(r.pool, r.tenant, supEmail) })
	r.call(t, "IdentityService/ProvisionPrincipal", r.token, map[string]any{
		"tenantId": r.tenant.String(), "email": supEmail, "password": pw, "role": "SUPERVISOR",
	}, nil, http.StatusOK)
	var sup struct {
		Token string `json:"token"`
	}
	r.call(t, "IdentityService/AuthenticateUser", "", map[string]any{
		"tenantId": r.tenant.String(), "username": supEmail, "password": pw}, &sup, http.StatusOK)
	r.call(t, "PbxService/GetSipRegistrar", sup.Token, map[string]any{"tenantId": r.tenant.String()}, nil, http.StatusForbidden)
}
