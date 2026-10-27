package auth_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

type gateSender struct{}

func (gateSender) Send(_ context.Context, _, _ string) error          { return nil }
func (gateSender) SendMagicLink(_ context.Context, _, _ string) error { return nil }

// The link plugins mint their own in-memory token store when none is set:
// a link minted on one replica is invalid on the next. Production refuses
// it the way it refuses the in-memory session and 2FA stores; DevMode and
// AllowInMemoryStores boot.
func TestLinkPluginsRefuseMemoryStore(t *testing.T) {
	plugins := map[string]func() auth.AuthPlugin{
		"password-reset": func() auth.AuthPlugin {
			return auth.NewPasswordResetPlugin(auth.PasswordResetConfig{EmailSender: gateSender{}})
		},
		"magic-link": func() auth.AuthPlugin {
			return auth.NewMagicLinkPlugin(auth.MagicLinkConfig{EmailSender: gateSender{}})
		},
		"email-verification": func() auth.AuthPlugin {
			return auth.NewEmailVerificationPlugin(auth.EmailVerificationConfig{EmailSender: gateSender{}})
		},
	}
	for name, plugin := range plugins {
		t.Run(name, func(t *testing.T) {
			boot := func(cfg auth.AuthConfig) error {
				cfg.JWTSecret = "k"
				cfg.SessionStore = stubDurableSessions{}
				mgr := auth.New(cfg)
				mgr.Use(auth.NewCorePlugin())
				mgr.Use(plugin())
				return mgr.Init(nil)
			}
			err := boot(auth.AuthConfig{})
			if err == nil || !strings.Contains(err.Error(), "AllowInMemoryStores") {
				t.Fatalf("production boot on the in-memory token store = %v, want a refusal naming AllowInMemoryStores", err)
			}
			if err := boot(auth.AuthConfig{AllowInMemoryStores: true}); err != nil {
				t.Errorf("acknowledged single node refused: %v", err)
			}
			if err := boot(auth.AuthConfig{DevMode: true}); err != nil {
				t.Errorf("dev mode refused: %v", err)
			}
		})
	}
}
