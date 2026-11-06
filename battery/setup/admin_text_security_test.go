package setup

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

func TestAdminRefusesUnstorableEmail(t *testing.T) {
	for _, invalid := range []string{"bad\x00@example.com", "bad\xff\u200b@example.com", "bad\xe2\x82@example.com"} {
		for _, override := range []func(string) (string, error){nil, func(string) (string, error) { return "safe@example.com", nil }, func(string) (string, error) { return invalid, nil }} {
			database := setupSQLite(t)
			manager := auth.New(auth.AuthConfig{UserStore: auth.NewEntityUserStore(database, "auth_users"), DevMode: true, CanonicalizeEmail: override})
			if err := manager.Init(nil); err != nil {
				t.Fatal(err)
			}
			step, _ := AdminStep(manager, database, "auth_users")
			if err := step.Run(t.Context(), map[string]string{"ADMIN_EMAIL": invalid, "ADMIN_PASSWORD": "securepass123"}); err == nil {
				t.Errorf("admin step accepts %q", invalid)
			}
			if override != nil {
				manager = auth.New(auth.AuthConfig{CanonicalizeEmail: func(string) (string, error) { return invalid, nil }})
				if _, err := canonicalAdminEmail(manager, "safe@example.com"); err == nil {
					t.Errorf("admin override emits %q", invalid)
				}
			}
			var count int
			if err := database.QueryRow(`SELECT COUNT(*) FROM auth_users`).Scan(&count); err != nil || count != 0 {
				t.Errorf("refused step wrote %d accounts: %v", count, err)
			}
		}
	}
}
