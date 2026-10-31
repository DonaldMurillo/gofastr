package auth

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// With Scope.SoftDelete on the users entity, CRUD delete only stamps
// deleted_at, and the auth store queried by id or email alone: the trashed
// account still signed in, and its sessions, API tokens and JWTs kept
// working (2026-10-27 A29). AutoMigrate creates the entity's table, with
// deleted_at, before the auth battery's EnsureSchema runs; the store sees
// the column there and stops resolving a trashed row.
func TestSoftDeletedUserCannotSignIn(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	// The table AutoMigrate makes for a soft-deleting users entity.
	if _, err := db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL DEFAULT '', roles TEXT NOT NULL DEFAULT '[]',
		password_set INTEGER NOT NULL DEFAULT 0, deleted_at DATETIME)`); err != nil {
		t.Fatalf("create users: %v", err)
	}
	users := NewEntityUserStore(db, "users")
	mgr := New(AuthConfig{
		JWTSecret:     "softdelete-test-secret",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     users,
		SessionStore:  NewEntitySessionStore(db, "sessions"),
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("init auth: %v", err)
	}
	ctx := context.Background()
	hash, err := HashPassword("securepass123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u, err := users.CreateUser(ctx, "gone@example.com", hash, []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	r := mountRoutes(mgr)

	login := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"email": "gone@example.com", "password": "securepass123"})
		req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	me := func(session string) int {
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "session_id", Value: session})
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr.Code
	}
	rr := login()
	if rr.Code != http.StatusOK {
		t.Fatalf("setup: login = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var session string
	for _, c := range rr.Result().Cookies() {
		if c.Name == "session_id" {
			session = c.Value
		}
	}
	if code := me(session); code != http.StatusOK {
		t.Fatalf("setup: /auth/me = %d", code)
	}

	// CRUD's soft delete.
	if _, err := db.Exec(`UPDATE users SET deleted_at = ? WHERE id = ?`, time.Now().UTC(), u.GetID()); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := users.FindByID(ctx, u.GetID()); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SECURITY: FindByID of a trashed user = %v, want ErrUserNotFound", err)
	}
	if _, _, err := users.FindByEmail(ctx, "gone@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SECURITY: FindByEmail of a trashed user = %v, want ErrUserNotFound", err)
	}
	if list, total, err := users.ListUsers(ctx, ListUsersOptions{Limit: 10}); err != nil || len(list) != 0 || total != 0 {
		t.Errorf("ListUsers with only a trashed user = %d rows, total %d, err %v; want none", len(list), total, err)
	}
	if rr := login(); rr.Code == http.StatusOK {
		t.Error("SECURITY: a trashed user signed in with their password")
	}
	if code := me(session); code == http.StatusOK {
		t.Error("SECURITY: a trashed user's existing session still resolves")
	}

	// Restoring the row brings the account back.
	if _, err := db.Exec(`UPDATE users SET deleted_at = NULL WHERE id = ?`, u.GetID()); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := users.FindByID(ctx, u.GetID()); err != nil {
		t.Errorf("FindByID after restore: %v", err)
	}
	if rr := login(); rr.Code != http.StatusOK {
		t.Errorf("login after restore = %d", rr.Code)
	}
}

// The Postgres catalog probe finds the column the same way PRAGMA does.
func TestSoftDeletedUserHiddenPostgres(t *testing.T) {
	db := openPGForBattery(t)
	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN deleted_at TIMESTAMPTZ`); err != nil {
		t.Fatalf("add deleted_at: %v", err)
	}
	users := NewEntityUserStore(db, "users")
	ctx := context.Background()
	if err := users.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	u, err := users.CreateUser(ctx, "gone@example.com", "x", []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := db.Exec(`UPDATE users SET deleted_at = now() WHERE id = $1`, u.GetID()); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := users.FindByID(ctx, u.GetID()); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SECURITY: FindByID of a trashed user on Postgres = %v, want ErrUserNotFound", err)
	}
	if _, _, err := users.FindByEmail(ctx, "gone@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SECURITY: FindByEmail of a trashed user on Postgres = %v, want ErrUserNotFound", err)
	}
}
