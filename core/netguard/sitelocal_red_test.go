//go:build red

package netguard

import (
	"net"
	"testing"
)

// CONTRACT-QUESTION red: the maintainer must decide whether deprecated
// IPv6 site-local unicast (fec0::/10, RFC 3879) counts as "internal".
// IsInternal's doc enumerates the covered ranges ("loopback,
// link-local, private, unspecified, multicast, CGNAT, or
// reserved/non-routable IPv4") and site-local is absent from that list,
// so this is a policy question, not a documented-contract violation.
// The argument for including it: the package already accepts
// defense-in-depth ranges that Linux does not route today (6to4
// 2002::/16, IPv4-compatible ::/96 — "the predicate must not depend on
// that staying true"), and fec0::/10 is the same class: reserved
// non-public space that no internet path delivers but an
// enterprise/host network may still route to internal services. The
// argument against: deprecated since 2004, unroutable on every current
// OS by default, so an outbound fetch to it fails anyway.

// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F2 Outbound fetch allow-list (guard completeness at the
// shared predicate every outbound sink checks)
// Property: IsInternal classifies every address that cannot originate
// from or route to the public internet as internal; site-local IPv6
// (fec0::/10) is reserved non-public space in that class.
// Surfaces: core/netguard/netguard.go::IsInternal / ::internalRange
// (the ladder checks IsLoopback, IsLinkLocalUnicast, IsPrivate,
// IsUnspecified, IsMulticast, plus v4-only CGNAT/0/8/240/4 nets and
// the translatedV4 prefixes; no site-local branch).
// Finding: verified by running: IsInternal(fec0::1) == false while
// every neighboring reserved range (fc00::/7 ULA, fe80::/10
// link-local, ::ffff:10.0.0.1, NAT64-wrapped metadata) reports true —
// a fec0::/10 literal passes the guard every outbound sink shares.
// Severity: low — exploitation needs a host network that still routes
// site-local IPv6 to something worth reading; on current default OS
// configs the dial fails after the guard lets it through.
// Fix direction: if the maintainer rules site-local internal, add
// fec0::/10 to internalRange next to the ULA/link-local checks (and to
// rangeReason); if not, extend IsInternal's doc to name the exclusion
// so the next audit doesn't re-derive this question.

// TestIsInternalSiteLocalV6 asserts the fec0::/10 range is classified
// internal alongside its reserved neighbors.
func TestIsInternalSiteLocalV6(t *testing.T) {
	for _, s := range []string{
		"fec0::1",           // site-local unicast, first address of fec0::/10
		"feff:ffff:ffff::1", // last /10 block address before ff00::/8 multicast
		"fed0::dead:beef",   // interior of the range
	} {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("unparsable probe %q", s)
		}
		if !IsInternal(ip) {
			t.Errorf("SECURITY: [netguard-sitelocal] IsInternal(%s) = false: fec0::/10 is "+
				"RFC-3879 site-local space, non-routable from the public internet, and every "+
				"outbound sink gates on this predicate — the same defense-in-depth class as "+
				"the 6to4 / IPv4-compatible prefixes the package already folds in.", s)
		}
	}
}
