//go:build e2e

// Package e2e closes gap G1 from the archived change
// pbx-extensions-projection: three scenarios of
// openspec/specs/pbx-core/endpoint-projection/spec.md that were proven by
// hand against the live engine and never automated.
//
//   - "A device registers immediately after creation"
//   - "A wrong secret is refused"
//   - "Deletion takes effect immediately"
//
// The mechanism under test is PJSIP Realtime projection (D-47): the ACL
// writes ps_* rows in the same transaction as the domain row and Asterisk
// reads them directly. There is no reload, no apply step and no dialplan.
// ARI dynamic object creation was tried and rejected
// (403 "Cannot create sorcery objects of type 'endpoint'"), so nothing
// here goes near it.
//
// Immediacy IS the assertion. The first REGISTER after CreateExtension
// must succeed on its first attempt: a retry loop would convert the
// property being tested — that configuration is live the moment the
// transaction commits — into one that merely holds eventually.
//
// Mutations are driven over the public HTTP API, so this proves the
// surface a console or a partner actually uses rather than the Go
// internals beneath it. Engine state is asserted directly in Postgres,
// because a 200 OK with no bound contact is precisely the silent failure
// this projection design exists to avoid.
//
// Prerequisites: the dev stack up (`mise run dev`) with migrations
// applied. Run:
//
//	cd api && go test -tags e2e ./internal/pbx/e2e/ -v
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

const (
	apiBase   = "http://127.0.0.1:8080"
	sipServer = "127.0.0.1:5060"
	localIP   = "127.0.0.1"

	dbURL = "postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable"

	// fixturePassword and fixtureHash are a synthetic dev-rig operator
	// (D-39: no real credential, no PII). The hash is this repository's
	// own Argon2id output for that password, verified against
	// application.PasswordHasher before being pinned here — a hash that
	// silently failed to verify would make every test below fail at
	// authentication, for a reason that looks nothing like the cause.
	fixturePassword = "e2e-fixture-operator-password"
	fixtureHash     = "$argon2id$v=19$m=19456,t=2,p=1$S2sQSViP/Ztyosl6nREwLg$b1iihB2OK3aZuaArz3DaXKooCO/+VrnoaU2/XFzVhGc"
)

// --- fixture ---------------------------------------------------------

type rig struct {
	pool   *corepostgres.Pool
	tenant shareddomain.TenantID
	token  string
}

// newRig seeds a synthetic tenant and an authenticated operator, then
// obtains a token through the real AuthenticateUser RPC rather than
// minting one — a hand-made token would prove the tests can reach the
// API, not that a caller can.
//
// The role bound is TENANT_ADMIN at tenant scope, deliberately, rather
// than a platform operator with system scope: extension management is
// something a customer's own administrator does, and a system-scoped
// binding covers every tenant, which would mask a failure that only
// affects ordinary tenant-scoped callers.
func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()

	pool, err := corepostgres.Open(ctx, dbURL)
	require.NoError(t, err, "dev Postgres must be up (mise run dev) with migrations applied")
	t.Cleanup(pool.Close)

	tenantID := shareddomain.NewTenantID()
	principalID := shareddomain.NewPrincipalID()
	username := fmt.Sprintf("e2e-op-%d", rand.Intn(1_000_000)) //nolint:gosec // fixture naming, not security

	// tenants carries no RLS — it is the root of tenancy — so this insert
	// needs no tenant context. The principal and binding do.
	_, err = pool.Unwrap().Exec(ctx,
		`INSERT INTO tenants (id, name, status, residency_zone) VALUES ($1, $2, 'ACTIVE', 'EU')`,
		tenantID.String(), "e2e-"+username)
	require.NoError(t, err)

	require.NoError(t, pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO principals (id, tenant_id, username, email, password_hash, role, status)
			 VALUES ($1, $2, $3, $4, $5, 'TENANT_ADMIN', 'ACTIVE')`,
			principalID.String(), tenantID.String(), username, username+"@e2e.invalid", fixtureHash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
			 VALUES ($1, $2, 'TENANT_ADMIN', 'tenant')`,
			principalID.String(), tenantID.String())
		return err
	}))

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_ = pool.WithTenant(cleanupCtx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			for _, stmt := range []string{
				`DELETE FROM audit_logs WHERE tenant_id = $1`,
				`DELETE FROM extensions WHERE tenant_id = $1`,
				`DELETE FROM role_bindings WHERE tenant_id = $1`,
				`DELETE FROM principals WHERE tenant_id = $1`,
			} {
				if _, err := tx.Exec(ctx, stmt, tenantID.String()); err != nil {
					return err
				}
			}
			return nil
		})
		_, _ = pool.Unwrap().Exec(cleanupCtx, `DELETE FROM tenants WHERE id = $1`, tenantID.String())
	})

	r := &rig{pool: pool, tenant: tenantID}
	r.token = r.authenticate(t, username)
	return r
}

