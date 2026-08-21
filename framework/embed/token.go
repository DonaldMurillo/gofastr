package embed

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token prefixes. They are load-bearing in exactly one way: they let a leaked
// string be identified at a glance (in a log, a bug report, a customer's page
// source) as a GoFastr embed credential rather than a session or an API token.
const (
	// NoncePrefix marks a single-use handshake nonce: the credential the app
	// author renders into a customer's page.
	NoncePrefix = "emb_"
	// GrantPrefix marks the short-lived stateless grant the frame receives in
	// exchange for a nonce. It never leaves the frame.
	GrantPrefix = "emg_"
)

// HKDF purposes. Domain separation means a nonce can never verify as a grant
// (or as a session token) even though all three are HMAC-SHA256 over JSON with
// keys derived from the same app secret. Versioned so the derivation can change
// without silently accepting the old form.
const (
	NoncePurpose = "gofastr/embed/nonce/v1"
	GrantPurpose = "gofastr/embed/grant/v1"
)

// Errors returned by the verify path. They are distinguished so a handler can
// answer "this was used" differently from "this is not ours", but note that
// handlers deliberately collapse them into one client-visible response, since
// telling a caller WHICH check failed is an oracle.
var (
	// ErrMalformed means the string is not a token of this kind at all.
	ErrMalformed = errors.New("embed: malformed token")
	// ErrBadSignature means the MAC did not verify under the given key.
	ErrBadSignature = errors.New("embed: bad token signature")
	// ErrExpired means the token verified but its expiry has passed.
	ErrExpired = errors.New("embed: token expired")
)

// nonceClaims is the signed payload of a handshake nonce.
//
// Field names are short because this string is pasted into a customer's HTML by
// hand; the full names live here, once.
type nonceClaims struct {
	Surface string   `json:"s"`            // embeddable surface name
	Subject string   `json:"u"`            // app-side identity the frame acts as
	Scopes  []string `json:"sc,omitempty"` // capability list carried into the grant
	Origin  string   `json:"o"`            // normalized origin allowed to frame it
	ID      string   `json:"n"`            // random single-use id; the burn key
	Expires int64    `json:"x"`            // unix seconds
}

// grantClaims is the signed payload of an exchanged grant. It has no ID: the
// grant is stateless and is deliberately NOT single-use, because the frame
// presents it on every island RPC for as long as it lives.
type grantClaims struct {
	Surface string   `json:"s"`
	Subject string   `json:"u"`
	Scopes  []string `json:"sc,omitempty"`
	Origin  string   `json:"o"`
	Expires int64    `json:"x"`
	// Deadline is the absolute end of the credential's life, fixed when the
	// nonce was exchanged and carried unchanged through every refresh. Without
	// it a refreshable grant is an immortal one: each refresh would push the
	// expiry out, and a frame left open in a tab would hold a credential
	// forever. Refresh past this instant is refused.
	Deadline int64 `json:"d"`
}

// Nonce is a verified handshake nonce.
type Nonce struct {
	Surface string
	Subject string
	Scopes  []string
	Origin  string
	ID      string
	Expires time.Time
}

// Grant is a verified frame credential.
type Grant struct {
	Surface string
	Subject string
	Scopes  []string
	Origin  string
	Expires time.Time
	// Deadline is the absolute end of the credential's life. Refreshes move
	// Expires; they never move Deadline.
	Deadline time.Time
}

// HasScope reports whether the grant carries scope.
func (g Grant) HasScope(scope string) bool {
	for _, s := range g.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

var b64 = base64.RawURLEncoding

// newNonceID returns a fresh 128-bit random id. It is the primary key the burn
// store enforces uniqueness on, so it must be unguessable AND unique; 128 bits
// of crypto/rand is both.
func newNonceID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("embed: generate nonce id: %w", err)
	}
	return b64.EncodeToString(buf[:]), nil
}

// sign encodes claims and appends a MAC, producing prefix + payload + "." + mac.
func sign(prefix string, key []byte, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("embed: marshal claims: %w", err)
	}
	encoded := b64.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	// The prefix goes INTO the MAC. Without it, a nonce payload and a grant
	// payload with identical fields would produce identical MACs, and the only
	// thing stopping a nonce from being presented as a grant would be the key
	// derivation. Two independent barriers is the point.
	mac.Write([]byte(prefix))
	mac.Write([]byte(encoded))
	return prefix + encoded + "." + b64.EncodeToString(mac.Sum(nil)), nil
}

