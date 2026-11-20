package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrTwoFactorEnforcement wraps a MintSession failure to apply the second-factor pending mark.
var ErrTwoFactorEnforcement = errors.New("auth: two-factor enforcement unavailable")

// MintSession creates a session for userID and marks it pending when the user has a second factor; every sign-in path mints through it.
//
//gofastr:mustuse pending
func (m *AuthManager) MintSession(ctx context.Context, userID string, ttl time.Duration) (sess *Session, pending bool, err error) {
	sess, err = m.SessionStore().Create(ctx, userID, ttl)
	if err != nil {
		return nil, false, err
	}
	pending, err = m.markPendingIfTwoFactorEnabled(ctx, sess.Token, userID)
	if err != nil {
		_ = m.SessionStore().Delete(ctx, sess.Token)
		return nil, false, fmt.Errorf("%w: %s", ErrTwoFactorEnforcement, err)
	}
	return sess, pending, nil
}

func (m *AuthManager) markPendingIfTwoFactorEnabled(ctx context.Context, sessionToken, userID string) (pending bool, err error) {
	for _, name := range m.order {
		checker, ok := m.plugins[name].(TwoFactorChecker)
		if !ok {
			continue
		}
		enabled, err := checker.HasTwoFactorEnabled(ctx, userID)
		if err != nil {
			slog.Default().Warn("auth: two-factor state lookup failed; rejecting login (fail-closed)",
				"plugin", name, "error", err)
			return false, fmt.Errorf("two-factor state lookup (%s): %w", name, err)
		}
		if !enabled {
			continue
		}
		marker, ok := m.SessionStore().(SessionPendingMarker)
		if !ok {
			slog.Default().Warn("auth: user has two-factor enabled but the session store does not implement SessionPendingMarker; rejecting login (fail-closed)",
				"plugin", name, "store", fmt.Sprintf("%T", m.SessionStore()))
			return false, fmt.Errorf("session store %T cannot mark a session pending two-factor", m.SessionStore())
		}
		if err := marker.MarkPendingTwoFactor(ctx, sessionToken); err != nil {
			slog.Default().Warn("auth: marking session pending two-factor failed; rejecting login (fail-closed)",
				"plugin", name, "error", err)
			return false, fmt.Errorf("mark pending two-factor: %w", err)
		}
		return true, nil
	}
	return false, nil
}
