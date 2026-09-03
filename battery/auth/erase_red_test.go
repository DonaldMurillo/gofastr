//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: importing battery/auth makes user erasure complete under the
// battery's own documented default table wiring (users / sessions /
// auth_twofa / users_oauth_links / magic_link_tokens).
// Surfaces: erase.go:init (registered erasers + IdentityEmail resolver),
// entity_store.go:NewEntityUserStore/NewEntitySessionStore (documented wiring
// "users"/"sessions" in agents.md and framework docs auth.md),
// entity_twofa_store.go (documented table "auth_twofa"), entity_oauth_links.go
// ("<user table>_oauth_links" convention → users_oauth_links),
// magiclink_sql.go (default table "magic_link_tokens").
// Finding: the erasers are registered under the auth_users/auth_sessions
// table names while the documented wiring names the tables users/sessions,
// and the IdentityEmail resolver reads auth_users — so under the documented
// defaults App.EraseUserData skips the user row, the session rows and the
// magic-link tokens (absent-table skip), leaving a still-loginable
// half-erased identity.
// Fix direction: register the erasers (and the IdentityEmail resolver)
// against the table names the battery's own stores are documented to use, or
// resolve the live table names from the configured stores at erase time.

package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func TestEraseRedCoversDefaultWiring(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	// The battery's documented default wiring: agents.md's setup snippet and
	// framework docs (auth.md) use NewEntityUserStore(db, "users") +
	// NewEntitySessionStore(db, "sessions"); entity_twofa_store.go's own
	// usage doc and auth.md use NewEntityTwoFAStore(db, "auth_twofa"); the
	// oauth-links table follows the "<user table>_oauth_links" convention
	// (users → users_oauth_links); NewSQLMagicLinkTokenStore defaults to
	// magic_link_tokens.
	users := NewEntityUserStore(db, "users")
	sessions := NewEntitySessionStore(db, "sessions")
	twofaRows := NewEntityTwoFAStore(db, "auth_twofa")
	if err := users.EnsureSchema(ctx); err != nil { // also creates users_oauth_links
		t.Fatalf("users EnsureSchema: %v", err)
	}
	if err := sessions.EnsureSchema(ctx); err != nil {
		t.Fatalf("sessions EnsureSchema: %v", err)
	}
	if err := twofaRows.EnsureSchema(ctx); err != nil {
		t.Fatalf("auth_twofa EnsureSchema: %v", err)
	}
	magicLinks, err := NewSQLMagicLinkTokenStore(db)
	if err != nil {
		t.Fatalf("NewSQLMagicLinkTokenStore: %v", err)
	}

	// One user's worth of rows in every table.
	email := "erase-red@example.com"
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	u, err := users.CreateUser(ctx, email, hash, []string{"user"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID := u.GetID()
	if _, err := sessions.Create(ctx, userID, time.Hour); err != nil {
		t.Fatalf("session Create: %v", err)
	}
	if err := users.LinkOAuth(ctx, userID, "google", "red-1"); err != nil {
		t.Fatalf("LinkOAuth: %v", err)
	}
	if err := twofaRows.SetTwoFA(ctx, userID, &TwoFAState{Enabled: true, Secret: GenerateSecret(), Verified: true}); err != nil {
		t.Fatalf("SetTwoFA: %v", err)
	}
	if _, err := magicLinks.CreateToken(ctx, email, time.Hour); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	rowsFor := func(table, col, val string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM "`+table+`" WHERE "`+col+`" = ?`, val).Scan(&n); err != nil {
			t.Fatalf("count %s.%s: %v", table, col, err)
		}
		return n
	}
	tables := map[string]struct{ col, val string }{
		"users":             {"id", userID},
		"sessions":          {"user_id", userID},
		"auth_twofa":        {"user_id", userID},
		"users_oauth_links": {"user_id", userID},
		"magic_link_tokens": {"email", email},
	}
	for table, m := range tables {
		if n := rowsFor(table, m.col, m.val); n == 0 {
			t.Fatalf("setup bug: %s holds no row for the user before erasure", table)
		}
	}

	// The erasure entry point battery/auth's init() registrations target.
	app := framework.NewApp(framework.WithDB(db))
	if _, err := app.EraseUserData(ctx, userID); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}

	for table, m := range tables {
		if n := rowsFor(table, m.col, m.val); n != 0 {
			t.Errorf("%s still holds %d row(s) for the erased user under the battery's documented default wiring; importing battery/auth must cover its own documented tables", table, n)
		}
	}
}