func (r *rig) authenticate(t *testing.T, username string) string {
	t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	r.call(t, "IdentityService/AuthenticateUser", "", map[string]any{
		"tenantId": r.tenant.String(), "username": username, "password": fixturePassword,
	}, &out, http.StatusOK)
	require.NotEmpty(t, out.Token, "authentication must yield a token")
	return out.Token
}

// call posts a ConnectRPC request over plain HTTP+JSON and decodes the
// response, asserting the status. Returns the raw body so a caller can
// inspect an error payload.
func (r *rig) call(t *testing.T, procedure, token string, body, out any, wantStatus int) []byte {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, apiBase+"/atsapbx.v1."+procedure, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "the app container must be up and serving %s", apiBase)
	defer func() { _ = resp.Body.Close() }()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	require.Equal(t, wantStatus, resp.StatusCode, "%s returned %s: %s", procedure, resp.Status, buf.String())

	if out != nil {
		require.NoError(t, json.Unmarshal(buf.Bytes(), out), "decode %s: %s", procedure, buf.String())
	}
	return buf.Bytes()
}

type extension struct {
	ExtensionID  string `json:"extensionId"`
	Number       string `json:"number"`
	AuthUsername string `json:"authUsername"`
	Registration string `json:"registration"`
}

func (r *rig) createExtension(t *testing.T, number string) (extension, string) {
	t.Helper()
	var out struct {
		Extension extension `json:"extension"`
		Secret    string    `json:"secret"`
	}
	r.call(t, "PbxService/CreateExtension", r.token, map[string]any{
		"tenantId": r.tenant.String(), "number": number,
		"displayName": "G1 " + number, "deviceType": "DEVICE_TYPE_SIP",
	}, &out, http.StatusOK)
	require.NotEmpty(t, out.Secret, "the generated secret is returned exactly once, here")
	return out.Extension, out.Secret
}

func (r *rig) registrationStatus(t *testing.T, extensionID string) string {
	t.Helper()
	var out struct {
		Extension extension `json:"extension"`
	}
	r.call(t, "PbxService/GetExtension", r.token, map[string]any{
		"tenantId": r.tenant.String(), "extensionId": extensionID,
	}, &out, http.StatusOK)
	return out.Extension.Registration
}

// contactCount counts live contacts for a projected endpoint. ps_contacts
// carries no RLS, so this needs no tenant context — and the endpoint is
// matched by the auth_username the API returned, never by an identifier
// this test derived, so the test cannot pass by reproducing a bug in the
// derivation.
func (r *rig) contactCount(t *testing.T, authUsername string) int {
	t.Helper()
	var n int
	require.NoError(t, r.pool.Unwrap().QueryRow(context.Background(),
		`SELECT count(*) FROM ps_contacts WHERE endpoint = $1 AND expiration_time > extract(epoch from now())::bigint`,
		authUsername).Scan(&n))
	return n
}

// uniqueNumber avoids the 1000/1001 dev fixtures and collisions between
// runs. ValidateNumber permits digits only, 2-20 characters.
func uniqueNumber() string {
	return fmt.Sprintf("87%04d", rand.Intn(10_000)) //nolint:gosec // fixture naming, not security
}

// --- SIP ------------------------------------------------------------

// registerOnce performs one REGISTER and returns the final status code.
// It does not retry: for the scenario under test, needing a second
// attempt is a failure, not a transient.
func registerOnce(t *testing.T, user, secret string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	defer func() { _ = ua.Close() }()

	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname(localIP))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	var recipient sip.Uri
	require.NoError(t, sip.ParseUri(fmt.Sprintf("sip:%s@%s", user, sipServer), &recipient))

	req := sip.NewRequest(sip.REGISTER, recipient)
	// Asterisk identifies an inbound REGISTER by matching the From user
	// against the endpoint id — proven during the D-47 spike, where a
	// From of the auth username alone returned 401. The projected
	// identifier and the SIP username are one value by design (D2), so
	// this is what a real device presents.
	req.RemoveHeader("From")
	req.AppendHeader(sip.NewHeader("From", fmt.Sprintf("<sip:%s@%s>", user, localIP)))
	req.RemoveHeader("To")
	req.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", user, sipServer)))
	req.RemoveHeader("Contact")
	req.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s>", user, localIP)))
	req.SetTransport("UDP")

	tx, err := client.TransactionRequest(ctx, req, sipgo.ClientRequestRegisterBuild)
	require.NoError(t, err)
	defer tx.Terminate()

	res, err := waitResponse(ctx, tx)
	require.NoError(t, err)
	if res.StatusCode != http.StatusUnauthorized {
		return res.StatusCode
	}

	chal, err := digest.ParseChallenge(res.GetHeader("WWW-Authenticate").Value())
	require.NoError(t, err)
	cred, err := digest.Digest(chal, digest.Options{
		Method: req.Method.String(), URI: sipServer, Username: user, Password: secret,
	})
	require.NoError(t, err)

	req2 := req.Clone()
	req2.RemoveHeader("Via")
	req2.AppendHeader(sip.NewHeader("Authorization", cred.String()))
	tx2, err := client.TransactionRequest(ctx, req2, sipgo.ClientRequestIncreaseCSEQ, sipgo.ClientRequestAddVia)
	require.NoError(t, err)
	defer tx2.Terminate()

	res2, err := waitResponse(ctx, tx2)
	require.NoError(t, err)
	return res2.StatusCode
}

