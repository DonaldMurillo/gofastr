package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// TestAuthErasersRegistered verifies the auth-owned tables are registered for
// data erasure when this package is imported, the erase-plane mirror of
// TestAuthExportersRegistered.
func TestAuthErasersRegistered(t *testing.T) {
	want := map[string]bool{"auth_users": false, "auth_sessions": false}
	for _, e := range datexport.AllErasers() {
		if _, ok := want[e.Name]; ok && e.Source == "auth" {
			want[e.Name] = true
			if e.Mode != datexport.EraseDelete {
				t.Errorf("%s mode = %v, want EraseDelete", e.Name, e.Mode)
			}
			if e.Column == "" {
				t.Errorf("%s has no match column", e.Name)
			}
		}
	}
	wantCol := map[string]string{"auth_sessions": "user_id", "auth_users": "id"}
	for _, e := range datexport.AllErasers() {
		if c, ok := wantCol[e.Name]; ok && e.Source == "auth" && e.Column != c {
			t.Errorf("%s column = %q, want %q", e.Name, e.Column, c)
		}
	}
	for name, saw := range want {
		if !saw {
			t.Errorf("%s eraser not registered", name)
		}
	}
}

// Every auth table that holds per-user state must be reachable by
// App.EraseUserData. A table left unregistered survives an erasure,
// 2FA secrets and OAuth links are credential material, not metadata.
func TestAuthErasersCoverCredentialTables(t *testing.T) {
	want := map[string]string{
		"auth_sessions":     "user_id",
		"auth_users":        "id",
		"auth_twofa":        "user_id",
		"users_oauth_links": "user_id",
	}
	got := map[string]string{}
	for _, e := range datexport.AllErasers() {
		if e.Source == "auth" {
			got[e.Table] = e.Column
		}
	}
	for table, col := range want {
		if got[table] != col {
			t.Errorf("auth eraser for %q: got column %q, want %q", table, got[table], col)
		}
	}
}

// magic_link_tokens is keyed by EMAIL, not user id, so a plain user-id eraser
// cannot reach it. battery/auth closes the gap declaratively: it registers an
// IdentityEmail resolver (auth_users.id → email) and a magic_link_tokens eraser
// that declares IdentityEmail. App.EraseUserData then resolves the email at
// erase time and deletes the user's outstanding tokens, so a pre-erasure magic
// link can no longer re-create the erased account.
func TestAuthErasersCoverMagicLinkTokens(t *testing.T) {
	// The email identity resolver is registered against the user table.
	r, ok := datexport.ResolveIdentity(datexport.IdentityEmail)
	if !ok {
		t.Fatal("IdentityEmail resolver not registered")
	}
	if r.Table != "auth_users" || r.IDColumn != "id" || r.ValueColumn != "email" {
		t.Errorf("IdentityEmail resolver = %+v, want {Table:auth_users, IDColumn:id, ValueColumn:email}", r)
	}

	// The magic-link token eraser declares the email identity (email column).
	var ml *datexport.DataEraser
	for _, e := range datexport.AllErasers() {
		if e.Name == "magic_link_tokens" && e.Source == "auth" {
			cp := e
			ml = &cp
		}
	}
	if ml == nil {
		t.Fatal("magic_link_tokens eraser not registered")
	}
	if ml.Table != "magic_link_tokens" || ml.Column != "email" {
		t.Errorf("magic_link_tokens = {Table:%q, Column:%q}, want {magic_link_tokens, email}", ml.Table, ml.Column)
	}
	if ml.Mode != datexport.EraseDelete {
		t.Errorf("magic_link_tokens mode = %v, want EraseDelete", ml.Mode)
	}
	if ml.Identity != datexport.IdentityEmail {
		t.Errorf("magic_link_tokens Identity = %v, want IdentityEmail", ml.Identity)
	}
}

// ─── end-to-end: the registrations actually erase every linked row ─────────
//
// Property: App.EraseUserData(userID), run against the tables the auth
// battery itself creates, removes EVERY row tied to that user — the user
// row, their sessions, their 2FA enrollment, their OAuth links, and
// their outstanding magic-link tokens (via the email identity) — while
// another user's rows in the same tables survive untouched. A table the
// registrations fail to reach keeps credential material alive after the
// "right to be forgotten" call reported success.

