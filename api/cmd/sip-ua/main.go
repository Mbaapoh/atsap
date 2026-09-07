// Command sip-ua is a standalone auto-answer SIP UA sidecar: the
// answering party for the telephony-core walking-skeleton e2e test
// (openspec change telephony-core-originate-bridge-hangup, task 10.2).
//
// It exists because host-resident in-process UAs are not reachable from
// the compose network in this environment (ufw drops UDP from the
// compose bridge into host-published ports), so the answering side must
// itself be a container on the `voip` network. It registers as PJSIP
// dev fixtures 1000 and 1001, answers every INVITE it receives, and
// otherwise does nothing — the e2e test hangs both legs up via ARI, not
// through this process.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
)

const (
	listenPort = "5070"
	password   = "devpassword123"
)

var extensions = []string{"1000", "1001"}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	sipServer := os.Getenv("SIP_SERVER")
	if sipServer == "" {
		sipServer = "asterisk:5060"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The Contact we advertise must be an IP Asterisk can actually route
	// back to on the voip network — never host.docker.internal, which
	// only resolves from the host side of the bridge. A UDP dial to the
	// SIP server (no packet sent, just route resolution) yields the
	// local address the kernel would use to reach it, i.e. this
	// container's own IP on that network.
	localIP, err := discoverLocalIP(sipServer)
	if err != nil {
		logger.Error("discover local IP", "error", err)
		os.Exit(1)
	}
	logger.Info("sip-ua starting", "sip_server", sipServer, "advertised_ip", localIP)

	ua, err := sipgo.NewUA()
	if err != nil {
		logger.Error("sipgo NewUA", "error", err)
		os.Exit(1)
	}
	client, err := sipgo.NewClient(ua, sipgo.WithClientHostname(localIP))
	if err != nil {
		logger.Error("sipgo NewClient", "error", err)
		os.Exit(1)
	}
	srv, err := sipgo.NewServer(ua)
	if err != nil {
		logger.Error("sipgo NewServer", "error", err)
		os.Exit(1)
	}
	contact := sip.ContactHeader{Address: sip.Uri{Host: localIP, Port: 5070}}
	dialogSrv := sipgo.NewDialogServerCache(client, contact)

	srv.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		logger.Info("invite received", "recipient", req.Recipient.String(), "source", req.Source())
		d, err := dialogSrv.ReadInvite(req, tx)
		if err != nil {
			logger.Warn("read invite", "error", err)
			return
		}
		if err := d.Respond(180, "Ringing", nil); err != nil {
			logger.Warn("180 failed", "error", err)
			return
		}
		// Asterisk's INVITE carries a real SDP offer (immediate offer,
		// not delayed) — a bodyless 200 OK leaves the offer/answer cycle
		// incomplete, and Asterisk hangs the channel up itself almost
		// immediately with cause 127 ("Interworking, unspecified"), BYE
		// Reason "SDP offer/answer incomplete" (observed directly).
		// This test doesn't need real audio, but the answer must be
		// syntactically valid and reachable-looking (our own IP, an RTP
		// port in the configured range) for Asterisk to accept it.
		if err := d.RespondSDP(sdpAnswer(localIP)); err != nil {
			logger.Warn("200 failed", "error", err)
			return
		}
		logger.Info("call answered", "call_id", req.CallID().Value())
		<-d.Context().Done()
		logger.Info("call ended", "call_id", req.CallID().Value())
	})
	srv.OnAck(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = dialogSrv.ReadAck(req, tx)
	})
	srv.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = dialogSrv.ReadBye(req, tx)
	})

	listenErr := make(chan error, 1)
	go func() { listenErr <- srv.ListenAndServe(ctx, "udp", "0.0.0.0:"+listenPort) }()
	select {
	case err := <-listenErr:
		logger.Error("listen failed", "error", err)
		os.Exit(1)
	case <-time.After(500 * time.Millisecond):
	}
	logger.Info("listening", "addr", "0.0.0.0:"+listenPort)

	for _, ext := range extensions {
		if err := registerWithRetry(ctx, client, sipServer, localIP, ext, logger); err != nil {
			logger.Error("register failed", "extension", ext, "error", err)
			os.Exit(1)
		}
		logger.Info("registered", "extension", ext)
	}

	logger.Info("sip-ua ready")
	go renewRegistrations(ctx, client, sipServer, localIP, logger)

	<-ctx.Done()
	logger.Info("shutting down")
	_ = client.Close()
	_ = srv.Close()
	_ = ua.Close()
}

// sdpRTPPort is a static port within rtp.conf's configured range
// (10000-20000). No real audio is needed for this test (task 10.2 only
// asserts call state, usage ticks, and events, per LLD-01 §8's DoD) — an
// unbound port here answers the offer/answer cycle without pretending to
// carry real media.
const sdpRTPPort = 20000

// sdpAnswer builds a minimal, valid SDP answer accepting PCMU (payload
// 0), matching Asterisk's offer and pjsip.conf's `allow = ulaw,alaw`.
func sdpAnswer(localIP string) []byte {
	return []byte(fmt.Sprintf(
		"v=0\r\n"+
			"o=- 0 0 IN IP4 %s\r\n"+
			"s=sip-ua\r\n"+
			"c=IN IP4 %s\r\n"+
			"t=0 0\r\n"+
			"m=audio %d RTP/AVP 0\r\n"+
			"a=rtpmap:0 PCMU/8000\r\n"+
			"a=sendrecv\r\n",
		localIP, localIP, sdpRTPPort))
}

