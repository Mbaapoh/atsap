//go:build e2e

// Calls the platform did not originate itself, against the running
// licensed app and live Asterisk (pbx-inbound-and-emergency task 6):
// a carrier INVITE to a DID rings the assigned extension, and a
// registered phone dials out, dials internally, and dials emergency
// under exhausted capacity. Refusals are final SIP responses, never
// silence — each test asserts the code on the wire.
//
// The agents on 5070/5071 are both UAS (they answer) and UAC (they
// dial and hang up): the carrier dials DIDs without credentials (the
// trunk authenticates by source IP), the phone dials with its generated
// secret. Call progress is observed two ways: the SIP final response on
// the dialling leg, and the persisted call state in Postgres.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
)

// outboundDialog is a UAC dialog this agent opened: enough to ACK the
// answer and to BYE the call later.
type outboundDialog struct {
	client       *sipgo.Client
	ua           *sipgo.UserAgent
	callID       string
	fromTag      string
	toTag        string
	remoteTarget string
	localContact string
	fromHeader   string
	toHeaderBase string
	cseq         uint32
	provisionals []int
}

// dial places an outbound INVITE from the agent's port to sip:to at the
// engine and returns the final status code. With a secret it answers a
// digest challenge (a registered phone); without one it dials bare (an
// IP-authenticated carrier). A 2xx is ACKed and the dialog kept for a
// later hangup.
func (a *agent) dial(t *testing.T, to, authUser, authSecret string) (int, *outboundDialog) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	ua, err := sipgo.NewUA(sipgo.WithUserAgent(a.user + "-uac"))
	require.NoError(t, err)
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname(a.host))
	require.NoError(t, err)

	var recipient sip.Uri
	require.NoError(t, sip.ParseUri(fmt.Sprintf("sip:%s@%s", to, sipServer), &recipient))
	newInvite := func() *sip.Request {
		req := sip.NewRequest(sip.INVITE, recipient)
		req.RemoveHeader("From")
		req.AppendHeader(sip.NewHeader("From", fmt.Sprintf("<sip:%s@%s>", authUser, a.host)))
		req.RemoveHeader("To")
		req.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", to, sipServer)))
		req.RemoveHeader("Contact")
		req.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s:%d>", authUser, a.host, a.port)))
		req.SetTransport("UDP")
		req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
		req.SetBody([]byte(a.sdp()))
		return req
	}

	dlg := &outboundDialog{client: client, ua: ua, localContact: fmt.Sprintf("<sip:%s@%s:%d>", authUser, a.host, a.port)}
	req := newInvite()
	authed := false
	for {
		// Default full build (Call-ID, CSeq, From tag, Max-Forwards):
		// AddVia alone leaves the request unbuilt and the stack
		// refuses it. The authenticated retry below keeps the same
		// Call-ID, From tag and CSeq by cloning.
		tx, err := client.TransactionRequest(ctx, req)
		require.NoError(t, err)
		final, done := func() (*sip.Response, bool) {
			for {
				res, err := waitResponse(ctx, tx)
				require.NoError(t, err)
				switch {
				case res.StatusCode < 200:
					dlg.provisionals = append(dlg.provisionals, res.StatusCode)
					continue
				case res.StatusCode == http.StatusUnauthorized && !authed && authSecret != "":
					return res, false
				default:
					return res, true
				}
			}
		}()
		tx.Terminate()
		if !done {
			chal, err := digest.ParseChallenge(final.GetHeader("WWW-Authenticate").Value())
			require.NoError(t, err, "the engine must challenge an extension dialling")
			cred, err := digest.Digest(chal, digest.Options{Method: "INVITE", URI: sipServer, Username: authUser, Password: authSecret})
			require.NoError(t, err)
			req = newInvite()
			req.RemoveHeader("Via")
			req.AppendHeader(sip.NewHeader("Authorization", cred.String()))
			authed = true
			continue
		}
		if final.StatusCode >= 200 && final.StatusCode < 300 {
			dlg.callID = final.CallID().Value()
			dlg.fromTag = headerParam(req.GetHeader("From").Value(), "tag")
			dlg.toTag = headerParam(final.GetHeader("To").Value(), "tag")
			dlg.remoteTarget = strings.Trim(final.GetHeader("Contact").Value(), " <>")
			dlg.fromHeader = req.GetHeader("From").Value()
			dlg.toHeaderBase = strings.Split(final.GetHeader("To").Value(), ";")[0]
			dlg.cseq = req.CSeq().SeqNo
			dlg.ack(t)
		} else {
			_ = ua.Close()
			_ = client.Close()
		}
		return final.StatusCode, dlg
	}
}

