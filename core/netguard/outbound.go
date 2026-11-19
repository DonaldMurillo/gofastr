package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const lookupTimeout = 3 * time.Second

// CheckOutboundURL refuses a URL that is not http(s), lacks a host or has
// userinfo and, unless allowPrivate, one whose host is an internal name or
// address; a DNS lookup that fails or passes 3s is allowed.
func CheckOutboundURL(raw, prefix string, allowPrivate bool) error {
	if raw == "" {
		return fmt.Errorf("%s: URL required", prefix)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: parse URL: %w", prefix, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%s: scheme %q not allowed (need http or https)", prefix, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: URL missing host", prefix)
	}
	if u.User != nil {
		return fmt.Errorf("%s: URL with embedded userinfo not allowed (credential leakage)", prefix)
	}
	if allowPrivate {
		return nil
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%s: URL missing host", prefix)
	}
	if IsInternalName(host) {
		return fmt.Errorf("%s: host %q targets internal infrastructure", prefix, host)
	}
	if ip := net.ParseIP(host); ip != nil {
		return refuse(prefix, ip)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if err := refuse(prefix, a.IP); err != nil {
			return err
		}
	}
	return nil
}

// IsInternalName reports whether host is localhost or ends in .localhost
// or .internal, in any case.
func IsInternalName(host string) bool {
	lower := strings.ToLower(host)
	return lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".internal")
}

func refuse(prefix string, ip net.IP) error {
	if reason := Reason(ip); reason != "" {
		return fmt.Errorf("%s: %s %s not allowed", prefix, reason, ip)
	}
	return nil
}

// GuardRoundTripper returns GuardedTransport for nil, the GuardPlainTransport
// clone of a plain transport, and rt wrapped in a *ResolveGuard otherwise.
func GuardRoundTripper(rt http.RoundTripper, prefix string) http.RoundTripper {
	if rt == nil {
		return GuardedTransport(false, prefix)
	}
	if tr, ok := rt.(*http.Transport); ok {
		if guarded, plain := GuardPlainTransport(tr, prefix); plain {
			return guarded
		}
	}
	return &ResolveGuard{Inner: rt, Prefix: prefix}
}

// ResolveGuard refuses a request whose host is or resolves to an internal
// address before Inner runs.
type ResolveGuard struct {
	Inner  http.RoundTripper
	Prefix string
}

// RoundTrip checks r's host, then delegates to Inner.
func (g *ResolveGuard) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if err := refuse(g.Prefix, ip); err != nil {
			return nil, err
		}
		return g.Inner.RoundTrip(r)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(r.Context(), host)
	if err != nil {
		return nil, fmt.Errorf("%s: resolve %q: %w", g.Prefix, host, err)
	}
	for _, ip := range ips {
		if err := refuse(g.Prefix, ip.IP); err != nil {
			return nil, err
		}
	}
	//gofastr:allow(resolvedial) fallback for a transport with its own proxy or dialer, which cannot be swapped; a plain one gets GuardPlainTransport
	return g.Inner.RoundTrip(r)
}
