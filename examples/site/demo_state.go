package main

// =============================================================================
// Per-visitor demo state. The interactive component demos (kanban sortable,
// optimistic create/delete, the interactive counter) mutate server state. On a
// public server that state cannot be one shared global: a single anonymous
// visitor could then vandalize the demo for everyone, and an unbounded slice
// (optimistic-create appends forever) is a memory-exhaustion target.
//
// So each browser gets its own slice of demo state, keyed by a site-owned
// `site-demo` cookie — deliberately NOT the framework session/auth cookie.
// This is an ISOLATION key, not an auth credential: anyone can set it, and
// copying someone else's value only lands you on the same harmless demo
// bucket. Flood protection is the bounded store (cap + TTL) plus the origin
// rate limit, not the cookie.
//
// The store (demoSessionStore, demo_store.go) carries both bounds that matter
// on a public surface: a hard LRU cap (the burst bound) and a TTL idle janitor
// (the steady-state bound).
// =============================================================================

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

const (
	// demoCookieName is the site-owned per-visitor isolation cookie. Not the
	// gofastr session cookie — demo isolation is a separate concern from auth.
	demoCookieName = "site-demo"

	// demoSessionTTL bounds how long an idle demo session survives. The store
	// slides it forward on every access; the cookie MaxAge below matches it and
	// is re-sent on every write so an actively-used session doesn't hit the
	// cookie's absolute expiry while the server still holds its state.
	demoSessionTTL = 30 * time.Minute

	// demoIDLen is the length of a minted id: 16 random bytes as lowercase
	// hex. The cookie value is validated to this exact shape before it's used
	// as a store key, so an attacker can't retain megabyte-sized keys and
	// defeat the entry-count memory bound.
	demoIDLen = 32

	// demoMaxSessions caps live demo sessions. An attacker minting cookies
	// can create at most this many before LRU eviction kicks in, bounding
	// total memory regardless of request rate.
	demoMaxSessions = 10_000

	// demoMaxCreateNotes caps the optimistic-create list per session so one
	// visitor's Add loop can't grow their own bucket without bound.
	demoMaxCreateNotes = 50
)

// demoState is one visitor's private copy of every stateful demo. Its own
// mutex guards all fields; render and mutate both take it.
type demoState struct {
	mu sync.Mutex

	kanban    []kanbanColumn
	kanbanVer int

	createNotes []optimisticNote
	createNext  int

	deleteNotes []optimisticNote

	counter int64
}

// seedDemoState builds a fresh demo session at its initial state — the same
// starting point every visitor sees on first contact and after a reset.
func seedDemoState() *demoState {
	return &demoState{
		kanban:      initialKanbanColumns(),
		kanbanVer:   1,
		createNotes: append([]optimisticNote(nil), initialOptimisticNotes...),
		createNext:  4,
		deleteNotes: append([]optimisticNote(nil), initialOptimisticNotes...),
	}
}

// demoSessions holds every visitor's demo state, bounded on both axes
// (see demo_store.go).
var demoSessions = newDemoSessionStore(demoMaxSessions, demoSessionTTL)

// resetDemoSessions clears every session so the next touch re-seeds. The e2e
// tests call this (via resetKanbanBoard / resetOptimisticNotes) for a clean
// slate between runs; each chromedp browser already gets its own cookie, so
// this is belt-and-suspenders isolation.
func resetDemoSessions() { demoSessions.clear() }

// demoStateWrite returns the caller's demo session, minting the site-demo
// cookie when absent or invalid. Use it from the /__site/* RPC handlers, which
// hold the ResponseWriter. The cookie is re-sent on every write so its MaxAge
// slides forward with activity (matching the store's sliding TTL). getOrCreate
// seeds a fresh session on first write.
func demoStateWrite(w http.ResponseWriter, r *http.Request) *demoState {
	id := readDemoCookie(r)
	if id == "" {
		id = newDemoID()
	}
	setDemoCookie(w, r, id)
	return demoSessions.getOrCreate(id)
}

// demoStateRead returns the caller's demo session for SSR — READ-ONLY, never
// mints a cookie or creates a store entry. Delegates to demoStateForRequest.
func demoStateRead(ctx context.Context) *demoState {
	return demoStateForRequest(app.RequestFromContext(ctx))
}

// demoStateForRequest is the read-only session lookup shared by SSR and the
// GET conflict handler. A nil request (static export / SSG), no cookie (first
// load, a crawler), an invalid cookie, or an expired/evicted session all
// render an ephemeral seed — the canonical starting point — WITHOUT populating
// or mutating the store beyond the existing entry's LRU/TTL touch. Only a write
// (an RPC via demoStateWrite) ever creates a session.
func demoStateForRequest(r *http.Request) *demoState {
	if r == nil {
		return seedDemoState()
	}
	id := readDemoCookie(r)
	if id == "" {
		return seedDemoState()
	}
	if s, ok := demoSessions.get(id); ok {
		return s
	}
	return seedDemoState()
}

// newDemoID returns a random 128-bit id as lowercase hex (demoIDLen chars).
// crypto/rand failure is effectively impossible; on the off chance it errors we
// fall back to a fixed, valid-shaped id rather than panic — worst case a few
// visitors share one demo bucket, harmless for throwaway state, and the value
// still passes isValidDemoID so it round-trips through the cookie.
func newDemoID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.Repeat("0", demoIDLen)
	}
	return hex.EncodeToString(b[:])
}

// readDemoCookie returns the site-demo cookie value when it has the exact shape
// of a minted id (demoIDLen lowercase-hex chars), else "". Rejecting any other
// value means an attacker-supplied cookie can never become an oversized store
// key: the write path mints a fresh bounded id and the read path renders a seed.
func readDemoCookie(r *http.Request) string {
	c, err := r.Cookie(demoCookieName)
	if err != nil || !isValidDemoID(c.Value) {
		return ""
	}
	return c.Value
}

// isValidDemoID reports whether s is exactly demoIDLen lowercase-hex chars.
func isValidDemoID(s string) bool {
	if len(s) != demoIDLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// setDemoCookie writes the site-demo cookie. Secure is set on TLS (direct or
// behind an https-terminating proxy) so it survives there, and left off on a
// plaintext loopback dev origin where a Secure cookie would be dropped. Not an
// auth cookie, so no __Host- prefix — just Lax + HttpOnly + a bounded MaxAge.
func setDemoCookie(w http.ResponseWriter, r *http.Request, id string) {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	http.SetCookie(w, &http.Cookie{
		Name:     demoCookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   int(demoSessionTTL / time.Second),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
