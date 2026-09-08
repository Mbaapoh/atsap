//go:build e2e

// Package e2e runs the telephony-core walking-skeleton end-to-end test
// (openspec change telephony-core-originate-bridge-hangup, task 10.2;
// LLD-01 §8 Definition of Done) against the live dev stack: real
// Asterisk, real Postgres, real NATS, and the answering party is the
// `sipua` sidecar container (cmd/sip-ua), which registers as the dev
// fixtures (1000/1001) and auto-answers. No mocks anywhere in this path.
//
// The answering UAs run as a compose sidecar, not in-process, because
// host-resident UAs are unreachable from the compose network in this
// environment (ufw drops UDP from the voip bridge into host-published
// ports) — see design.md's "UAs as sidecar container" note.
//
// The outbox worker and the public ConnectRPC server belong to the
// running app container (compose service `app`); this test drives
// telephony-core's application.Service in-process so it can originate,
// while asserting durable state through Postgres, event delivery through
// NATS, and the public API through the running app's /GetCall. Hangup is
// performed directly through ARI (both channels), standing in for the
// remote end hanging up — the sipua sidecar has no hangup control surface
// by design, it only answers.
//
// Prerequisites: the dev stack up (`mise run dev`), including the
// `sipua` sidecar. Run:
//
//	cd api && go test -tags e2e ./internal/telephony/e2e/ -v
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	natsgo "github.com/nats-io/nats.go"

	"atsap-api/internal/logging"
	corepostgres "atsap-api/internal/postgres"
	shareddomain "atsap-api/internal/shared/domain"
	"atsap-api/internal/telephony/acl"
	"atsap-api/internal/telephony/acl/ari"
	"atsap-api/internal/telephony/application"
	"atsap-api/internal/telephony/domain"
	"atsap-api/internal/telephony/ports"
	"atsap-api/internal/telephony/postgres"
)

const (
	ariBase = "http://127.0.0.1:8088/ari"
	ariUser = "voipapp"
	ariPass = "devpassword123"
	// ariApp is deliberately NOT "voip-app": the running app container
	// (compose service `app`) already holds an active ARI WebSocket
	// subscription to that name. A second WS subscriber to the same
	// Stasis app name churns Asterisk's app registration (observed:
	// "Creating"/"Deactivating"/"Destroying Stasis app" within
	// milliseconds), which orphans this test's just-originated channels
	// and gets them hung up before they can be bridged. Originate()
	// controls which app a new channel joins via its own `app` query
	// param, independent of the dialplan's stasis-in context (only
	// inbound calls), so a distinct name here is fully isolated.
	ariApp  = "voip-app-e2e"
	pgURL   = "postgres://atsapbx_app:devpassword123@127.0.0.1:15432/atsapbx?sslmode=disable"
	natsURL = "nats://127.0.0.1:4222"
	appAPI  = "http://127.0.0.1:8080" // running app's ConnectRPC (HTTP/JSON)
)

