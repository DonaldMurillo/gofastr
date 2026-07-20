package framework

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
)

// minSecretLen is the floor for the app secret. 32 characters of a
// random secret carries enough entropy for every key derived from it;
// anything shorter is a misconfiguration worth failing loudly on.
const minSecretLen = 32

// uihostSessionPurpose domain-separates the uihost session-signing key
// from every other key derived from the app secret. Versioned so the
// derivation can change without silently invalidating unrelated keys.
const uihostSessionPurpose = "gofastr/uihost/session/v1"

// WithSecret sets the app-wide secret. Subsystem keys (starting with
// the uihost session-signing key) are HKDF-derived from it, so one
// secret shared across replicas is all a multi-replica deployment
// configures. Equivalent zero-code path: set GOFASTR_SECRET in the
// environment (or a .env file); an explicit WithSecret wins over the
// env var. Panics on a secret shorter than 32 characters — a short
// secret weakens every derived key at once.
func WithSecret(secret string) AppOption {
	validated := validateSecret(secret)
	return func(a *App) {
		a.secret = []byte(validated)
	}
}

// validateSecret enforces the length floor shared by WithSecret and the
// GOFASTR_SECRET env path. Returns its input so call sites stay
// one-liners.
func validateSecret(secret string) string {
	if len(secret) < minSecretLen {
		panic("framework: app secret must be at least 32 characters — generate one with `openssl rand -base64 32` and pass it via WithSecret or GOFASTR_SECRET")
	}
	return secret
}

// deriveKey derives a 32-byte subsystem key from the app secret via
// HKDF-SHA256 with a per-purpose info string. Purposes are constants —
// a bad parameter is a programming error, hence panic.
func deriveKey(secret []byte, purpose string) []byte {
	key, err := hkdf.Key(sha256.New, secret, nil, purpose, 32)
	if err != nil {
		panic("framework: deriveKey(" + purpose + "): " + err.Error())
	}
	return key
}

// sessionKeyForMount resolves the session-signing key handed to a
// mounted UI host. A nil, nil return means "no key to hand over" — the
// host self-mints a per-boot key, which is only sound on a single
// replica. With a fanout attached (the multi-replica signal) and no
// secret configured, it errors so boot fails closed instead of half of
// all session checks 401ing in production.
func sessionKeyForMount(secret []byte, fanoutAttached bool) ([]byte, error) {
	if len(secret) > 0 {
		return deriveKey(secret, uihostSessionPurpose), nil
	}
	if fanoutAttached {
		return nil, errors.New("framework: WithFanout requires an app secret — session tokens minted on one replica must verify on every other. Set WithSecret or GOFASTR_SECRET to the same random value (≥32 chars) on every replica")
	}
	return nil, nil
}
