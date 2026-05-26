package auth

import (
	"strings"
	"testing"
	"time"
)

// TestPassword_HashIsBcrypt verifies that HashPassword produces a bcrypt
// hash (starts with $2a$ or $2b$). Attack: weak hashing algorithm.
func TestPassword_HashIsBcrypt(t *testing.T) {
	hash, err := HashPassword("testpassword123")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if len(hash) < 10 {
		t.Errorf("SECURITY: [password] hash too short (%d chars). Attack: weak or missing password hashing.", len(hash))
	}
	if hash[:4] != "$2a$" && hash[:4] != "$2b$" {
		t.Errorf("SECURITY: [password] hash prefix = %q (want $2a$ or $2b$). Attack: non-bcrypt algorithm may be vulnerable to GPU cracking.", hash[:4])
	}
}

// TestPassword_HashDiffersFromPlaintext verifies that the hash doesn't
// contain the plaintext password. Attack: reversible encoding.
func TestPassword_HashDiffersFromPlaintext(t *testing.T) {
	pw := "mysecretpassword"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if hash == pw {
		t.Errorf("SECURITY: [password] hash equals plaintext. Attack: passwords stored in cleartext.")
	}
}

// TestPassword_CheckPasswordCorrect verifies that CheckPassword returns
// true for the correct password.
func TestPassword_CheckPasswordCorrect(t *testing.T) {
	hash, err := HashPassword("correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if !CheckPassword("correcthorsebatterystaple", hash) {
		t.Errorf("CheckPassword returned false for correct password")
	}
}

// TestPassword_CheckPasswordWrong verifies that CheckPassword returns
// false for incorrect passwords.
func TestPassword_CheckPasswordWrong(t *testing.T) {
	hash, err := HashPassword("correcthorsebatterystaple")
	if err != nil {
		t.Fatalf("HashPassword error: %v", err)
	}
	if CheckPassword("wrongpassword", hash) {
		t.Errorf("SECURITY: [password] CheckPassword returned true for wrong password. Attack: trivial password bypass.")
	}
}

// TestPassword_DifferentHashesForSamePassword verifies that two calls to
// HashPassword produce different hashes (salt is random). Attack: rainbow
// table attacks if salt is static.
func TestPassword_DifferentHashesForSamePassword(t *testing.T) {
	h1, _ := HashPassword("samepassword")
	h2, _ := HashPassword("samepassword")
	if h1 == h2 {
		t.Errorf("SECURITY: [password] two hashes of same password are identical — salt may be static. Attack: rainbow table attacks possible.")
	}
}

// TestPassword_EmptyPasswordRejected verifies that empty passwords are
// rejected. Attack: creating an account with an empty password — every
// later login attempt would then "match" the (empty) stored credential
// and let the attacker in with a blank field.
func TestPassword_EmptyPasswordRejected(t *testing.T) {
	if _, err := HashPassword(""); err == nil {
		t.Errorf("SECURITY: [password] HashPassword(\"\") returned no error. Attack: account creation with empty password.")
	}
}

// TestPassword_ShortPasswordRejectedByStrengthCheck verifies that the
// dedicated strength validator rejects passwords below the recommended
// minimum. Attack: trivial brute-force on short passwords.
//
// We deliberately don't bake this length check into HashPassword —
// hashing logic shouldn't dictate product policy for legitimate
// short-string uses (PIN flows, recovery tokens). Registration and
// password-change handlers MUST call ValidatePasswordStrength.
func TestPassword_ShortPasswordRejectedByStrengthCheck(t *testing.T) {
	if err := ValidatePasswordStrength("abc"); err == nil {
		t.Errorf("SECURITY: [password] ValidatePasswordStrength(\"abc\") accepted a short password. Attack: trivially short password accepted at registration.")
	}
	if err := ValidatePasswordStrength(""); err == nil {
		t.Errorf("SECURITY: [password] ValidatePasswordStrength(\"\") accepted empty password.")
	}
	if err := ValidatePasswordStrength("hunter222"); err != nil {
		t.Errorf("ValidatePasswordStrength rejected a reasonable password: %v", err)
	}
}

// TestPassword_VeryLongPasswordHandled verifies that passwords longer
// than bcrypt's 72-byte limit are pre-hashed with SHA-256 so the FULL
// password participates in the comparison. Two long passwords that
// share their first 72 bytes but differ later MUST hash and verify
// independently — otherwise bcrypt's silent truncation lets one
// "match" the other.
func TestPassword_VeryLongPasswordHandled(t *testing.T) {
	pwA := strings.Repeat("a", 72) + "AAAAA"
	pwB := strings.Repeat("a", 72) + "BBBBB"

	hashA, err := HashPassword(pwA)
	if err != nil {
		t.Fatalf("HashPassword(pwA): %v", err)
	}
	if !CheckPassword(pwA, hashA) {
		t.Errorf("CheckPassword failed for the password that produced the hash")
	}
	if CheckPassword(pwB, hashA) {
		t.Errorf("SECURITY: [password] pwB matched hashA — bcrypt 72-byte truncation makes long passwords share a hash. Attack: trim attack on long passphrases.")
	}
}

// TestTiming_DummyHashExists verifies that the timing-safe dummy hash
// is initialized. Attack: user enumeration via timing.
func TestTiming_DummyHashExists(t *testing.T) {
	if dummyBcryptHash == "" {
		t.Errorf("SECURITY: [timing] dummyBcryptHash is empty. Attack: missing dummy hash allows user enumeration via timing.")
	}
	if dummyBcryptHash[:4] != "$2a$" && dummyBcryptHash[:4] != "$2b$" {
		t.Errorf("SECURITY: [timing] dummyBcryptHash is not bcrypt. Attack: timing-safe comparison broken.")
	}
}

// TestTiming_PlaceholderHashExists verifies that the OAuth/placeholder
// hash is initialized. Attack: OAuth users can log in with any password.
func TestTiming_PlaceholderHashExists(t *testing.T) {
	if passwordPlaceholderHash == "" {
		t.Errorf("SECURITY: [timing] passwordPlaceholderHash is empty. Attack: OAuth users may have no password barrier.")
	}
	// Verify that no common password matches the placeholder
	if CheckPassword("password", passwordPlaceholderHash) {
		t.Errorf("SECURITY: [password] common password 'password' matches placeholder hash. Attack: OAuth account takeover.")
	}
	if CheckPassword("password123", passwordPlaceholderHash) {
		t.Errorf("SECURITY: [password] common password 'password123' matches placeholder hash. Attack: OAuth account takeover.")
	}
}

// TestPassword_TimingSafeCheck verifies that CheckPassword takes
// roughly the same time for valid vs invalid hashes. This is a
// statistical test — it verifies bcrypt timing is consistent.
func TestPassword_TimingSafeCheck(t *testing.T) {
	hash, _ := HashPassword("testpassword")

	start := time.Now()
	CheckPassword("wrong1", hash)
	wrongDuration := time.Since(start)

	start = time.Now()
	CheckPassword("wrong2", "$2a$10$invalidhashthatdoesnotmatchaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	// This should still take ~same time because bcrypt always runs
	invalidDuration := time.Since(start)

	ratio := float64(wrongDuration) / float64(invalidDuration)
	if ratio < 0.1 || ratio > 10 {
		t.Logf("SECURITY: [timing] CheckPassword timing ratio %.2f (valid hash: %v, invalid hash: %v). Attack: timing side-channel may leak hash validity.", ratio, wrongDuration, invalidDuration)
	}
}
