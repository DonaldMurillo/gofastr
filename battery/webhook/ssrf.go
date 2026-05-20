package webhook

import (
	"fmt"
	"net"
	"net/url"
	"strings"
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