// ack confirms a 2xx to an INVITE this dialog opened.
func (d *outboundDialog) ack(t *testing.T) {
	t.Helper()
	var target sip.Uri
	require.NoError(t, sip.ParseUri(d.remoteTarget, &target))
	ack := sip.NewRequest(sip.ACK, target)
	ack.AppendHeader(sip.NewHeader("From", d.fromHeader))
	ack.AppendHeader(sip.NewHeader("To", d.toHeaderBase+";tag="+d.toTag))
	ack.AppendHeader(sip.NewHeader("Call-ID", d.callID))
	ack.AppendHeader(sip.NewHeader("CSeq", fmt.Sprintf("%d ACK", d.cseq)))
	ack.AppendHeader(sip.NewHeader("Contact", d.localContact))
	ack.SetTransport("UDP")
	require.NoError(t, d.client.WriteRequest(ack))
}

// hangup BYEs a dialog this agent opened and expects a 200.
func (d *outboundDialog) hangup(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() { _ = d.ua.Close(); _ = d.client.Close() }()

	d.cseq++
	var target sip.Uri
	require.NoError(t, sip.ParseUri(d.remoteTarget, &target))
	bye := sip.NewRequest(sip.BYE, target)
	bye.AppendHeader(sip.NewHeader("From", d.fromHeader))
	bye.AppendHeader(sip.NewHeader("To", d.toHeaderBase+";tag="+d.toTag))
	bye.AppendHeader(sip.NewHeader("Call-ID", d.callID))
	bye.AppendHeader(sip.NewHeader("CSeq", fmt.Sprintf("%d BYE", d.cseq)))
	bye.AppendHeader(sip.NewHeader("Contact", d.localContact))
	bye.SetTransport("UDP")
	tx, err := d.client.TransactionRequest(ctx, bye)
	require.NoError(t, err)
	defer tx.Terminate()
	res, err := waitResponse(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode, "BYE must be answered")
}

// hangupAnswered BYEs the most recent call this agent answered (UAS
// side): the dialog confirmed by the ACK it received.
func (a *agent) hangupAnswered(t *testing.T) {
	t.Helper()
	a.mu.Lock()
	var dlg *uasDialog
	for _, d := range a.answered {
		if d.localTo != "" && (dlg == nil || d.cseq > dlg.cseq) {
			c := *d
			dlg = &c
		}
	}
	a.mu.Unlock()
	require.NotNil(t, dlg, "no answered dialog to hang up")

	ua, err := sipgo.NewUA()
	require.NoError(t, err)
	defer func() { _ = ua.Close() }()
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname(a.host))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var target sip.Uri
	require.NoError(t, sip.ParseUri(strings.Trim(dlg.remoteContact, " <>"), &target))
	bye := sip.NewRequest(sip.BYE, target)
	// From is us (our To identity with our tag, as the ACK named it),
	// To is them (their From, tag included).
	bye.AppendHeader(sip.NewHeader("From", dlg.localTo))
	bye.AppendHeader(sip.NewHeader("To", dlg.remoteFrom))
	bye.AppendHeader(sip.NewHeader("Call-ID", dlg.callID))
	bye.AppendHeader(sip.NewHeader("CSeq", fmt.Sprintf("%d BYE", dlg.cseq+1)))
	bye.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s:%d>", a.user, a.host, a.port)))
	bye.SetTransport("UDP")
	tx, err := client.TransactionRequest(ctx, bye)
	require.NoError(t, err)
	defer tx.Terminate()
	res, err := waitResponse(ctx, tx)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, res.StatusCode, "BYE must be answered")
}