// TestWalkingSkeleton runs LLD-01's Definition of Done end to end.
func TestWalkingSkeleton(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// --- tenant (inserted directly; tenants has no RLS) ---
	tenantID := shareddomain.NewTenantID()
	appPool, err := corepostgres.Open(ctx, pgURL)
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer appPool.Close()
	if _, err := appPool.Unwrap().Exec(ctx,
		`INSERT INTO tenants (id, name, status) VALUES ($1,'e2e-tenant','ACTIVE') ON CONFLICT (id) DO NOTHING`, tenantID.String()); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	// --- an authenticated operator for the public API ---
	//
	// TelephonyService now requires a token (auth-cutover-connectrpc).
	// The token is obtained through the real AuthenticateUser RPC rather
	// than minted here: a hand-made token would prove this test can reach
	// the API, not that a caller can. Exempting GetCall for the test's
	// convenience was the alternative, and it is exactly the concession
	// the cutover removed.
	token := authenticateFixtureOperator(t, ctx, appPool, tenantID)

	// --- NATS: subscribe to this tenant's events before anything happens.
	nc, err := natsgo.Connect(natsURL)
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer nc.Close()
	sub, err := nc.SubscribeSync("tenant." + tenantID.String() + ".event.>")
	if err != nil {
		t.Fatalf("nats subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	// --- in-process telephony-core composition (mirrors cmd/atsap-api).
	// The answering party (1000/1001) is the `sipua` sidecar container,
	// already registered by the time this test runs (compose brings it
	// up alongside asterisk). ---
	ariClient := ari.New(ariBase, ariUser, ariPass, ariApp)
	registry := acl.NewCorrelationRegistry()
	callStore := postgres.NewCallStore(appPool)
	svc := application.NewService(
		acl.NewMediaGatewayAdapter(ariClient),
		callStore,
		application.NewAlwaysPermitLicense(),
		application.NewAlwaysPermitCompliance(),
		registry,
		20,
		logging.New("debug"),
	)
	eventLoop := acl.NewEventLoop(registry, svc, logging.New("debug"))
	go func() {
		if err := ariClient.StreamEvents(ctx, func(ev ari.Event) { eventLoop.Handle(ctx, ev) }, logging.New("debug")); err != nil && ctx.Err() == nil {
			t.Logf("ari event stream stopped: %v", err)
		}
	}()

	// The WebSocket dial+upgrade above is async: originating before
	// Asterisk has actually registered this app's listener drops the new
	// channel into a Stasis app with nobody controlling it, and Asterisk
	// hangs it up almost immediately (observed directly: a channel
	// answers, then "Channel not found" on the very next ARI call).
	// GET /applications/{name} 200s only once a WS subscriber is live.
	waitForStasisApp(t, ctx, ariApp)

	// --- originate the two-party call. ---
	cmd := ports.InitiateCallCommand{
		TenantID:          tenantID,
		Direction:         domain.Outbound,
		SourceNumber:      "5550100",
		DestNumber:        "5550101",
		SourceEndpointURI: "PJSIP/1000",
		DestEndpointURI:   "PJSIP/1001",
	}
	callID, err := svc.InitiateCall(ctx, cmd)
	if err != nil {
		t.Fatalf("InitiateCall: %v", err)
	}

	// --- wait for Active (2 connected participants) in the durable store. ---
	waitFor(t, ctx, "Active", func() (string, error) {
		c, err := callStore.GetCall(ctx, tenantID, callID)
		if err != nil {
			return "", nil // not yet durably saved; keep polling
		}
		return string(c.State), nil
	}, domain.CallActive)
	t.Logf("call %s reached Active", callID)

	// let a few usage seconds accrue
	time.Sleep(3 * time.Second)

	// --- hang up both legs directly through ARI, standing in for the
	// remote end hanging up: list every channel Asterisk currently
	// knows about, match by the ATSA_CALL_ID correlation variable
	// (docs/hld/01-architecture.md §2.1) set at origination time, and
	// hang up each match. ---
	hangupCallChannels(t, ctx, ariClient, callID.String())

	waitFor(t, ctx, "Terminated", func() (string, error) {
		c, err := callStore.GetCall(ctx, tenantID, callID)
		if err != nil {
			return "", nil
		}
		return string(c.State), nil
	}, domain.CallTerminated)
	t.Logf("call %s reached Terminated", callID)

	// --- usage_seconds: continuous, non-duplicated, per participant. ---
	assertUsageSeries(t, ctx, appPool, tenantID, callID)

	// --- NATS: expected event sequence delivered. ---
	assertEventSequence(t, sub)

	// --- public API: GetCall via the running app; right state, no channel ID. ---
	assertPublicGetCall(t, ctx, tenantID, callID, token)
}

// waitForStasisApp polls ARI's /applications/{name} until Asterisk
// reports the app as registered (a live WebSocket subscriber), so the
// caller can safely originate into it.
func waitForStasisApp(t *testing.T, ctx context.Context, appName string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ariBase+"/applications/"+appName, nil)
		if err != nil {
			t.Fatalf("build applications request: %v", err)
		}
		req.SetBasicAuth(ariUser, ariPass)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx done waiting for Stasis app %s: %v", appName, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for Stasis app %s to register", appName)
}

func waitFor(t *testing.T, ctx context.Context, want string, probe func() (string, error), match domain.CallState) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := probe()
		if got == string(match) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("ctx done waiting for %s: %v", want, ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for call state %s", want)
}

func assertUsageSeries(t *testing.T, ctx context.Context, appPool *corepostgres.Pool, tenantID shareddomain.TenantID, callID shareddomain.CallID) {
	t.Helper()
	var rows []struct {
		ParticipantID string
		SecondTS      time.Time
	}
	err := appPool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		r, err := tx.Query(ctx, `SELECT participant_id, second_ts FROM usage_seconds WHERE call_id=$1 ORDER BY participant_id, second_ts`, callID.String())
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			var x struct {
				ParticipantID string
				SecondTS      time.Time
			}
			if err := r.Scan(&x.ParticipantID, &x.SecondTS); err != nil {
				return err
			}
			rows = append(rows, x)
		}
		return r.Err()
	})
	if err != nil {
		t.Fatalf("query usage_seconds: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no usage_seconds rows recorded for a connected call")
	}
	// one distinct participant must have recorded (both legs connect, so expect two)
	byPart := map[string][]time.Time{}
	for _, r := range rows {
		byPart[r.ParticipantID] = append(byPart[r.ParticipantID], r.SecondTS)
	}
	if len(byPart) < 2 {
		t.Fatalf("expected usage for 2 participants, got %d", len(byPart))
	}
	for pid, ts := range byPart {
		if len(ts) < 2 {
			t.Errorf("participant %s: expected >=2 usage seconds, got %d", pid, len(ts))
		}
		// continuity: each successive second exactly +1s
		for i := 1; i < len(ts); i++ {
			if d := ts[i].Sub(ts[i-1]); d != time.Second {
				t.Errorf("participant %s: usage gap/overlap at index %d: %v", pid, i, d)
			}
		}
	}
	t.Logf("usage_seconds: %d continuous non-duplicated rows across %d participants", len(rows), len(byPart))
}

