package auth

import (
	"strings"
	"testing"
)

// SECURITY TEST — fixed 2026-09-05, red-probe round 4.
// Family: F23 Signed and serialized payload confusion
// Property: an HMAC signing key configured from developer input must meet a
// minimum length or construction fails closed — a state token travels in the
// browser's address bar (provider redirect URL, logs, referers), so a short
// key is brute-forceable offline against a captured token.
// Surfaces: oauth2.go::NewOAuth2Plugin (minStateSecretLen floor, the
// relay.New / framework secret.go minSecretLen / sessiontoken minKeyLen=16
// precedent) and oauth2.go::generateState/validateAndConsumeState
// (HMAC-SHA256 under that key; the payload includes the link-flow userID,
// so a forged state binds a provider identity to a chosen account id).
// Fix: NewOAuth2Plugin panics on a non-empty StateSecret under 16 bytes;
// empty keeps meaning "mint a random 32-byte key" (single-instance
// deployments), and a floor-length secret is accepted.

// TestStateSecretTooShortRefused asserts construction fails closed on
// sub-floor StateSecret values while the random-key default and a real
// secret keep working.
func TestStateSecretTooShortRefused(t *testing.T) {
	for _, secret := range []string{"s", strings.Repeat("a", 8), strings.Repeat("a", 15)} {
		func() {
			defer func() { _ = recover() }()
			_ = NewOAuth2Plugin(OAuth2Config{StateSecret: secret})
			t.Errorf("SECURITY: [oauth-state-key] NewOAuth2Plugin accepted StateSecret of %d bytes — an HMAC key under the 16-byte floor must fail closed at construction (state tokens are offline-brute-forceable from any redirect URL)", len(secret))
		}()
	}

	// Positive controls: empty mints a random key, a floor-length secret is
	// accepted. Neither may panic.
	_ = NewOAuth2Plugin(OAuth2Config{})
	_ = NewOAuth2Plugin(OAuth2Config{StateSecret: strings.Repeat("a", 16)})
}
