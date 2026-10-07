package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// changePasswordHandler handles POST {base}/password: a signed-in user
// replaces their own password, proving the current one.
//
// Body (JSON only, strict): {"current_password": "…", "password": "…"},
// plus an optional "confirm_password" a form sends, which must equal
// "password". A refusal the user can fix answers 422 with the
// form-errors envelope, {"error", "fields": {<field>: [message]}}, so a
// form shows the message beside the field.
//
// The caller is the interactive session behind the cookie: an API token
// or an embed grant resolves to the same user but is refused, the way
// token management refuses them, and a session still owed its second
// factor (pending, or minted before the user enrolled one) is refused
// the way the 2FA step-up refuses it. The current-password check spends
// the login limiters (per IP, and per account on the same key login
// uses), so a stolen session cannot brute-force the password here
// faster than at the login form.
//
// On success the user's other sessions and their outstanding reset
// links are revoked (a link mailed before the change would otherwise set
// the password back), and the caller is signed in on a fresh session
// that keeps the second factor the old one had passed. API tokens are
// left alone: they are credentials the user minted on purpose and
// revokes on their own.
func (c *CorePlugin) changePasswordHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectCrossSiteForm(w, r) {
			return
		}
		if !guardAuthLimit(c.loginLimit, w, r) {
			return
		}
		ctx := r.Context()
		if _, tokenAuth := TokenScopes(ctx); tokenAuth {
			writeAuthError(w, http.StatusUnauthorized, "changing the password requires an interactive session, not an API token")
			return
		}
		if _, embedded := embed.GrantFromContext(ctx); embedded {
			writeAuthError(w, http.StatusUnauthorized, "changing the password requires an interactive session, not an embedded surface")
			return
		}
		sess, err := c.mgr.requestSession(r, false)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if sess.PendingTwoFactor {
			writeAuthError(w, http.StatusForbidden, "two-factor verification required")
			return
		}
		twoFA, err := c.mgr.twoFactorEnabled(ctx, sess.UserID)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "two-factor state lookup failed")
			return
		}
		if twoFA && !sess.TwoFactorVerified {
			writeAuthError(w, http.StatusForbidden, "two-factor verification required")
			return
		}

		var body struct {
			Current  string  `json:"current_password"`
			Password string  `json:"password"`
			Confirm  *string `json:"confirm_password"`
		}
		if !decodeJSONLimited(w, r, &body) {
			return
		}
		switch {
		case body.Current == "":
			writeAuthFieldError(w, "current password required", "current_password", "is required")
			return
		case body.Password == "":
			writeAuthFieldError(w, "new password required", "password", "is required")
			return
		case len(body.Password) > 128:
			writeAuthFieldError(w, "password too long", "password", "must be at most 128 characters")
			return
		case ValidatePasswordStrength(body.Password) != nil:
			writeAuthFieldError(w, "password too short", "password",
				fmt.Sprintf("must be at least %d characters", RecommendedMinPasswordBytes))
			return
		case body.Confirm != nil && *body.Confirm != body.Password:
			writeAuthFieldError(w, "passwords do not match", "confirm_password", "does not match the new password")
			return
		}

		store := c.mgr.UserStore()
		setter, ok := store.(PasswordSetter)
		if !ok {
			writeAuthError(w, http.StatusInternalServerError, "user store does not implement PasswordSetter")
			return
		}
		user, err := store.FindByID(ctx, sess.UserID)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		email, err := c.mgr.canonicalizeEmail(user.GetEmail())
		if err != nil {
			email = user.GetEmail()
		}
		if c.loginLimitAccount != nil {
			if allowed, retry := c.loginLimitAccount.AllowContext(ctx, "account:"+email); !allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retry.Seconds()))
				writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}
		_, hash, err := store.FindByEmail(ctx, user.GetEmail())
		if err != nil || !CheckPassword(body.Current, hash) {
			c.mgr.emitSecurity(ctx, SecurityEvent{
				Kind: "password.change_failed", UserID: sess.UserID, Remote: remoteHost(r),
				Meta: map[string]string{"reason": "bad_credentials"},
			})
			writeAuthFieldError(w, "current password is incorrect", "current_password", "is incorrect")
			return
		}
		if body.Password == body.Current {
			writeAuthFieldError(w, "new password matches the current one", "password", "must differ from the current password")
			return
		}

		newHash, err := HashPassword(body.Password)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "hash failed")
			return
		}
		if err := setter.SetPassword(ctx, sess.UserID, newHash); err != nil {
			writeAuthError(w, http.StatusInternalServerError, "set password failed")
			return
		}
		c.mgr.emitSecurity(ctx, SecurityEvent{Kind: "password.changed", UserID: sess.UserID, Remote: remoteHost(r)})

		if p, ok := c.mgr.Plugin("password-reset"); ok {
			if reset, ok := p.(*PasswordResetPlugin); ok {
				reset.dropLinks(ctx, sess.UserID, "password_change", remoteHost(r))
			}
		}
		c.mgr.revokeUserSessions(ctx, sess.UserID, "password_change", remoteHost(r))
		_ = c.mgr.SessionStore().Delete(ctx, sess.Token)

		writeCredentialHeaders(w)
		if fresh, ok := c.remint(ctx, sess); ok {
			mintSessionCookie(w, c.mgr.Config(), fresh.Token, fresh.ExpiresAt)
		} else {
			mintSessionCookie(w, c.mgr.Config(), "", time.Time{})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
	}
}

