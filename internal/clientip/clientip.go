// Package clientip reads the client address out of X-Forwarded-For the one
// way that survives an appending proxy: from the right.
//
// A proxy that appends (nginx $proxy_add_x_forwarded_for,
// httputil.ReverseProxy, most CDNs) leaves whatever the client sent on the
// left and writes the address it observed on the right. The leftmost entry
// is therefore client-supplied, and keying a rate limit or an access log on
// it lets one client rotate it per request or name a third party. Walking
// from the right and skipping the operator's own proxy tiers lands on the
// first hop no trusted proxy vouches past: the address the outermost
// trusted proxy saw.
//
// core/middleware.RateLimit, framework/ratelimit.ClientIP and battery/log
// share this walk so the rule has one implementation.
package clientip

import (
	"net"
	"net/http"
	"strings"
)

// Proxies is a parsed trusted-proxy list. Bare IPs match exactly; CIDR
// entries match by containment. The zero value trusts nothing.
type Proxies struct {
	ips  []net.IP
	nets []*net.IPNet
}

// ParseProxies parses entries as IPs or CIDRs. Entries that are neither are
// dropped, so a typo trusts nothing rather than everything.
func ParseProxies(entries []string) Proxies {
	var p Proxies
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(e); err == nil {
			p.nets = append(p.nets, n)
			continue
		}
		if ip := net.ParseIP(e); ip != nil {
			p.ips = append(p.ips, ip)
		}
	}
	return p
}

// Empty reports whether no proxy is trusted.
func (p Proxies) Empty() bool { return len(p.ips) == 0 && len(p.nets) == 0 }

// Contains reports whether ip is one of the trusted proxies.
func (p Proxies) Contains(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, n := range p.nets {
		if n.Contains(ip) {
			return true
		}
	}
	for _, t := range p.ips {
		if t.Equal(ip) {
			return true
		}
	}
	return false
}

// ContainsAddr is Contains for a textual address, with or without a port.
func (p Proxies) ContainsAddr(addr string) bool {
	return p.Contains(ParseHop(addr))
}

// Forwarded walks every X-Forwarded-For line in h as one comma-separated
// list, from the right, skipping hops in trusted, and returns the first hop
// that is not trusted. It reports false when the header is absent, when
// every hop is trusted, or when the walk meets an entry that is not an IP
// before it finds an untrusted one: a trusted proxy never writes junk, so
// junk there means the list cannot be read and the caller falls back to the
// TCP peer.
//
// The caller decides whether the immediate peer may speak for the header
// at all; Forwarded only reads it.
func Forwarded(h http.Header, trusted Proxies) (net.IP, bool) {
	lines := h.Values("X-Forwarded-For")
	for li := len(lines) - 1; li >= 0; li-- {
		hops := strings.Split(lines[li], ",")
		for i := len(hops) - 1; i >= 0; i-- {
			hop := strings.TrimSpace(hops[i])
			if hop == "" {
				continue
			}
			ip := ParseHop(hop)
			if ip == nil {
				return nil, false
			}
			if trusted.Contains(ip) {
				continue
			}
			return ip, true
		}
	}
	return nil, false
}

// ParseHop parses one address as an IP, accepting a trailing port
// ("203.0.113.7:4711", "[2001:db8::1]:443") as some proxies append it.
// It returns nil for anything else.
func ParseHop(s string) net.IP {
	s = strings.TrimSpace(s)
	if ip := net.ParseIP(s); ip != nil {
		return ip
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return net.ParseIP(host)
	}
	return nil
}