// newEraseE2EDB provisions the canonical auth tables with the REAL store
// constructors (the exact DDL a host runs), seeds a victim and a keeper,
// and returns the db plus both user ids.
func newEraseE2EDB(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	users := NewEntityUserStore(db, "auth_users")
	if err := users.EnsureSchema(ctx); err != nil {
		t.Fatalf("users EnsureSchema: %v", err)
	}
	sessions := NewEntitySessionStore(db, "auth_sessions")
	if err := sessions.EnsureSchema(ctx); err != nil {
		t.Fatalf("sessions EnsureSchema: %v", err)
	}
	twofa := NewEntityTwoFAStore(db, "auth_twofa")
	if err := twofa.EnsureSchema(ctx); err != nil {
		t.Fatalf("twofa EnsureSchema: %v", err)
	}
	if _, err := NewSQLMagicLinkTokenStore(db); err != nil {
		t.Fatalf("magic-link store: %v", err)
	}
	// The canonical OAuth link table ("users_oauth_links" is derived from
	// a user table NAMED "users"; the erase registration pins that name).
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS users_oauth_links (
		provider TEXT NOT NULL, provider_id TEXT NOT NULL, user_id TEXT NOT NULL,
		email TEXT, name TEXT, avatar_url TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (provider, provider_id))`); err != nil {
		t.Fatalf("oauth links table: %v", err)
	}

	mk := func(email string) string {
		t.Helper()
		u, err := users.CreateUser(ctx, email, "hash", []string{"user"})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return u.GetID()
	}
	victim := mk("victim@erased.example")
	keeper := mk("keeper@keep.example")

	seed := func(stmt string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, stmt, args...); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	if _, err := sessions.Create(ctx, victim, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(ctx, keeper, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := twofa.SetTwoFA(ctx, victim, &TwoFAState{Enabled: true, Secret: "S1", BackupCodes: []string{"h1"}, Verified: true}); err != nil {
		t.Fatal(err)
	}
	if err := twofa.SetTwoFA(ctx, keeper, &TwoFAState{Enabled: true, Secret: "S2", BackupCodes: []string{"h2"}, Verified: true}); err != nil {
		t.Fatal(err)
	}
	seed(`INSERT INTO users_oauth_links (provider, provider_id, user_id, email) VALUES ('google', 'g-victim', $1, 'victim@erased.example')`, victim)
	seed(`INSERT INTO users_oauth_links (provider, provider_id, user_id, email) VALUES ('google', 'g-keeper', $1, 'keeper@keep.example')`, keeper)
	ml, err := NewSQLMagicLinkTokenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ml.CreateToken(ctx, "victim@erased.example", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := ml.CreateToken(ctx, "keeper@keep.example", time.Hour); err != nil {
		t.Fatal(err)
	}
	return db, victim, keeper
}

func countRows(t *testing.T, db *sql.DB, where string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM "+where, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", where, err)
	}
	return n
}

func TestAuthEraseEndToEndDeletesLinkedRows(t *testing.T) {
	db, victim, keeper := newEraseE2EDB(t)
	app := framework.NewApp(framework.WithDB(db))

	rep, err := app.EraseUserData(context.Background(), victim)
	if err != nil {
		t.Fatalf("EraseUserData: %v (report=%+v)", err, rep)
	}

	// Every victim-linked row must be gone…
	checks := []struct {
		table string
		where string
		args  []any
	}{
		{"auth_users", "auth_users WHERE id = $1", []any{victim}},
		{"auth_sessions", "auth_sessions WHERE user_id = $1", []any{victim}},
		{"auth_twofa", "auth_twofa WHERE user_id = $1", []any{victim}},
		{"users_oauth_links", "users_oauth_links WHERE user_id = $1", []any{victim}},
		{"magic_link_tokens", "magic_link_tokens WHERE email = $1", []any{"victim@erased.example"}},
	}
	for _, c := range checks {
		if n := countRows(t, db, c.where, c.args...); n != 0 {
			t.Errorf("SECURITY: [erase-completeness] %s kept %d row(s) for the erased user — credential material survives a reported-successful erasure", c.table, n)
		}
	}
	// …while the keeper's rows all survive.
	keep := []struct {
		where string
		args  []any
	}{
		{"auth_users WHERE id = $1", []any{keeper}},
		{"auth_sessions WHERE user_id = $1", []any{keeper}},
		{"auth_twofa WHERE user_id = $1", []any{keeper}},
		{"users_oauth_links WHERE user_id = $1", []any{keeper}},
		{"magic_link_tokens WHERE email = $1", []any{"keeper@keep.example"}},
	}
	for _, c := range keep {
		if n := countRows(t, db, c.where, c.args...); n != 1 {
			t.Errorf("erasure over-deleted: %q matched %d rows for the untouched keeper, want 1", c.where, n)
		}
	}
}

// Idempotent re-run: with the user row gone, the email identity cannot
// resolve, the magic-link eraser is SKIPPED (not failed), and the second
// erasure completes without error and without touching the keeper.
func TestAuthEraseIdempotentSecondRun(t *testing.T) {
	db, victim, keeper := newEraseE2EDB(t)
	app := framework.NewApp(framework.WithDB(db))
	ctx := context.Background()
	if _, err := app.EraseUserData(ctx, victim); err != nil {
		t.Fatalf("first erasure: %v", err)
	}
	rep, err := app.EraseUserData(ctx, victim)
	if err != nil {
		t.Fatalf("second erasure must succeed idempotently: %v (report=%+v)", err, rep)
	}
	if n := countRows(t, db, "auth_users WHERE id = $1", keeper); n != 1 {
		t.Errorf("second erasure disturbed the keeper: %d rows, want 1", n)
	}
	if n := countRows(t, db, "magic_link_tokens WHERE email = $1", "keeper@keep.example"); n != 1 {
		t.Errorf("second erasure disturbed the keeper's magic links: %d, want 1", n)
	}
}