func waitResponse(ctx context.Context, tx sip.ClientTransaction) (*sip.Response, error) {
	select {
	case res := <-tx.Responses():
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-tx.Done():
		return nil, fmt.Errorf("transaction completed without a response")
	}
}

// --- the three G1 scenarios ------------------------------------------

// TestG1_ProjectionIsLiveWithoutAnApplyStep runs the three scenarios in
// sequence because they share one extension's lifetime, and because
// scenario 3 (deletion) is the natural teardown for scenarios 1 and 2.
func TestG1_ProjectionIsLiveWithoutAnApplyStep(t *testing.T) {
	r := newRig(t)
	number := uniqueNumber()

	ext, secret := r.createExtension(t, number)
	require.NotEmpty(t, ext.AuthUsername)
	assert.Equal(t, "REGISTRATION_STATUS_NOT_REGISTERED", ext.Registration,
		"an extension with no device attached is not registered")

	// --- Scenario: A device registers immediately after creation -----
	//
	// No sleep, no retry, no reload. The REGISTER is issued straight
	// after the create returns, and its first attempt must succeed:
	// immediacy is the property, so tolerating a retry would test
	// something weaker than the spec says.
	t.Run("registers immediately after creation", func(t *testing.T) {
		require.Equal(t, http.StatusOK, registerOnce(t, ext.AuthUsername, secret),
			"a device must be able to register the moment CreateExtension returns — no apply step, no reload")

		// A 200 with no bound contact is the silent failure this design
		// exists to prevent, so the contact is asserted, not inferred.
		assert.Equal(t, 1, r.contactCount(t, ext.AuthUsername),
			"REGISTER returned 200; a contact must actually be bound in the engine's own state")

		assert.Equal(t, "REGISTRATION_STATUS_REGISTERED", r.registrationStatus(t, ext.ExtensionID),
			"the public API must report the registration the engine accepted")

		// The stored credential must not be recoverable: the digest is
		// held, the plaintext column is never written.
		var password *string
		require.NoError(t, r.pool.Unwrap().QueryRow(context.Background(),
			`SELECT password FROM ps_auths WHERE id = $1`, ext.AuthUsername).Scan(&password))
		assert.Nil(t, password, "no plaintext secret may ever be stored, including in engine-facing state")
	})

	// --- Scenario: A wrong secret is refused --------------------------
	t.Run("wrong secret is refused", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized,
			registerOnce(t, ext.AuthUsername, "not-the-generated-secret"),
			"credentials are enforced from the stored digest, not merely present")

		assert.Equal(t, http.StatusOK, registerOnce(t, ext.AuthUsername, secret),
			"and the correct secret still works afterwards — a refusal must not lock the endpoint out")
	})

	// --- Scenario: Deletion takes effect immediately ------------------
	t.Run("deletion takes effect immediately", func(t *testing.T) {
		r.call(t, "PbxService/DeleteExtension", r.token, map[string]any{
			"tenantId": r.tenant.String(), "extensionId": ext.ExtensionID,
		}, nil, http.StatusOK)

		assert.Equal(t, http.StatusUnauthorized, registerOnce(t, ext.AuthUsername, secret),
			"the correct secret must stop working the moment the extension is deleted — no reload issued")

		// The endpoint itself is what deprovisioning removes, and its
		// absence is what refuses the REGISTER above.
		var endpoints int
		require.NoError(t, r.pool.Unwrap().QueryRow(context.Background(),
			`SELECT count(*) FROM ps_endpoints WHERE id = $1`, ext.AuthUsername).Scan(&endpoints))
		assert.Zero(t, endpoints, "the projected endpoint must be gone")

		// The contact row is deliberately NOT removed: contacts are the
		// engine's own bookkeeping, written on registration and pruned by
		// it on expiry, and the ACL removing them would be reaching past
		// its remit (projector.RemoveExtension). An orphaned contact
		// cannot be reached — its endpoint no longer exists — so this
		// asserts the documented behaviour rather than a tidier one that
		// was never built. See finding G3 in coverage.md.
		assert.Equal(t, 1, r.contactCount(t, ext.AuthUsername),
			"the contact is left for the engine to prune; if this ever reaches zero the ACL has started deleting contacts and G3 needs revisiting")
	})
}

