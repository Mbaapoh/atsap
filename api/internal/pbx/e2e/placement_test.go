//go:build e2e

// Calls placed through the running app's public API, against live
// Asterisk (pbx-call-placement task 6). The app must be licensed
// (`make dev-licence`) — an unlicensed dev app is in Setup and refuses
// every call, which is the failure this change exists to end.
//
// The far ends are SIP agents run by this test on host ports: phones that
// register as API-created extensions, and a "carrier" an IP-authenticated
// trunk points at. Asterisk reaches the host at the Docker network's
// gateway address, which is resolved at runtime (E2E_HOST_ADDR overrides).
//
// The agents listen on 5070 and 5071 by default (E2E_PHONE_PORT,
// E2E_CARRIER_PORT) because a host firewall that admits the Docker bridge
// usually admits exactly the ports the UAT phones use: on the reference
// dev host, ufw allows UDP 5070-5071 and 18000-18039 from 172.18.0.0/16
// and drops everything else, silently — Asterisk retransmits INVITEs into
// the void and the only symptom is a call that never answers.
// They signal SIP and answer with SDP; endpoint-measured media is the UAT
// tier's job (make uat-placement), with real baresip phones.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostAddr is the address Asterisk, inside Docker, reaches this host by.
func hostAddr(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("E2E_HOST_ADDR"); v != "" {
		return v
	}
	container := os.Getenv("ASTERISK_CONTAINER")
	if container == "" {
		container = "deploy-asterisk-1"
	}
	out, err := exec.Command("docker", "inspect", container, // #nosec G204 -- fixed test command
		"--format", "{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}").Output()
	require.NoError(t, err, "cannot resolve the address Asterisk reaches the host by; set E2E_HOST_ADDR")
	addr := strings.TrimSpace(string(out))
	require.NotEmpty(t, addr, "the engine container has no network gateway; set E2E_HOST_ADDR")
	return addr
}

// agent is a SIP endpoint on a host port: a phone or a carrier. It
// answers INVITEs with 200 + SDP, or — when ringOnly — holds them at 180,
// as a phone nobody picks up does.
//
// For the inbound tier it also dials: dial places an outbound INVITE
// (optionally digest-authenticated) and returns the final status, and
// hangupAnswered ends a call this agent answered. Dialog state for both
// directions is tracked so either end can hang up.
type agent struct {
	host     string
	port     int
	user     string
	ringOnly atomic.Bool

	mu      sync.Mutex
	invites []string // Request-URI user of each INVITE received
	byes    atomic.Int32
	// inbound dialogs this agent answered, by Call-ID: enough to BYE them.
	answered map[string]*uasDialog
}

type uasDialog struct {
	callID        string
	remoteFrom    string // From header value of the INVITE, with tag
	localTo       string // To header value of the ACK, with our tag
	remoteContact string // Contact URI of the INVITE
	cseq          uint32
}

func startAgent(t *testing.T, host string, port int, user string) *agent {
	t.Helper()
	a := &agent{host: host, port: port, user: user, answered: map[string]*uasDialog{}}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent(user))
	require.NoError(t, err)
	srv, err := sipgo.NewServer(ua)
	require.NoError(t, err)

	srv.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		callID := req.CallID().Value()
		a.mu.Lock()
		a.invites = append(a.invites, req.Recipient.User)
		a.answered[callID] = &uasDialog{
			callID:        callID,
			remoteFrom:    req.GetHeader("From").Value(),
			remoteContact: req.GetHeader("Contact").Value(),
			cseq:          req.CSeq().SeqNo,
		}
		a.mu.Unlock()
		_ = tx.Respond(sip.NewResponseFromRequest(req, 180, "Ringing", nil))
		if a.ringOnly.Load() {
			return
		}
		res := sip.NewResponseFromRequest(req, 200, "OK", []byte(a.sdp()))
		res.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
		res.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s:%d>", a.user, a.host, a.port)))
		_ = tx.Respond(res)
	})
	srv.OnAck(func(req *sip.Request, _ sip.ServerTransaction) {
		// The ACK carries our To tag: the dialog is now confirmed and
		// either end may BYE it.
		a.mu.Lock()
		defer a.mu.Unlock()
		if d, ok := a.answered[req.CallID().Value()]; ok {
			d.localTo = req.GetHeader("To").Value()
		}
	})
	srv.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
		a.byes.Add(1)
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnCancel(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})
	srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.ListenAndServe(ctx, "udp", fmt.Sprintf("0.0.0.0:%d", port)) }()
	t.Cleanup(func() { cancel(); _ = srv.Close(); _ = ua.Close() })
	time.Sleep(100 * time.Millisecond) // let the socket bind before anything dials it
	return a
}