// discoverLocalIP returns the local address the kernel would use to
// reach sipServer, without sending any packet (UDP dial only resolves
// routing).
func discoverLocalIP(sipServer string) (string, error) {
	conn, err := net.Dial("udp", sipServer)
	if err != nil {
		return "", fmt.Errorf("dial %s: %w", sipServer, err)
	}
	defer func() { _ = conn.Close() }()
	host, _, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return "", fmt.Errorf("split local addr: %w", err)
	}
	return host, nil
}

// reRegisterInterval is how often registrations are renewed.
//
// Asterisk's AOR default_expiration is 3600s, and a registration is
// simply forgotten when it lapses — the endpoint then has no contact to
// dial, and an Originate to it fails with "Allocation failed" rather
// than anything that names the real cause. Renewing well inside that
// window (rather than at, say, 55 minutes) means a few consecutive
// failures still leave time to recover before the contact is lost.
const reRegisterInterval = 15 * time.Minute

// renewRegistrations re-registers every extension periodically for as
// long as the process runs.
//
// Without this the sidecar answers calls for an hour and then silently
// stops: registration lapses, Asterisk drops the contact, and the next
// originate fails in a way that looks like an Asterisk fault. Found
// exactly that way — the e2e suite failed after the container had been
// up five hours, and a restart "fixed" it.
func renewRegistrations(ctx context.Context, client *sipgo.Client, sipServer, localIP string, logger *slog.Logger) {
	ticker := time.NewTicker(reRegisterInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, ext := range extensions {
				// A failed renewal is logged and retried on the next
				// tick rather than being fatal: the existing
				// registration is still valid for a while yet, so
				// exiting here would turn a transient blip into an
				// outage.
				if err := register(ctx, client, sipServer, localIP, ext); err != nil {
					logger.Warn("re-registration failed, will retry", "extension", ext, "error", err)
					continue
				}
				logger.Debug("re-registered", "extension", ext)
			}
		}
	}
}

// registerWithRetry retries register: at compose startup Asterisk is
// only guaranteed service_started, not healthy, so early REGISTERs can
// hit a SIP stack that isn't listening yet.
func registerWithRetry(ctx context.Context, client *sipgo.Client, sipServer, localIP, user string, logger *slog.Logger) error {
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := register(ctx, client, sipServer, localIP, user); err != nil {
			lastErr = err
			logger.Warn("register attempt failed, retrying", "extension", user, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("register %s: retries exhausted: %w", user, lastErr)
}

// register performs a SIP REGISTER for user against sipServer, handling
// the digest challenge Asterisk issues for the dev-fixture endpoints.
func register(ctx context.Context, client *sipgo.Client, sipServer, localIP, user string) error {
	var recipient sip.Uri
	if err := sip.ParseUri(fmt.Sprintf("sip:%s@%s", user, sipServer), &recipient); err != nil {
		return fmt.Errorf("parse register uri: %w", err)
	}
	req := sip.NewRequest(sip.REGISTER, recipient)
	// Asterisk matches the REGISTER to endpoint 1000/1001 by the From/To
	// user; the UA's default identity matches nothing. Override all
	// three user-bearing headers with the extension identity, advertised
	// at our discovered routable IP.
	req.RemoveHeader("From")
	req.AppendHeader(sip.NewHeader("From", fmt.Sprintf("<sip:%s@%s>", user, localIP)))
	req.RemoveHeader("To")
	req.AppendHeader(sip.NewHeader("To", fmt.Sprintf("<sip:%s@%s>", user, sipServer)))
	req.RemoveHeader("Contact")
	req.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<sip:%s@%s:%s>", user, localIP, listenPort)))
	req.SetTransport("UDP")

	tx, err := client.TransactionRequest(ctx, req, sipgo.ClientRequestRegisterBuild)
	if err != nil {
		return fmt.Errorf("register request: %w", err)
	}
	defer tx.Terminate()
	res, err := waitResponse(ctx, tx)
	if err != nil {
		return err
	}
	if res.StatusCode == 401 {
		challenge := res.GetHeader("WWW-Authenticate")
		chal, err := digest.ParseChallenge(challenge.Value())
		if err != nil {
			return fmt.Errorf("parse challenge: %w", err)
		}
		cred, err := digest.Digest(chal, digest.Options{
			Method: req.Method.String(), URI: sipServer, Username: user, Password: password,
		})
		if err != nil {
			return fmt.Errorf("digest: %w", err)
		}
		req2 := req.Clone()
		req2.RemoveHeader("Via")
		req2.AppendHeader(sip.NewHeader("Authorization", cred.String()))
		tx2, err := client.TransactionRequest(ctx, req2, sipgo.ClientRequestIncreaseCSEQ, sipgo.ClientRequestAddVia)
		if err != nil {
			return fmt.Errorf("register retry: %w", err)
		}
		defer tx2.Terminate()
		res, err = waitResponse(ctx, tx2)
		if err != nil {
			return err
		}
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("register %s: got %d", user, res.StatusCode)
	}
	return nil
}

func waitResponse(ctx context.Context, tx sip.ClientTransaction) (*sip.Response, error) {
	select {
	case res := <-tx.Responses():
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-tx.Done():
		return nil, fmt.Errorf("transaction done without response")
	}
}
