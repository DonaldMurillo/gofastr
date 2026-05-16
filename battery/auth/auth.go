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

// OAuthLinker is the optional UserStore extension that lets the OAuth2
// callback bind a user to a (provider, provider_id) pair. Stores that
// implement this interface get the safer flow:
//
//  1. FindByOAuth(provider, providerID) — returns the locally-linked user.
//  2. If no match, fall back to FindByEmail. A pre-existing local account
//     with the same email is REFUSED (409) to prevent silent takeover by
//     a non-verifying provider.
//  3. If no match by either path, CreateUser + LinkOAuth.
//
// Stores that do NOT implement OAuthLinker get the legacy email-only
// behavior — keep that path off in production.
type OAuthLinker interface {
	FindByOAuth(ctx context.Context, provider, providerID string) (User, error)
	LinkOAuth(ctx context.Context, userID, provider, providerID string) error
}

// Sentinel errors for auth operations.
var (
	ErrUnauthorized = errors.New("auth: unauthorized")
	ErrForbidden    = errors.New("auth: forbidden")

	// ErrUserNotFound is returned by UserStore.FindByEmail / FindByID when
	// the user does not exist. Distinct from a DB / transport error so
	// downstream handlers (magic-link, OAuth) can decide whether to
	// auto-create. Treating any error as "not found" is the bug class
	// this sentinel exists to avoid.
	ErrUserNotFound = errors.New("auth: user not found")

	// ErrEmailTaken is returned by UserStore.CreateUser when the email
	// is already registered. Distinct from generic DB errors so the
	// register handler can return 409 instead of 500.
	ErrEmailTaken = errors.New("auth: email already taken")
)
