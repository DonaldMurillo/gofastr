package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// TestArgon2_HashAndVerify proves Argon2Hasher produces a PHC-format argon2id
// hash and verifies it back. argon2id is the modern memory-hard default; the
// framework ships bcrypt by default and offers argon2 as an opt-in.
func TestArgon2_HashAndVerify(t *testing.T) {
	h := Argon2Hasher{}
	hash, err := h.Hash("super-secret-123!")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("argon2id hash must start with $argon2id$, got %q", hash)
	}
	if !h.Verify("super-secret-123!", hash) {
		t.Error("Verify failed for the correct password")
	}
	if h.Verify("wrong-password", hash) {
		t.Error("SECURITY: Verify succeeded for the wrong password")
	}
}

// TestArgon2_DifferentSaltPerHash guards against a static salt: two hashes of
// the same password must differ (random salt), or a single DB leak cracks every
// account at once.
func TestArgon2_DifferentSaltPerHash(t *testing.T) {
	h := Argon2Hasher{}
	a, _ := h.Hash("same-password")
	b, _ := h.Hash("same-password")
	if a == b {
		t.Error("SECURITY: identical argon2 hashes for the same password — salt is not random")
	}
}

// TestArgon2_RejectsEmpty: an empty password must never hash, or every
// "no password" field at signup is silently accepted.
func TestArgon2_RejectsEmpty(t *testing.T) {
	if _, err := (Argon2Hasher{}).Hash(""); err == nil {
		t.Error("Argon2Hasher.Hash(\"\") must return an error")
	}
}

// TestCheckPassword_AutoDetectsArgon2: CheckPassword (the package-level verify
// used by the login flow) must verify an argon2id hash even though the default
// hasher is bcrypt. This enables gradual migration: existing bcrypt rows keep
// working while new passwords are hashed with argon2.
func TestCheckPassword_AutoDetectsArgon2(t *testing.T) {
	hash, err := (Argon2Hasher{}).Hash("migrate-me")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("migrate-me", hash) {
		t.Error("CheckPassword did not auto-detect / verify an argon2id hash")
	}
	// Bcrypt hashes must still verify (no regression on the default path).
	b, err := HashPassword("bcrypt-still-works")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("bcrypt-still-works", b) {
		t.Error("bcrypt verification regressed after adding argon2 dispatch")
	}
}

// TestDefaultHasherArgon2OptIn: setting DefaultHasher = Argon2Hasher before
// auth.Init makes HashPassword emit argon2id, and CheckPassword round-trips it.
func TestDefaultHasherArgon2OptIn(t *testing.T) {
	orig := DefaultHasher
	DefaultHasher = Argon2Hasher{}
	defer func() { DefaultHasher = orig }()

	hash, err := HashPassword("opt-in-test")
	if err != nil {
		t.Fatalf("HashPassword with Argon2 default: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("DefaultHasher=Argon2 produced %q, want an $argon2id$ hash", hash)
	}
	if !CheckPassword("opt-in-test", hash) {
		t.Error("argon2 round-trip via HashPassword/CheckPassword failed")
	}
}

// TestArgon2_VerifyRejectsResourceExhaustionParams: a malicious/corrupted
// stored hash demanding ~4 TiB of memory (or huge time/threads) must NOT be
// fed to argon2.IDKey, that would OOM/peg-CPU the process on every verify, a
// per-login DoS. Verify rejects it fast, before any heavy allocation.
func TestArgon2_VerifyRejectsResourceExhaustionParams(t *testing.T) {
	enc := base64.RawStdEncoding.EncodeToString
	malicious := "$argon2id$v=19$m=4294967295,t=1,p=1$" + // m = 4 GiB-ish KiB ≈ 4 TiB
		enc([]byte("1234567890123456")) + "$" + enc([]byte("12345678901234567890123456789012"))

	start := time.Now()
	got := (Argon2Hasher{}).Verify("any-password", malicious)
	elapsed := time.Since(start)

	if got {
		t.Error("SECURITY: Verify accepted a resource-exhaustion argon2 hash")
	}
	// Must reject BEFORE invoking IDKey, a sub-second bound proves it never
	// attempted the multi-TiB allocation.
	if elapsed > time.Second {
		t.Errorf("Verify took %v on a hostile hash; it must reject before IDKey runs", elapsed)
	}
}
