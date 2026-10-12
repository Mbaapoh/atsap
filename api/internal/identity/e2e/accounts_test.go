//go:build e2e

// identity-accounts-and-sessions task 8.2: console sign-in, sessions and
// credential recovery against the running app — a real cookie jar, real
// Argon2id, real Postgres, and mail read back from Mailpit.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/identity/application"
	"atsap-api/internal/identity/domain"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
)

const (
	appURL     = "http://localhost:8080"
	mailpitURL = "http://localhost:8025"
	dbURL      = "postgres://atsapbx_app:devpassword123@127.0.0.1:15432/atsapbx?sslmode=disable"
	refreshCk  = "atsa_refresh"
)

// world is the suite's fixture: two tenants, a platform operator, and the
// secrets the test must never see in a response body or a log.
type world struct {
	pool     *corepostgres.Pool
	tenantA  shareddomain.TenantID
	tenantB  shareddomain.TenantID
	opToken  string
	run      string
	secrets  []string
	startLog time.Time
}

func (w *world) email(name string) string { return fmt.Sprintf("%s-%s@e2e.example.com", name, w.run) }

func setup(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	pool, err := corepostgres.Open(ctx, dbURL)
	require.NoError(t, err, "dev Postgres must be up with migrations applied")
	t.Cleanup(pool.Close)
	w := &world{pool: pool, tenantA: shareddomain.NewTenantID(), tenantB: shareddomain.NewTenantID(),
		run: fmt.Sprint(time.Now().UnixNano()), startLog: time.Now().Add(-time.Second)}

	for _, id := range []shareddomain.TenantID{w.tenantA, w.tenantB} {
		_, err := pool.Unwrap().Exec(ctx, `INSERT INTO tenants (id, name, status, residency_zone) VALUES ($1, $2, 'ACTIVE', 'EU')`,
			id.String(), "e2e-identity-"+id.String()[:8])
		require.NoError(t, err)
	}
	const opPassword = "e2e-operator-password"
	hash, err := application.NewPasswordHasher().Hash(opPassword)
	require.NoError(t, err)
	var opUser string
	require.NoError(t, pool.Unwrap().QueryRow(ctx, `INSERT INTO users (email, display_name) VALUES ($1, 'e2e operator') RETURNING id`,
		w.email("op")).Scan(&opUser))
	_, err = pool.Unwrap().Exec(ctx, `INSERT INTO user_credentials (user_id, kind, secret) VALUES ($1, 'password', $2)`, opUser, hash)
	require.NoError(t, err)
	opPrincipal := shareddomain.NewPrincipalID()
	require.NoError(t, pool.WithTenant(ctx, w.tenantA, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO principals (id, tenant_id, user_id, role) VALUES ($1, $2, $3, 'PLATFORM_ADMIN')`,
			opPrincipal.String(), w.tenantA.String(), opUser); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO role_bindings (principal_id, tenant_id, role, scope) VALUES ($1, $2, 'PLATFORM_ADMIN', 'system')`,
			opPrincipal.String(), w.tenantA.String())
		return err
	}))
	t.Cleanup(func() { w.cleanup() })

	var out struct{ Token string }
	call(t, nil, "IdentityService/AuthenticateUser", "", map[string]any{
		"tenantId": w.tenantA.String(), "username": w.email("op"), "password": opPassword}, &out, http.StatusOK)
	w.opToken = out.Token
	w.secrets = append(w.secrets, opPassword, w.opToken)
	return w
}

// cleanup removes everything the suite created, memberships before people
// (they reference them), and clears the throttle rows its failures wrote
// so a later run from this host starts clean.
func (w *world) cleanup() {
	ctx := context.Background()
	for _, tenant := range []shareddomain.TenantID{w.tenantA, w.tenantB} {
		_ = w.pool.WithTenant(ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			for _, q := range []string{
				`DELETE FROM audit_logs WHERE tenant_id = $1`, `DELETE FROM role_bindings WHERE tenant_id = $1`,
				`DELETE FROM api_keys WHERE tenant_id = $1`, `DELETE FROM principals WHERE tenant_id = $1`,
			} {
				if _, err := tx.Exec(ctx, q, tenant.String()); err != nil {
					return err
				}
			}
			return nil
		})
		_, _ = w.pool.Unwrap().Exec(ctx, `DELETE FROM tenants WHERE id = $1`, tenant.String())
	}
	_, _ = w.pool.Unwrap().Exec(ctx, `DELETE FROM users WHERE email LIKE $1`, "%-"+w.run+"@e2e.example.com")
	_, _ = w.pool.Unwrap().Exec(ctx, `DELETE FROM login_throttle`)
}

