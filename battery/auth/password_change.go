package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
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
			writeAuthError(w, r, http.StatusUnauthorized, reasonPasswordChangeToken)
			return
		}
		if _, embedded := embed.GrantFromContext(ctx); embedded {
			writeAuthError(w, r, http.StatusUnauthorized, reasonPasswordChangeEmbed)
			return
		}
		sess, err := c.mgr.requestSession(r, false)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
			return
		}
		if sess.PendingTwoFactor {
			writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
			return
		}
		twoFA, err := c.mgr.twoFactorEnabled(ctx, sess.UserID)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonTwoFactorStateLookupFailed)
			return
		}
		if twoFA && !sess.TwoFactorVerified {
			writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
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
			writeAuthFieldError(w, r, reasonCurrentPasswordRequired, "current_password", schema.Violation{Reason: schema.ReasonRequired})
			return
		case body.Password == "":
			writeAuthFieldError(w, r, reasonNewPasswordRequired, "password", schema.Violation{Reason: schema.ReasonRequired})
			return
		case len(body.Password) > 128:
			writeAuthFieldError(w, r, reasonPasswordTooLong, "password", schema.Violation{Reason: schema.ReasonMaxLength, Params: map[string]any{"max": 128}})
			return
		case ValidatePasswordStrength(body.Password) != nil:
			writeAuthFieldError(w, r, reasonPasswordTooShortField, "password", schema.Violation{Reason: schema.ReasonMinLength, Params: map[string]any{"min": RecommendedMinPasswordBytes}})
			return
		case body.Confirm != nil && *body.Confirm != body.Password:
			writeAuthFieldError(w, r, reasonPasswordsMismatch, "confirm_password", schema.Violation{Reason: reasonConfirmMismatch})
			return
		}

		store := c.mgr.UserStore()
		setter, ok := store.(PasswordSetter)
		if !ok {
			writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoPasswordSetter)
			return
		}
		user, err := store.FindByID(ctx, sess.UserID)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, handler.ReasonAuthenticationRequired)
			return
		}
		email, err := c.mgr.canonicalizeEmail(user.GetEmail())
		if err != nil {
			email = user.GetEmail()
		}
		if c.loginLimitAccount != nil {
			if allowed, retry := c.loginLimitAccount.AllowContext(ctx, "account:"+email); !allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retry.Seconds()))
				writeAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
				return
			}
		}
		_, hash, err := store.FindByEmail(ctx, user.GetEmail())
		if err != nil || !verifyPassword(body.Current, hash) {
			c.mgr.emitSecurity(ctx, SecurityEvent{
				Kind: "password.change_failed", UserID: sess.UserID, Remote: remoteHost(r),
				Meta: map[string]string{"reason": "bad_credentials"},
			})
			writeAuthFieldError(w, r, reasonCurrentPasswordIncorrect, "current_password", schema.Violation{Reason: reasonCurrentMismatch})
			return
		}
		if body.Password == body.Current {
			writeAuthFieldError(w, r, reasonPasswordUnchanged, "password", schema.Violation{Reason: reasonMustDiffer})
			return
		}

		newHash, err := HashPassword(body.Password)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonHashFailed)
			return
		}
		if err := swapPassword(ctx, store, setter, sess.UserID, user.GetEmail(), hash, newHash); err != nil {
			if errors.Is(err, errPasswordMoved) {
				// A reset or another change landed while the new hash was
				// being computed. The password just checked is no longer
				// the account's, so the change fails like a wrong one.
				c.mgr.emitSecurity(ctx, SecurityEvent{
					Kind: "password.change_failed", UserID: sess.UserID, Remote: remoteHost(r),
					Meta: map[string]string{"reason": "credential_changed"},
				})
				writeAuthFieldError(w, r, reasonCurrentPasswordIncorrect, "current_password", schema.Violation{Reason: reasonCurrentMismatch})
				return
			}
			writeAuthError(w, r, http.StatusInternalServerError, reasonSetPasswordFailed)
			return
		}
		c.mgr.emitSecurity(ctx, SecurityEvent{Kind: "password.changed", UserID: sess.UserID, Remote: remoteHost(r)})

		if p, ok := c.mgr.Plugin("password-reset"); ok {
			if reset, ok := p.(*PasswordResetPlugin); ok {
				reset.dropLinks(ctx, sess.UserID, "password_change", remoteHost(r))
			}
		}
		// The change stands, but a failed purge leaves a stolen cookie on
		// another device working, so it is not reported as done.
		if err := c.mgr.revokeUserSessions(ctx, sess.UserID, "password_change", remoteHost(r)); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonChangeIncomplete)
			return
		}
		// A failed link purge logs rather than fails: the change stands.
		if err := c.mgr.dropMagicLinks(ctx, sess.UserID, "password_change", remoteHost(r)); err != nil {
			slog.Warn("magic-link purge failed", "reason", "password_change",
				"user_hash", hashedIdentifier(sess.UserID), "err", err)
		}
		_ = c.mgr.SessionStore().Delete(ctx, sess.Token)

		writeCredentialHeaders(w)
		if fresh, ok := c.remint(ctx, sess, user.GetEmail(), newHash); ok {
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
//
// A reset, another change or an account claim that finished after this
// change revoked the sessions purged nothing the remint makes. So once the
// fresh session is stored, the credential is re-read the way a login does:
// a hash that is no longer newHash, the one this change wrote, means the
// password is no longer the caller's, and the session goes.
func (c *CorePlugin) remint(ctx context.Context, old *Session, email, newHash string) (*Session, bool) {
	fresh, pending, err := c.mgr.MintSession(ctx, old.UserID, c.mgr.Config().SessionTTL)
	if err != nil {
		slog.Warn("password-change could not re-issue the session", "user_hash", hashedIdentifier(old.UserID), "err", err)
		return nil, false
	}
	if pending {
		marker, ok := c.mgr.SessionStore().(SessionTwoFAMarker)
		if !old.TwoFactorVerified || !ok || marker.MarkTwoFactorVerified(ctx, fresh.Token) != nil {
			_ = c.mgr.SessionStore().Delete(ctx, fresh.Token)
			return nil, false
		}
	}
	if !credentialUnchanged(ctx, c.mgr.UserStore(), email, old.UserID, newHash) {
		undo, cancel := undoContext(ctx)
		defer cancel()
		if err := c.mgr.SessionStore().Delete(undo, fresh.Token); err != nil {
			slog.Warn("password-change: dropping a session re-issued across a credential change failed",
				"user_hash", hashedIdentifier(old.UserID), "err", err)
		}
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

// errPasswordMoved reports that the hash a password change verified is no
// longer the stored one.
var errPasswordMoved = errors.New("auth: the password changed during the request")

// swapPassword writes newHash only while oldHash, the hash the change
// verified, is still stored. Written unconditionally, a reset that landed
// during the bcrypt round was overwritten by the stale request's password.
// A PasswordSwapper store does it in one statement; any other store is
// re-read just before the write, which narrows the window to the gap
// between the two calls.
func swapPassword(ctx context.Context, store UserStore, setter PasswordSetter, userID, email, oldHash, newHash string) error {
	if sw, ok := store.(PasswordSwapper); ok {
		swapped, err := sw.SwapPassword(ctx, userID, oldHash, newHash)
		if err != nil {
			return err
		}
		if !swapped {
			return errPasswordMoved
		}
		return nil
	}
	_, current, err := store.FindByEmail(ctx, email)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(current), []byte(oldHash)) != 1 {
		return errPasswordMoved
	}
	return setter.SetPassword(ctx, userID, newHash)
}

// revokeUserSessions deletes every session of the user and records it.
// A store without SessionUserPurger leaves them, and that gap is logged.
// A purge that fails returns its error: the caller must not report the
// account secured while the old sessions still work.
func (m *AuthManager) revokeUserSessions(ctx context.Context, userID, reason, remote string) error {
	purger, ok := m.SessionStore().(SessionUserPurger)
	if !ok {
		slog.Warn("could not revoke existing sessions: session store does not implement SessionUserPurger",
			"reason", reason, "user_hash", hashedIdentifier(userID))
		return nil
	}
	n, err := purger.DeleteByUser(ctx, userID)
	if err != nil {
		slog.Warn("session revocation failed", "reason", reason, "user_hash", hashedIdentifier(userID), "err", err)
		return err
	}
	m.emitSecurity(ctx, SecurityEvent{
		Kind: "session.revoked", UserID: userID, Remote: remote,
		Meta: map[string]string{"reason": reason, "count": strconv.Itoa(n)},
	})
	return nil
}

// dropMagicLinks spends every outstanding magic link mailed to the user.
// A link mailed before a password change or reset, the one an attacker
// with mailbox access may hold, otherwise still signs in afterwards. The
// magic-link store keys on the canonical address the send handler wrote,
// so the stored address is canonicalized first: a raw "Mixed@Example.com"
// matched no row. A token store without MagicLinkTokenPurger keeps the
// links, and that gap is logged; the error is a purge that failed.
func (m *AuthManager) dropMagicLinks(ctx context.Context, userID, reason, remote string) error {
	p, ok := m.Plugin("magic-link")
	if !ok {
		return nil
	}
	mp, ok := p.(*MagicLinkPlugin)
	if !ok {
		return nil
	}
	user, err := m.UserStore().FindByID(ctx, userID)
	if err != nil {
		return err
	}
	email, err := m.canonicalizeEmail(user.GetEmail())
	if err != nil {
		email = user.GetEmail()
	}
	n, supported, err := purgePurposeTokens(ctx, mp.tokenStore, purposeMagicLink, email)
	switch {
	case err != nil:
		return err
	case !supported:
		slog.Warn("could not drop outstanding magic links: token store does not implement MagicLinkTokenPurger",
			"reason", reason, "user_hash", hashedIdentifier(userID))
	case n > 0:
		m.emitSecurity(ctx, SecurityEvent{
			Kind: "magiclink_tokens.purged", UserID: userID, Remote: remote,
			Meta: map[string]string{"reason": reason, "count": strconv.Itoa(n)},
		})
	}
	return nil
}

// writeAuthFieldError answers 422 with the form-errors envelope: the
// flat error fields writeAuthError writes, plus the one field's
// violation as fields (text) and field_reasons (code).
func writeAuthFieldError(w http.ResponseWriter, r *http.Request, reason i18n.Phrase, field string, why schema.Violation) {
	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	ctx := requestContext(r)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":         reason.Text(ctx),
		"success":       false,
		"code":          http.StatusUnprocessableEntity,
		"reason":        reason.Code(),
		"fields":        map[string][]string{field: {why.Text(ctx)}},
		"field_reasons": map[string][]string{field: {why.Reason.Code()}},
	})
}