// headerParam reads a ";tag=xx" style parameter from a header value.
func headerParam(value, name string) string {
	for _, part := range strings.Split(value, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, name+"=") {
			return strings.TrimPrefix(part, name+"=")
		}
	}
	return ""
}

// --- rig -----------------------------------------------------------------

// inboundRig is a tenant with a registered phone, an IP-authenticated
// carrier trunk, an outbound route, one DID, and CM as its emergency
// country — the full arriving-call surface through the public API.
type inboundRig struct {
	*rig
	host     string
	phone    *agent
	phoneSec string
	ext      extension
	carrier  *agent
	trunkID  string
	did      string
}

func newInboundRig(t *testing.T, channelLimit int) *inboundRig {
	t.Helper()
	r := newRig(t)
	p := &inboundRig{rig: r, host: hostAddr(t)}
	t.Cleanup(func() {
		// Remove the trunk through the API first: that is what removes
		// its engine projection (including the identify row). A leaked
		// identify row matches every SIP packet from this host to the
		// dead trunk and poisons later tests. Best effort: the test
		// itself may have deleted the trunk already.
		p.callNoAssert(t, "PbxService/DeleteTrunk", map[string]any{
			"tenantId": p.tenant.String(), "trunkId": p.trunkID,
		})
		// The same for extensions (projection rows, plus the DIDs the
		// delete cascades): list and delete each, best effort.
		var listed struct {
			Extensions []struct {
				ExtensionID string `json:"extensionId"`
			} `json:"extensions"`
		}
		p.call(t, "PbxService/ListExtensions", p.token, map[string]any{
			"tenantId": p.tenant.String(), "limit": 100,
		}, &listed, http.StatusOK)
		for _, e := range listed.Extensions {
			p.callNoAssert(t, "PbxService/DeleteExtension", map[string]any{
				"tenantId": p.tenant.String(), "extensionId": e.ExtensionID,
			})
		}
		_ = r.pool.WithTenant(context.Background(), r.tenant, func(ctx context.Context, tx pgx.Tx) error {
			for _, stmt := range []string{
				`DELETE FROM placed_call_legs WHERE tenant_id = $1`,
				`DELETE FROM inbound_numbers WHERE tenant_id = $1`,
				`DELETE FROM emergency_numbers WHERE tenant_id = $1`,
				`DELETE FROM emergency_settings WHERE tenant_id = $1`,
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

	var secret string
	p.ext, secret = r.createExtension(t, uniqueNumber())
	p.phoneSec = secret
	phonePort, carrierPort := envPort(t, "E2E_PHONE_PORT", 5070), envPort(t, "E2E_CARRIER_PORT", 5071)
	p.phone = startAgent(t, p.host, phonePort, p.ext.AuthUsername)
	registerAgent(t, p.phone, p.ext.AuthUsername, secret)
	require.Eventually(t, func() bool { return r.registrationStatus(t, p.ext.ExtensionID) == "REGISTRATION_STATUS_REGISTERED" },
		5*time.Second, 200*time.Millisecond, "the phone must show as registered")
	p.carrier = startAgent(t, p.host, carrierPort, "carrier")

	var trunk struct {
		Trunk trunkMsg `json:"trunk"`
	}
	r.call(t, "PbxService/CreateTrunk", r.token, map[string]any{
		"tenantId": r.tenant.String(), "name": "e2e inbound carrier", "host": p.host, "port": carrierPort,
		"channelLimit": channelLimit,
	}, &trunk, http.StatusOK)
	p.trunkID = trunk.Trunk.TrunkID
	// "11" carries 112 and Cameroon's 117/118/119 to the carrier; "9"
	// makes 911 ROUTABLE, so the test can show it is ordinary for a CM
	// tenant (refused by capacity) rather than merely unroutable (404).
	for _, prefix := range []string{"+44", "11", "9"} {
		r.call(t, "PbxService/CreateRoute", r.token, map[string]any{
			"tenantId": r.tenant.String(), "prefixPattern": prefix, "trunkId": p.trunkID,
		}, nil, http.StatusOK)
	}

	p.did = "+442071234567"
	r.call(t, "PbxService/CreateInboundNumber", r.token, map[string]any{
		"tenantId": r.tenant.String(), "number": p.did, "extensionId": p.ext.ExtensionID,
	}, nil, http.StatusOK)
	r.call(t, "PbxService/SetEmergencyProfile", r.token, map[string]any{
		"tenantId": r.tenant.String(), "countryCode": "CM",
	}, nil, http.StatusOK)
	return p
}

// callNoAssert posts to the API, ignoring the status: cleanup that
// must not fail a test that already deleted the resource itself.
func (p *inboundRig) callNoAssert(t *testing.T, procedure string, body map[string]any) {
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
}

// callState polls the tenant's latest call until it reaches want.
func (p *inboundRig) callState(t *testing.T, want string) string {
	t.Helper()
	var id, state string
	require.Eventually(t, func() bool {
		// calls is RLS-protected: read it under the tenant, or every row
		// is invisible and the poll can never succeed. A call not yet
		// written is "not yet", not a failure.
		err := p.pool.WithTenant(context.Background(), p.tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT id, state FROM calls ORDER BY started_at DESC LIMIT 1`).Scan(&id, &state)
		})
		return err == nil && state == want
	}, 15*time.Second, 200*time.Millisecond, "latest call must reach %s (now %s)", want, state)
	return id
}

// exhaustCapacity holds ringing outbound calls until Screening refuses
// an ordinary call, and returns the held call ids for cleanup.
func (p *inboundRig) exhaustCapacity(t *testing.T) []string {
	t.Helper()
	p.phone.ringOnly.Store(true)
	p.carrier.ringOnly.Store(true)
	var held []string
	t.Cleanup(func() {
		for _, id := range held {
			p.call(t, "PbxService/HangupCall", p.token,
				map[string]any{"tenantId": p.tenant.String(), "callId": id}, nil, http.StatusOK)
		}
	})
	refused := false
	for i := 0; i < 40 && !refused; i++ {
		var out struct {
			CallID string `json:"callId"`
		}
		raw := func() string {
			payload, err := json.Marshal(map[string]any{"tenantId": p.tenant.String(),
				"extensionId": p.ext.ExtensionID, "destination": fmt.Sprintf("+4420700000%02d", i)})
			require.NoError(t, err)
			req, err := http.NewRequest(http.MethodPost, apiBase+"/atsapbx.v1.PbxService/PlaceCall", bytes.NewReader(payload))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+p.token)
			resp, err := e2eHTTP.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(resp.Body)
			if resp.StatusCode == http.StatusOK {
				require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
			}
			return buf.String()
		}()
		switch {
		case out.CallID != "":
			held = append(held, out.CallID)
		case strings.Contains(raw, "capacity verdict"):
			refused = true
		default:
			t.Fatalf("unexpected placement result: %s", raw)
		}
	}
	require.True(t, refused, "licensed capacity must run out within 40 ringing calls")
	p.phone.ringOnly.Store(false)
	p.carrier.ringOnly.Store(false)
	return held
}

// --- 6.1 -----------------------------------------------------------------

// A carrier INVITE to an assigned DID rings the extension and answers
// to Active; both ends hang up cleanly. Then an unassigned DID gets
// 404, and a deleted trunk's carrier is refused.
func TestInbound_CarrierCallToAssignedDIDRingsExtension(t *testing.T) {
	p := newInboundRig(t, 30)

	code, carrierDlg := p.carrier.dial(t, p.did, "carrier", "")
	require.Equal(t, http.StatusOK, code, "a call to an assigned DID connects")
	assert.Contains(t, carrierDlg.provisionals, 180, "the caller hears ringing while the destination sets up")
	assert.Contains(t, p.phone.invited(), p.ext.AuthUsername, "the assigned extension rang")
	callID := p.callState(t, "Active")

	carrierDlg.hangup(t)
	p.phone.hangupAnswered(t)
	p.callState(t, "Terminated")
	_ = callID

	unassigned, _ := p.carrier.dial(t, "+442071234599", "carrier", "")
	assert.Equal(t, http.StatusNotFound, unassigned, "an unallocated DID gets 404")

	p.call(t, "PbxService/DeleteTrunk", p.token, map[string]any{
		"tenantId": p.tenant.String(), "trunkId": p.trunkID,
	}, nil, http.StatusOK)
	deletedCode, _ := p.carrier.dial(t, p.did, "carrier", "")
	assert.NotEqual(t, http.StatusOK, deletedCode, "a deleted trunk's carrier is refused")
	t.Logf("deleted trunk's carrier received SIP %d", deletedCode)
}

// Capacity exhausted: a carrier call to an assigned DID gets 503 (a
// plain one — Retry-After is the recorded engine gap).
func TestInbound_CapacityExhaustedIs503(t *testing.T) {
	p := newInboundRig(t, 64)
	p.exhaustCapacity(t)

	code, _ := p.carrier.dial(t, p.did, "carrier", "")
	assert.Equal(t, http.StatusServiceUnavailable, code, "exhausted capacity is 503 at the SIP edge")
}

// --- 6.2 -----------------------------------------------------------------

// A registered phone dials out (carrier answers, Active), dials another
// extension (internal, Active), dials an unroutable number (404) — and,
// with capacity exhausted, dials a CM national emergency number
// (connects) while 911 is refused 503.
func TestDeskPhone_DialsOutInternalUnroutableAndEmergency(t *testing.T) {
	p := newInboundRig(t, 64)

	// External: the carrier answers.
	code, phoneDlg := p.phone.dial(t, "+442071234500", p.ext.AuthUsername, p.phoneSec)
	require.Equal(t, http.StatusOK, code, "a desk phone dials out over the trunk")
	assert.Equal(t, []string{"442071234500"}, p.carrier.invited(), "the carrier received the dialled digits, + dropped")
	p.callState(t, "Active")
	before := p.carrier.byes.Load()
	phoneDlg.hangup(t)
	// The caller hung up: the platform must release the far end itself.
	// The far end does not BYE in turn — its dialog is already over.
	require.Eventually(t, func() bool { return p.carrier.byes.Load() > before }, 5*time.Second, 100*time.Millisecond,
		"the far end must receive the platform's BYE when the caller hangs up")
	p.callState(t, "Terminated")

	// Internal: a second extension on the carrier's port (the trunk
	// needs no registration, so the port can host a phone too).
	bob, bobSec := p.createExtension(t, uniqueNumber())
	registerAgent(t, p.carrier, bob.AuthUsername, bobSec)
	code, bobDlg := p.phone.dial(t, bob.Number, p.ext.AuthUsername, p.phoneSec)
	require.Equal(t, http.StatusOK, code, "a desk phone dials another extension")
	p.callState(t, "Active")
	before = p.carrier.byes.Load()
	bobDlg.hangup(t)
	// The caller hung up: the platform must release the far end itself.
	// The far end does not BYE in turn — its dialog is already over.
	require.Eventually(t, func() bool { return p.carrier.byes.Load() > before }, 5*time.Second, 100*time.Millisecond,
		"the far end must receive the platform's BYE when the caller hangs up")
	p.callState(t, "Terminated")

	// Unroutable.
	code, _ = p.phone.dial(t, "333000111222", p.ext.AuthUsername, p.phoneSec)
	assert.Equal(t, http.StatusNotFound, code, "an unroutable dialled number gets 404")

	// Emergency under exhausted capacity.
	p.exhaustCapacity(t)
	code, emgDlg := p.phone.dial(t, "117", p.ext.AuthUsername, p.phoneSec)
	require.Equal(t, http.StatusOK, code, "a CM national emergency number connects with capacity exhausted")
	p.callState(t, "Active")
	before = p.carrier.byes.Load()
	emgDlg.hangup(t)
	// The caller hung up: the platform must release the far end itself.
	// The far end does not BYE in turn — its dialog is already over.
	require.Eventually(t, func() bool { return p.carrier.byes.Load() > before }, 5*time.Second, 100*time.Millisecond,
		"the far end must receive the platform's BYE when the caller hangs up")
	p.callState(t, "Terminated")

	code, _ = p.phone.dial(t, "911", p.ext.AuthUsername, p.phoneSec)
	assert.Equal(t, http.StatusServiceUnavailable, code, "911 is ordinary in CM: refused 503 while exhausted")
}
