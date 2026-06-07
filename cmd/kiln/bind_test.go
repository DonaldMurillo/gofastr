package main

import "testing"

// The mutating tool surface (add_entity/delete_entity/undo/reset_session
// via POST /kiln/tool/{name}) is UNAUTHENTICATED. Binding to all
// interfaces (":8765") exposes that surface to any co-located host on a
// shared LAN, who can then rewrite the in-memory app. The default bind
// must be loopback so the runtime is reachable only from localhost;
// exposing it requires an explicit opt-in.
func TestDefaultBindIsLoopback(t *testing.T) {
	opts := parseFlags(nil)
	if opts.addr != "127.0.0.1:8765" {
		t.Fatalf("default --addr = %q, want loopback 127.0.0.1:8765 — "+
			"binding to all interfaces exposes the unauthenticated tool surface", opts.addr)
	}
}

// An explicit --addr is honored verbatim so an operator who knowingly
// wants a non-loopback bind can opt in.
func TestExplicitAddrHonored(t *testing.T) {
	opts := parseFlags([]string{"--addr", ":7777"})
	if opts.addr != ":7777" {
		t.Fatalf("explicit --addr = %q, want :7777", opts.addr)
	}
	opts = parseFlags([]string{"--addr", "0.0.0.0:9000"})
	if opts.addr != "0.0.0.0:9000" {
		t.Fatalf("explicit --addr = %q, want 0.0.0.0:9000", opts.addr)
	}
}