func envPort(t *testing.T, name string, def int) int {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	var p int
	_, err := fmt.Sscanf(v, "%d", &p)
	require.NoError(t, err, "%s must be a port number", name)
	return p
}

// sdp offers PCMU on a port nothing reads: the call signals and bridges;
// whether audio flows is measured on real phones in UAT.
func (a *agent) sdp() string {
	return fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 %[1]s\r\ns=-\r\nc=IN IP4 %[1]s\r\nt=0 0\r\n"+
		"m=audio %[2]d RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\na=sendrecv\r\n", a.host, 18030+a.port%2)
}

func (a *agent) invited() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.invites...)
}

// registerAgent registers an API-created extension with Contact at the
// agent, so Asterisk sends that extension's INVITEs to it.
func registerAgent(t *testing.T, a *agent, user, secret string) {
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
	req.RemoveHeader("From")
	req.AppendHeader(sip.NewHeader("From", fmt.Sprintf("<sip:%s@%s>", user, localIP)))
	req.RemoveHeader("To")
	req.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", user, sipServer)))
	req.RemoveHeader("Contact")
	req.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s:%d>", user, a.host, a.port)))
	req.AppendHeader(sip.NewHeader("Expires", "300"))
	req.SetTransport("UDP")

	tx, err := client.TransactionRequest(ctx, req, sipgo.ClientRequestRegisterBuild)
	require.NoError(t, err)
	defer tx.Terminate()
	res, err := waitResponse(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, res.StatusCode)

	chal, err := digest.ParseChallenge(res.GetHeader("WWW-Authenticate").Value())
	require.NoError(t, err)
	cred, err := digest.Digest(chal, digest.Options{Method: "REGISTER", URI: sipServer, Username: user, Password: secret})
	require.NoError(t, err)
	req2 := req.Clone()
	req2.RemoveHeader("Via")
	req2.AppendHeader(sip.NewHeader("Authorization", cred.String()))
	tx2, err := client.TransactionRequest(ctx, req2, sipgo.ClientRequestIncreaseCSEQ, sipgo.ClientRequestAddVia)
	require.NoError(t, err)
	defer tx2.Terminate()
	res2, err := waitResponse(ctx, tx2)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res2.StatusCode, "the extension must register with its generated secret")
}

// placementRig is a tenant with one registered phone, a carrier and an
// IP-authenticated trunk to it.
type placementRig struct {
	*rig
	host    string
	phone   *agent
	carrier *agent
	ext     extension
	trunkID string
}

