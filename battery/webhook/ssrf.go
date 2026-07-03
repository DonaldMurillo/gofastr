package webhook

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// validateSubscriberURL rejects URLs that obviously target internal
// infrastructure. Webhooks delivered to RFC1918, loopback, link-local
// or cloud metadata endpoints are SSRF vectors when subscribers are
// user-provided.
//
// When allowPrivate is true the host check is skipped — used by the
// test helper and by apps that explicitly want to deliver to private
// networks. The scheme guard runs in both modes.
func validateSubscriberURL(raw string, allowPrivate bool) error {
	if raw == "" {
		return fmt.Errorf("webhook: subscriber URL required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhook: parse URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("webhook: scheme %q not allowed (need http or https)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("webhook: URL missing host")
	}
	if u.User != nil {
		return fmt.Errorf("webhook: URL with embedded userinfo not allowed (credential leakage)")
	}
	if allowPrivate {
		return nil
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook: URL missing host")
	}
	// Hostname-only checks first — cheaper than DNS and catch the
	// obvious cloud-metadata names regardless of how they resolve.
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" ||
		strings.HasSuffix(lowerHost, ".localhost") ||
		strings.HasSuffix(lowerHost, ".internal") ||
		lowerHost == "metadata.google.internal" {
		return fmt.Errorf("webhook: host %q targets internal infrastructure", host)
	}
	// Resolve literal IPs without DNS — the parsed host may already be
	// a numeric address.
	if ip := net.ParseIP(host); ip != nil {
		if err := rejectInternalIP(ip); err != nil {
			return err
		}
		return nil
	}
	// Hostname — resolve and re-check. A more rigorous defense is to
	// hook net.Dialer.Control at connect time (also catches DNS
	// rebinding); this lookup catches the common case at registration.
	addrs, err := net.LookupIP(host)
	if err != nil {
		// Leave DNS failure for the worker — registration shouldn't
		// require the receiver to be live. The dial-time guard catches
		// any resolution that lands on an internal IP later.
		return nil
	}
	for _, ip := range addrs {
		if err := rejectInternalIP(ip); err != nil {
			return err
		}
	}
	return nil
}

// ssrfGuardedTransport builds the outbound HTTP transport for webhook
// delivery. The net.Dialer.Control hook re-runs rejectInternalIP on the
// ACTUAL resolved (network, address) at connect time. This closes the
// DNS-rebinding / TOCTOU window: validateSubscriberURL only checks the
// host at Subscribe() time, so a host that resolves public at
// registration and is later re-pointed at 169.254.169.254 / 127.0.0.1 /
// an RFC1918 address would otherwise be dialed by the worker.
//
// When allowPrivate is true the dial-time check is skipped (dev/test
// posture), matching the registration-time AllowPrivateNetworks opt-out.
func ssrfGuardedTransport(allowPrivate bool) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !allowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// Control receives the already-resolved numeric address;
				// a non-IP here is unexpected — refuse rather than dial
				// blind.
				return fmt.Errorf("webhook: dial address %q is not a resolved IP", address)
			}
			return rejectInternalIP(ip)
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = dialer.DialContext
	return tr
}

// ssrfGuardClient returns a shallow copy of c whose transport enforces
// the SSRF guard without disturbing the caller's routing:
//
//   - nil transport → the guarded default transport (dial-time hook,
//     strongest: the Control callback sees the actual resolved IP at
//     connect time).
//   - any caller-supplied transport → left untouched and wrapped with a
//     per-request resolved-IP check on the request TARGET that refuses
//     before the inner transport runs. Swapping the transport's dialer
//     instead would silently break legitimate custom routing — an
//     egress proxy on a private IP, an SSH tunnel, a unix-socket or
//     custom-DNS dialer — because those dial addresses are internal by
//     design while the delivery target is not. The per-request check
//     resolves once up front, so unlike the dialer hook it cannot see a
//     mid-flight re-resolve — still strictly safer than no guard.
func ssrfGuardClient(c *http.Client) *http.Client {
	cc := *c
	if c.Transport == nil {
		cc.Transport = ssrfGuardedTransport(false)
	} else {
		cc.Transport = &ssrfGuardedRoundTripper{inner: c.Transport}
	}
	return &cc
}

// ssrfGuardedRoundTripper is the fallback guard for custom RoundTrippers
// the dialer hook cannot reach: it resolves the request host and refuses
// when any resolved address is internal.
type ssrfGuardedRoundTripper struct{ inner http.RoundTripper }

func (g *ssrfGuardedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	host := r.URL.Hostname()
	ips, err := net.DefaultResolver.LookupIPAddr(r.Context(), host)
	if err != nil {
		return nil, fmt.Errorf("webhook: resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		if err := rejectInternalIP(ip.IP); err != nil {
			return nil, err
		}
	}
	return g.inner.RoundTrip(r)
}

func rejectInternalIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("webhook: nil IP")
	}
	// Cover loopback, link-local, multicast, unspecified, and the
	// well-known private blocks. ip.IsPrivate covers RFC1918 + the
	// IPv6 unique-local range.
	if ip.IsUnspecified() {
		return fmt.Errorf("webhook: unspecified IP %s not allowed", ip)
	}
	if ip.IsLoopback() {
		return fmt.Errorf("webhook: loopback IP %s not allowed", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("webhook: private IP %s not allowed", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("webhook: link-local IP %s not allowed (covers cloud metadata)", ip)
	}
	if ip.IsMulticast() {
		return fmt.Errorf("webhook: multicast IP %s not allowed", ip)
	}
	// Reject the AWS/GCP metadata IPv4 explicitly even outside the
	// link-local range so a misconfigured proxy doesn't slip past.
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return fmt.Errorf("webhook: cloud metadata IP %s not allowed", ip)
	}
	return nil
}
