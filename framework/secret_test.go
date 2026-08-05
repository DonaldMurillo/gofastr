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

// --- Graceful rotation (GOFASTR_SECRET / WithSecretRotation) ---

func TestWithSecretRotationRejectsShortCurrent(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithSecretRotation accepted a short current secret")
		}
	}()
	WithSecretRotation("too-short", strings.Repeat("p", 32))
}

func TestWithSecretRotationRejectsShortPrevious(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("WithSecretRotation accepted a short previous secret")
		}
	}()
	WithSecretRotation(strings.Repeat("c", 32), "too-short-prev")
}

func TestWithSecretRotationSetsCurrentAndPrevious(t *testing.T) {
	cur := strings.Repeat("c", 32)
	prev := strings.Repeat("p", 32)
	a := NewApp(WithoutDefaultMiddleware(), WithSecretRotation(cur, prev))
	if string(a.secret) != cur {
		t.Fatalf("current secret = %q, want %q", a.secret, cur)
	}
	if len(a.previousSecrets) != 1 || string(a.previousSecrets[0]) != prev {
		t.Fatalf("previousSecrets = %v, want [%q]", a.previousSecrets, prev)
	}
}

// WithSecret (the no-rotation path) must leave previousSecrets empty so a
// stale rotation window from an earlier option doesn't linger.
func TestWithSecretLeavesPreviousEmpty(t *testing.T) {
	a := NewApp(WithoutDefaultMiddleware(), WithSecret(strings.Repeat("c", 32)))
	if len(a.previousSecrets) != 0 {
		t.Fatalf("WithSecret populated previousSecrets = %v", a.previousSecrets)
	}
}

func TestSessionKeysForMountDerivesPrevious(t *testing.T) {
	cur := []byte(strings.Repeat("c", 32))
	prev := []byte(strings.Repeat("p", 32))
	current, prevKeys, err := sessionKeysForMount(cur, [][]byte{prev}, false)
	if err != nil {
		t.Fatalf("sessionKeysForMount: %v", err)
	}
	if len(current) != 32 {
		t.Fatalf("current key len = %d, want 32", len(current))
	}
	if len(prevKeys) != 1 || len(prevKeys[0]) != 32 {
		t.Fatalf("previous keys = %v, want one 32-byte key", prevKeys)
	}
	// The previous-derived key must equal what the OLD secret alone would
	// have produced — that is the whole point: a token minted before the
	// rotation was signed by deriveKey(prev, session-purpose).
	wantPrev := deriveKey(prev, uihostSessionPurpose)
	if !bytes.Equal(prevKeys[0], wantPrev) {
		t.Fatal("previous-derived key does not match the pre-rotation derivation")
	}
	if bytes.Equal(current, prevKeys[0]) {
		t.Fatal("current and previous derived keys are identical")
	}
}

func TestSessionKeysForMountFanoutNoSecretFails(t *testing.T) {
	if _, _, err := sessionKeysForMount(nil, [][]byte{[]byte(strings.Repeat("p", 32))}, true); err == nil {
		t.Fatal("fanout without a current secret did not fail closed")
	}
}

func TestSessionKeysForMountNoSecretReturnsNil(t *testing.T) {
	current, prevKeys, err := sessionKeysForMount(nil, [][]byte{[]byte(strings.Repeat("p", 32))}, false)
	if err != nil || current != nil || prevKeys != nil {
		t.Fatalf("= %v, %v, %v; want nil, nil, nil (host self-mints)", current, prevKeys, err)
	}
}

func TestSecretPreviousEnvFallback(t *testing.T) {
	t.Setenv("GOFASTR_SECRET", strings.Repeat("c", 32))
	t.Setenv("GOFASTR_SECRET_PREVIOUS", strings.Repeat("p", 32))
	a := NewApp(WithoutDefaultMiddleware())
	if string(a.secret) != strings.Repeat("c", 32) {
		t.Fatal("GOFASTR_SECRET not picked up")
	}
	if len(a.previousSecrets) != 1 || string(a.previousSecrets[0]) != strings.Repeat("p", 32) {
		t.Fatalf("GOFASTR_SECRET_PREVIOUS not picked up: %v", a.previousSecrets)
	}
}

// GOFASTR_SECRET_PREVIOUS is comma-separated and trims whitespace; empty
// fields are skipped.
func TestSecretPreviousEnvCommaSeparated(t *testing.T) {
	t.Setenv("GOFASTR_SECRET", strings.Repeat("c", 32))
	t.Setenv("GOFASTR_SECRET_PREVIOUS", "  "+strings.Repeat("a", 32)+" , "+strings.Repeat("b", 32)+"  ")
	a := NewApp(WithoutDefaultMiddleware())
	if len(a.previousSecrets) != 2 {
		t.Fatalf("previousSecrets = %d entries, want 2", len(a.previousSecrets))
	}
}

// An explicit WithSecretRotation option wins over the GOFASTR_SECRET_PREVIOUS env.
func TestSecretRotationExplicitOptionWinsOverEnv(t *testing.T) {
	t.Setenv("GOFASTR_SECRET_PREVIOUS", strings.Repeat("env-prev", 8))
	opt := strings.Repeat("opt-prev", 8) // 64 chars, ≥32
	a := NewApp(WithoutDefaultMiddleware(), WithSecretRotation(strings.Repeat("c", 32), opt))
	if len(a.previousSecrets) != 1 || string(a.previousSecrets[0]) != opt {
		t.Fatalf("explicit option did not win over env: %v", a.previousSecrets)
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
