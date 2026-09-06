package netguard

import (
	"net"
	"testing"
)

// Property: an IPv6 address in an IPv4-translation prefix (NAT64
// RFC 6052 well-known 64:ff9b::/96, RFC 8215 local-use
// 64:ff9b:1::/48, deprecated IPv4-compatible ::/96, 6to4 2002::/16)
// is internal exactly when the IPv4 address it carries is internal.
//
// Surfaces: every caller that feeds a resolved peer into this
// predicate, battery/webhook's registration check AND its dial-time
// net.Dialer.Control guard, framework/experimental/harness webfetch's
// assertPublicHost + ssrfDialControl. All of them normalize only the
// IPv4-mapped ::ffff:0:0/96 form (via net.IP.To4) and pass the
// translation prefixes through as "public" IPv6.
//
// Reachability: an attacker-controlled DNS AAAA record (static, no
// rebinding needed) or URL literal for e.g. evil.com =
// 64:ff9b::a9fe:a9fe. On hosts whose network routes 64:ff9b::/96 to a
// NAT64 translator (AWS NAT Gateway NAT64 / GCP Cloud NAT NAT64, the
// documented pattern for IPv6-only subnets since 2025), the
// translator extracts the embedded 169.254.169.254 and forwards to the
// cloud instance-metadata service. The IPv4-compatible ::/96 and 6to4
// 2002::/16 forms are defense-in-depth: Linux routes only
// ::ffff:0:0/96 onto IPv4, so those two are not directly routable
// today, but the predicate must not depend on that staying true.
func TestIsInternalTranslationPrefixes(t *testing.T) {
	cases := []struct {
		name     string
		ip       net.IP
		internal bool
	}{
		// NAT64 well-known prefix carrying cloud metadata.
		{"NAT64 metadata 64:ff9b::a9fe:a9fe", net.ParseIP("64:ff9b::a9fe:a9fe"), true},
		// NAT64 RFC 8215 local-use prefix carrying cloud metadata. The
		// /48 embedding of RFC 6052 splits the IPv4 across bytes 6-7 and
		// 9-10, skipping the reserved u octet at byte 8. It is NOT
		// contiguous. Cross-check against RFC 6052 s2.4's own example:
		// 2001:db8:122::/48 + 192.0.2.33 = 2001:db8:122:c000:2:2100::.
		{"NAT64-local metadata 64:ff9b:1:a9fe:a9:fe00::", net.ParseIP("64:ff9b:1:a9fe:a9:fe00::"), true},
		// NAT64 carrying RFC1918 private space.
		{"NAT64 private 64:ff9b::a00:1", net.ParseIP("64:ff9b::a00:1"), true},
		// Deprecated IPv4-compatible form carrying loopback (a non-::1
		// loopback, which dodges the ip[15]==1 loopback check).
		{"compat loopback ::127.0.0.2", net.ParseIP("::127.0.0.2"), true},
		// 6to4 embedding RFC1918 private space (10.0.0.1).
		{"6to4 private 2002:a00:1::", net.ParseIP("2002:a00:1::"), true},

		// Translation prefixes carrying genuinely public IPv4 must stay
		// external. 64:ff9b::/96 is the sanctioned egress path for
		// IPv6-only workloads, so blocking it wholesale would break
		// legitimate fetches.
		{"NAT64 public 64:ff9b::808:808", net.ParseIP("64:ff9b::808:808"), false},
		{"6to4 public 2002:808:808::", net.ParseIP("2002:808:808::"), false},

		// Non-canonical /48: the same prefix and the same bytes 6-10, but a
		// non-zero 40-bit suffix, which RFC 6052 §2.2 requires to be zero.
		// It carries no embedded IPv4, so reading one out of it would refuse
		// an unrelated address.
		{"non-canonical /48 suffix 64:ff9b:1:a9fe:a9:fe00:0:1", net.ParseIP("64:ff9b:1:a9fe:a9:fe00:0:1"), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsInternal(c.ip); got != c.internal {
				t.Fatalf("IsInternal(%v) = %v, want %v", c.ip, got, c.internal)
			}
			// Reason is the twin surface: battery/webhook rejects via
			// Reason, not IsInternal, so the diagnostic must agree.
			if got := Reason(c.ip); (got != "") != c.internal {
				t.Fatalf("Reason(%v) = %q, want non-empty=%v", c.ip, got, c.internal)
			}
		})
	}
}