// closeGraceWindow moves the rotation that retired secret back past the
// grace window, so presenting it is a replay without waiting 30 seconds.
func (w *world) closeGraceWindow(t *testing.T, secret string) {
	t.Helper()
	tag, err := w.pool.Unwrap().Exec(context.Background(), `
		UPDATE sessions SET previous_rotated_at = previous_rotated_at - $2::interval
		WHERE previous_digest = $1`, application.DigestSecret(secret), fmt.Sprintf("%d seconds", int(domain.RefreshGraceWindow.Seconds())+1))
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "the session that retired the secret")
}

// browser is a console session: a cookie jar that sends atsa_refresh only
// where a browser would.
func browser(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func refreshCookie(t *testing.T, c *http.Client) string {
	t.Helper()
	u, _ := url.Parse(appURL + "/atsapbx.v1.AuthService/RefreshSession")
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == refreshCk {
			return ck.Value
		}
	}
	return ""
}

// call posts a Connect JSON request and decodes it, asserting the status.
func call(t *testing.T, c *http.Client, procedure, token string, body, out any, want int) (*http.Response, []byte) {
	t.Helper()
	if c == nil {
		c = &http.Client{Timeout: 10 * time.Second}
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, appURL+"/atsapbx.v1."+procedure, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, want, resp.StatusCode, "%s: %s", procedure, b)
	if out != nil {
		require.NoError(t, json.Unmarshal(b, out))
	}
	return resp, b
}

type session struct {
	Access *struct {
		Token    string `json:"token"`
		TenantID string `json:"tenantId"`
	} `json:"access"`
	Memberships []struct {
		TenantID string `json:"tenantId"`
	} `json:"memberships"`
	PasswordChangeRequired bool `json:"passwordChangeRequired"`
}

func (w *world) provision(t *testing.T, tenant shareddomain.TenantID, name, password, role string) {
	t.Helper()
	body := map[string]any{"tenantId": tenant.String(), "email": w.email(name), "displayName": name, "role": role}
	if password != "" {
		body["password"] = password
		w.secrets = append(w.secrets, password)
	}
	call(t, nil, "IdentityService/ProvisionPrincipal", w.opToken, body, nil, http.StatusOK)
}

// mailpit reads the dev mail catcher, with a timeout like every client here.
var mailpit = &http.Client{Timeout: 5 * time.Second}

var linkSecret = regexp.MustCompile(`#([A-Za-z0-9_-]{20,})`)

// mailedSecret waits for the newest mail to an address and returns the
// link secret in it.
func (w *world) mailedSecret(t *testing.T, to, kind string) string {
	t.Helper()
	var secret string
	require.Eventually(t, func() bool {
		resp, err := mailpit.Get(mailpitURL + "/api/v1/search?query=" + url.QueryEscape(`to:"`+to+`"`))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		var list struct {
			Messages []struct{ ID string } `json:"messages"`
		}
		if json.NewDecoder(resp.Body).Decode(&list) != nil || len(list.Messages) == 0 {
			return false
		}
		mresp, err := mailpit.Get(mailpitURL + "/api/v1/message/" + list.Messages[0].ID)
		if err != nil {
			return false
		}
		defer func() { _ = mresp.Body.Close() }()
		var msg struct{ Text string }
		if json.NewDecoder(mresp.Body).Decode(&msg) != nil || !strings.Contains(msg.Text, "/"+kind+"#") {
			return false
		}
		m := linkSecret.FindStringSubmatch(msg.Text)
		if len(m) != 2 {
			return false
		}
		secret = m[1]
		return true
	}, 10*time.Second, 200*time.Millisecond, "a %s mail reaches %s", kind, to)
	w.secrets = append(w.secrets, secret)
	return secret
}

