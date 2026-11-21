package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

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
				w.Header().Set("Retry-After", ratelimit.RetryAfterSeconds(retry))
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
		if err := c.mgr.revokeUserSessions(ctx, sess.UserID, "password_change", remoteHost(r)); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonChangeIncomplete)
			return
		}
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
		writeAuthJSON(w, r, http.StatusOK, map[string]any{"updated": true}, skipCredentialHeaders)
	}
}

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

var errPasswordMoved = errors.New("auth: the password changed during the request")

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

func (m *AuthManager) dropMagicLinks(ctx context.Context, userID, reason, remote string) error {
	type mailed struct {
		store   MagicLinkTokenStore
		purpose tokenPurpose
		kind    string
	}
	var flows []mailed
	if p, ok := m.Plugin("magic-link"); ok {
		if mp, ok := p.(*MagicLinkPlugin); ok {
			flows = append(flows, mailed{mp.tokenStore, purposeMagicLink, "magiclink_tokens.purged"})
		}
	}
	if p, ok := m.Plugin("email-code"); ok {
		if ep, ok := p.(*EmailCodePlugin); ok {
			flows = append(flows, mailed{ep.tokenStore, purposeEmailCode, "emailcode_tokens.purged"})
		}
	}
	if len(flows) == 0 {
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
	var firstErr error
	for _, f := range flows {
		n, supported, err := purgePurposeTokens(ctx, f.store, f.purpose, email)
		switch {
		case err != nil:
			if firstErr == nil {
				firstErr = err
			}
		case !supported:
			slog.Warn("could not drop outstanding mailed sign-ins: token store does not implement MagicLinkTokenPurger",
				"flow", string(f.purpose), "reason", reason, "user_hash", hashedIdentifier(userID))
		case n > 0:
			m.emitSecurity(ctx, SecurityEvent{
				Kind: f.kind, UserID: userID, Remote: remote,
				Meta: map[string]string{"reason": reason, "count": strconv.Itoa(n)},
			})
		}
	}
	return firstErr
}

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
