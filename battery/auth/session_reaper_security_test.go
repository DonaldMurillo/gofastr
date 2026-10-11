package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestMemorySessionStoreCreateSweepsExpiredSessions(t *testing.T) {
	store := NewMemorySessionStore()
	store.sessions["abandoned"] = &Session{Token: "abandoned", ExpiresAt: time.Now().Add(-time.Hour)}

	if _, err := store.Create(context.Background(), "fresh-user", time.Hour); err != nil {
		t.Fatalf("Create: %v", err)
	}
	store.mu.RLock()
	count := len(store.sessions)
	_, staleRetained := store.sessions["abandoned"]
	store.mu.RUnlock()
	if staleRetained || count != 1 {
		t.Fatalf("memory session store retained expired sessions after a new login: stale=%v count=%d", staleRetained, count)
	}
}

func TestEntitySessionStoreCreateSweepsExpiredSessions(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("CREATE TABLE sessions (id TEXT PRIMARY KEY, token TEXT NOT NULL UNIQUE, user_id TEXT NOT NULL, created_at DATETIME NOT NULL, expires_at DATETIME NOT NULL, two_factor_verified BOOLEAN NOT NULL DEFAULT FALSE, pending_two_factor BOOLEAN NOT NULL DEFAULT FALSE)"); err != nil {
		t.Fatalf("create sessions table: %v", err)
	}
	store := NewEntitySessionStore(db, "sessions")
	ctx := context.Background()
	insertExpiredSession(t, store, "abandoned-user")

	if _, err := store.Create(ctx, "fresh-user", time.Hour); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions WHERE expires_at < $1", time.Now().UTC()).Scan(&count); err != nil {
		t.Fatalf("count expired sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("SQL session store retained expired rows after a new login: %d remain", count)
	}
}
