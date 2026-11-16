package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Found in review of the emailed-code plugin: two ways an erasure missed the
// email-keyed sign-in tokens (magic link, emailed code). A user row stored
// before canonicalization ("Victim@...") bound its raw spelling against
// tokens holding the canonical one, and the in-memory token store's eraser
// ran after the user row was gone, with no email to match. Both stores are
// erased here through App.EraseUserData, from a mixed-case user row.
func TestEraseReachesEmailKeyedTokensFromLegacyRow(t *testing.T) {
	for _, store := range []string{"sql", "memory"} {
		t.Run(store, func(t *testing.T) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			ctx := context.Background()

			users := NewEntityUserStore(db, "users")
			var tokens MagicLinkTokenStore = NewMemoryMagicLinkTokenStore()
			if store == "sql" {
				if tokens, err = NewSQLMagicLinkTokenStore(db); err != nil {
					t.Fatalf("token store: %v", err)
				}
			}
			mgr := New(AuthConfig{
				JWTSecret: "erase-email-tokens", SessionCookie: "session_id", SessionTTL: time.Hour,
				UserStore: users, SessionStore: NewEntitySessionStore(db, "sessions"),
				AllowInMemoryStores: true,
			})
			mgr.Use(NewCorePlugin())
			mgr.Use(NewMagicLinkPlugin(MagicLinkConfig{TokenStore: tokens, DevMode: true}))
			mgr.Use(NewEmailCodePlugin(EmailCodeConfig{TokenStore: tokens, DevMode: true}))
			if err := mgr.Init(nil); err != nil {
				t.Fatalf("Init: %v", err)
			}
			t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })

			mint := func(stored, canonical string) string {
				t.Helper()
				u, err := users.CreateUser(ctx, canonical, "unused-hash", []string{"user"})
				if err != nil {
					t.Fatalf("CreateUser: %v", err)
				}
				// The spelling a row written before canonicalization kept.
				if _, err := db.ExecContext(ctx, `UPDATE users SET email = ? WHERE id = ?`, stored, u.GetID()); err != nil {
					t.Fatalf("legacy spelling: %v", err)
				}
				for _, p := range []tokenPurpose{purposeMagicLink, purposeEmailCode} {
					if _, err := createPurposeToken(ctx, tokens, p, canonical, time.Hour); err != nil {
						t.Fatalf("mint %s: %v", p, err)
					}
				}
				return u.GetID()
			}
			victim := mint("Victim@Erase.Example", "victim@erase.example")
			mint("Keeper@Erase.Example", "keeper@erase.example")

			// Counted by purging what is left: the erasure must already
			// have taken every victim token, and none of the keeper's.
			peeker := tokens.(MagicLinkTokenPurger)
			app := framework.NewApp(framework.WithDB(db))
			rep, err := app.EraseUserData(ctx, victim)
			if err != nil {
				t.Fatalf("EraseUserData: %v", err)
			}
			for _, payload := range []string{"magiclink:victim@erase.example", "emailcode:victim@erase.example"} {
				if n, _ := peeker.DeleteTokensForPayload(ctx, payload); n != 0 {
					t.Errorf("token %q survived the erasure (%d); report %+v", payload, n, rep)
				}
			}
			for _, payload := range []string{"magiclink:keeper@erase.example", "emailcode:keeper@erase.example"} {
				if n, _ := peeker.DeleteTokensForPayload(ctx, payload); n != 1 {
					t.Errorf("keeper token %q: %d, want 1", payload, n)
				}
			}
		})
	}
}
