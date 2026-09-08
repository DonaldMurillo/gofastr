package main

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Pins: the session store is BOUNDED, the examples/site/demo_store.go
// shape — a hard LRU cap (maxLiveSessions=128) evicts on every create,
// and a background janitor (sweepExpired) reclaims expired sessions and
// support credentials so the 10-minute TTL is not only lazy.
//
// Surfaces: session.go::createSession (LRU insert + evictOverflowLocked),
// lookup (LRU refresh), sweepExpired/janitor (idle reclaim of sessions
// and supporters). Before the fix a support-key holder scripting
// POST /support/sessions grew the sessions map, the byToken map, and one
// resident channel goroutine per session without bound, and a session
// nobody revisited was NEVER dropped despite the TTL (dropLocked ran
// only on an exact-id revisit).

// TestSessionStoreBounded: 300 scripted POST /support/sessions from one
// logged-in support key. The store must hold at most the cap afterwards.
func TestSessionStoreBounded(t *testing.T) {
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
	const cap = 128
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

	if live > cap {
		t.Errorf("SECURITY: [webmcp-sessionstore-unbounded] %d POSTs to /support/sessions by one support-key holder left %d live sessions resident (bound %d): %d join-token entries and ~%d channel goroutines are held for sessions nobody will ever revisit — the store is a memory-exhaustion target a single valid credential can grow without limit",
			admitted, live, cap, tokens, goroutines-baseGoroutines)
	}
}

// TestSessionStoreSweepReclaims: the idle sweep half of the bound — an
// expired session and an expired support credential are both reclaimed
// by sweepExpired (what the janitor runs on a cadence), not left
// resident until an exact-id revisit.
func TestSessionStoreSweepReclaims(t *testing.T) {
	srv, a := newTestApp(t)

	// One live session via the real path, then backdate its expiry.
	cookie := supportLogin(t, srv)
	resp, err := noRedirectClient.Do(withCookie(t, srv, cookie, "POST", "/support/sessions", nil))
	if err != nil {
		t.Fatalf("setup broken: create session: %v", err)
	}
	resp.Body.Close()
	a.mu.Lock()
	var sid string
	for id, s := range a.sessions {
		sid = id
		s.expires = time.Now().Add(-time.Second) // idle beyond sessionTTL
	}
	// And one stale support credential past its cookieTTL.
	stale := "stale-support-credential"
	a.supporters[stale] = time.Now().Add(-time.Second)
	a.mu.Unlock()

	a.sweepExpired()
	a.mu.Lock()
	live, tokens := len(a.sessions), len(a.byToken)
	supporters := len(a.supporters)
	a.mu.Unlock()
	if live != 0 || tokens != 0 {
		t.Errorf("SECURITY: [webmcp-sessionstore-unbounded] sweepExpired left %d sessions and %d join tokens resident after expiry — the TTL must reclaim a session nobody revisits (channel goroutine, token entry, sockets), not only an exact-id lookup", live, tokens)
	}
	if a.supporterAlive(stale) {
		t.Errorf("SECURITY: [webmcp-sessionstore-unbounded] sweepExpired left an expired support credential resident (supporters=%d) — a 12h cookieTTL nothing sweeps is an unbounded map of client-minted state", supporters)
	}
	if a.lookup(sid) != nil {
		t.Error("SECURITY: [webmcp-sessionstore-unbounded] lookup returned a session whose expiry the sweep should have reclaimed")
	}
}

// supporterAlive reports whether a support credential is still resident.
func (a *assistApp) supporterAlive(v string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.supporters[v]
	return ok
}
