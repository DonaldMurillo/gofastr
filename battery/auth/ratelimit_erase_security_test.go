package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// An erasure deletes the per-account login limiter's rows for the user:
// their key is the scope plus "account:" plus the email, so a surviving
// attempt or block row keeps the erased address. Another account's rows
// stay, and a host-set Scope is followed.
func TestEraseRemovesLoginLimitRows(t *testing.T) {
	for _, scope := range []string{"", "acct_login"} {
		t.Run("scope="+scope, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			ctx := context.Background()

			users := NewEntityUserStore(db, "users")
			sessions := NewEntitySessionStore(db, "sessions")
			if err := users.EnsureSchema(ctx); err != nil {
				t.Fatalf("users EnsureSchema: %v", err)
			}
			if err := sessions.EnsureSchema(ctx); err != nil {
				t.Fatalf("sessions EnsureSchema: %v", err)
			}
			limits := NewSQLRateLimitStore(db, "login_limits")
			cfg := RateLimiterConfig{MaxAttempts: 1, Window: time.Hour, BlockDuration: time.Hour, Store: limits, Scope: scope}
			mgr := New(AuthConfig{
				JWTSecret:                "erase-ratelimit", // not-a-secret: test fixture
				SessionCookie:            "session_id",
				SessionTTL:               time.Hour,
				UserStore:                users,
				SessionStore:             sessions,
				LoginRateLimitPerAccount: &cfg,
			})
			mgr.Use(NewCorePlugin())
			if err := mgr.Init(nil); err != nil {
				t.Fatalf("Init: %v", err)
			}

			hash, err := HashPassword("password123")
			if err != nil {
				t.Fatalf("HashPassword: %v", err)
			}
			const email, other = "erase-limit@example.com", "keep-limit@example.com"
			u, err := users.CreateUser(ctx, email, hash, []string{"user"})
			if err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			// The core plugin's own limiter, keyed as the login handler keys it.
			// Two attempts against MaxAttempts 1 leave an attempt row and a block.
			limiter := newScopedRateLimiter(cfg, "login_account")
			for _, e := range []string{email, email, other, other} {
				limiter.AllowContext(ctx, "account:"+e)
			}

			count := func(table, email string) int {
				t.Helper()
				var n int
				if err := db.QueryRow(`SELECT COUNT(*) FROM "`+table+`" WHERE rl_key LIKE ?`, "%account:"+email).Scan(&n); err != nil {
					t.Fatalf("count %s: %v", table, err)
				}
				return n
			}
			for _, table := range []string{"login_limits", "login_limits_attempts"} {
				if count(table, email) == 0 || count(table, other) == 0 {
					t.Fatalf("setup bug: %s holds no row for both accounts", table)
				}
			}

			app := framework.NewApp(framework.WithDB(db))
			if _, err := app.EraseUserData(ctx, u.GetID()); err != nil {
				t.Fatalf("EraseUserData: %v", err)
			}
			for _, table := range []string{"login_limits", "login_limits_attempts"} {
				if n := count(table, email); n != 0 {
					t.Errorf("%s still holds %d row(s) keyed by the erased email", table, n)
				}
				if count(table, other) == 0 {
					t.Errorf("%s lost another account's rows", table)
				}
			}
		})
	}
}