// newPlacementRig starts the phone, and the carrier unless withCarrier is
// false (an internal call needs the second allowed port for its callee).
func newPlacementRig(t *testing.T, withCarrier bool, channelLimit int) *placementRig {
	t.Helper()
	r := newRig(t)
	t.Cleanup(func() {
		// Trunks through the API first, so their engine projection —
		// identify row included — goes with them. A SQL delete here left
		// orphaned identify rows matching this host's address, which
		// captured later tests' SIP traffic.
		r.releaseConfiguration()
		_ = r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
			for _, stmt := range []string{
				`DELETE FROM placed_call_legs WHERE tenant_id = $1`,
				`DELETE FROM carrier_routes WHERE tenant_id = $1`,
				`DELETE FROM carrier_trunks WHERE tenant_id = $1`,
			} {
				if _, err := tx.Exec(ctx, stmt, r.tenant.String()); err != nil {
					return err
				}
			}
			return nil
		})
	})
	p := &placementRig{rig: r, host: hostAddr(t)}

	var secret string
	p.ext, secret = r.createExtension(t, uniqueNumber())
	phonePort, carrierPort := envPort(t, "E2E_PHONE_PORT", 5070), envPort(t, "E2E_CARRIER_PORT", 5071)
	p.phone = startAgent(t, p.host, phonePort, p.ext.AuthUsername)
	registerAgent(t, p.phone, p.ext.AuthUsername, secret)
	require.Eventually(t, func() bool { return r.registrationStatus(t, p.ext.ExtensionID) == "REGISTRATION_STATUS_REGISTERED" },
		5*time.Second, 200*time.Millisecond, "the phone must show as registered before it can place a call")

	if withCarrier {
		p.carrier = startAgent(t, p.host, carrierPort, "carrier")
	} else {
		p.carrier = &agent{} // never dialled; asserted unused
	}
	var trunk struct {
		Trunk trunkMsg `json:"trunk"`
	}
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "e2e carrier", "host": p.host, "port": carrierPort,
		"channelLimit": channelLimit,
	}, &trunk, http.StatusOK)
	p.trunkID = trunk.Trunk.TrunkID
	for _, prefix := range []string{"+44", "11"} {
		r.call(t, "PbxService/CreateRoute", r.token, map[string]any{
			"tenantId": r.tenant.String(), "prefixPattern": prefix, "trunkId": p.trunkID,
		}, nil, http.StatusOK)
	}
	return p
}

// place calls PlaceCall and returns the call id, or "" with the error body
// when wantStatus is not 200.
func (p *placementRig) place(t *testing.T, from extension, dest string, wantStatus int) (string, string) {
	t.Helper()
	var out struct {
		CallID string `json:"callId"`
	}
	var target any = &out
	if wantStatus != http.StatusOK {
		target = nil
	}
	body := p.call(t, "PbxService/PlaceCall", p.token, map[string]any{
		"tenantId": p.tenant.String(), "extensionId": from.ExtensionID, "destination": dest,
	}, target, wantStatus)
	return out.CallID, string(body)
}

func (p *placementRig) state(t *testing.T, callID string) string {
	t.Helper()
	var out struct {
		State string `json:"state"`
	}
	p.call(t, "TelephonyService/GetCall", p.token, map[string]any{
		"tenantId": p.tenant.String(), "callId": callID,
	}, &out, http.StatusOK)
	return out.State
}

func (p *placementRig) hangup(t *testing.T, callID string) {
	t.Helper()
	p.call(t, "PbxService/HangupCall", p.token, map[string]any{
		"tenantId": p.tenant.String(), "callId": callID,
	}, nil, http.StatusOK)
}

// --- 6.1 -------------------------------------------------------------------