func TestAccounts_EndToEnd(t *testing.T) {
	w := setup(t)
	const anaPassword = "ana-correct-password"
	w.provision(t, w.tenantA, "ana", anaPassword, "AGENT")
	w.provision(t, w.tenantB, "ana", "", "AGENT") // an existing person: membership only, no mail

	t.Run("discovery, sign-in as a two-tenant person, choose a tenant", func(t *testing.T) {
		var d struct{ Method string }
		call(t, nil, "AuthService/DiscoverSignIn", "", map[string]any{"email": w.email("ana")}, &d, http.StatusOK)
		assert.Equal(t, "SIGN_IN_METHOD_PASSWORD", d.Method)

		b := browser(t)
		var s session
		_, body := call(t, b, "AuthService/SignIn", "", map[string]any{"email": w.email("ana"), "password": anaPassword}, &s, http.StatusOK)
		assert.Nil(t, s.Access, "two memberships: no token until one is chosen")
		assert.Len(t, s.Memberships, 2)
		cookie := refreshCookie(t, b)
		require.NotEmpty(t, cookie, "the refresh secret arrives as a cookie")
		assert.NotContains(t, string(body), cookie, "and never in the body")
		w.secrets = append(w.secrets, cookie)

		var a session
		call(t, b, "AuthService/RefreshSession", "", map[string]any{"tenantId": w.tenantA.String()}, &a, http.StatusOK)
		require.NotNil(t, a.Access)
		w.secrets = append(w.secrets, a.Access.Token)
		call(t, nil, "TelephonyService/ListCalls", a.Access.Token, map[string]any{"tenantId": w.tenantA.String()}, nil, http.StatusOK)
		call(t, nil, "TelephonyService/ListCalls", a.Access.Token, map[string]any{"tenantId": w.tenantB.String()}, nil, http.StatusBadRequest)

		var bt session
		call(t, b, "AuthService/RefreshSession", "", map[string]any{"tenantId": w.tenantB.String()}, &bt, http.StatusOK)
		require.NotNil(t, bt.Access)
		w.secrets = append(w.secrets, bt.Access.Token)
		call(t, nil, "TelephonyService/ListCalls", bt.Access.Token, map[string]any{"tenantId": w.tenantB.String()}, nil, http.StatusOK)
	})

	t.Run("a replayed refresh cookie revokes the session", func(t *testing.T) {
		b := browser(t)
		call(t, b, "AuthService/SignIn", "", map[string]any{"email": w.email("ana"), "password": anaPassword}, nil, http.StatusOK)
		var s session
		call(t, b, "AuthService/RefreshSession", "", map[string]any{"tenantId": w.tenantA.String()}, &s, http.StatusOK)
		old := refreshCookie(t, b)
		call(t, b, "AuthService/RefreshSession", "", map[string]any{}, nil, http.StatusOK)
		current := refreshCookie(t, b)
		require.NotEqual(t, old, current, "rotated")
		w.secrets = append(w.secrets, old, current, s.Access.Token)
		// Outside the grace window (identity-refresh-grace): the rotation
		// is moved back past it rather than waiting 30 seconds.
		w.closeGraceWindow(t, old)

		thief := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest(http.MethodPost, appURL+"/atsapbx.v1.AuthService/RefreshSession", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: refreshCk, Value: old})
		resp, err := thief.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the replay is refused")

		call(t, b, "AuthService/RefreshSession", "", map[string]any{}, nil, http.StatusUnauthorized)
		call(t, nil, "TelephonyService/ListCalls", s.Access.Token, map[string]any{"tenantId": w.tenantA.String()}, nil, http.StatusUnauthorized)
	})

	t.Run("a refresh whose response was lost is retried inside the grace window", func(t *testing.T) {
		b := browser(t)
		call(t, b, "AuthService/SignIn", "", map[string]any{"email": w.email("ana"), "password": anaPassword}, nil, http.StatusOK)
		call(t, b, "AuthService/RefreshSession", "", map[string]any{"tenantId": w.tenantA.String()}, nil, http.StatusOK)
		kept := refreshCookie(t, b)

		// The server rotates, but the response is lost: the browser never
		// stores the new cookie.
		lost := &http.Client{Timeout: 10 * time.Second}
		req, _ := http.NewRequest(http.MethodPost, appURL+"/atsapbx.v1.AuthService/RefreshSession", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: refreshCk, Value: kept})
		resp, err := lost.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var lostCookie string
		for _, ck := range resp.Cookies() {
			if ck.Name == refreshCk {
				lostCookie = ck.Value
			}
		}
		require.NotEmpty(t, lostCookie)

		// The browser retries with the cookie it kept, inside the window.
		var s session
		call(t, b, "AuthService/RefreshSession", "", map[string]any{}, &s, http.StatusOK)
		require.NotNil(t, s.Access, "the retry signs the person in as before")
		retried := refreshCookie(t, b)
		require.NotEqual(t, kept, retried)
		w.secrets = append(w.secrets, kept, lostCookie, retried, s.Access.Token)
		call(t, nil, "TelephonyService/ListCalls", s.Access.Token, map[string]any{"tenantId": w.tenantA.String()}, nil, http.StatusOK)

		// The cookie issued in the lost response no longer works; the
		// retried session goes on.
		thief := &http.Client{Timeout: 10 * time.Second}
		req, _ = http.NewRequest(http.MethodPost, appURL+"/atsapbx.v1.AuthService/RefreshSession", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: refreshCk, Value: lostCookie})
		resp, err = thief.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		call(t, b, "AuthService/RefreshSession", "", map[string]any{}, nil, http.StatusOK)

		var retries int
		require.NoError(t, w.pool.Unwrap().QueryRow(context.Background(), `
			SELECT count(*) FROM auth_events e JOIN users u ON u.id = e.user_id
			WHERE u.email = $1 AND e.kind = 'session.refresh_retried'`, w.email("ana")).Scan(&retries))
		assert.GreaterOrEqual(t, retries, 1, "the retry is recorded")
	})

	t.Run("sign-out ends access at once", func(t *testing.T) {
		b := browser(t)
		var s session
		call(t, b, "AuthService/SignIn", "", map[string]any{"email": w.email("ana"), "password": anaPassword}, nil, http.StatusOK)
		call(t, b, "AuthService/RefreshSession", "", map[string]any{"tenantId": w.tenantA.String()}, &s, http.StatusOK)
		w.secrets = append(w.secrets, s.Access.Token)
		call(t, b, "AuthService/SignOut", s.Access.Token, map[string]any{}, nil, http.StatusOK)
		assert.Empty(t, refreshCookie(t, b), "the cookie is cleared")
		call(t, nil, "TelephonyService/ListCalls", s.Access.Token, map[string]any{"tenantId": w.tenantA.String()}, nil, http.StatusUnauthorized)
	})

	t.Run("reset by email", func(t *testing.T) {
		call(t, nil, "AuthService/RequestPasswordReset", "", map[string]any{"email": w.email("ana")}, nil, http.StatusOK)
		call(t, nil, "AuthService/RequestPasswordReset", "", map[string]any{"email": w.email("ghost")}, nil, http.StatusOK)
		secret := w.mailedSecret(t, w.email("ana"), "reset")
		const next = "ana-after-reset-password"
		w.secrets = append(w.secrets, next)
		call(t, nil, "AuthService/CompletePasswordReset", "", map[string]any{"secret": secret, "newPassword": next}, nil, http.StatusOK)
		call(t, nil, "AuthService/CompletePasswordReset", "", map[string]any{"secret": secret, "newPassword": next}, nil, http.StatusBadRequest)
		call(t, nil, "AuthService/SignIn", "", map[string]any{"email": w.email("ana"), "password": next}, nil, http.StatusOK)
		call(t, nil, "AuthService/SignIn", "", map[string]any{"email": w.email("ana"), "password": anaPassword}, nil, http.StatusUnauthorized)
	})

	t.Run("an administrator reset forces a change first", func(t *testing.T) {
		w.provision(t, w.tenantA, "carol", "carol-own-password", "AGENT")
		var listed struct {
			Principals []struct {
				Principal struct {
					PrincipalID string `json:"principalId"`
					Email       string `json:"email"`
				} `json:"principal"`
			} `json:"principals"`
		}
		call(t, nil, "IdentityService/ListPrincipals", w.opToken, map[string]any{"tenantId": w.tenantA.String()}, &listed, http.StatusOK)
		var carol string
		for _, p := range listed.Principals {
			if p.Principal.Email == w.email("carol") {
				carol = p.Principal.PrincipalID
			}
		}
		require.NotEmpty(t, carol)
		const temporary = "carol-temporary-pass"
		w.secrets = append(w.secrets, temporary)
		call(t, nil, "IdentityService/ResetPrincipalPassword", w.opToken, map[string]any{
			"tenantId": w.tenantA.String(), "principalId": carol, "temporaryPassword": temporary}, nil, http.StatusOK)

		var s session
		call(t, nil, "AuthService/SignIn", "", map[string]any{"email": w.email("carol"), "password": temporary}, &s, http.StatusOK)
		require.NotNil(t, s.Access)
		assert.True(t, s.PasswordChangeRequired)
		w.secrets = append(w.secrets, s.Access.Token)
		call(t, nil, "TelephonyService/ListCalls", s.Access.Token, map[string]any{"tenantId": w.tenantA.String()}, nil, http.StatusForbidden)
		call(t, nil, "AuthService/ChangePassword", s.Access.Token, map[string]any{
			"currentPassword": temporary, "newPassword": "carol-chosen-password"}, nil, http.StatusOK)
		w.secrets = append(w.secrets, "carol-chosen-password")
		call(t, nil, "TelephonyService/ListCalls", s.Access.Token, map[string]any{"tenantId": w.tenantA.String()}, nil, http.StatusOK)
	})

	t.Run("an invitation sets a first password", func(t *testing.T) {
		w.provision(t, w.tenantA, "dan", "", "AGENT")
		secret := w.mailedSecret(t, w.email("dan"), "invite")
		call(t, nil, "AuthService/CompletePasswordReset", "", map[string]any{"secret": secret, "newPassword": "dan-first-password"}, nil, http.StatusOK)
		w.secrets = append(w.secrets, "dan-first-password")
		var s session
		call(t, nil, "AuthService/SignIn", "", map[string]any{"email": w.email("dan"), "password": "dan-first-password"}, &s, http.StatusOK)
		require.NotNil(t, s.Access)
	})

	// Last: it blocks this host's address for half a minute.
	t.Run("six wrong passwords are throttled", func(t *testing.T) {
		w.provision(t, w.tenantA, "erin", "erin-own-password", "AGENT")
		// Earlier subtests' deliberate failures came from this same host and
		// count against its address key, as they should. Start the address
		// clean so this measures one account's run of failures.
		_, err := w.pool.Unwrap().Exec(context.Background(), `DELETE FROM login_throttle`)
		require.NoError(t, err)
		for i := 0; i < 5; i++ {
			call(t, nil, "AuthService/SignIn", "", map[string]any{"email": w.email("erin"), "password": "erin-wrong-password"}, nil, http.StatusUnauthorized)
		}
		resp, body := call(t, nil, "AuthService/SignIn", "", map[string]any{"email": w.email("erin"), "password": "erin-own-password"}, nil, http.StatusTooManyRequests)
		assert.Contains(t, string(body), "resource_exhausted")
		assert.NotEmpty(t, resp.Header.Get("Retry-After"), "the delay is stated")
	})

	t.Run("no secret in any log line", func(t *testing.T) {
		out, err := exec.Command("docker", "logs", "--since", w.startLog.UTC().Format(time.RFC3339), "deploy-app-1").CombinedOutput()
		require.NoError(t, err)
		logs := string(out)
		require.Contains(t, logs, "identity: authentication failed", "these are this run's logs: its failures are in them")
		require.Contains(t, logs, w.email("erin"), "including the throttled account, by email only")
		require.GreaterOrEqual(t, len(w.secrets), 15, "every secret the run handled is checked")
		for _, s := range w.secrets {
			assert.NotContains(t, logs, s, "a password, token or link secret reached the app log")
		}
	})
}
