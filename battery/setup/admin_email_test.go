package setup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

// Login canonicalizes the submitted address and FindByEmail matches it
// exactly, so an admin stored under the raw "Ops@Example.com" could never
// log in. The step stores the address login will look up.
func TestAdminStepCanonicalizesEmail(t *testing.T) {
	ctx := context.Background()

	t.Run("default canonicalization", func(t *testing.T) {
		mgr, db := setupAuthManager(t)
		step, _ := AdminStep(mgr, db, "auth_users")
		if err := step.Run(ctx, map[string]string{
			"ADMIN_EMAIL":    " Ops@Example.com" + string(rune(0xFEFF)),
			"ADMIN_PASSWORD": "securepass123",
		}); err != nil {
			t.Fatalf("AdminStep.Run: %v", err)
		}
		want, _ := auth.CanonicalEmail("Ops@Example.com")
		if _, _, err := mgr.UserStore().FindByEmail(ctx, want); err != nil {
			t.Fatalf("admin not found under the login spelling %q: %v", want, err)
		}
	})

	t.Run("host override", func(t *testing.T) {
		db := setupSQLite(t)
		mgr := auth.New(auth.AuthConfig{
			JWTSecret: "test-secret",
			UserStore: auth.NewEntityUserStore(db, "auth_users"),
			DevMode:   true,
			CanonicalizeEmail: func(e string) (string, error) {
				if strings.Contains(e, "!") {
					return "", errors.New("refused")
				}
				local, domain, _ := strings.Cut(strings.ToLower(e), "@")
				local, _, _ = strings.Cut(local, "+")
				return local + "@" + domain, nil
			},
		})
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("auth Init: %v", err)
		}
		step, _ := AdminStep(mgr, db, "auth_users")
		if err := step.Run(ctx, map[string]string{
			"ADMIN_EMAIL":    "Ops+setup@Example.com",
			"ADMIN_PASSWORD": "securepass123",
		}); err != nil {
			t.Fatalf("AdminStep.Run: %v", err)
		}
		if _, _, err := mgr.UserStore().FindByEmail(ctx, "ops@example.com"); err != nil {
			t.Fatalf("admin not stored under the host's canonical form: %v", err)
		}
		if err := step.Run(ctx, map[string]string{
			"ADMIN_EMAIL":    "bad!@example.com",
			"ADMIN_PASSWORD": "securepass123",
		}); err == nil {
			t.Fatal("an address the host's canonicalizer refuses must fail the step")
		}
	})
}
