package embed

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Token prefixes, which mark a leaked string as an embed credential.
const (
	// NoncePrefix marks a single-use handshake nonce.
	NoncePrefix = "emb_"
	// GrantPrefix marks the stateless grant a nonce is exchanged for.
	GrantPrefix = "emg_"
)

// HKDF purposes the nonce and grant keys are derived under.
const (
	NoncePurpose = "gofastr/embed/nonce/v1"
	GrantPurpose = "gofastr/embed/grant/v1"
)

var (
	// ErrMalformed means the string is not a token of this kind at all.
	ErrMalformed = errors.New("embed: malformed token")
	// ErrBadSignature means the MAC did not verify under the given key.
	ErrBadSignature = errors.New("embed: bad token signature")
	// ErrExpired means the token verified but its expiry has passed.
	ErrExpired = errors.New("embed: token expired")
)

type nonceClaims struct {
	Surface string   `json:"s"`            // embeddable surface name
	Subject string   `json:"u"`            // app-side identity the frame acts as
	Scopes  []string `json:"sc,omitempty"` // capability list carried into the grant
	Origin  string   `json:"o"`            // normalized origin allowed to frame it
	ID      string   `json:"n"`            // random single-use id; the burn key
	Expires int64    `json:"x"`            // unix seconds
}

type grantClaims struct {
	Surface string   `json:"s"`
	Subject string   `json:"u"`
	Scopes  []string `json:"sc,omitempty"`
	Origin  string   `json:"o"`
	Expires int64    `json:"x"`
	// Deadline is the absolute end of the credential's life, unchanged by refresh.
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
	// Deadline is the absolute end of the credential's life; refreshes never move it.
	Deadline time.Time
}

// HasScope reports whether the grant carries scope.
func (g Grant) HasScope(scope string) bool {
	return slices.Contains(g.Scopes, scope)
}

var b64 = base64.RawURLEncoding

func newNonceID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("embed: generate nonce id: %w", err)
	}
	return b64.EncodeToString(buf[:]), nil
}

func sign(prefix string, key []byte, claims any) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("embed: marshal claims: %w", err)
	}
	encoded := b64.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(prefix))
	mac.Write([]byte(encoded))
	return prefix + encoded + "." + b64.EncodeToString(mac.Sum(nil)), nil
}

func verify(prefix string, key []byte, token string) ([]byte, error) {
	if len(key) == 0 {
		return nil, ErrBadSignature
	}
	if !strings.HasPrefix(token, prefix) {
		return nil, ErrMalformed
	}
	rest := token[len(prefix):]
	encoded, sigPart, ok := strings.CutLast(rest, ".")
	if !ok || encoded == "" || sigPart == "" {
		return nil, ErrMalformed
	}
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
	payload, err := b64.DecodeString(encoded)
	if err != nil {
		return nil, ErrMalformed
	}
	return payload, nil
}

// MintNonce signs a single-use nonce; it errors on an empty key or a bad origin.
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

// VerifyNonce checks a nonce's signature, claims and expiry, not whether it is burned.
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

// MintGrant signs a grant whose expiry is clamped to deadline.
func MintGrant(key []byte, n Nonce, ttl time.Duration, deadline time.Time, now time.Time) (string, error) {
	if len(key) == 0 {
		return "", errors.New("embed: no signing key")
	}
	exp := now.Add(ttl)
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

// VerifyGrant checks a grant's signature, required claims and expiry.
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
