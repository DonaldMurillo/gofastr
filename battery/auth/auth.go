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

// OAuthLinker is the REQUIRED UserStore extension for OAuth login: it binds
// a user to a durable (provider, provider_id) pair. OAuth2Plugin.Init fails
// closed in production when the configured store is not an OAuthLinker (only
// DevMode / AllowInMemoryStores relaxes it), so the legacy email-only path is
// gone. EntityUserStore now implements this by default.
//
// The callback's resolveOAuthUser decision table (see oauth2.go):
//
//  1. FindByOAuth(provider, providerID) matches → LOGIN as that user.
//  2. No link, but the provider's email (email_verified) matches an existing
//     local account:
//     - account HAS a password → REFUSED (409): the user must log in with
//     their password and add the provider via the authenticated link flow
//     (GET /auth/oauth/{provider}/link) — protects the local credential
//     from IdP-email takeover.
//     - account is PASSWORDLESS → AUTO-LINK + login (safe migration: a prior
//     OAuth-created account re-binds to the same verified identity).
//  3. Email is UNVERIFIED, or no email match → CreateUser + LinkOAuth (a
//     fresh distinct account; an unverified email never binds to an existing
//     one). See framework/docs/content/auth.md for the full contract.
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