// Property: every guarded range is EXACT — the first and last address of
// the block are internal, and the address one position outside on either
// side is external. An off-by-one in any mask lets an internal address
// through as "public" (SSRF straight to metadata / private services) or
// refuses a public one; the boundary is the only place that can break.
// Reason must agree at every boundary, since battery/webhook rejects via
// Reason, not IsInternal.
func TestRangeBoundariesExact(t *testing.T) {
	families := []struct {
		name          string
		first, last   net.IP // inside the guarded block
		before, after net.IP
		// Expected verdict of the immediate neighbours. Every block but
		// fec0::/10 sits next to public space on both sides, so those
		// stay false; site-local's neighbours (fe80::/10 below, ff00::/8
		// above) are themselves guarded ranges, so the neighbour verdict
		// there is true and the public probe for mask width is fe00::,
		// the first address a too-wide /8 mask would swallow.
		beforeInternal, afterInternal bool
	}{
		{"loopback v4 127.0.0.0/8", net.ParseIP("127.0.0.0"), net.ParseIP("127.255.255.255"),
			net.ParseIP("126.255.255.255"), net.ParseIP("128.0.0.0"), false, false},
		{"private v4 10.0.0.0/8", net.ParseIP("10.0.0.0"), net.ParseIP("10.255.255.255"),
			net.ParseIP("9.255.255.255"), net.ParseIP("11.0.0.0"), false, false},
		{"private v4 172.16.0.0/12", net.ParseIP("172.16.0.0"), net.ParseIP("172.31.255.255"),
			net.ParseIP("172.15.255.255"), net.ParseIP("172.32.0.0"), false, false},
		{"private v4 192.168.0.0/16", net.ParseIP("192.168.0.0"), net.ParseIP("192.168.255.255"),
			net.ParseIP("192.167.255.255"), net.ParseIP("192.169.0.0"), false, false},
		{"link-local v4 169.254.0.0/16", net.ParseIP("169.254.0.0"), net.ParseIP("169.254.255.255"),
			net.ParseIP("169.253.255.255"), net.ParseIP("169.255.0.0"), false, false},
		{"CGNAT 100.64.0.0/10", net.ParseIP("100.64.0.0"), net.ParseIP("100.127.255.255"),
			net.ParseIP("100.63.255.255"), net.ParseIP("100.128.0.0"), false, false},
		{"unique-local fc00::/7", net.ParseIP("fc00::"), net.ParseIP("fdff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"),
			net.ParseIP("fbff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"), net.ParseIP("fe00::"), false, false},
		{"link-local v6 fe80::/10", net.ParseIP("fe80::"), net.ParseIP("febf:ffff:ffff:ffff:ffff:ffff:ffff:ffff"),
			net.ParseIP("fe7f:ffff:ffff:ffff:ffff:ffff:ffff:ffff"), net.ParseIP("fec0::"), false, true},
		{"site-local v6 fec0::/10", net.ParseIP("fec0::"), net.ParseIP("feff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"),
			net.ParseIP("fe00::"), net.ParseIP("ff00::"), false, true},
	}
	for _, f := range families {
		t.Run(f.name, func(t *testing.T) {
			for _, c := range []struct {
				ip       net.IP
				internal bool
			}{{f.first, true}, {f.last, true}, {f.before, f.beforeInternal}, {f.after, f.afterInternal}} {
				if got := IsInternal(c.ip); got != c.internal {
					t.Errorf("SECURITY: [netguard] IsInternal(%v) = %v at a %s boundary, want %v. "+
						"Attack: an off-by-one mask is a public↔internal misclassification at the SSRF guard.", c.ip, got, f.name, c.internal)
				}
				if got := Reason(c.ip); (got != "") != c.internal {
					t.Errorf("Reason(%v) disagrees with the boundary verdict: %q", c.ip, got)
				}
			}
		})
	}
}

// Pins that deprecated IPv6 site-local unicast (fec0::/10, RFC 3879)
// classifies as internal, found by the 2026-09-04 red-probe round; fixed
// by adding the block to internalRange and rangeReason next to the
// ULA/link-local checks.
// Property: IsInternal classifies every address that cannot originate from
// or route to the public internet as internal; site-local IPv6 (fec0::/10)
// is reserved non-public space in that class, the same defense-in-depth
// family as the 6to4 / IPv4-compatible prefixes the package already folds
// in, so the predicate must not depend on current OS defaults refusing
// the dial.
// Surfaces: core/netguard/netguard.go::internalRange + rangeReason (the
// ladder shared by IsInternal and Reason), which every outbound sink
// (battery/webhook, framework/experimental/harness) gates on.
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
		if Reason(ip) == "" {
			t.Errorf("SECURITY: [netguard-sitelocal] Reason(%s) = \"\": battery/webhook rejects via Reason, so an address IsInternal flags but Reason cannot name is a half-fix.", s)
		}
	}
}

