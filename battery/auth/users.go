package auth

import (
	"context"
	"errors"
)

// ListUsersOptions controls paged user enumeration. Limit and Offset
// are clamped by AuthManager.ListUsers before the store sees them, so
// custom UserLister implementations can assume sane values.
type ListUsersOptions struct {
	Limit  int
	Offset int
}

// UserLister is the optional UserStore extension that enumerates
// accounts in a stable order. Back-offices use it instead of raw SQL
// against the auth_users table.
//
// Stores that do not implement it are unsupported: AuthManager.ListUsers
// returns ErrListUsersUnsupported rather than silently returning empty
// results, so a misconfigured deployment fails loudly instead of
// rendering a blank user list.
type UserLister interface {
	ListUsers(ctx context.Context, opts ListUsersOptions) (users []User, total int, err error)
}

// ErrListUsersUnsupported is returned by AuthManager.ListUsers when the
// configured UserStore does not implement UserLister. Distinct from a
// transport error so callers can present "feature unavailable" vs "DB
// down" appropriately.
var ErrListUsersUnsupported = errors.New("auth: user store does not support listing users")

// ListUsers enumerates accounts through the configured UserStore when
// it implements UserLister. It is the supported replacement for raw SQL
// against the auth_users table. There is no HTTP route — call it from
// trusted server code (an admin handler, a back-office screen).
//
// opts is clamped: Limit<=0 becomes 50, values above 500 are capped at
// 500 (an unbounded list is a DoS vector on a large user table), and a
// negative Offset is treated as 0.
//
// Returns ErrListUsersUnsupported when the store lacks the UserLister
// interface, so a deployment that forgot to wire a listable store is
// told explicitly rather than handed an empty page.
func (m *AuthManager) ListUsers(ctx context.Context, opts ListUsersOptions) ([]User, int, error) {
	lister, ok := m.userStore.(UserLister)
	if !ok {
		return nil, 0, ErrListUsersUnsupported
	}
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	return lister.ListUsers(ctx, opts)
}
