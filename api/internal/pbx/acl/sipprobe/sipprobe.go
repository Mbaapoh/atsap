// Package sipprobe checks a SIP carrier the way a SIP element checks a
// peer: one OPTIONS request over UDP, answered or not (RFC 3261 §11). It
// is how the platform observes a trunk's health itself, because Asterisk
// does not qualify realtime AORs (pbx-trunk-health-probing design D1,
// D-74). It speaks only as much SIP as that needs, on the standard
// library: no SIP stack is added for one request.
package sipprobe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"atsap-api/internal/pbx/domain"
	"atsap-api/internal/pbx/ports"
)

// Resolver resolves a carrier's host name; *net.Resolver satisfies it.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Result is what one check found: the outcome, and for an answer its
// final status code and the round trip from the first request sent. It is
// pbx-core's CarrierCheck, so a *Prober is a ports.CarrierProbe.
type Result = ports.CarrierCheck

var _ ports.CarrierProbe = (*Prober)(nil)

// Defaults for a check.
const (
	// DefaultTimeout bounds a whole check, retransmissions included.
	DefaultTimeout = 4 * time.Second
	// DefaultFirstRetransmit is RFC 3261's T1: UDP is unreliable, so the
	// request is sent again at T1, 2·T1, 4·T1… until the budget is spent.
	DefaultFirstRetransmit = 500 * time.Millisecond
	// resolveTimeout bounds the name lookup within the check.
	resolveTimeout = 2 * time.Second
)

// Prober sends checks. The zero value uses the system resolver and the
// defaults above.
type Prober struct {
	Resolver        Resolver
	Timeout         time.Duration
	FirstRetransmit time.Duration
}

// Probe checks the carrier at host:port. It never returns an error: every
// failure is an outcome the caller records.
func (p *Prober) Probe(ctx context.Context, host string, port int) Result {
	timeout, t1 := p.Timeout, p.FirstRetransmit
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if t1 <= 0 {
		t1 = DefaultFirstRetransmit
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ip, outcome := p.resolve(ctx, host)
	if outcome != domain.ProbeAnswered {
		return Result{Outcome: outcome}
	}
	conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: ip, Port: port})
	if err != nil {
		return Result{Outcome: domain.ProbeNoResponse}
	}
	defer func() { _ = conn.Close() }()

	callID := randomHex(16) + "@atsapbx"
	req := optionsRequest(host, port, conn.LocalAddr().(*net.UDPAddr), callID)
	return exchange(ctx, conn, req, callID, t1)
}

// resolve returns the address to check, preferring IPv4, or HostNotFound
// when the name cannot be resolved — whether DNS says it does not exist
// or the lookup fails: a mistyped name often fails as a temporary error
// rather than a clean NXDOMAIN, and the useful report is the same.
func (p *Prober) resolve(ctx context.Context, host string) (net.IP, domain.ProbeOutcome) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, domain.ProbeAnswered
	}
	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	lookupCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	addrs, err := resolver.LookupIPAddr(lookupCtx, host)
	if err != nil || len(addrs) == 0 {
		return nil, domain.ProbeHostNotFound
	}
	for _, a := range addrs {
		if v4 := a.IP.To4(); v4 != nil {
			return v4, domain.ProbeAnswered
		}
	}
	return addrs[0].IP, domain.ProbeAnswered
}

// exchange sends req, retransmitting on RFC 3261's schedule, until a
// final response for callID arrives or ctx ends.
func exchange(ctx context.Context, conn *net.UDPConn, req []byte, callID string, t1 time.Duration) Result {
	start := time.Now()
	deadline, _ := ctx.Deadline()
	interval, nextSend := t1, start
	buf := make([]byte, 8192)
	for {
		if ctx.Err() != nil {
			return Result{Outcome: domain.ProbeNoResponse}
		}
		if !time.Now().Before(nextSend) {
			if _, err := conn.Write(req); err != nil {
				return Result{Outcome: domain.ProbeNoResponse}
			}
			nextSend = time.Now().Add(interval)
			interval *= 2
		}
		wait := nextSend
		if deadline.Before(wait) {
			wait = deadline
		}
		if err := conn.SetReadDeadline(wait); err != nil {
			return Result{Outcome: domain.ProbeNoResponse}
		}
		n, err := conn.Read(buf)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue // time to retransmit, or the budget is spent
			}
			// A refusal (ICMP port unreachable) or a closed socket: nothing
			// is listening, and waiting longer will not change that.
			return Result{Outcome: domain.ProbeNoResponse}
		}
		status, ok := finalStatus(buf[:n], callID)
		if !ok {
			continue // provisional, malformed, or another call's
		}
		return Result{Outcome: domain.ProbeAnswered, Status: status, RTT: time.Since(start)}
	}
}

// optionsRequest is an OPTIONS request to host:port from local (RFC 3261
// §8.1.1): a new branch, tag and Call-ID per check, and rport so a
// response finds its way back through NAT.
func optionsRequest(host string, port int, local *net.UDPAddr, callID string) []byte {
	target := "sip:" + net.JoinHostPort(host, strconv.Itoa(port))
	from := net.JoinHostPort(local.IP.String(), strconv.Itoa(local.Port))
	var b strings.Builder
	fmt.Fprintf(&b, "OPTIONS %s SIP/2.0\r\n", target)
	fmt.Fprintf(&b, "Via: SIP/2.0/UDP %s;branch=z9hG4bK%s;rport\r\n", from, randomHex(8))
	b.WriteString("Max-Forwards: 70\r\n")
	fmt.Fprintf(&b, "From: <sip:health@%s>;tag=%s\r\n", from, randomHex(6))
	fmt.Fprintf(&b, "To: <%s>\r\n", target)
	fmt.Fprintf(&b, "Call-ID: %s\r\n", callID)
	b.WriteString("CSeq: 1 OPTIONS\r\n")
	b.WriteString("User-Agent: AtsaPBX trunk check\r\n")
	b.WriteString("Accept: application/sdp\r\n")
	b.WriteString("Content-Length: 0\r\n\r\n")
	return []byte(b.String())
}

// finalStatus reads a response's status code when it is a final (≥ 200)
// response to the request with callID.
func finalStatus(msg []byte, callID string) (int, bool) {
	head, _, _ := strings.Cut(string(msg), "\r\n\r\n")
	lines := strings.Split(head, "\r\n")
	if len(lines) == 0 {
		return 0, false
	}
	fields := strings.Fields(lines[0])
	if len(fields) < 2 || fields[0] != "SIP/2.0" {
		return 0, false
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil || status < 200 || status > 699 {
		return 0, false
	}
	for _, line := range lines[1:] {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		// "i" is Call-ID's compact form (RFC 3261 §7.3.3).
		if strings.EqualFold(name, "Call-ID") || name == "i" {
			return status, strings.TrimSpace(value) == callID
		}
	}
	return 0, false
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
