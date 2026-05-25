// Package auth implements the capability-token model: claim set,
// internal JWT-like encoding (no third-party dep), revocation list,
// and the issuance flow with TTY/notification confirmation.
//
// See docs/harness-architecture.md § Authentication.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// Claims is the token claim set. Verifiers MUST reject a token whose
// Ver they do not recognize. See § Protocol versioning → Token
// versioning.
type Claims struct {
	Ver             int                  `json:"ver"`
	JTI             ids.JTI              `json:"jti"`
	Sessions        []ids.SessionID      `json:"sessions,omitempty"`
	Commands        []string             `json:"commands,omitempty"`
	IdentityClass   control.IdentityClass `json:"identity_class"`
	NotBefore       int64                `json:"nbf,omitempty"`
	ExpiresAt       int64                `json:"exp"`
	CanMint         bool                 `json:"can_mint"`
	CriticalClaims  []string             `json:"critical_claims,omitempty"`
}

// VerCurrent is the claim set version this binary issues. Verifiers
// rejecting an unrecognized Ver fail-closed.
const VerCurrent = 1

// AllowsCommand reports whether the token can issue the given command
// kind. An empty Commands list means "any command."
func (c Claims) AllowsCommand(kind string) bool {
	if len(c.Commands) == 0 {
		return true
	}
	for _, k := range c.Commands {
		if k == kind {
			return true
		}
	}
	return false
}

// AllowsSession reports whether the token may attach to the given
// session. An empty Sessions list means "any session" — only
// allowed for the bootstrap token.
func (c Claims) AllowsSession(s ids.SessionID) bool {
	if len(c.Sessions) == 0 {
		return true
	}
	for _, x := range c.Sessions {
		if x == s {
			return true
		}
	}
	return false
}

// Expired reports whether the token is past its exp at the given time.
func (c Claims) Expired(now time.Time) bool {
	return now.Unix() >= c.ExpiresAt
}

// Encoder signs and encodes claim sets. Use a per-machine secret;
// never persist tokens to disk plaintext (the bootstrap token is
// returned to the requester and held in their keychain).
type Encoder struct {
	secret []byte
}

// NewEncoder returns an Encoder with the given HMAC secret.
//
// In production, the secret is loaded from the credstore at boot. A
// fresh secret invalidates every outstanding token.
func NewEncoder(secret []byte) *Encoder {
	if len(secret) < 32 {
		// Refuse weak secrets; the user-facing error is "weak secret".
		panic("auth: secret must be at least 32 bytes")
	}
	cp := make([]byte, len(secret))
	copy(cp, secret)
	return &Encoder{secret: cp}
}

// GenerateSecret returns a fresh 32-byte HMAC secret.
func GenerateSecret() ([]byte, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return b, err
}

// Encode signs the claim set with HMAC-SHA256 and returns the
// `<payload>.<signature>` token string. Both halves are
// URL-safe-base64 without padding.
func (e *Encoder) Encode(c Claims) (string, error) {
	if c.Ver == 0 {
		c.Ver = VerCurrent
	}
	if c.JTI == "" {
		c.JTI = ids.NewJTI()
	}
	if c.ExpiresAt == 0 {
		c.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
	}
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	sig := e.sign(payload)
	return payload + "." + sig, nil
}

// Decode parses and verifies a token. Returns the claim set or an
// error if the signature is invalid or the version is unrecognized.
//
// Per § Unknown-field policy → token claims, a token with critical
// claims the verifier doesn't recognize is rejected; non-critical
// unknown claims are ignored. The current verifier recognizes all
// fields above.
func (e *Encoder) Decode(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Claims{}, errors.New("auth: malformed token")
	}
	payload, sig := parts[0], parts[1]
	expected := e.sign(payload)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return Claims{}, errors.New("auth: signature mismatch")
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Claims{}, fmt.Errorf("auth: decode body: %w", err)
	}
	var c Claims
	if err := json.Unmarshal(body, &c); err != nil {
		return Claims{}, fmt.Errorf("auth: parse claims: %w", err)
	}
	if c.Ver != VerCurrent {
		return Claims{}, fmt.Errorf("auth: token ver %d not supported (current %d)", c.Ver, VerCurrent)
	}
	// Fail-closed on unrecognized critical claims. We currently
	// recognize every defined claim, so any string in CriticalClaims
	// outside the known set is a v(N+1) token a v(N) verifier sees.
	for _, cc := range c.CriticalClaims {
		if !knownCriticalClaim(cc) {
			return Claims{}, fmt.Errorf("auth: unknown critical claim %q", cc)
		}
	}
	return c, nil
}

func (e *Encoder) sign(payload string) string {
	m := hmac.New(sha256.New, e.secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func knownCriticalClaim(s string) bool {
	switch s {
	case "sessions", "commands", "identity_class", "exp", "nbf", "can_mint":
		return true
	}
	return false
}

// RevocationList tracks revoked JTIs. Concurrency-safe in-memory
// store; persistent backing is the caller's responsibility (the
// docs commit to ~/.local/state/gofastr/harness/revocations.db).
type RevocationList struct {
	mu      sync.RWMutex
	revoked map[ids.JTI]int64 // jti → revoke unix-ts
}

// NewRevocationList returns an empty RevocationList.
func NewRevocationList() *RevocationList {
	return &RevocationList{revoked: make(map[ids.JTI]int64)}
}

// Revoke marks a JTI revoked.
func (r *RevocationList) Revoke(jti ids.JTI) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revoked[jti] = time.Now().Unix()
}

// IsRevoked reports whether a JTI has been revoked.
func (r *RevocationList) IsRevoked(jti ids.JTI) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.revoked[jti]
	return ok
}

// Snapshot returns a copy of the current revocation set. Used for
// persistence at shutdown.
func (r *RevocationList) Snapshot() map[ids.JTI]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[ids.JTI]int64, len(r.revoked))
	for k, v := range r.revoked {
		out[k] = v
	}
	return out
}

// Verify validates a token end-to-end: signature, version, not-before,
// expiry, revocation. Returns the parsed claims on success.
func Verify(enc *Encoder, rl *RevocationList, token string, now time.Time) (Claims, error) {
	c, err := enc.Decode(token)
	if err != nil {
		return Claims{}, err
	}
	if c.NotBefore > 0 && now.Unix() < c.NotBefore {
		return Claims{}, errors.New("auth: token not yet valid")
	}
	if c.Expired(now) {
		return Claims{}, &ExpiredError{ExpAt: time.Unix(c.ExpiresAt, 0)}
	}
	if rl != nil && rl.IsRevoked(c.JTI) {
		return Claims{}, &RevokedError{JTI: c.JTI}
	}
	return c, nil
}

// ExpiredError matches control.ReasonTokenExpired on the wire.
type ExpiredError struct{ ExpAt time.Time }

func (e *ExpiredError) Error() string { return "auth: token expired at " + e.ExpAt.Format(time.RFC3339) }

// RevokedError matches control.ReasonTokenRevoked on the wire.
type RevokedError struct{ JTI ids.JTI }

func (e *RevokedError) Error() string { return "auth: token " + string(e.JTI) + " revoked" }
