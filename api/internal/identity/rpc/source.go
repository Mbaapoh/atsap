package rpc

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"connectrpc.com/connect"
)

// SourceResolver decides the caller's address for throttling and auth
// events (design D7). The connection peer is the answer unless the peer is
// a configured trusted proxy, in which case the right-most untrusted entry
// of X-Forwarded-For is. With no trusted proxies — the default — the
// header is ignored entirely: anyone can write it.
type SourceResolver struct {
	trusted []netip.Prefix
}

// NewSourceResolver trusts the given proxy networks.
func NewSourceResolver(trusted []netip.Prefix) SourceResolver {
	return SourceResolver{trusted: trusted}
}

// ParseTrustedProxies parses ATSAPBX_TRUSTED_PROXIES: comma-separated
// addresses or CIDR prefixes.
func ParseTrustedProxies(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			addr, err := netip.ParseAddr(part)
			if err != nil {
				return nil, err
			}
			out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			return nil, err
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// Resolve returns the caller's address, or "" when unknown.
func (r SourceResolver) Resolve(peer connect.Peer, header http.Header) string {
	host, _, err := net.SplitHostPort(peer.Addr)
	if err != nil {
		host = peer.Addr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	if !r.isTrusted(addr) {
		return addr.String()
	}
	hops := strings.Split(header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		a = a.Unmap()
		if !r.isTrusted(a) {
			return a.String()
		}
	}
	return addr.String()
}

func (r SourceResolver) isTrusted(a netip.Addr) bool {
	for _, p := range r.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
