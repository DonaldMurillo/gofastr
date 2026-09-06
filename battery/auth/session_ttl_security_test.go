package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Pins that a negative session TTL fails closed on every built-in session
// store, found by the 2026-09-04 red-probe round; fixed by making
// MemorySessionStore.Create reject ttl < 0 (ttl == 0 keeps the documented
// one-week default) instead of silently substituting it, so both stores
// agree instead of the memory store minting its longest session from the
// caller's most restrictive input.
// Family: F12 Clock and expiry semantics (non-positive TTL at session creation)
// Property: a non-positive session TTL fails closed on EVERY built-in session
// store — a caller that (mis)configures a zero-or-negative lifetime never
// receives a live session, and above all never a MAXIMAL one. Zero is the
// documented "use the default" sentinel on the memory store; negative is
// rejected by both stores.
// Surfaces: session.go::MemorySessionStore.Create, entity_store.go::
// EntitySessionStore.Create. Handlers pass AuthConfig.SessionTTL straight
// through (core.go loginHandler/registerHandler, magiclink.go verifyHandler),
// so the store is where the semantics land.

// TestSessionNegativeTTLFailsClosed feeds a negative TTL to both built-in
// session stores and asserts the resulting session is never live.
func TestSessionNegativeTTLFailsClosed(t *testing.T) {
	ctx := context.Background()

	mem := NewMemorySessionStore()
	s1, err := mem.Create(ctx, "u-neg", -time.Hour)
	if err == nil {
		// Explicit rejection (mirroring EntitySessionStore) is the
		// preferred fail-closed answer; minting an already-expired
		// session would be acceptable only if it is never live.
		if _, gerr := mem.Get(ctx, s1.Token); gerr == nil {
			t.Errorf("SECURITY: [session-ttl] MemorySessionStore.Create(ttl=-1h) minted a LIVE session "+
				"(ExpiresAt=%v): a negative TTL must fail closed (error or already-expired), as "+
				"EntitySessionStore does; only ttl==0 is documented as the default.", s1.ExpiresAt)
		}
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver unavailable: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ent := NewEntitySessionStore(db, "sessions_negttl")
	if err := ent.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	s2, err := ent.Create(ctx, "u-neg", -time.Hour)
	if err != nil {
		// Explicit rejection is the strongest fail-closed answer.
		return
	}
	if _, err := ent.Get(ctx, s2.Token); err == nil {
		t.Errorf("SECURITY: [session-ttl] EntitySessionStore.Create(ttl=-1h) minted a live session "+
			"(ExpiresAt=%v); a negative TTL must be already-expired here too", s2.ExpiresAt)
	}
}
