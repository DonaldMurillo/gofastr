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
// predicate — battery/webhook's registration check AND its dial-time
// net.Dialer.Control guard, framework/experimental/harness webfetch's
// assertPublicHost + ssrfDialControl. All of them normalize only the
// IPv4-mapped ::ffff:0:0/96 form (via net.IP.To4) and pass the
// translation prefixes through as "public" IPv6.
//
// Reachability: an attacker-controlled DNS AAAA record (static — no
// rebinding needed) or URL literal for e.g. evil.com =
// 64:ff9b::a9fe:a9fe. On hosts whose network routes 64:ff9b::/96 to a
// NAT64 translator (AWS NAT Gateway NAT64 / GCP Cloud NAT NAT64 — the
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
		// 9-10, skipping the reserved u octet at byte 8 — it is NOT
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
		// external — 64:ff9b::/96 is the sanctioned egress path for
		// IPv6-only workloads, so blocking it wholesale would break
		// legitimate fetches.
		{"NAT64 public 64:ff9b::808:808", net.ParseIP("64:ff9b::808:808"), false},
		{"6to4 public 2002:808:808::", net.ParseIP("2002:808:808::"), false},
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
