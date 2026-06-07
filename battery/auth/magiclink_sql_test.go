package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// compile-time interface check.
var _ MagicLinkTokenStore = (*SQLMagicLinkTokenStore)(nil)

func newSQLStore(t *testing.T) *SQLMagicLinkTokenStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver unavailable")
	}
	t.Cleanup(func() { db.Close() })
	s, err := NewSQLMagicLinkTokenStore(db)
	if err != nil {
		t.Fatalf("NewSQLMagicLinkTokenStore: %v", err)
	}
	return s
}

func TestSQLMagicLink_CreateRedeem(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	tok, err := s.CreateToken(ctx, "a@b.dev", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	email, err := s.RedeemToken(ctx, tok)
	if err != nil || email != "a@b.dev" {
		t.Fatalf("redeem = %q, %v; want a@b.dev", email, err)
	}
}

func TestSQLMagicLink_SingleUse(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	tok, _ := s.CreateToken(ctx, "a@b.dev", time.Minute)
	if _, err := s.RedeemToken(ctx, tok); err != nil {
		t.Fatalf("first redeem: %v", err)
	}
	if _, err := s.RedeemToken(ctx, tok); err != ErrTokenNotFound {
		t.Errorf("second redeem err = %v, want ErrTokenNotFound (single-use)", err)
	}
}

func TestSQLMagicLink_Expired(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	tok, _ := s.CreateToken(ctx, "a@b.dev", -time.Second) // already expired
	if _, err := s.RedeemToken(ctx, tok); err != ErrTokenNotFound {
		t.Errorf("expired redeem err = %v, want ErrTokenNotFound", err)
	}
}

func TestSQLMagicLink_Unknown(t *testing.T) {
	s := newSQLStore(t)
	if _, err := s.RedeemToken(context.Background(), "nope"); err != ErrTokenNotFound {
		t.Errorf("unknown redeem err = %v, want ErrTokenNotFound", err)
	}
}

func TestSQLMagicLink_Cleanup(t *testing.T) {
	s := newSQLStore(t)
	ctx := context.Background()
	_, _ = s.CreateToken(ctx, "live@b.dev", time.Minute)
	_, _ = s.CreateToken(ctx, "dead@b.dev", -time.Second)
	n, err := s.Cleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("cleanup purged %d, want 1", n)
	}
}
