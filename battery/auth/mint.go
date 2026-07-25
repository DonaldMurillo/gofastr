package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// ErrTwoFactorEnforcement wraps a MintSession failure whose cause was the
// second-factor pending mark rather than session creation. Callers
// distinguish the two so the login-failure audit event names the real
// reason; both are a 500 to the client.
var ErrTwoFactorEnforcement = errors.New("auth: two-factor enforcement unavailable")

// MintSession creates a session for userID and applies the second-factor
// pending mark. Every login path — password, magic link, OAuth callback,
// and any host-added plugin — must go through here rather than calling
// SessionStore().Create directly, so a new path cannot silently skip the
// factor by construction.
//
// Fail-closed: when the pending mark cannot be established the session is
// deleted and an error returned; the caller must reject the login.
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

// markPendingIfTwoFactorEnabled queries any registered TwoFactorChecker
// plugins and, if any reports the user has 2FA enabled, marks the new
// session as pending — a default-deny posture so missing the
// /2fa/challenge call doesn't leave the session fully privileged.
//
// It lives on the manager, not on CorePlugin, because EVERY path that
// mints a session has to call it. When it hung off the password handler,
// magic-link verify and the OAuth callback each created a session the
// enforcement layer never learned about: PendingTwoFactor stayed false,
// so those sessions were treated as fully authenticated and could
// disable the second factor outright. Use MintSession rather than
// calling this directly.
//
// Fail-closed contract: if 2FA state can't be determined (checker error)
// or the pending mark can't be established (store doesn't implement
// SessionPendingMarker, or the mark call fails), it returns an error and
// the caller must reject the login. Anything else silently downgrades
// 2FA-enrolled accounts to password-only auth.
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
