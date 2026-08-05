package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// Claims represents the data embedded in a JWT token.
type Claims struct {
	UserID    string
	Email     string
	Roles     []string
	ExpiresAt time.Time
	IssuedAt  time.Time
}

// JWTAuth manages JWT token generation and validation.
type JWTAuth struct {
	// Secret is the current signing key; new tokens are always signed
	// with it.
	Secret string
	// PreviousSecrets are verify-only signing keys retained for a drain
	// window after a JWTSecret rotation, mirroring the CSRF AdditionalKeys
	// idiom. Tokens signed by any key here still validate; the operator
	// drops them once old tokens have expired. GenerateToken never signs
	// with these.
	PreviousSecrets []string
	Expiry          time.Duration
	Issuer          string
}

// NewJWTAuth creates a new JWTAuth with the given secret and token expiry duration.
func NewJWTAuth(secret string, expiry time.Duration) *JWTAuth {
	return &JWTAuth{
		Secret: secret,
		Expiry: expiry,
		Issuer: "gofastr",
	}
}

// GenerateToken creates a signed JWT token for the given user.
// The token contains the user's ID, email, and roles.
func (j *JWTAuth) GenerateToken(user User) (string, error) {
	if user == nil {
		return "", ErrUnauthorized
	}

	now := time.Now()
	claims := Claims{
		UserID:    user.GetID(),
		Email:     user.GetEmail(),
		Roles:     user.GetRoles(),
		ExpiresAt: now.Add(j.Expiry),
		IssuedAt:  now,
	}

	return encodeToken(j.Secret, j.Issuer, claims)
}

// ValidateToken parses and validates a JWT token string. It accepts the
// current signing key OR any previous (rotation) key, so a JWTSecret
// rotation drains over a token TTL instead of invalidating every session at
// once. Each candidate signature is compared with hmac.Equal via
// decodeToken (constant-time); the count and order of keys is not secret, so
// first-match-return is sound. Returns the claims if valid, or an error.
func (j *JWTAuth) ValidateToken(tokenString string) (Claims, error) {
	// Try the current secret first, then each previous key. decodeToken is
	// a full parse (signature + header + payload + issuer), and only a key
	// that produces a valid signature can pass it, so re-parsing per key is
	// safe and bounded by the (small) rotation set.
	candidates := make([]string, 0, 1+len(j.PreviousSecrets))
	candidates = append(candidates, j.Secret)
	candidates = append(candidates, j.PreviousSecrets...)

	var claims Claims
	matched := false
	for _, secret := range candidates {
		if secret == "" {
			continue
		}
		c, err := decodeToken(secret, j.Issuer, tokenString)
		if err == nil {
			claims = c
			matched = true
			break
		}
	}
	if !matched {
		return Claims{}, fmt.Errorf("%w: no matching signing key", ErrUnauthorized)
	}

	if claims.UserID == "" {
		return Claims{}, fmt.Errorf("%w: token has empty subject", ErrUnauthorized)
	}

	now := time.Now()
	if now.After(claims.ExpiresAt) {
		return Claims{}, fmt.Errorf("%w: token expired", ErrUnauthorized)
	}
	// Reject tokens that claim to have been issued in the future. Allow
	// a small skew for clock drift between issuer and verifier.
	if !claims.IssuedAt.IsZero() && claims.IssuedAt.After(now.Add(2*time.Minute)) {
		return Claims{}, fmt.Errorf("%w: token issued in the future", ErrUnauthorized)
	}

	return claims, nil
}

// claimsToUser converts Claims into a User.
func claimsToUser(c Claims) User {
	return &BasicUser{
		ID:    c.UserID,
		Email: c.Email,
		Roles: c.Roles,
	}
}

// userToContext stores the user derived from claims into the context.
func userToContext(ctx context.Context, user User) context.Context {
	return handler.SetUser(ctx, user)
}