func TestPlaceCall_OutboundOverATrunkConnectsAndHangsUp(t *testing.T) {
	p := newPlacementRig(t, true, 4)

	callID, _ := p.place(t, p.ext, "+442071234567", http.StatusOK)
	require.Eventually(t, func() bool { return p.state(t, callID) == "Active" }, 10*time.Second, 200*time.Millisecond,
		"both legs answered: the call must be Active")
	assert.Contains(t, p.phone.invited(), p.ext.AuthUsername, "the caller's phone was rung")
	assert.Equal(t, []string{"442071234567"}, p.carrier.invited(), "the carrier received the dialled digits, + dropped")

	p.hangup(t, callID)
	require.Eventually(t, func() bool { return p.state(t, callID) == "Terminated" }, 5*time.Second, 200*time.Millisecond)
	require.Eventually(t, func() bool { return p.phone.byes.Load() >= 1 && p.carrier.byes.Load() >= 1 },
		5*time.Second, 100*time.Millisecond,
		"HangupCall must release BOTH legs on the engine: each far end receives a BYE, not just a Terminated record")
	p.hangup(t, callID) // idempotent

	var live int
	require.NoError(t, p.pool.WithTenant(context.Background(), p.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM placed_call_legs WHERE released_at IS NULL`).Scan(&live)
	}))
	assert.Zero(t, live, "the trunk channel is freed")
}

// --- 6.2 -------------------------------------------------------------------

func TestPlaceCall_InternalCallConnectsWithoutATrunk(t *testing.T) {
	p := newPlacementRig(t, false, 4)
	callee, secret := p.createExtension(t, uniqueNumber())
	calleePhone := startAgent(t, p.host, envPort(t, "E2E_CARRIER_PORT", 5071), callee.AuthUsername)
	registerAgent(t, calleePhone, callee.AuthUsername, secret)

	callID, _ := p.place(t, p.ext, callee.Number, http.StatusOK)
	require.Eventually(t, func() bool { return p.state(t, callID) == "Active" }, 10*time.Second, 200*time.Millisecond)
	assert.NotEmpty(t, calleePhone.invited(), "the other extension rang")
	assert.Empty(t, p.carrier.invited(), "no trunk was used")
	p.hangup(t, callID)
}

func TestPlaceCall_RefusalsThroughTheWire(t *testing.T) {
	p := newPlacementRig(t, true, 1)

	_, body := p.place(t, p.ext, "+33123456789", http.StatusBadRequest)
	assert.Contains(t, body, "unroutable")

	// channel_limit 1: a ringing call holds the only channel.
	p.carrier.ringOnly.Store(true)
	first, _ := p.place(t, p.ext, "+442071234567", http.StatusOK)
	_, body = p.place(t, p.ext, "+442071234568", http.StatusBadRequest)
	assert.Contains(t, body, "trunk capacity")
	p.hangup(t, first)

	p.carrier.ringOnly.Store(false)
	second, _ := p.place(t, p.ext, "+442071234569", http.StatusOK)
	p.hangup(t, second)
}

// An emergency call connects with licensed capacity exhausted. Capacity
// is exhausted for real: calls left ringing hold licensed channels until
// Screening refuses an ordinary call, and only then is 112 dialled.
func TestPlaceCall_EmergencyConnectsWithCapacityExhausted(t *testing.T) {
	p := newPlacementRig(t, true, 64)
	p.phone.ringOnly.Store(true)
	p.carrier.ringOnly.Store(true)

	var held []string
	t.Cleanup(func() {
		for _, id := range held {
			p.call(t, "PbxService/HangupCall", p.token, map[string]any{"tenantId": p.tenant.String(), "callId": id}, nil, http.StatusOK)
		}
	})

	refused := false
	for i := 0; i < 40 && !refused; i++ {
		var out struct {
			CallID string `json:"callId"`
		}
		payload := map[string]any{"tenantId": p.tenant.String(), "extensionId": p.ext.ExtensionID,
			"destination": fmt.Sprintf("+4420700000%02d", i)}
		raw := p.callAny(t, "PbxService/PlaceCall", payload, &out)
		switch {
		case out.CallID != "":
			held = append(held, out.CallID)
		case strings.Contains(raw, "capacity verdict"):
			refused = true
		default:
			t.Fatalf("unexpected placement result: %s", raw)
		}
	}
	require.True(t, refused, "licensed capacity must run out within 40 ringing calls (dev licence: 16 + burst)")

	p.phone.ringOnly.Store(false)
	p.carrier.ringOnly.Store(false)
	callID, _ := p.place(t, p.ext, "112", http.StatusOK)
	held = append(held, callID)
	require.Eventually(t, func() bool { return p.state(t, callID) == "Active" }, 10*time.Second, 200*time.Millisecond,
		"an emergency call connects even with capacity exhausted (INV-01)")
}

// callAny posts without asserting a status, for loops that expect
// either a call or a refusal, and decodes a success into out.
func (p *placementRig) callAny(t *testing.T, procedure string, body map[string]any, out any) string {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, apiBase+"/atsapbx.v1."+procedure, bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := e2eHTTP.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.Unmarshal(buf.Bytes(), out))
	}
	return buf.String()
}
