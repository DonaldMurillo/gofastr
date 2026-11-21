package auth

import (
	"context"
	"errors"
	"time"
)

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

const undoTimeout = 10 * time.Second

func undoContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), undoTimeout)
}

func (s credentialSnapshot) moved(ctx context.Context, m *AuthManager) bool {
	if !s.ok {
		return false
	}
	return !credentialUnchanged(ctx, m.UserStore(), s.email, s.userID, s.hash)
}

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