// TestG1_UnauthenticatedCreateIsRefused guards the interceptor wiring:
// PbxService is mounted WITH the auth interceptor, and a regression that
// mounted it without would leave every scenario above passing while the
// API was open to anyone.
func TestG1_UnauthenticatedCreateIsRefused(t *testing.T) {
	r := newRig(t)
	body := r.call(t, "PbxService/CreateExtension", "", map[string]any{
		"tenantId": r.tenant.String(), "number": uniqueNumber(),
		"displayName": "unauthenticated", "deviceType": "DEVICE_TYPE_SIP",
	}, nil, http.StatusUnauthorized)
	assert.Contains(t, strings.ToLower(string(body)), "unauthenticated")
}

// --- G2: cross-tenant separation, live -------------------------------

// TestG2_IdenticalNumbersInTwoTenantsRemainSeparate closes the second gap
// the archived change recorded.
//
// It was previously covered at the database (the same number in two
// tenants yields independent rows under real RLS) and in the derivation
// (identifiers come from the extension UUID, never the number), but never
// staged against the engine. The stated reason — that a second tenant
// could not be bootstrapped through the API — was true of `bootstrap`,
// which refuses once a tenant exists, but not of the fixture path this
// suite already uses.
//
// What only a live run can show: that Asterisk resolves two
// simultaneously-present endpoints to the right one. `ps_*` is a single
// flat namespace shared by every tenant, so if the projected identifier
// were ever derived from the tenant-local number, one tenant's phone
// would register against the other's endpoint. That is the failure this
// test exists to make impossible to ship.
func TestG2_IdenticalNumbersInTwoTenantsRemainSeparate(t *testing.T) {
	tenantA := newRig(t)
	tenantB := newRig(t)
	require.NotEqual(t, tenantA.tenant, tenantB.tenant)

	// The SAME number in both tenants — the whole point.
	const shared = "8700"

	extA, secretA := tenantA.createExtension(t, shared)
	extB, secretB := tenantB.createExtension(t, shared)

	assert.Equal(t, shared, extA.Number)
	assert.Equal(t, shared, extB.Number, "a number is tenant-local; both tenants may hold 8700")
	require.NotEqual(t, extA.AuthUsername, extB.AuthUsername,
		"identical numbers must project to different engine identifiers, or one tenant's phone could register against the other's endpoint")
	require.NotEqual(t, secretA, secretB)

	// Register tenant A's device only.
	require.Equal(t, http.StatusOK, registerOnce(t, extA.AuthUsername, secretA))

	assert.Equal(t, "REGISTRATION_STATUS_REGISTERED", tenantA.registrationStatus(t, extA.ExtensionID),
		"tenant A's extension is the one that registered")
	assert.Equal(t, "REGISTRATION_STATUS_NOT_REGISTERED", tenantB.registrationStatus(t, extB.ExtensionID),
		"tenant B's identically-numbered extension must be untouched by tenant A's device")

	assert.Equal(t, 1, tenantA.contactCount(t, extA.AuthUsername))
	assert.Zero(t, tenantB.contactCount(t, extB.AuthUsername),
		"no contact may be attributed to the tenant whose device never registered")

	// And tenant B's own credential still works — proving B's endpoint is
	// live and independent, not merely absent.
	require.Equal(t, http.StatusOK, registerOnce(t, extB.AuthUsername, secretB))
	assert.Equal(t, "REGISTRATION_STATUS_REGISTERED", tenantB.registrationStatus(t, extB.ExtensionID))
	assert.Equal(t, "REGISTRATION_STATUS_REGISTERED", tenantA.registrationStatus(t, extA.ExtensionID),
		"and registering B must not have disturbed A")

	// A's secret must not authenticate B's endpoint.
	assert.Equal(t, http.StatusUnauthorized, registerOnce(t, extB.AuthUsername, secretA),
		"one tenant's secret must never authenticate another tenant's endpoint")
}
