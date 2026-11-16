package framework

import (
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"strings"
)

const minSecretLen = 32

const uihostSessionPurpose = "gofastr/uihost/session/v1"

const (
	embedNoncePurpose = "gofastr/embed/nonce/v1"
	embedGrantPurpose = "gofastr/embed/grant/v1"
)

// WithSecret sets the app secret every subsystem key is derived from,
// overriding GOFASTR_SECRET and any previous secrets. It panics on a
// secret shorter than 32 characters.
func WithSecret(secret string) AppOption {
	validated := validateSecret(secret)
	return func(a *App) {
		a.secret = []byte(validated)
		a.previousSecrets = nil
		a.secretOptionSet = true
	}
}

func validateSecret(secret string) string {
	if len(secret) < minSecretLen {
		panic("framework: app secret must be at least 32 characters: generate one with `openssl rand -base64 32` and pass it via WithSecret or GOFASTR_SECRET")
	}
	return secret
}

// WithSecretRotation sets the current app secret and previous secrets
// that still verify what they signed, such as sessions for one TTL. The
// env form is GOFASTR_SECRET plus a comma-separated
// GOFASTR_SECRET_PREVIOUS. It panics on any secret shorter than 32
// characters.
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

func deriveKey(secret []byte, purpose string) []byte {
	key, err := hkdf.Key(sha256.New, secret, nil, purpose, 32)
	if err != nil {
		panic("framework: deriveKey(" + purpose + "): " + err.Error())
	}
	return key
}

func sessionKeyForMount(secret []byte, fanoutAttached bool) ([]byte, error) {
	current, _, err := sessionKeysForMount(secret, nil, fanoutAttached)
	return current, err
}

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

func splitSecretList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		out = append(out, t)
		k := strings.Trim(p, " \t")
		if len(parts) == 1 {
			k = p
		}
		if k != t {
			out = append(out, k)
		}
	}
	return out
}

func embedKeysForMount(secret []byte) (nonceKey, grantKey []byte, err error) {
	if len(secret) == 0 {
		return nil, nil, errors.New("framework: embeddable surfaces require an app secret: a per-boot key would invalidate every outstanding nonce on restart and would never verify on a second replica. Set WithSecret or GOFASTR_SECRET to the same random value (≥32 chars) on every replica")
	}
	return deriveKey(secret, embedNoncePurpose), deriveKey(secret, embedGrantPurpose), nil
}

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
