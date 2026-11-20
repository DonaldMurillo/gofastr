package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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

	cred string
}

// JWTAuth manages JWT token generation and validation.
type JWTAuth struct {
	// Secret signs new tokens.
	Secret string
	// PreviousSecrets are verify-only keys kept for a rotation's drain window.
	PreviousSecrets []string
	Expiry          time.Duration
	Issuer          string

	users UserStore

	onResolve func()
}

// NewJWTAuth creates a new JWTAuth with the given secret and token expiry duration.
func NewJWTAuth(secret string, expiry time.Duration) *JWTAuth {
	return &JWTAuth{
		Secret: secret,
		Expiry: expiry,
		Issuer: "gofastr",
	}
}

// SetUserStore sets the store RequireAuth re-reads each token's owner from.
func (j *JWTAuth) SetUserStore(users UserStore) {
	j.users = users
}

// GenerateToken creates a signed JWT token for the given user.
func (j *JWTAuth) GenerateToken(user User) (string, error) {
	if user == nil {
		return "", ErrUnauthorized
	}
	hash := ""
	if j.users != nil {
		h, err := j.storedHash(context.Background(), user)
		if err != nil {
			return "", err
		}
		hash = h
	}
	return j.generateToken(user, hash)
}

func (j *JWTAuth) generateToken(user User, hash string) (string, error) {
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
	if j.users != nil {
		claims.cred = credentialTag(j.Secret, claims.UserID, hash)
	}

	return encodeToken(j.Secret, j.Issuer, claims)
}

func credentialTag(secret, userID, hash string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("gofastr-jwt-credential\x00"))
	mac.Write([]byte(userID))
	mac.Write([]byte{0})
	mac.Write([]byte(hash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}

func (j *JWTAuth) storedHash(ctx context.Context, user User) (string, error) {
	row, hash, err := j.users.FindByEmail(ctx, user.GetEmail())
	if err != nil {
		return "", fmt.Errorf("%w: credential lookup: %v", ErrUnauthorized, err)
	}
	if row == nil || row.GetID() != user.GetID() {
		return "", fmt.Errorf("%w: credential lookup found another user", ErrUnauthorized)
	}
	return hash, nil
}

func (j *JWTAuth) credentialCurrent(ctx context.Context, c Claims, user User) bool {
	if c.cred == "" {
		return false
	}
	hash, err := j.storedHash(ctx, user)
	if err != nil {
		return false
	}
	ok := false
	for _, secret := range append([]string{j.Secret}, j.PreviousSecrets...) {
		if secret == "" {
			continue
		}
		if hmac.Equal([]byte(c.cred), []byte(credentialTag(secret, user.GetID(), hash))) {
			ok = true
		}
	}
	return ok
}

// ValidateToken checks a token's signature (current or previous key), subject, expiry and issue time, and returns its claims.
func (j *JWTAuth) ValidateToken(tokenString string) (Claims, error) {
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
	if !claims.IssuedAt.IsZero() && claims.IssuedAt.After(now.Add(2*time.Minute)) {
		return Claims{}, fmt.Errorf("%w: token issued in the future", ErrUnauthorized)
	}

	return claims, nil
}

func (j *JWTAuth) resolveOwner(ctx context.Context, c Claims) (User, bool) {
	if j.users == nil {
		return claimsToUser(c), true
	}
	u, err := j.users.FindByID(ctx, c.UserID)
	if err != nil || u == nil {
		return nil, false
	}
	if !j.credentialCurrent(ctx, c, u) {
		return nil, false
	}
	return u, true
}

func claimsToUser(c Claims) User {
	return &BasicUser{
		ID:    c.UserID,
		Email: c.Email,
		Roles: c.Roles,
	}
}

func userToContext(ctx context.Context, user User) context.Context {
	return handler.SetUser(ctx, user)
}
