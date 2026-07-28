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

// An embed host gets no per-boot fallback key. A session that fails to verify
// is re-minted on the next render and nobody notices; an embed nonce that fails
// to verify is gone — single-use, one-minute life, already rendered into a page
// on someone else's site that this app cannot re-render. So the secret is
// required, and its absence is a boot failure rather than embeds that break on
// every restart and on every second replica.
func TestEmbedKeysRequireASecret(t *testing.T) {
	if _, _, err := embedKeysForMount(nil); err == nil {
		t.Fatal("embed keys were derived without an app secret")
	}

	secret := []byte("test-secret-test-secret-test-sec")
	nonceKey, grantKey, err := embedKeysForMount(secret)
	if err != nil {
		t.Fatalf("embedKeysForMount: %v", err)
	}
	if len(nonceKey) != 32 || len(grantKey) != 32 {
		t.Fatalf("key lengths = %d / %d, want 32 / 32", len(nonceKey), len(grantKey))
	}
	// The two must differ from each other and from the session key, or a
	// credential minted for one purpose verifies for another.
	sessionKey, err := sessionKeyForMount(secret, false)
	if err != nil {
		t.Fatalf("sessionKeyForMount: %v", err)
	}
	keys := map[string]string{
		"nonce":   string(nonceKey),
		"grant":   string(grantKey),
		"session": string(sessionKey),
	}
	seen := map[string]string{}
	for name, k := range keys {
		if other, dup := seen[k]; dup {
			t.Fatalf("the %s and %s keys are identical — HKDF domain separation is not doing anything", name, other)
		}
		seen[k] = name
	}

	// Derivation is deterministic: the same secret on another replica must
	// produce the same keys, or a nonce minted on one never verifies on the
	// other.
	n2, g2, err := embedKeysForMount(secret)
	if err != nil {
		t.Fatalf("embedKeysForMount: %v", err)
	}
	if string(n2) != string(nonceKey) || string(g2) != string(grantKey) {
		t.Fatal("key derivation is not deterministic — replicas would not accept each other's tokens")
	}
}
