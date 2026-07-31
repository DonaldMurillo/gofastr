package netguard

import (
	"net"
	"testing"
)

// TestIsInternal pins the one definition of "this address is internal"
// that every outbound-fetch surface checks against. The rule: anything
// loopback, link-local (which includes cloud instance metadata),
// private (RFC1918 + IPv6 unique-local), unspecified, multicast, or
// CGNAT counts as internal and must be rejected before dialing a
// caller-supplied URL.
//
// DNS is deliberately out of scope for this predicate: resolve-then-
// fetch is a TOCTOU, so the reference callers check the peer at dial
// time via net.Dialer.Control. These tests assert only the
// address-literal predicate and its Reason diagnostic.
func TestIsInternal(t *testing.T) {
	cases := []struct {
		name     string
		ip       net.IP
		internal bool
	}{
		// loopback
		{"loopback v4 127.0.0.1", net.ParseIP("127.0.0.1"), true},
		{"loopback v6 ::1", net.ParseIP("::1"), true},

		// private (RFC1918 + IPv6 unique-local fc00::/7)
		{"private 10.0.0.1", net.ParseIP("10.0.0.1"), true},
		{"private 172.16.0.1", net.ParseIP("172.16.0.1"), true},
		{"private 192.168.1.1", net.ParseIP("192.168.1.1"), true},
		{"unique-local fc00::1", net.ParseIP("fc00::1"), true},

		// link-local
		{"link-local v4 169.254.0.1", net.ParseIP("169.254.0.1"), true},
		{"link-local v6 fe80::1", net.ParseIP("fe80::1"), true},

		// cloud instance-metadata — inside link-local, but named
		// separately so a loosened link-local rule cannot reach it.
		{"cloud instance-metadata 169.254.169.254", net.ParseIP("169.254.169.254"), true},

		// CGNAT (RFC 6598, 100.64.0.0/10). net.IP.IsPrivate does NOT
		// cover this block; see the dedicated assertion below.
		{"CGNAT 100.64.0.1", net.ParseIP("100.64.0.1"), true},

		// unspecified
		{"unspecified v4 0.0.0.0", net.ParseIP("0.0.0.0"), true},
		{"unspecified v6 ::", net.ParseIP("::"), true},

		// multicast
		{"multicast v4 224.0.0.1", net.ParseIP("224.0.0.1"), true},
		{"multicast v6 ff02::1", net.ParseIP("ff02::1"), true},

		// IPv4-mapped IPv6 normalization: ::ffff:a.b.c.d must be reduced
		// to its 4-byte form before the v4 range checks run, so a mapped
		// internal literal cannot slip past.
		{"mapped loopback ::ffff:127.0.0.1", net.ParseIP("::ffff:127.0.0.1"), true},
		{"mapped public ::ffff:8.8.8.8", net.ParseIP("::ffff:8.8.8.8"), false},

		// public
		{"public 8.8.8.8", net.ParseIP("8.8.8.8"), false},
		{"public 1.1.1.1", net.ParseIP("1.1.1.1"), false},
		{"public 2606:4700:4700::1111", net.ParseIP("2606:4700:4700::1111"), false},

		// nil input — a caller with no address has nothing to vouch for.
		{"nil address", nil, true},
	}

	t.Run("internal addresses", func(t *testing.T) {
		for _, c := range cases {
			if !c.internal {
				continue
			}
			c := c
			t.Run(c.name, func(t *testing.T) {
				if got := IsInternal(c.ip); !got {
					t.Fatalf("IsInternal(%v) = false, want true", c.ip)
				}
			})
		}
	})

	t.Run("external addresses", func(t *testing.T) {
		for _, c := range cases {
			if c.internal {
				continue
			}
			c := c
			t.Run(c.name, func(t *testing.T) {
				if got := IsInternal(c.ip); got {
					t.Fatalf("IsInternal(%v) = true, want false", c.ip)
				}
			})
		}
	})

	// CGNAT must be caught by IsInternal precisely because the stdlib's
	// net.IP.IsPrivate does not cover the 100.64.0.0/10 block. If a Go
	// release ever extends IsPrivate to it, this assertion flags that
	// the package's own cgnatRange is redundant.
	t.Run("CGNAT not covered by stdlib IsPrivate", func(t *testing.T) {
		cg := net.ParseIP("100.64.0.1")
		if cg.IsPrivate() {
			t.Fatal("100.64.0.1 is now reported private by net.IP.IsPrivate; the package's cgnatRange may be redundant")
		}
		if !IsInternal(cg) {
			t.Fatal("100.64.0.1 must be treated as internal regardless of stdlib coverage")
		}
	})

	// Cross-check: Reason must be non-empty exactly when IsInternal is
	// true. A predicate and its diagnostic must agree; a divergence
	// would mean an internal address reaches a caller with no error
	// text, or a public address produces a spurious "internal" reason.
	t.Run("Reason agrees with IsInternal", func(t *testing.T) {
		for _, c := range cases {
			got := IsInternal(c.ip)
			reason := Reason(c.ip)
			switch {
			case got && reason == "":
				t.Errorf("IsInternal(%v) = true but Reason is empty", c.ip)
			case !got && reason != "":
				t.Errorf("IsInternal(%v) = false but Reason = %q (non-empty)", c.ip, reason)
			}
		}
	})

	// Exact text for the two boundary cases error messages quote
	// directly: nil, and a public address. The metadata address is
	// asserted separately since it carries its own Reason branch.
	t.Run("Reason text", func(t *testing.T) {
		if got := Reason(nil); got != "nil address" {
			t.Errorf("Reason(nil) = %q, want %q", got, "nil address")
		}
		if got := Reason(net.ParseIP("8.8.8.8")); got != "" {
			t.Errorf("Reason(8.8.8.8) = %q, want empty", got)
		}
		if got := Reason(net.ParseIP("169.254.169.254")); got == "" {
			t.Error("Reason(169.254.169.254) is empty, want a non-empty reason")
		}
	})

	// Hostnames are the caller's job, not this predicate's: net.ParseIP
	// returns nil for any non-IP literal, and IsInternal(nil) is true.
	// A caller that resolves hostnames itself and passes the result is
	// the only safe pattern — passing a bare hostname would be swallowed
	// here as "nil" and silently blocked.
	t.Run("hostname literals are not this predicate's concern", func(t *testing.T) {
		if ip := net.ParseIP("example.com"); ip != nil {
			t.Fatalf("net.ParseIP(%q) = %v, want nil", "example.com", ip)
		}
	})
}
