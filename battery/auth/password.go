package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// RecommendedMinPasswordBytes is the minimum length application
// registration flows should enforce on new passwords. Applications use
// [ValidatePasswordStrength] to check it; [HashPassword] itself only
// rejects the empty string because hashing logic shouldn't dictate
// product policy (PIN flows, recovery tokens, machine accounts, etc.
// may legitimately fall outside this length).
const RecommendedMinPasswordBytes = 8

// ErrPasswordEmpty is returned by HashPassword when the input is the
// empty string — a blank field at signup would be silently accepted
// otherwise, and every login attempt with no password would match.
var ErrPasswordEmpty = errors.New("auth: password is empty")

// ErrPasswordTooShort is returned by ValidatePasswordStrength when the
// input is shorter than RecommendedMinPasswordBytes.
var ErrPasswordTooShort = errors.New("auth: password too short")

// HashPassword hashes a plaintext password using bcrypt.
//
// The empty string is rejected with ErrPasswordEmpty. No other length
// policy is enforced — call [ValidatePasswordStrength] from the
// registration flow when you want to require a minimum length.
//
// Inputs longer than 72 bytes are pre-hashed with SHA-256 before
// bcrypt. bcrypt silently truncates anything past 72 bytes, so without
// the pre-hash a 200-character passphrase would be indistinguishable
// from its first 72 characters. The pre-hash is base64-encoded to
// stay within bcrypt's usual byte-range and to avoid NUL bytes that
// bcrypt would terminate on.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrPasswordEmpty
	}
	input := []byte(password)
	if len(input) > 72 {
		sum := sha256.Sum256(input)
		input = []byte(base64.RawStdEncoding.EncodeToString(sum[:]))
	}
	bytes, err := bcrypt.GenerateFromPassword(input, bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// ValidatePasswordStrength returns ErrPasswordEmpty for an empty input
// and ErrPasswordTooShort for anything shorter than
// RecommendedMinPasswordBytes. Use it from registration / password-
// change handlers to enforce a length floor without baking policy
// into the hash function.
func ValidatePasswordStrength(password string) error {
	if password == "" {
		return ErrPasswordEmpty
	}
	if len(password) < RecommendedMinPasswordBytes {
		return ErrPasswordTooShort
	}
	return nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
// Returns true if the password matches.
//
// The same SHA-256 pre-hash applied in [HashPassword] is applied here
// for inputs longer than 72 bytes, so a long passphrase that was hashed
// at registration time still verifies at login time.
func CheckPassword(password, hash string) bool {
	input := []byte(password)
	if len(input) > 72 {
		sum := sha256.Sum256(input)
		input = []byte(base64.RawStdEncoding.EncodeToString(sum[:]))
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), input)
	return err == nil
}

// dummyBcryptHash is a pre-computed bcrypt hash used to keep loginHandler
// timing-safe. When a username does not exist (or FindByEmail errors), we
// run CheckPassword against this hash so the response time is the same
// as when the user exists with a wrong password. Without this, an attacker
// can enumerate registered emails by measuring response time
// (bcrypt at default cost is ~50ms vs ~10µs for "no user").
var dummyBcryptHash string

// passwordPlaceholderHash is stored as the password_hash for users created
// via OAuth or magic-link (they never log in via password). It's a real
// bcrypt hash of an unguessable random secret — recording it once at init
// avoids a per-signup ~50ms bcrypt + 64-byte allocation that previously
// happened for every first-time auto-create.
//
// Because the input is random and discarded, no password the user types
// can match — CheckPassword always returns false against this hash.
// Because the hash IS a real bcrypt structure, CheckPassword still spends
// real bcrypt time on it, preserving timing safety on the login path.
var passwordPlaceholderHash string

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("auth: precomputing dummy bcrypt hash: %v", err))
	}
	dummyBcryptHash = string(h)

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Sprintf("auth: generating placeholder secret: %v", err))
	}
	hp, err := bcrypt.GenerateFromPassword(secret, bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("auth: precomputing placeholder hash: %v", err))
	}
	passwordPlaceholderHash = string(hp)
}
