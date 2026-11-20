package webhook

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/netguard"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func validateSubscriberURL(raw string, allowPrivate bool) error {
	if !textsafe.Storable(raw) {
		return fmt.Errorf("webhook: subscriber URL must be valid UTF-8 without NUL")
	}
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
	lowerHost := strings.ToLower(host)
	if lowerHost == "localhost" ||
		strings.HasSuffix(lowerHost, ".localhost") ||
		strings.HasSuffix(lowerHost, ".internal") ||
		lowerHost == "metadata.google.internal" {
		return fmt.Errorf("webhook: host %q targets internal infrastructure", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := rejectInternalIP(ip); err != nil {
			return err
		}
		return nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil
	}
	for _, ip := range addrs {
		if err := rejectInternalIP(ip); err != nil {
			return err
		}
	}
	return nil
}

func ssrfGuardClient(c *http.Client) *http.Client {
	cc := *c
	if c.Transport == nil {
		cc.Transport = netguard.GuardedTransport(false, "webhook")
	} else if tr, ok := c.Transport.(*http.Transport); ok {
		if guarded, plain := netguard.GuardPlainTransport(tr, "webhook"); plain {
			cc.Transport = guarded
		} else {
			cc.Transport = &ssrfGuardedRoundTripper{inner: c.Transport}
		}
	} else {
		cc.Transport = &ssrfGuardedRoundTripper{inner: c.Transport}
	}
	return &cc
}

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
	//gofastr:allow(resolvedial) fallback for a transport with its own proxy or dialer, which cannot be swapped; a plain one gets netguard.GuardPlainTransport
	return g.inner.RoundTrip(r)
}

func rejectInternalIP(ip net.IP) error {
	if reason := netguard.Reason(ip); reason != "" {
		return fmt.Errorf("webhook: %s %s not allowed", reason, ip)
	}
	return nil
}
