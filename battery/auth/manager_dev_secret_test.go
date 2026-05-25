package auth_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
)

func TestDevMode_MintsRandomJWTSecretWhenEmpty(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{DevMode: true})
	cfg := mgr.Config()
	if cfg.JWTSecret == "" {
		t.Fatal("DevMode with empty JWTSecret should mint a random one")
	}
	// 32 random bytes → base64 URL encoding is ~43 characters.
	if len(cfg.JWTSecret) < 32 {
		t.Fatalf("minted secret looks too short: len=%d", len(cfg.JWTSecret))
	}

	other := auth.New(auth.AuthConfig{DevMode: true}).Config().JWTSecret
	if other == cfg.JWTSecret {
		t.Fatal("two dev-mode managers minted the SAME secret — should be random per process")
	}
}

func TestDevMode_PreservesExplicitJWTSecret(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{DevMode: true, JWTSecret: "explicit-key"})
	if got := mgr.Config().JWTSecret; got != "explicit-key" {
		t.Fatalf("explicit secret overridden: %q", got)
	}
}

func TestProdMode_DoesNotAutoMintSecret(t *testing.T) {
	mgr := auth.New(auth.AuthConfig{}) // DevMode false
	if got := mgr.Config().JWTSecret; got != "" {
		t.Fatalf("prod-mode auto-minted JWTSecret: %q (should require explicit config)", got)
	}
	// Sanity: cookie name should be the secure prod default.
	if !strings.HasPrefix(mgr.Config().SessionCookie, "__Host-") {
		t.Fatalf("prod-mode SessionCookie = %q, want __Host- prefix", mgr.Config().SessionCookie)
	}
}
