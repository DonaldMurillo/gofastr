package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// Claims is the token claim set.
type Claims struct {
	Ver            int                   `json:"ver"`
	JTI            ids.JTI               `json:"jti"`
	Sessions       []ids.SessionID       `json:"sessions,omitempty"`
	Commands       []string              `json:"commands,omitempty"`
	IdentityClass  control.IdentityClass `json:"identity_class"`
	NotBefore      int64                 `json:"nbf,omitempty"`
	ExpiresAt      int64                 `json:"exp"`
	CanMint        bool                  `json:"can_mint"`
	CriticalClaims []string              `json:"critical_claims,omitempty"`
}

// VerCurrent is the claim set version this binary issues and the only one it verifies.
const VerCurrent = 1

// AllowsCommand reports whether the token may send the command kind; an empty Commands allows any.
func (c Claims) AllowsCommand(kind string) bool {
	if len(c.Commands) == 0 {
		return true
	}
	return slices.Contains(c.Commands, kind)
}

// AllowsSession reports whether the token may drive the session; an empty Sessions allows any.
func (c Claims) AllowsSession(s ids.SessionID) bool {
	if len(c.Sessions) == 0 {
		return true
	}
	return slices.Contains(c.Sessions, s)
}

// Expired reports whether the token is past its exp at the given time.
func (c Claims) Expired(now time.Time) bool {
	return now.Unix() >= c.ExpiresAt
}

// Encoder signs and verifies claim sets under one HMAC secret.
type Encoder struct {
	secret []byte
}

// NewEncoder returns an Encoder over a copy of secret; it panics on a secret under 32 bytes.
func NewEncoder(secret []byte) *Encoder {
	if len(secret) < 32 {
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

// Encode fills a zero Ver, JTI and exp (24h) and returns the `<payload>.<signature>` token.
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

// Decode checks the signature, the version and the critical claims and returns the claim set.
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

// RevocationList is a concurrency-safe in-memory set of revoked JTIs.
type RevocationList struct {
	mu      sync.RWMutex
	revoked map[ids.JTI]int64
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

// Snapshot returns a copy of the revocation set, JTI to revoke time in Unix seconds.
func (r *RevocationList) Snapshot() map[ids.JTI]int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[ids.JTI]int64, len(r.revoked))
	maps.Copy(out, r.revoked)
	return out
}

// Verify decodes the token and refuses it before nbf, at or after exp, or once its JTI is revoked.
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

func (e *ExpiredError) Error() string {
	return "auth: token expired at " + e.ExpAt.Format(time.RFC3339)
}

// RevokedError matches control.ReasonTokenRevoked on the wire.
type RevokedError struct{ JTI ids.JTI }

func (e *RevokedError) Error() string { return "auth: token " + string(e.JTI) + " revoked" }
