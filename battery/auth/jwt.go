package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/gofastr/gofastr/core/handler"
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
	Secret string
	Expiry time.Duration
	Issuer string
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

// ValidateToken parses and validates a JWT token string.
// Returns the claims if the token is valid, or an error otherwise.
func (j *JWTAuth) ValidateToken(tokenString string) (Claims, error) {
	claims, err := decodeToken(j.Secret, j.Issuer, tokenString)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %v", ErrUnauthorized, err)
	}

	if time.Now().After(claims.ExpiresAt) {
		return Claims{}, fmt.Errorf("%w: token expired", ErrUnauthorized)
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
