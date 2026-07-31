// Package netguard holds the one definition of "this address is
// internal" that every outbound-fetch surface checks against.
//
// It exists because the same predicate had been written twice with
// different coverage — battery/webhook's rejectInternalIP and
// framework/experimental/harness's isInternalIP — and the narrower copy was the one
// guarding the surface that delivers signed payloads to caller-supplied
// URLs. A predicate that only half the callers agree on is not a
// predicate. New SSRF-adjacent surfaces call this rather than growing a
// third copy.
//
// This package answers "is this IP internal". Enforcing it correctly
// against DNS is the caller's job: resolve-then-fetch is a TOCTOU, so
// the reference callers check the peer at dial time via
// net.Dialer.Control, which sees the address the connection actually
// went to.
package netguard

import "net"

// cgnatRange is the RFC 6598 carrier-grade NAT block (100.64.0.0/10).
// net.IP.IsPrivate does not cover it, but it is non-routable internal
// space — a cloud provider's internal services and a customer's own LAN
// both live behind it.
var cgnatRange = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// metadataIPv4 is the cloud instance-metadata address. It already falls
// inside the link-local range; naming it separately means a deployment
// that ever loosens the link-local rule still cannot reach it.
var metadataIPv4 = net.IPv4(169, 254, 169, 254)

// IsInternal reports whether ip is loopback, link-local (which covers
// cloud instance metadata), private (RFC1918 + IPv6 unique-local),
// unspecified, multicast, or CGNAT.
//
// IPv4-mapped IPv6 addresses (`::ffff:a.b.c.d`) are normalized to their
// 4-byte form first, so a mapped internal literal cannot slip past the
// v4 range checks.
func IsInternal(ip net.IP) bool {
	if ip == nil {
		// A caller with no address to check has nothing to vouch for.
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsUnspecified(),
		ip.IsLoopback(),
		ip.IsPrivate(),
		ip.IsLinkLocalUnicast(),
		ip.IsLinkLocalMulticast(),
		ip.IsMulticast(),
		ip.IsInterfaceLocalMulticast():
		return true
	}
	if cgnatRange.Contains(ip) {
		return true
	}
	return ip.Equal(metadataIPv4)
}

// Reason returns a short description of why ip counts as internal, for
// error messages. Returns "" when IsInternal(ip) is false.
func Reason(ip net.IP) string {
	if ip == nil {
		return "nil address"
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsUnspecified():
		return "unspecified address"
	case ip.IsLoopback():
		return "loopback address"
	case ip.IsPrivate():
		return "private address"
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		return "link-local address (covers cloud instance metadata)"
	case ip.IsMulticast(), ip.IsInterfaceLocalMulticast():
		return "multicast address"
	case cgnatRange.Contains(ip):
		return "carrier-grade NAT address (RFC 6598)"
	case ip.Equal(metadataIPv4):
		return "cloud instance-metadata address"
	}
	return ""
}
