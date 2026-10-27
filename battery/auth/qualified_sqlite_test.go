package auth

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// SQLite names the schema on the index, never on the indexed table, so a
// "main.<table>" store rendered CREATE INDEX ... ON "main"."<table>" and
// failed with a syntax error. TestQualifiedTableIndexes is the Postgres
// twin.
func TestSQLiteQualifiedTableIndexes(t *testing.T) {
	ctx := context.Background()
	stores := []struct {
		name, index string
		ensure      func(db *sql.DB) error
	}{
		{"user", "members_oauth_links_user_idx", func(db *sql.DB) error {
			return NewEntityUserStore(db, "main.members").EnsureSchema(ctx)
		}},
		{"session", "idx_sessions_expires", func(db *sql.DB) error {
			return NewEntitySessionStore(db, "main.sessions").EnsureSchema(ctx)
		}},
		{"passkey", "passkeys_user_id", func(db *sql.DB) error {
			pk, err := NewSQLPasskeyStore(db, "main.passkeys")
			if err != nil {
				return err
			}
			return pk.EnsureSchema(ctx)
		}},
		{"rate limit", "rl_attempts_key_time", func(db *sql.DB) error {
			return NewSQLRateLimitStore(db, "main.rl").EnsureSchema(ctx)
		}},
		{"api token", "tokens_owner_idx", func(db *sql.DB) error {
			_, err := NewSQLAPITokenStore(db, WithAPITokenTable("main.tokens"))
			return err
		}},
	}
	for _, s := range stores {
		t.Run(s.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = db.Close() })
			if err := s.ensure(db); err != nil {
				t.Fatalf("schema-qualified table: %v", err)
			}
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, s.index).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Fatalf("index %s count = %d, want 1", s.index, n)
			}
		})
	}
}
