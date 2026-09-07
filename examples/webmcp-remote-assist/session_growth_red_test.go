//go:build red

package main

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
//
// Property: an authenticated session-creation endpoint bounds the in-process
// state each accepted request mints (hard cap or background TTL sweeper) —
// the repo's own convention for cookie-minting demo state
// (examples/site/demo_store.go: "a flood of cookie-minting requests evicts
// the least-recently-used session rather than growing memory without
// bound"; "an unbounded slice is a memory-exhaustion target").
//
// Surfaces: session.go::createSession (:224-240 — randomID x2, map inserts
// into sessions/byToken, one `go s.channel.Run()` per accepted POST),
// session.go::lookup (:271-283 — expiry is LAZY: dropLocked runs only on an
// exact-id revisit), session.go::mintSupportCookie (:350-358 — supporters
// entries carry a 12h TTL nothing ever sweeps), main.go::127-128
// (POST /support/sessions gated by the support key alone — no cap, no rate
// limit).
//
// Finding (verified: no ticker/janitor/sweep anywhere in the package): a
// support-key holder scripting POST /support/sessions grows the sessions
// map, the byToken map, and one resident channel goroutine per session
// without bound; a session nobody revisits is NEVER dropped despite the
// 10-minute sessionTTL. Proposed bound: 128 live sessions (the SSE
// seat-cap order of magnitude, core/stream's defaultSeatsPerPrincipal=16,
// sized up for whole assist sessions). Fix direction: hard LRU cap +
// background idle sweep, the shape demo_store.go already ships.
//
// Sibling conventions: main_test.go (newTestApp / supportLogin /
// noRedirectClient / withCookie).

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
)

// TestSessionStoreRedBounded: 300 scripted POST /support/sessions from one
// logged-in support key. The store must hold at most the proposed cap
// afterwards (refused past it, or least-recently-used evicted) — today every
// POST mints a session, a join-token entry, and a goroutine, and none of
// them are ever reclaimed without an exact-id revisit.
func TestSessionStoreRedBounded(t *testing.T) {
	srv, a := newTestApp(t)

	// Baseline first: a fresh app must start near-empty or the flood count
	// below proves nothing.
	a.mu.Lock()
	baseSessions := len(a.sessions)
	baseTokens := len(a.byToken)
	a.mu.Unlock()
	if baseSessions > 8 {
		t.Fatalf("setup broken: fresh app already holds %d sessions", baseSessions)
	}
	baseGoroutines := runtime.NumGoroutine()

	cookie := supportLogin(t, srv)

	const flood = 300
	const proposedCap = 128
	admitted := 0
	for i := range flood {
		resp, err := noRedirectClient.Do(withCookie(t, srv, cookie, "POST", "/support/sessions", nil))
		if err != nil {
			t.Fatalf("setup broken: POST #%d: %v", i+1, err)
		}
		loc := resp.Header.Get("Location")
		code := resp.StatusCode
		resp.Body.Close()
		// The first POST against an empty store must succeed; later ones
		// may legitimately be refused once a cap exists (that is the fix,
		// not a setup failure).
		if i == 0 && (code != http.StatusSeeOther || !strings.HasPrefix(loc, "/support/session/")) {
			t.Fatalf("setup broken: first POST = %d -> %q, want 303 to the session page", code, loc)
		}
		if code == http.StatusSeeOther {
			admitted++
		}
	}

	a.mu.Lock()
	live := len(a.sessions)
	tokens := len(a.byToken)
	a.mu.Unlock()
	goroutines := runtime.NumGoroutine()
	t.Logf("flood=%d admitted=%d sessions=%d (baseline %d) byToken=%d (baseline %d) goroutines %d -> %d",
		flood, admitted, live, baseSessions, tokens, baseTokens, baseGoroutines, goroutines)

	if live > proposedCap {
		t.Errorf("SECURITY: [webmcp-sessionstore-unbounded] %d POSTs to /support/sessions by one support-key holder left %d live sessions resident (proposed bound %d): %d join-token entries and ~%d channel goroutines are held for sessions nobody will ever revisit, and the 10-minute TTL is lazy (dropped only on an exact-id lookup) so nothing reclaims them — the store is a memory-exhaustion target a single valid credential can grow without limit",
			admitted, live, proposedCap, tokens, goroutines-baseGoroutines)
	}
}