// remint signs the caller back in after a change revoked every session
// of theirs. The fresh session goes through MintSession like any login,
// and a second factor the old session had passed carries over: the
// caller proved it in this session and the password just now. It reports
// false when no session could be made; the password change stands and
// the caller signs in again.
func (c *CorePlugin) remint(ctx context.Context, old *Session) (*Session, bool) {
	fresh, pending, err := c.mgr.MintSession(ctx, old.UserID, c.mgr.Config().SessionTTL)
	if err != nil {
		slog.Warn("password-change could not re-issue the session", "user_hash", hashedIdentifier(old.UserID), "err", err)
		return nil, false
	}
	if !pending {
		return fresh, true
	}
	marker, ok := c.mgr.SessionStore().(SessionTwoFAMarker)
	if !old.TwoFactorVerified || !ok || marker.MarkTwoFactorVerified(ctx, fresh.Token) != nil {
		_ = c.mgr.SessionStore().Delete(ctx, fresh.Token)
		return nil, false
	}
	return fresh, true
}

// twoFactorEnabled reports whether any registered TwoFactorChecker says
// the user has a second factor. A lookup error is returned, never read
// as "no factor".
func (m *AuthManager) twoFactorEnabled(ctx context.Context, userID string) (bool, error) {
	for _, name := range m.order {
		checker, ok := m.plugins[name].(TwoFactorChecker)
		if !ok {
			continue
		}
		enabled, err := checker.HasTwoFactorEnabled(ctx, userID)
		if err != nil {
			return false, err
		}
		if enabled {
			return true, nil
		}
	}
	return false, nil
}

// revokeUserSessions deletes every session of the user and records it.
// A store without SessionUserPurger leaves them, and that gap is logged.
func (m *AuthManager) revokeUserSessions(ctx context.Context, userID, reason, remote string) {
	purger, ok := m.SessionStore().(SessionUserPurger)
	if !ok {
		slog.Warn("could not revoke existing sessions: session store does not implement SessionUserPurger",
			"reason", reason, "user_hash", hashedIdentifier(userID))
		return
	}
	n, err := purger.DeleteByUser(ctx, userID)
	if err != nil {
		slog.Warn("session revocation failed", "reason", reason, "user_hash", hashedIdentifier(userID), "err", err)
		return
	}
	m.emitSecurity(ctx, SecurityEvent{
		Kind: "session.revoked", UserID: userID, Remote: remote,
		Meta: map[string]string{"reason": reason, "count": strconv.Itoa(n)},
	})
}

// writeAuthFieldError answers 422 with the form-errors envelope: the
// flat error fields writeAuthError writes, plus the one field's message.
func writeAuthFieldError(w http.ResponseWriter, msg, field, fieldMsg string) {
	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   msg,
		"success": false,
		"code":    http.StatusUnprocessableEntity,
		"fields":  map[string][]string{field: {fieldMsg}},
	})
}
