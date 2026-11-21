package auth

import (
	"context"
	"errors"
)

// User represents an authenticated user.
type User interface {
	GetID() string
	GetEmail() string
	GetRoles() []string
}

// BasicUser is a simple implementation of the User interface.
type BasicUser struct {
	ID    string
	Email string
	Roles []string
}

func (u *BasicUser) GetID() string      { return u.ID }
func (u *BasicUser) GetEmail() string   { return u.Email }
func (u *BasicUser) GetRoles() []string { return u.Roles }

// Authenticator verifies credentials and returns the authenticated user.
type Authenticator interface {
	Authenticate(ctx context.Context, credentials Credentials) (User, error)
}

// Credentials holds the information needed to authenticate a user.
type Credentials struct {
	Email    string // email address
	Password string // password (for email/password auth)
	Provider string // OAuth provider name (e.g. "google", "github")
	Token    string // API key or bearer token
}

// OAuthLinker binds a user to a (provider, provider_id) pair; OAuth2Plugin requires it in production.
type OAuthLinker interface {
	FindByOAuth(ctx context.Context, provider, providerID string) (User, error)
	LinkOAuth(ctx context.Context, userID, provider, providerID string) error
}

// Sentinel errors for auth operations.
var (
	ErrUnauthorized = errors.New("auth: unauthorized")
	ErrForbidden    = errors.New("auth: forbidden")

	// ErrUserNotFound means no such user, as distinct from a store failure.
	ErrUserNotFound = errors.New("auth: user not found")

	// ErrEmailTaken means CreateUser found the address registered.
	ErrEmailTaken = errors.New("auth: email already taken")
)