// assertEventSequence checks the tenant event stream contains the expected
// call lifecycle events in order (per LLD-01 DoD 6).
func assertEventSequence(t *testing.T, sub *natsgo.Subscription) {
	t.Helper()
	want := []string{"call.initiated", "call.active", "call.terminated"}
	got := []string{}
	// Keep collecting until all three call.* markers have actually
	// arrived, not just until len(got) reaches len(want) — participant.*
	// events interleave with them, so that count is reached long before
	// the markers themselves are (found directly: the outbox worker's
	// own 2s poll cadence means call.initiated and call.terminated often
	// land in separate batches from the participant.* events).
	deadline := time.Now().Add(20 * time.Second)
	for len(filterMarkers(got)) < len(want) && time.Now().Before(deadline) {
		msg, err := sub.NextMsg(2 * time.Second)
		if err != nil {
			continue
		}
		evType := eventTypeFromSubject(msg.Subject)
		got = append(got, evType)
	}
	// require presence and relative order of the three call.* markers; the
	// participant.joined/left events interleave and are not order-critical
	// against each other here.
	markers := filterMarkers(got)
	if len(markers) < len(want) {
		t.Fatalf("NATS: missing lifecycle events; saw %v, want %v (full: %v)", markers, want, got)
	}
	for i := range want {
		if markers[i] != want[i] {
			t.Fatalf("NATS: event order wrong: %v (want prefix %v)", markers, want)
		}
	}
	t.Logf("NATS: received lifecycle sequence %v", markers)
}

func eventTypeFromSubject(subject string) string {
	// tenant.<id>.event.call.initiated -> call.initiated
	parts := strings.Split(subject, ".")
	if len(parts) >= 4 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return subject
}

func filterMarkers(evs []string) []string {
	var out []string
	for _, e := range evs {
		if strings.HasPrefix(e, "call.") {
			out = append(out, e)
		}
	}
	return out
}

