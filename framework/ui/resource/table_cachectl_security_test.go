package resource

// Pins: the island table fragment carries Cache-Control: no-store —
// its rows are scoped to the caller, so no intermediary or bfcache may
// retain one user's fragment at another's URL.
// Property: a GET fragment whose rows are scoped to the caller carries
// Cache-Control: no-store — battery/admin/fragmentcache_security_test.go
// pins the admin twin of this exact fragment ("an intermediary or the
// back/forward cache must never be able to retain one admin's row
// fragment"); core-ui/widget /state's no-store pin is the same
// fragment-freshness argument.
// Surfaces: framework/ui/resource/resource.go::TableHandler :561-611 —
// gates (sign-in/IslandPolicy/read-permission) then Content-Type only at
// :609 and the caller's rows as HTML; no CC, no Vary.
// Finding: cmd/gofastr/blueprint.go:5733-5735 emits a TableHandler GET
// route for EVERY list-bearing entity in every generated app (e.g.
// examples/meridian/app.go:317 wires customersList behind cookie login) —
// a shared cache that stores no-freshness 200s replays user A's row
// fragment at user B's URL; the fragment is also the data-fui-poll/rpc
// freshness source.
// Fix direction: Cache-Control: no-store beside the Content-Type set.

import (
	"strings"
	"testing"
)

func TestTableFragmentNoStore(t *testing.T) {
	cfg := islandConfig(&stubSource{rows: []map[string]any{
		{"id": "s1", "name": "caller-row"},
	}})
	rr := islandRequest(t, cfg, struct{}{})

	if rr.Code != 200 {
		t.Fatalf("setup broken: status %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("setup broken: content type %q", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "caller-row") {
		t.Fatalf("setup broken: fragment has no rows: %s", rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("SECURITY: [table-fragment-cachectl] signed-in island table fragment carries Cache-Control %q — TableHandler sets only Content-Type, and the codegen ships this route for every list-bearing entity behind the default cookie login, so a shared cache or bfcache can retain one user's row fragment and replay it at another's URL (the admin battery pins no-store for its exact twin)", cc)
	}
}
