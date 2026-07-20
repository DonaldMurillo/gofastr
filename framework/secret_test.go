package framework

import (
	"bytes"
	"strings"
	"testing"
)

func TestWithSecretRejectsShort(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithSecret accepted a short secret")
		}
	}()
	WithSecret("too-short")
}

func TestSecretEnvFallback(t *testing.T) {
	t.Setenv("GOFASTR_SECRET", strings.Repeat("e", 32))
	a := NewApp(WithoutDefaultMiddleware())
	if string(a.secret) != strings.Repeat("e", 32) {
		t.Fatal("GOFASTR_SECRET env was not picked up by NewApp")
	}
	// Explicit WithSecret wins over the env var.
	b := NewApp(WithoutDefaultMiddleware(), WithSecret(strings.Repeat("w", 32)))
	if string(b.secret) != strings.Repeat("w", 32) {
		t.Fatal("WithSecret did not win over GOFASTR_SECRET")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	a := deriveKey(secret, "purpose-a")
	b := deriveKey(secret, "purpose-a")
	if !bytes.Equal(a, b) {
		t.Fatal("same secret+purpose derived different keys")
	}
	if len(a) != 32 {
		t.Fatalf("derived key length = %d, want 32", len(a))
	}
}

func TestDeriveKeySeparatesPurposes(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	if bytes.Equal(deriveKey(secret, "purpose-a"), deriveKey(secret, "purpose-b")) {
		t.Fatal("different purposes derived the same key")
	}
}

func TestSessionKeySingleReplicaNoSecret(t *testing.T) {
	key, err := sessionKeyForMount(nil, false)
	if err != nil || key != nil {
		t.Fatalf("= %v, %v; want nil, nil (host self-mints)", key, err)
	}
}

func TestSessionKeyFanoutNoSecretFails(t *testing.T) {
	if _, err := sessionKeyForMount(nil, true); err == nil {
		t.Fatal("fanout without secret did not fail closed")
	}
}

func TestSessionKeyDerivedWhenSecretSet(t *testing.T) {
	secret := []byte(strings.Repeat("s", 32))
	key, err := sessionKeyForMount(secret, true)
	if err != nil || len(key) != 32 {
		t.Fatalf("= %v, %v; want 32-byte key, nil", key, err)
	}
	if bytes.Equal(key, secret[:32]) {
		t.Fatal("session key is the raw secret — must be derived")
	}
}
