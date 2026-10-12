package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// The token table stores each payload tagged with its flow
// ("magiclink:<email>", "pwreset:<user id>", "verify:<user id>"), but the
// magic-link eraser matched the bare email and nothing matched the other
// two. An erasure reported success while every live magic link, reset link
// and verification link of the erased user survived it. The earlier tests
// seeded bare emails, a row shape the battery never writes.
func TestEraseReachesTaggedTokens(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()

	users := NewEntityUserStore(db, "users")
	links, err := NewSQLMagicLinkTokenStore(db)
	if err != nil {
		t.Fatalf("token store: %v", err)
	}
	mgr := New(AuthConfig{
		JWTSecret:     "erase-tagged",
		SessionCookie: "session_id",
		SessionTTL:    time.Hour,
		UserStore:     users,
		SessionStore:  NewEntitySessionStore(db, "sessions"),
	})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{TokenStore: links, DevMode: true}))
	mgr.Use(NewPasswordResetPlugin(PasswordResetConfig{TokenStore: links, DevMode: true}))
	mgr.Use(NewEmailVerificationPlugin(EmailVerificationConfig{TokenStore: links, DevMode: true}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	mint := func(email string) string {
		t.Helper()
		u, err := users.CreateUser(ctx, email, "unused-hash", []string{"user"})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		id := u.GetID()
		for _, tc := range []struct {
			p       tokenPurpose
			payload string
		}{{purposeMagicLink, email}, {purposeReset, id}, {purposeVerify, id}} {
			if _, err := createPurposeToken(ctx, links, tc.p, tc.payload, time.Hour); err != nil {
				t.Fatalf("mint %s: %v", tc.p, err)
			}
		}
		return id
	}
	victim := mint("victim@erase.example")
	keeper := mint("keeper@erase.example")

	count := func(payload string) int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM magic_link_tokens WHERE email = ?`, payload).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	app := framework.NewApp(framework.WithDB(db))
	if _, err := app.EraseUserData(ctx, victim); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	for _, payload := range []string{"magiclink:victim@erase.example", "pwreset:" + victim, "verify:" + victim} {
		if n := count(payload); n != 0 {
			t.Errorf("token %q survived the erasure (%d row)", payload, n)
		}
	}
	for _, payload := range []string{"magiclink:keeper@erase.example", "pwreset:" + keeper, "verify:" + keeper} {
		if n := count(payload); n != 1 {
			t.Errorf("keeper token %q: %d rows, want 1", payload, n)
		}
	}
}