// Property: classification depends on the ADDRESS, not on which of its
// byte representations arrived. netguard's callers hand it 16-byte
// IPv4-in-IPv6 values (net.ParseIP), 4-byte values (To4 results,
// net.IP{a,b,c,d} literals), and net.IPv4(...) values; the IPv4-mapped
// ::ffff:a.b.c.d form is how an IPv6 socket presents an IPv4 peer, so a
// mapped internal literal that slipped the v4 checks would be an SSRF
// straight past every dial-time Control guard.
func TestMappedMatchesPlainClassification(t *testing.T) {
	pairs := []struct {
		plain, mapped net.IP
		internal      bool
	}{
		{net.ParseIP("169.254.169.254"), net.ParseIP("::ffff:169.254.169.254"), true}, // cloud metadata
		{net.ParseIP("100.64.0.1"), net.ParseIP("::ffff:100.64.0.1"), true},           // CGNAT
		{net.ParseIP("10.1.2.3"), net.ParseIP("::ffff:10.1.2.3"), true},               // RFC1918
		{net.ParseIP("127.0.0.1"), net.ParseIP("::ffff:127.0.0.1"), true},             // loopback
		{net.ParseIP("8.8.8.8"), net.ParseIP("::ffff:8.8.8.8"), false},                // genuinely public
	}
	for _, p := range pairs {
		if a, b := IsInternal(p.plain), IsInternal(p.mapped); a != b || a != p.internal {
			t.Errorf("SECURITY: [netguard] representation changed the verdict: plain %v=%v, mapped %v=%v, want %v. "+
				"Attack: an IPv6 socket presents the peer as ::ffff:a.b.c.d; a mapped internal literal must not pass as public.",
				p.plain, a, p.mapped, b, p.internal)
		}
		if Reason(p.mapped) == "" && p.internal {
			t.Errorf("Reason(%v) empty for a mapped internal address", p.mapped)
		}
	}
	// 4-byte slices classify identically to their 16-byte forms.
	for _, c := range []struct {
		ip       net.IP
		internal bool
	}{
		{net.IP{169, 254, 169, 254}, true},
		{net.IP{100, 64, 0, 1}, true},
		{net.IPv4(100, 64, 0, 1), true},
		{net.IP{8, 8, 8, 8}, false},
	} {
		if got := IsInternal(c.ip); got != c.internal {
			t.Errorf("IsInternal(%v) = %v, want %v (4-byte form)", c.ip, got, c.internal)
		}
	}
}

// Property: reserved, non-routable IPv4 space is internal. The ladder
// in internalRange enumerates loopback/private/link-local/multicast/
// CGNAT, but three whole families of non-public space classify as
// "public":
//
//   - 0.0.0.0/8 ("this network", RFC 791): only the bare 0.0.0.0 is
//     caught by IsUnspecified; 0.0.0.1 … 0.255.255.255 pass. Linux and
//     macOS route destinations in 0/8 to the local host (the classic
//     `http://0/`-family SSRF bypass), so a fetch guard that waves them
//     through as public reaches localhost services.
//   - 240.0.0.0/4 (reserved for future use, RFC 1112): never routable
//     on the public internet.
//   - 255.255.255.255 (limited broadcast): one packet to every host on
//     the local segment — a LAN-wide probe from any SSRF-able fetch.
//
// Surfaces: IsInternal and Reason together (battery/webhook rejects via
// Reason, so an address IsInternal flags but Reason cannot name is a
// half-fix).
func TestIsInternalReservedIPv4Ranges(t *testing.T) {
	reserved := []net.IP{
		net.IPv4(0, 0, 0, 1),         // 0.0.0.0/8, past bare unspecified
		net.IPv4(0, 1, 2, 3),         // 0.0.0.0/8 interior
		net.IPv4(240, 0, 0, 1),       // 240.0.0.0/4 first host
		net.IPv4(255, 255, 255, 254), // 240/4 interior
		net.IPv4(255, 255, 255, 255), // limited broadcast
	}
	for _, ip := range reserved {
		if !IsInternal(ip) {
			t.Errorf("SECURITY: [netguard] IsInternal(%v) = false — reserved non-routable space classifies as public. Attack: SSRF fetch to 0/8 reaches the local host on Linux/macOS; 255.255.255.255 broadcasts to the whole LAN segment.", ip)
		}
		if Reason(ip) == "" {
			t.Errorf("SECURITY: [netguard] Reason(%v) = \"\" for reserved space — webhook's Reason-based rejection cannot name it.", ip)
		}
	}
	// False-positive guard: genuinely public unicast stays external.
	for _, ip := range []net.IP{net.IPv4(8, 8, 8, 8), net.IPv4(1, 1, 1, 1)} {
		if IsInternal(ip) {
			t.Errorf("IsInternal(%v) = true for a public address (overreach)", ip)
		}
	}
}