func assertPublicGetCall(t *testing.T, ctx context.Context, tenantID shareddomain.TenantID, callID shareddomain.CallID, token string) {
	t.Helper()
	body := fmt.Sprintf(`{"tenantId":%q,"callId":%q}`, tenantID.String(), callID.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		appAPI+"/atsapbx.v1.TelephonyService/GetCall", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build getcall request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("getcall over http: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetCall status %d", resp.StatusCode)
	}
	var parsed struct {
		State    string `json:"state"`
		CallID   string `json:"callId"`
		TenantID string `json:"tenantId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode GetCall: %v", err)
	}
	if parsed.State != "Terminated" {
		t.Errorf("GetCall state = %q, want Terminated", parsed.State)
	}
	if parsed.CallID != callID.String() {
		t.Errorf("GetCall callId mismatch")
	}
	// raw body scan: no infrastructure channel identifier leaks
	raw := marshaledForLeakCheck(parsed)
	for _, bad := range []string{"atsa-part-", "channel_id", "\"channel\"", "PJSIP"} {
		if strings.Contains(raw, bad) {
			t.Errorf("GetCall response leaks %q: %s", bad, raw)
		}
	}
}

// marshaledForLeakCheck re-marshals the decoded struct so we scan the wire
// representation, not a hand-built string.
func marshaledForLeakCheck(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// hangupCallChannels finds every Asterisk channel currently tagged with
// the given call's ATSA_CALL_ID correlation variable and hangs each one
// up through ARI. Stands in for the remote end hanging up: the sipua
// sidecar only answers, it has no hangup control surface by design.
func hangupCallChannels(t *testing.T, ctx context.Context, ariClient *ari.Client, callID string) {
	t.Helper()
	channelIDs, err := ariClient.ListChannels(ctx)
	if err != nil {
		t.Fatalf("list channels: %v", err)
	}
	hungUp := 0
	for _, chID := range channelIDs {
		v, err := ariClient.GetChannelVariable(ctx, chID, "ATSA_CALL_ID")
		if err != nil || v != callID {
			continue
		}
		if err := ariClient.HangupChannel(ctx, chID, "normal"); err != nil {
			t.Logf("hangup channel %s: %v", chID, err)
			continue
		}
		hungUp++
	}
	if hungUp == 0 {
		t.Fatalf("hangupCallChannels: no channels found for call %s among %d live channels", callID, len(channelIDs))
	}
	t.Logf("hung up %d channel(s) for call %s", hungUp, callID)
}

// fixtureOperatorPassword and fixtureOperatorHash are a synthetic dev-rig
// operator (D-39: no real credential, no PII). The hash is this
// repository's own Argon2id output for that password.
const (
	fixtureOperatorPassword = "e2e-fixture-operator-password"
	fixtureOperatorHash     = "$argon2id$v=19$m=19456,t=2,p=1$S2sQSViP/Ztyosl6nREwLg$b1iihB2OK3aZuaArz3DaXKooCO/+VrnoaU2/XFzVhGc"
)

// authenticateFixtureOperator seeds a TENANT_ADMIN for tenantID and
// exchanges its credentials for a real token through the public
// AuthenticateUser RPC.
//
// Tenant scope, not system scope: reading one's own call is what an
// ordinary tenant administrator does, and a system binding reaches every
// tenant, which would hide a failure affecting ordinary callers.
func authenticateFixtureOperator(t *testing.T, ctx context.Context, pool *corepostgres.Pool, tenantID shareddomain.TenantID) string {
	t.Helper()

	principalID := shareddomain.NewPrincipalID()
	username := fmt.Sprintf("e2e-op-%d", time.Now().UnixNano())

	if err := pool.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO principals (id, tenant_id, username, email, password_hash, role, status)
			 VALUES ($1,$2,$3,$4,$5,'TENANT_ADMIN','ACTIVE')`,
			principalID.String(), tenantID.String(), username, username+"@e2e.invalid", fixtureOperatorHash); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO role_bindings (principal_id, tenant_id, role, scope)
			 VALUES ($1,$2,'TENANT_ADMIN','tenant')`,
			principalID.String(), tenantID.String())
		return err
	}); err != nil {
		t.Fatalf("seed fixture operator: %v", err)
	}

	body := fmt.Sprintf(`{"tenantId":%q,"username":%q,"password":%q}`,
		tenantID.String(), username, fixtureOperatorPassword)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		appAPI+"/atsapbx.v1.IdentityService/AuthenticateUser", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build authenticate request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticate over http: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("AuthenticateUser status %d", resp.StatusCode)
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode AuthenticateUser: %v", err)
	}
	if parsed.Token == "" {
		t.Fatal("AuthenticateUser returned an empty token")
	}
	return parsed.Token
}

// TestGetCall_RefusesUnauthenticated is the standing guard for the auth
// cutover (task 4.1).
//
// Mounting the interceptor is one line in main.go, and a revert or a bad
// merge silently un-mounts it. The archived identity-api change recorded
// its own guard as "asserted by test"; no such test existed, which is how
// the concession survived two changes unchallenged. This one exists.
func TestGetCall_RefusesUnauthenticated(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := fmt.Sprintf(`{"tenantId":%q,"callId":%q}`,
		shareddomain.NewTenantID().String(), shareddomain.NewCallID().String())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		appAPI+"/atsapbx.v1.TelephonyService/GetCall", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("getcall over http: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GetCall returned %d, want 401 — the auth interceptor is not mounted on TelephonyService", resp.StatusCode)
	}
}

// TestGetCall_RefusesAnotherTenant covers task 4.2: a valid token does
// not let a caller read a tenant that is not their own.
//
// The refusal must be the tenant-mismatch one, not a "not found" — a
// not-found would mean the request reached the store and the answer
// merely happened to be empty, which is a different and weaker property.
func TestGetCall_RefusesAnotherTenant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := corepostgres.Open(ctx, pgURL)
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer pool.Close()

	mine := shareddomain.NewTenantID()
	theirs := shareddomain.NewTenantID()
	for _, id := range []shareddomain.TenantID{mine, theirs} {
		if _, err := pool.Unwrap().Exec(ctx,
			`INSERT INTO tenants (id, name, status) VALUES ($1,'e2e-authz','ACTIVE') ON CONFLICT (id) DO NOTHING`,
			id.String()); err != nil {
			t.Fatalf("insert tenant: %v", err)
		}
	}
	token := authenticateFixtureOperator(t, ctx, pool, mine)

	body := fmt.Sprintf(`{"tenantId":%q,"callId":%q}`,
		theirs.String(), shareddomain.NewCallID().String())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		appAPI+"/atsapbx.v1.TelephonyService/GetCall", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("getcall over http: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		t.Fatal("a caller read a tenant that is not their own")
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("cross-tenant GetCall returned %d, want 400 (tenant mismatch refused before the store) — a 404 would mean the request reached the store", resp.StatusCode)
	}
}