// verify checks the prefix and MAC and returns the raw payload bytes.
func verify(prefix string, key []byte, token string) ([]byte, error) {
	// An empty key is not a key. HMAC accepts one and computes a MAC anybody
	// can reproduce, so a caller that reached a verifier before its key was set
	// would authenticate forged claims instead of failing. The minting side has
	// always refused it; refusing here too makes the two halves agree.
	if len(key) == 0 {
		return nil, ErrBadSignature
	}
	if !strings.HasPrefix(token, prefix) {
		return nil, ErrMalformed
	}
	rest := token[len(prefix):]
	dot := strings.LastIndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 {
		return nil, ErrMalformed
	}
	encoded, sigPart := rest[:dot], rest[dot+1:]
	sig, err := b64.DecodeString(sigPart)
	if err != nil {
		return nil, ErrMalformed
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(prefix))
	mac.Write([]byte(encoded))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return nil, ErrBadSignature
	}
	// Decode AFTER the MAC check so malformed-but-signed is the only decode
	// path an attacker can reach.
	payload, err := b64.DecodeString(encoded)
	if err != nil {
		return nil, ErrMalformed
	}
	return payload, nil
}

// MintNonce signs a single-use handshake nonce. Nothing is stored. The nonce
// becomes "used" only when the exchange endpoint burns its id.
func MintNonce(key []byte, surface, subject, origin string, scopes []string, ttl time.Duration, now time.Time) (string, error) {
	if len(key) == 0 {
		return "", errors.New("embed: no signing key")
	}
	normalized, err := NormalizeOrigin(origin)
	if err != nil {
		return "", err
	}
	id, err := newNonceID()
	if err != nil {
		return "", err
	}
	return sign(NoncePrefix, key, nonceClaims{
		Surface: surface,
		Subject: subject,
		Scopes:  scopes,
		Origin:  normalized,
		ID:      id,
		Expires: now.Add(ttl).Unix(),
	})
}

// VerifyNonce checks a nonce's signature and expiry. It does NOT check whether
// the nonce has been used. That is the burn store's job, and the separation is
// deliberate: signature verification is pure and testable, burning is I/O.
func VerifyNonce(key []byte, token string, now time.Time) (Nonce, error) {
	payload, err := verify(NoncePrefix, key, token)
	if err != nil {
		return Nonce{}, err
	}
	var c nonceClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Nonce{}, ErrMalformed
	}
	if c.Surface == "" || c.ID == "" || c.Origin == "" {
		return Nonce{}, ErrMalformed
	}
	exp := time.Unix(c.Expires, 0)
	if !now.Before(exp) {
		return Nonce{}, ErrExpired
	}
	return Nonce{
		Surface: c.Surface,
		Subject: c.Subject,
		Scopes:  c.Scopes,
		Origin:  c.Origin,
		ID:      c.ID,
		Expires: exp,
	}, nil
}

// MintGrant signs the frame credential a verified nonce is exchanged for.
// deadline caps the total life of this credential across every later refresh.
func MintGrant(key []byte, n Nonce, ttl time.Duration, deadline time.Time, now time.Time) (string, error) {
	if len(key) == 0 {
		return "", errors.New("embed: no signing key")
	}
	exp := now.Add(ttl)
	// Never mint past the deadline: a grant whose expiry outlived its own
	// absolute cap would keep verifying after refresh stopped being allowed.
	if exp.After(deadline) {
		exp = deadline
	}
	return sign(GrantPrefix, key, grantClaims{
		Surface:  n.Surface,
		Subject:  n.Subject,
		Scopes:   n.Scopes,
		Origin:   n.Origin,
		Expires:  exp.Unix(),
		Deadline: deadline.Unix(),
	})
}

// VerifyGrant checks a grant's signature and expiry.
func VerifyGrant(key []byte, token string, now time.Time) (Grant, error) {
	payload, err := verify(GrantPrefix, key, token)
	if err != nil {
		return Grant{}, err
	}
	var c grantClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Grant{}, ErrMalformed
	}
	if c.Surface == "" || c.Origin == "" {
		return Grant{}, ErrMalformed
	}
	exp := time.Unix(c.Expires, 0)
	if !now.Before(exp) {
		return Grant{}, ErrExpired
	}
	return Grant{
		Surface:  c.Surface,
		Subject:  c.Subject,
		Scopes:   c.Scopes,
		Origin:   c.Origin,
		Expires:  exp,
		Deadline: time.Unix(c.Deadline, 0),
	}, nil
}
