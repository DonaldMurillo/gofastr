package auth

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a plaintext password using bcrypt.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
// Returns true if the password matches.
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
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
