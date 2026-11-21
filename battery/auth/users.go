package auth

import (
	"context"
	"errors"
)

// ListUsersOptions pages ListUsers; AuthManager.ListUsers clamps it.
type ListUsersOptions struct {
	Limit  int
	Offset int
}

// UserLister is the UserStore extension that lists accounts in a stable order.
type UserLister interface {
	ListUsers(ctx context.Context, opts ListUsersOptions) (users []User, total int, err error)
}

// ErrListUsersUnsupported means the UserStore is not a UserLister.
var ErrListUsersUnsupported = errors.New("auth: user store does not support listing users")

// ListUsers lists accounts through a UserLister, clamping Limit to 1..500 (default 50).
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
