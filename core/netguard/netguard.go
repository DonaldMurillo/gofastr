// Package netguard holds the one definition of "this address is
// internal" that every outbound-fetch surface checks against.
//
// It exists because the same predicate had been written twice with
// different coverage, battery/webhook's rejectInternalIP and
// framework/experimental/harness's isInternalIP, and the narrower copy was the one
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
// space. A cloud provider's internal services and a customer's own LAN
// both live behind it.
var cgnatRange = net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// thisNetworkV4 is the RFC 791 "this network" block 0.0.0.0/8. Only the
// bare 0.0.0.0 is caught by IsUnspecified; the rest (0.0.0.1 …) is
// reserved space that Linux and macOS route to the local host — the
// classic `http://0/` SSRF bypass.
var thisNetworkV4 = net.IPNet{IP: net.IPv4(0, 0, 0, 0), Mask: net.CIDRMask(8, 32)}

// reservedV4 is the RFC 1112 "reserved for future use" block
// 240.0.0.0/4, which ends at 255.255.255.255: never routable on the
// public internet, and its last address is the limited-broadcast that
// reaches every host on the local segment.
var reservedV4 = net.IPNet{IP: net.IPv4(240, 0, 0, 0), Mask: net.CIDRMask(4, 32)}

// siteLocalV6 is the deprecated IPv6 site-local unicast block fec0::/10
// (RFC 3513 §2.5.7, deprecated by RFC 3879). Deprecated does not mean
// gone: it is reserved non-routable space in the same defense-in-depth
// class as 6to4 and the IPv4-compatible prefix — no internet path
// delivers to it, and an enterprise or host network that still routes
// it can reach internal services, so the predicate must not depend on
// current OS defaults refusing the dial.
var siteLocalV6 = net.IPNet{IP: net.ParseIP("fec0::"), Mask: net.CIDRMask(10, 128)}

// metadataIPv4 is the cloud instance-metadata address. It already falls
// inside the link-local range; naming it separately means a deployment
// that ever loosens the link-local rule still cannot reach it.
var metadataIPv4 = net.IPv4(169, 254, 169, 254)

// IsInternal reports whether ip is loopback, link-local (which covers
// cloud instance metadata), private (RFC1918 + IPv6 unique-local),
// unspecified, multicast, site-local IPv6 (deprecated fec0::/10), CGNAT,
// or reserved/non-routable IPv4 space (0.0.0.0/8, 240.0.0.0/4).
//
// IPv4-mapped IPv6 addresses (`::ffff:a.b.c.d`) are normalized to their
// 4-byte form first, so a mapped internal literal cannot slip past the
// v4 range checks. Addresses in an IPv4-*translation* prefix (NAT64,
// 6to4, IPv4-compatible) are checked against the IPv4 they carry. See
// [translatedV4].
func IsInternal(ip net.IP) bool {
	if ip == nil {
		// A caller with no address to check has nothing to vouch for.
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if internalRange(ip) {
		return true
	}
	// Not internal as written. If it is a translation address, it is only
	// as safe as the IPv4 it carries: a NAT64 translator on the path will
	// forward to that IPv4. Checked AFTER the direct ranges so `::` and
	// `::1` are classified by their own rules rather than by the
	// all-zero IPv4-compatible payload they would otherwise decode to.
	if v4 := translatedV4(ip); v4 != nil {
		return internalRange(v4)
	}
	return false
}

// internalRange is the range check itself, shared by IsInternal and
// Reason so the predicate and its diagnostic can never disagree, the
// drift this package was created to end.
func internalRange(ip net.IP) bool {
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
	if cgnatRange.Contains(ip) || thisNetworkV4.Contains(ip) || reservedV4.Contains(ip) ||
		siteLocalV6.Contains(ip) {
		return true
	}
	return ip.Equal(metadataIPv4)
}

// translatedV4 returns the IPv4 address carried by an IPv4-translation
// IPv6 address, or nil when ip is not one.
//
// net.IP.To4 normalizes only the IPv4-*mapped* form (`::ffff:0:0/96`).
// These other encodings also resolve to an IPv4 destination, so a guard
// that skips them lets `64:ff9b::a9fe:a9fe`, cloud instance metadata
// behind NAT64, read as a public address:
//
//   - NAT64 well-known prefix `64:ff9b::/96` (RFC 6052): v4 at bytes 12-16.
//   - NAT64 local-use prefix `64:ff9b:1::/48` (RFC 8215): the RFC 6052
//     /48 embedding splits the v4 across bytes 6-7 and 9-10, skipping
//     the reserved u octet at byte 8. It is NOT contiguous.
//   - 6to4 `2002::/16` (RFC 3056): v4 at bytes 2-6.
//   - Deprecated IPv4-compatible `::/96` (RFC 4291): v4 at bytes 12-16.
//
// 6to4 and IPv4-compatible are defense in depth: Linux routes only
// `::ffff:0:0/96` onto IPv4 today, so neither is directly routable. The
// predicate must not depend on that staying true.
func translatedV4(ip net.IP) net.IP {
	if len(ip) != net.IPv6len {
		return nil
	}
	switch {
	case ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b:
		// RFC 8215 local-use 64:ff9b:1::/48. The u octet (byte 8) must be
		// zero, and so must the 40-bit suffix (bytes 11-15). RFC 6052
		// §2.2 defines both as zero for a canonical embedding. Checking
		// the suffix keeps an unrelated address that merely shares the
		// prefix, such as 64:ff9b:1:a9fe:a9:fe00:0:1, from being read as
		// an embedded IPv4 and refused.
		if ip[4] == 0x00 && ip[5] == 0x01 && ip[8] == 0x00 && isZero(ip[11:]) {
			return net.IPv4(ip[6], ip[7], ip[9], ip[10])
		}
		// RFC 6052 well-known 64:ff9b::/96.
		if isZero(ip[4:12]) {
			return net.IPv4(ip[12], ip[13], ip[14], ip[15])
		}
	case ip[0] == 0x20 && ip[1] == 0x02:
		// 6to4.
		return net.IPv4(ip[2], ip[3], ip[4], ip[5])
	case isZero(ip[0:12]):
		// IPv4-compatible. `::` and `::1` never reach here. IsInternal
		// and Reason both run the direct range checks first.
		return net.IPv4(ip[12], ip[13], ip[14], ip[15])
	}
	return nil
}

func isZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
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
	if r := rangeReason(ip); r != "" {
		return r
	}
	// Same fallback as IsInternal: a translation address inherits the
	// verdict of the IPv4 it carries, so the diagnostic must too.
	if v4 := translatedV4(ip); v4 != nil {
		if r := rangeReason(v4); r != "" {
			return r + ", carried by an IPv4-translation address"
		}
	}
	return ""
}

// rangeReason is internalRange's half of the pair: the same ladder,
// worded for humans. Keep the two in lockstep.
func rangeReason(ip net.IP) string {
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
	case thisNetworkV4.Contains(ip):
		return `reserved "this network" address (0.0.0.0/8)`
	case cgnatRange.Contains(ip):
		return "carrier-grade NAT address (RFC 6598)"
	case reservedV4.Contains(ip):
		return "reserved address (240.0.0.0/4; includes limited broadcast)"
	case siteLocalV6.Contains(ip):
		return "site-local address (fec0::/10; deprecated by RFC 3879)"
	case ip.Equal(metadataIPv4):
		return "cloud instance-metadata address"
	}
	return ""
}
