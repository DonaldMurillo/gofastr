package loopback

import (
	"slices"
	"testing"
)

// Property: the harness sidecar pins Host to the authority it actually
// bound, so a DNS-rebound request (attacker domain re-pointed at
// 127.0.0.1) is rejected before it can read the chat page, which
// carries the bearer token in a meta tag.
func TestLoopbackGuardsPinHost(t *testing.T) {
	hosts, origins := LoopbackGuards("127.0.0.1:41234")

	for _, want := range []string{"127.0.0.1:41234", "localhost:41234"} {
		if !HostAllowed(want, hosts) {
			t.Errorf("loopback authority %q should be allowed, got %v", want, hosts)
		}
	}
	// The rebind shapes: attacker's own name, and a bare name that
	// resolves to loopback but isn't the bound authority.
	for _, bad := range []string{
		"attacker.example:41234",
		"evil.test:41234",
		"127.0.0.1:9999", // right host, wrong port
		"",
	} {
		if HostAllowed(bad, hosts) {
			t.Errorf("Host %q must be rejected, allowed by %v", bad, hosts)
		}
	}
	if len(origins) == 0 {
		t.Fatal("no Origins pinned — browser guard would be unconfigured")
	}
	for _, o := range origins {
		if o[:7] != "http://" {
			t.Errorf("origin %q is not an http authority", o)
		}
	}
}

// An explicit non-loopback bind pins to exactly that authority rather
// than widening to loopback aliases.
func TestLoopbackGuardsExplicitBind(t *testing.T) {
	hosts, _ := LoopbackGuards("192.168.1.50:8080")

	if !HostAllowed("192.168.1.50:8080", hosts) {
		t.Fatalf("explicit bind authority rejected: %v", hosts)
	}
	for _, bad := range []string{"localhost:8080", "127.0.0.1:8080", "attacker.example:8080"} {
		if HostAllowed(bad, hosts) {
			t.Errorf("Host %q must not be allowed for an explicit LAN bind: %v", bad, hosts)
		}
	}
}

// A bind to a less-usual loopback address (127.0.0.2, which Linux and
// an aliased macOS serve) must accept the exact authority the listener
// reports — the URL the tool prints and opens — alongside the standard
// spellings, while neighbouring loopback IPs stay out.
func TestLoopbackGuardsKeepTheBoundLoopbackAuthority(t *testing.T) {
	hosts, origins := LoopbackGuards("127.0.0.2:41234")
	if !HostAllowed("127.0.0.2:41234", hosts) {
		t.Fatalf("the bound loopback authority is refused by its own guard: %v", hosts)
	}
	if !slices.Contains(origins, "http://127.0.0.2:41234") {
		t.Errorf("the bound loopback Origin is refused by its own guard: %v", origins)
	}
	for _, want := range []string{"127.0.0.1:41234", "localhost:41234"} {
		if !HostAllowed(want, hosts) {
			t.Errorf("standard spelling %q refused: %v", want, hosts)
		}
	}
	for _, bad := range []string{"127.0.0.3:41234", "127.0.0.2:9999", "attacker.example:41234"} {
		if HostAllowed(bad, hosts) {
			t.Errorf("Host %q must be rejected: %v", bad, hosts)
		}
	}
}

func TestLoopbackGuardsWildcardBindsKeepStandardAliases(t *testing.T) {
	wantHosts := []string{"127.0.0.1:41234", "localhost:41234", "[::1]:41234"}
	wantOrigins := []string{"http://127.0.0.1:41234", "http://localhost:41234", "http://[::1]:41234"}
	for _, addr := range []string{":41234", "0.0.0.0:41234", "[::]:41234"} {
		hosts, origins := LoopbackGuards(addr)
		if !slices.Equal(hosts, wantHosts) || !slices.Equal(origins, wantOrigins) {
			t.Errorf("LoopbackGuards(%q) = %v, %v; want %v, %v", addr, hosts, origins, wantHosts, wantOrigins)
		}
	}
}

// hostAllowed fails closed: an unpinned (empty) list rejects everything
// rather than degrading to allow-all.
func TestHostAllowedFailsClosed(t *testing.T) {
	if HostAllowed("127.0.0.1:1", nil) {
		t.Fatal("empty allow-list must deny, not admit")
	}
}
