package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// SECURITY TEST — fixed 2026-09-05, red-probe round 4.
// Family: F19 State accumulation by cheap or unauthenticated writes
// Property: rows that an unauthenticated request can mint must not outlive
// their usefulness — a store whose growth surface is anonymous must reap
// expired rows on its own write path, the SQLRateLimitStore.Allow posture
// ("Amortized sweep so abandoned keys don't accumulate forever").
// Surfaces: magiclink_sql.go::SQLMagicLinkTokenStore.CreateToken (the
// shared SQLMagicLinkTokenStore backs magic-link send AND password-reset
// AND email-verification tokens via createPurposeToken); the memory twin
// magiclink.go::MemoryMagicLinkTokenStore is reaped by the MagicLinkPlugin
// OnStart ticker, which also belt-and-braces the SQL store.
// Fix: CreateToken reaps expired rows at most once per
// magicLinkSweepInterval (the sweep runs after the insert, so an
// already-expired mint sweeps itself), and MagicLinkPlugin.OnStart runs a
// Cleanup ticker so hosts get reaping without wiring anything.

// TestMagicLinkStoreSweepsExpired mints an already-expired token and then a
// live one; the mint path must not leave the expired row behind.
func TestMagicLinkStoreSweepsExpired(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1) // :memory: is per-connection; one conn shares the DB

	s, err := NewSQLMagicLinkTokenStore(db)
	if err != nil {
		t.Fatalf("NewSQLMagicLinkTokenStore: %v", err)
	}
	ctx := context.Background()

	if _, err := s.CreateToken(ctx, "stale@example.com", -time.Second); err != nil {
		t.Fatalf("mint expired token: %v", err)
	}
	if _, err := s.CreateToken(ctx, "live@example.com", time.Hour); err != nil {
		t.Fatalf("mint live token: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM magic_link_tokens WHERE expires_at <= ?`,
		time.Now().Unix()).Scan(&n); err != nil {
		t.Fatalf("count expired rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("SECURITY: [magiclink-reaper] %d expired token rows survived a later mint — anonymous-minted rows are never reaped (no production caller of Cleanup), the table grows until the disk fills", n)
	}

	// Positive control: the live row is still there and still peeks.
	if _, err := s.PeekToken(ctx, mustLiveToken(t, s)); err != nil {
		t.Fatalf("live token no longer peeks after sweep: %v", err)
	}
}

// mustLiveToken re-mints and returns a token for the live address; the sweep
// assertion is about expired rows, not about the live flow breaking.
func mustLiveToken(t *testing.T, s *SQLMagicLinkTokenStore) string {
	t.Helper()
	tok, err := s.CreateToken(context.Background(), "live@example.com", time.Hour)
	if err != nil {
		t.Fatalf("re-mint live token: %v", err)
	}
	return tok
}
