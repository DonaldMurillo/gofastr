package auth

import (
	"context"
	"errors"
	"time"
)

// credentialSnapshot is the user's stored password hash, read before a
// request that mints a lasting credential (an API token, a passkey) reads
// its body. A reset, change or account claim moves the hash before it
// revokes anything, so a request held open across one finds the hash
// moved when it re-checks after storing, and undoes what it stored.
type credentialSnapshot struct {
	userID, email, hash string
	ok                  bool // false: nothing to compare (no store, no such user)
}

func (m *AuthManager) snapshotCredential(ctx context.Context, userID string) (credentialSnapshot, error) {
	store := m.UserStore()
	if store == nil {
		return credentialSnapshot{}, nil
	}
	u, err := store.FindByID(ctx, userID)
	if errors.Is(err, ErrUserNotFound) {
		// A credential for a missing owner never authenticates.
		return credentialSnapshot{}, nil
	}
	if err != nil {
		return credentialSnapshot{}, err
	}
	_, hash, err := store.FindByEmail(ctx, u.GetEmail())
	if errors.Is(err, ErrUserNotFound) {
		return credentialSnapshot{}, nil
	}
	if err != nil {
		return credentialSnapshot{}, err
	}
	return credentialSnapshot{userID: userID, email: u.GetEmail(), hash: hash, ok: true}, nil
}

// moved reports whether the hash changed since the snapshot. A failed
// re-read counts as moved: the caller undoes its write rather than keep
// a credential it could not vouch for.
// undoTimeout bounds the store call that takes back a credential a fence
// refused.
const undoTimeout = 10 * time.Second

// undoContext is the context a fence's undo runs on: the request's values
// without its cancellation, bounded by undoTimeout. A client that hangs up
// between the insert and the undo cancels the request context, and an
// undo run on it fails and leaves the credential it raced in the store.
func undoContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), undoTimeout)
}

func (s credentialSnapshot) moved(ctx context.Context, m *AuthManager) bool {
	if !s.ok {
		return false
	}
	return !credentialUnchanged(ctx, m.UserStore(), s.email, s.userID, s.hash)
}

// revokeUserAPITokens revokes every live API token userID owns, through the
// store the TokensPlugin was built with. A credential reset that left these
// alive did not lock out whoever held the account before it: a token minted
// with the old password kept authenticating afterwards.
//
// supported is false when no TokensPlugin is registered; tokens a host mints
// with IssueToken against its own store are the host's to revoke.
func (m *AuthManager) revokeUserAPITokens(ctx context.Context, userID string) (n int, supported bool, err error) {
	p, ok := m.Plugin("api-tokens")
	if !ok {
		return 0, false, nil
	}
	tp, ok := p.(*TokensPlugin)
	if !ok || tp.tokens == nil {
		return 0, false, nil
	}
	list, err := tp.tokens.List(ctx, "user", userID)
	if err != nil {
		return 0, true, err
	}
	for _, t := range list {
		if t.RevokedAt != nil {
			continue
		}
		if err := tp.tokens.Revoke(ctx, t.ID, "user", userID); err != nil {
			if errors.Is(err, ErrTokenNotFound) {
				continue
			}
			return n, true, err
		}
		n++
	}
	return n, true, nil
}
