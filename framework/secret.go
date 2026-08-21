package framework

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"strings"
)

// minSecretLen is the floor for the app secret. 32 characters of a
// random secret carries enough entropy for every key derived from it;
// anything shorter is a misconfiguration worth failing loudly on.
const minSecretLen = 32

// uihostSessionPurpose domain-separates the uihost session-signing key
// from every other key derived from the app secret. Versioned so the
// derivation can change without silently invalidating unrelated keys.
const uihostSessionPurpose = "gofastr/uihost/session/v1"

// Embed credential purposes. Two of them, not one: a handshake nonce and a
// frame grant carry near-identical claims, so deriving them from the same key
// would leave only the token prefix separating a single-use credential from a
// long-lived one. Two keys makes the swap unrepresentable rather than
// merely checked.
const (
	embedNoncePurpose = "gofastr/embed/nonce/v1"
	embedGrantPurpose = "gofastr/embed/grant/v1"
)

// WithSecret sets the app-wide secret. Subsystem keys (starting with
// the uihost session-signing key) are HKDF-derived from it, so one
// secret shared across replicas is all a multi-replica deployment
// configures. Equivalent zero-code path: set GOFASTR_SECRET in the
// environment (or a .env file); an explicit WithSecret wins over the
// env var. Panics on a secret shorter than 32 characters, a short
// secret weakens every derived key at once.
func WithSecret(secret string) AppOption {
	validated := validateSecret(secret)
	return func(a *App) {
		a.secret = []byte(validated)
		// An explicit secret option is authoritative for the whole
		// rotation window: WithSecret is the no-rotation shorthand, so
		// it must also close a window a stale GOFASTR_SECRET_PREVIOUS
		// would otherwise hold open.
		a.previousSecrets = nil
		a.secretOptionSet = true
	}
}

// validateSecret enforces the length floor shared by WithSecret and the
// GOFASTR_SECRET env path. Returns its input so call sites stay
// one-liners.
func validateSecret(secret string) string {
	if len(secret) < minSecretLen {
		panic("framework: app secret must be at least 32 characters: generate one with `openssl rand -base64 32` and pass it via WithSecret or GOFASTR_SECRET")
	}
	return secret
}

// WithSecretRotation sets the current app secret and zero or more
// previous secrets accepted (verify-only) for graceful rotation,
// mirroring the CSRF AdditionalKeys idiom. New session tokens are
// signed with the current secret; tokens signed by any previous secret
// still verify for a drain window (one session TTL), so rotating
// GOFASTR_SECRET no longer logs every user out at once. Each previous
// secret must independently meet the 32-char floor.
//
// Equivalent env form: GOFASTR_SECRET (current) plus
// GOFASTR_SECRET_PREVIOUS (comma-separated previous secrets). WithSecret
// is the no-rotation shorthand and stays unchanged.
func WithSecretRotation(current string, previous ...string) AppOption {
	validated := validateSecret(current)
	prev := make([][]byte, 0, len(previous))
	for _, p := range previous {
		prev = append(prev, []byte(validateSecret(p)))
	}
	return func(a *App) {
		a.secret = []byte(validated)
		a.previousSecrets = prev
		a.secretOptionSet = true
	}
}

// deriveKey derives a 32-byte subsystem key from the app secret via
// HKDF-SHA256 with a per-purpose info string. Purposes are constants:
// a bad parameter is a programming error, hence panic.
func deriveKey(secret []byte, purpose string) []byte {
	key, err := hkdf.Key(sha256.New, secret, nil, purpose, 32)
	if err != nil {
		panic("framework: deriveKey(" + purpose + "): " + err.Error())
	}
	return key
}

// sessionKeyForMount resolves the single session-signing key handed to a
// mounted UI host (the no-rotation path). A nil, nil return means "no key
// to hand over", the host self-mints a per-boot key, which is only sound
// on a single replica. With a fanout attached (the multi-replica signal)
// and no secret configured, it errors so boot fails closed instead of half
// of all session checks 401ing in production. For graceful rotation use
// sessionKeysForMount, which also returns verify-only previous keys.
func sessionKeyForMount(secret []byte, fanoutAttached bool) ([]byte, error) {
	current, _, err := sessionKeysForMount(secret, nil, fanoutAttached)
	return current, err
}

// sessionKeysForMount resolves the current session-signing key and the
// verify-only previous keys handed to a mounted UI host for graceful
// GOFASTR_SECRET rotation (mirroring the CSRF AdditionalKeys idiom).
// current is derived from the active secret and signs new tokens; each
// previous key is derived from a retired secret and only verifies, so a
// rotation drains over a session TTL instead of logging everyone out at
// once. The nil/nil/error semantics for single-replica and fanout cases
// match sessionKeyForMount. Previous keys without a current secret are
// meaningless and are not returned.
func sessionKeysForMount(secret []byte, previous [][]byte, fanoutAttached bool) ([]byte, [][]byte, error) {
	if len(secret) > 0 {
		current := deriveKey(secret, uihostSessionPurpose)
		var prevKeys [][]byte
		for _, p := range previous {
			prevKeys = append(prevKeys, deriveKey(p, uihostSessionPurpose))
		}
		return current, prevKeys, nil
	}
	if fanoutAttached {
		return nil, nil, errors.New("framework: WithFanout requires an app secret: session tokens minted on one replica must verify on every other. Set WithSecret or GOFASTR_SECRET to the same random value (≥32 chars) on every replica")
	}
	return nil, nil, nil
}

// splitSecretList splits a comma-separated secret list (e.g.
// GOFASTR_SECRET_PREVIOUS) into trimmed, non-empty entries. Each entry is
// validated against the length floor by the caller, not here.
func splitSecretList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// embedKeysForMount resolves the nonce and grant signing keys handed to a
// mounted embed host.
//
// Unlike sessions there is no self-minted per-boot fallback. A session that
// fails to verify is re-minted on the next render and the visitor never
// notices; an embed nonce that fails to verify is GONE, it is single-use, it
// lives for a minute, and it was rendered into a page on someone else's site
// that the app cannot re-render. So a secret is required, and an app that
// hands out pieces of itself without one fails at boot rather than serving
// embeds that break on every restart and on every second replica.
func embedKeysForMount(secret []byte) (nonceKey, grantKey []byte, err error) {
	if len(secret) == 0 {
		return nil, nil, errors.New("framework: embeddable surfaces require an app secret: a per-boot key would invalidate every outstanding nonce on restart and would never verify on a second replica. Set WithSecret or GOFASTR_SECRET to the same random value (≥32 chars) on every replica")
	}
	return deriveKey(secret, embedNoncePurpose), deriveKey(secret, embedGrantPurpose), nil
}

// embedPreviousKeysForMount derives the verify-only embed keys for each
// retired app secret, so a rotation does not invalidate outstanding nonces
// and grants. Previous secrets go through the SAME HKDF derivation as the
// current one, a raw secret is never used as a key.
func embedPreviousKeysForMount(previous [][]byte) (nonceKeys, grantKeys [][]byte) {
	for _, p := range previous {
		if len(p) == 0 {
			continue
		}
		nonceKeys = append(nonceKeys, deriveKey(p, embedNoncePurpose))
		grantKeys = append(grantKeys, deriveKey(p, embedGrantPurpose))
	}
	return nonceKeys, grantKeys
}
