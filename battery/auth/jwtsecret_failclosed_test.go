package auth

import (
	"strings"
	"testing"
)

// Production mode (DevMode=false) with no JWTSecret must refuse to
// initialize, an empty HMAC key means forgeable JWTs. The error has to
// carry the remedy, not only the symptom. Previously this only warned.
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
	mgr := New(AuthConfig{DevMode: false, JWTSecret: "a-real-secret", AllowInMemoryStores: true}) // opt into the memory session store; this test targets the JWT-secret path
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init with explicit secret should succeed: %v", err)
	}
	if mgr.JWT() == nil {
		t.Error("JWT helper should be configured when a secret is set")
	}
}

// DevMode with no JWTSecret boots on a freshly minted random secret: tokens
// verify on the manager that signed them and on no other.
func TestInit_DevModeEmptySecretOK(t *testing.T) {
	boot := func() *AuthManager {
		mgr := New(AuthConfig{DevMode: true})
		if err := mgr.Init(nil); err != nil {
			t.Fatalf("DevMode without JWTSecret should still boot: %v", err)
		}
		if mgr.JWT() == nil {
			t.Fatal("DevMode boot left the JWT helper unset")
		}
		if n := len(mgr.Config().JWTSecret); n < 43 {
			t.Fatalf("minted JWTSecret is %d chars, want >= 43 (32 random bytes)", n)
		}
		return mgr
	}
	a, b := boot(), boot()
	token, err := a.JWT().GenerateToken(&testUser{id: "u1", email: "u1@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.JWT().ValidateToken(token); err != nil {
		t.Fatalf("token does not verify on its own manager: %v", err)
	}
	if _, err := b.JWT().ValidateToken(token); err == nil {
		t.Fatal("token verified on a second DevMode manager: the minted secret is not random")
	}
}
