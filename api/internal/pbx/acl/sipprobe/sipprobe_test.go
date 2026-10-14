package sipprobe

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"atsap-api/internal/pbx/domain"
)

// responder is a carrier on the loopback: for each request it receives it
// calls reply, and sends back whatever reply returns, in order.
type responder struct {
	conn     *net.UDPConn
	requests atomic.Int32
	mu       sync.Mutex
	first    string
}

func newResponder(t *testing.T, reply func(req string, n int) []string) *responder {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	r := &responder{conn: conn}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			req := string(buf[:n])
			count := int(r.requests.Add(1))
			r.mu.Lock()
			if r.first == "" {
				r.first = req
			}
			r.mu.Unlock()
			for _, resp := range reply(req, count) {
				_, _ = conn.WriteToUDP([]byte(resp), from)
			}
		}
	}()
	return r
}

func (r *responder) port() int { return r.conn.LocalAddr().(*net.UDPAddr).Port }

// response answers req with status, echoing its Call-ID unless callID is
// given.
func response(req string, status string, callID string) string {
	if callID == "" {
		callID = header(req, "Call-ID")
	}
	return "SIP/2.0 " + status + "\r\n" +
		"Via: " + header(req, "Via") + "\r\n" +
		"Call-ID: " + callID + "\r\n" +
		"CSeq: 1 OPTIONS\r\n" +
		"Content-Length: 0\r\n\r\n"
}

func header(msg, name string) string {
	for _, line := range strings.Split(msg, "\r\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fast() *Prober {
	return &Prober{Timeout: 600 * time.Millisecond, FirstRetransmit: 100 * time.Millisecond}
}

func TestProbe_AnswersAreAnswered(t *testing.T) {
	for _, status := range []string{"200 OK", "403 Forbidden", "404 Not Found", "503 Service Unavailable"} {
		t.Run(status, func(t *testing.T) {
			r := newResponder(t, func(req string, _ int) []string { return []string{response(req, status, "")} })
			got := fast().Probe(context.Background(), "127.0.0.1", r.port())
			assert.Equal(t, domain.ProbeAnswered, got.Outcome)
			assert.Equal(t, status[:3], strconv.Itoa(got.Status))
			assert.Positive(t, got.RTT)
			assert.Less(t, got.RTT, 500*time.Millisecond)
		})
	}
}

// The request is a well-formed OPTIONS addressed to the carrier, so a real
// carrier answers it (RFC 3261 §8.1.1, §11).
func TestProbe_SendsAWellFormedOptions(t *testing.T) {
	r := newResponder(t, func(req string, _ int) []string { return []string{response(req, "200 OK", "")} })
	fast().Probe(context.Background(), "127.0.0.1", r.port())

	r.mu.Lock()
	req := r.first
	r.mu.Unlock()
	lines := strings.Split(req, "\r\n")
	assert.Equal(t, "OPTIONS sip:127.0.0.1:"+strconv.Itoa(r.port())+" SIP/2.0", lines[0])
	assert.Regexp(t, `^SIP/2\.0/UDP 127\.0\.0\.1:\d+;branch=z9hG4bK[0-9a-f]+;rport$`, header(req, "Via"))
	assert.Equal(t, "70", header(req, "Max-Forwards"))
	assert.Regexp(t, `;tag=[0-9a-f]+$`, header(req, "From"))
	assert.Equal(t, "<sip:127.0.0.1:"+strconv.Itoa(r.port())+">", header(req, "To"))
	assert.NotEmpty(t, header(req, "Call-ID"))
	assert.Equal(t, "1 OPTIONS", header(req, "CSeq"))
	assert.Equal(t, "0", header(req, "Content-Length"))
	assert.True(t, strings.HasSuffix(req, "\r\n\r\n"), "headers end with an empty line")
}

// Provisional responses are not answers: the probe waits for the final one.
func TestProbe_WaitsPastProvisionalResponses(t *testing.T) {
	r := newResponder(t, func(req string, _ int) []string {
		return []string{response(req, "100 Trying", ""), response(req, "200 OK", "")}
	})
	got := fast().Probe(context.Background(), "127.0.0.1", r.port())
	assert.Equal(t, domain.ProbeAnswered, got.Outcome)
	assert.Equal(t, 200, got.Status)
}

// A packet for another call is not this check's answer.
func TestProbe_IgnoresAnotherCallsResponse(t *testing.T) {
	r := newResponder(t, func(req string, _ int) []string {
		return []string{response(req, "503 Service Unavailable", "someone-else@elsewhere"), response(req, "200 OK", "")}
	})
	got := fast().Probe(context.Background(), "127.0.0.1", r.port())
	assert.Equal(t, 200, got.Status, "the stray 503 was ignored")
}

// Silence is no response, after retransmitting as UDP requires.
func TestProbe_SilenceIsNoResponseAfterRetransmitting(t *testing.T) {
	r := newResponder(t, func(string, int) []string { return nil })
	start := time.Now()
	got := fast().Probe(context.Background(), "127.0.0.1", r.port())
	assert.Equal(t, domain.ProbeNoResponse, got.Outcome)
	assert.GreaterOrEqual(t, time.Since(start), 550*time.Millisecond, "it waited its budget")
	assert.GreaterOrEqual(t, int(r.requests.Load()), 3, "sent at 0, 100 and 300 ms at least")
}

// A carrier that answers only a retransmission is still answering.
func TestProbe_AnswerToARetransmission(t *testing.T) {
	r := newResponder(t, func(req string, n int) []string {
		if n < 2 {
			return nil // the first datagram is "lost"
		}
		return []string{response(req, "200 OK", "")}
	})
	got := fast().Probe(context.Background(), "127.0.0.1", r.port())
	assert.Equal(t, domain.ProbeAnswered, got.Outcome)
}

// Nothing listening: the ICMP refusal ends the check early as no response.
func TestProbe_RefusedPortIsNoResponse(t *testing.T) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	port := conn.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, conn.Close())

	got := fast().Probe(context.Background(), "127.0.0.1", port)
	assert.Equal(t, domain.ProbeNoResponse, got.Outcome)
}

type fakeResolver struct{ err error }

func (f fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) { return nil, f.err }

func TestProbe_HostThatDoesNotResolve(t *testing.T) {
	p := fast()
	p.Resolver = fakeResolver{err: &net.DNSError{Err: "no such host", Name: "locslhodt", IsNotFound: true}}
	assert.Equal(t, domain.ProbeHostNotFound, p.Probe(context.Background(), "locslhodt", 5060).Outcome)
}

// A resolver that is down says nothing about the host: no response, not
// "host not found".
func TestProbe_ResolverFailureIsNoResponse(t *testing.T) {
	p := fast()
	p.Resolver = fakeResolver{err: &net.DNSError{Err: "i/o timeout", Name: "carrier.example", IsTimeout: true}}
	assert.Equal(t, domain.ProbeNoResponse, p.Probe(context.Background(), "carrier.example", 5060).Outcome)
	p.Resolver = fakeResolver{err: errors.New("boom")}
	assert.Equal(t, domain.ProbeNoResponse, p.Probe(context.Background(), "carrier.example", 5060).Outcome)
}

// A cancelled check stops at once.
func TestProbe_StopsWhenCancelled(t *testing.T) {
	r := newResponder(t, func(string, int) []string { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	got := (&Prober{Timeout: 5 * time.Second, FirstRetransmit: 500 * time.Millisecond}).Probe(ctx, "127.0.0.1", r.port())
	assert.Equal(t, domain.ProbeNoResponse, got.Outcome)
	assert.Less(t, time.Since(start), time.Second)
}
