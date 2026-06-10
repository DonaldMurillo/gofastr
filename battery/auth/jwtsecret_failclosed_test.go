package auth

import (
	"strings"
	"testing"
)

// Production mode (DevMode=false) with no JWTSecret must refuse to
// initialize — an empty HMAC key means forgeable JWTs. The error has to
// carry the remedy, not just the symptom. Previously this only warned.
func TestInit_ProdEmptySecretFailsClosed(t *testing.T) {
	mgr := New(AuthConfig{DevMode: false}) // JWTSecret empty
	err := mgr.Init(nil)
	if err == nil {
		t.Fatal("Init must fail closed with DevMode=false and no JWTSecret")
	}
	for _, want := range []string{"JWTSecret", "DevMode: true"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

func TestInit_ProdWithSecretOK(t *testing.T) {
	mgr := New(AuthConfig{DevMode: false, JWTSecret: "a-real-secret"})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init with explicit secret should succeed: %v", err)
	}
	if mgr.JWT() == nil {
		t.Error("JWT helper should be configured when a secret is set")
	}
}

func TestInit_DevModeEmptySecretOK(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true}) // defaults() mints a per-process secret
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("DevMode without JWTSecret should still boot: %v", err)
	}
}
