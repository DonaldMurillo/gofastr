package main

import "testing"

// Property: the harness sidecar pins Host to the authority it actually
// bound, so a DNS-rebound request (attacker domain re-pointed at
// 127.0.0.1) is rejected before it can read the chat page — which
// carries the bearer token in a meta tag.
func TestLoopbackGuardsPinHost(t *testing.T) {
	hosts, origins := loopbackGuards("127.0.0.1:41234")

	for _, want := range []string{"127.0.0.1:41234", "localhost:41234"} {
		if !hostAllowed(want, hosts) {
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
		if hostAllowed(bad, hosts) {
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
	hosts, _ := loopbackGuards("192.168.1.50:8080")

	if !hostAllowed("192.168.1.50:8080", hosts) {
		t.Fatalf("explicit bind authority rejected: %v", hosts)
	}
	for _, bad := range []string{"localhost:8080", "127.0.0.1:8080", "attacker.example:8080"} {
		if hostAllowed(bad, hosts) {
			t.Errorf("Host %q must not be allowed for an explicit LAN bind: %v", bad, hosts)
		}
	}
}

// hostAllowed fails closed: an unpinned (empty) list rejects everything
// rather than degrading to allow-all.
func TestHostAllowedFailsClosed(t *testing.T) {
	if hostAllowed("127.0.0.1:1", nil) {
		t.Fatal("empty allow-list must deny, not admit")
	}
}
