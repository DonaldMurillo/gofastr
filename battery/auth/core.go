package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// CorePlugin serves password login, logout, me, register and password change.
type CorePlugin struct {
	mgr               *AuthManager
	loginLimit        *RateLimiter
	loginLimitAccount *RateLimiter
	registerLimit     *RateLimiter
	notices           *mailQueue
	twoFactorURL      string
	clearSiteData     string
}

var _ AuthPluginOnStop = (*CorePlugin)(nil)

const loginAccountScope = "login_account"

// NewCorePlugin creates the core auth plugin.
func NewCorePlugin() *CorePlugin {
	return &CorePlugin{notices: newMailQueue("core"), clearSiteData: defaultLogoutClearSiteData}
}

const defaultLogoutClearSiteData = `"cache", "storage"`

var clearSiteDataDirectives = map[string]bool{
	"cache": true, "cookies": true, "storage": true, "executionContexts": true,
	"clientHints": true, "prefetchCache": true, "prerenderCache": true, "*": true,
}

// WithLogoutClearSiteData sets the Clear-Site-Data types logout sends (none sends no header); it panics on an unknown type.
func (c *CorePlugin) WithLogoutClearSiteData(directives ...string) *CorePlugin {
	quoted := make([]string, 0, len(directives))
	for _, d := range directives {
		if !clearSiteDataDirectives[d] {
			panic("auth: WithLogoutClearSiteData: unknown Clear-Site-Data type " + strconv.Quote(d))
		}
		quoted = append(quoted, `"`+d+`"`)
	}
	c.clearSiteData = strings.Join(quoted, ", ")
	return c
}

// WithTwoFactorURL sets the same-origin page a form login of a 2FA account goes to; Init requires it next to a 2FA plugin.
func (c *CorePlugin) WithTwoFactorURL(path string) *CorePlugin {
	c.twoFactorURL = path
	return c
}

// OnStop stops the duplicate-registration notices; ctx bounds the wait.
func (c *CorePlugin) OnStop(ctx context.Context) error {
	return c.notices.stop(ctx)
}

// Name returns the plugin name.
func (c *CorePlugin) Name() string { return "core" }

// Init builds the login and register limiters.
func (c *CorePlugin) Init(mgr *AuthManager) error {
	c.mgr = mgr
	if err := checkTwoFactorURL("core", "WithTwoFactorURL", c.twoFactorURL); err != nil {
		return err
	}
	if err := requireTwoFactorURL(mgr, "core", "NewCorePlugin().WithTwoFactorURL", c.twoFactorURL, "a form login", "its next page"); err != nil {
		return err
	}
	cfg := mgr.Config()
	if cfg.LoginRateLimit != nil {
		ipCfg := *cfg.LoginRateLimit
		ipCfg.DevMode = cfg.DevMode
		c.loginLimit = newScopedRateLimiter(ipCfg, "login_ip")
	}
	if cfg.LoginRateLimitPerAccount != nil {
		c.loginLimitAccount = newScopedRateLimiter(*cfg.LoginRateLimitPerAccount, loginAccountScope)
	}
	if cfg.RegisterRateLimit != nil {
		c.registerLimit = newScopedRateLimiter(*cfg.RegisterRateLimit, "register")
	}
	return nil
}

// RegisterRoutes mounts login, logout, me, register and password under basePath.
func (c *CorePlugin) RegisterRoutes(r *router.Router, basePath string) {
	r = r.Group("", browserAccountOnly)
	r.Post(basePath+"/login", c.loginHandler())
	r.Post(basePath+"/logout", c.logoutHandler())
	r.Get(basePath+"/me", c.meHandler())
	r.Post(basePath+"/register", c.registerHandler())
	r.Post(basePath+"/password", c.changePasswordHandler())
}

func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !crossSiteForm(r) {
		return false
	}
	writeFormAuthError(w, r, http.StatusForbidden, reasonCrossSiteRequest)
	return true
}

func guardAuthLimit(rl *RateLimiter, w http.ResponseWriter, r *http.Request) bool {
	if rl == nil {
		return true
	}
	allowed, retry := rl.AllowContext(r.Context(), rl.clientIP(r))
	if allowed {
		return true
	}
	w.Header().Set("Retry-After", ratelimit.RetryAfterSeconds(retry))
	if isFormRequest(r) {
		writeFormAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
	} else {
		writeAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
	}
	return false
}

func mintSessionCookie(w http.ResponseWriter, cfg AuthConfig, token string, expires time.Time) {
	c := &http.Cookie{
		Name:     cfg.SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.SessionSecure,
		SameSite: http.SameSiteStrictMode,
	}
	if token == "" {
		c.Expires = time.Unix(0, 0)
		c.MaxAge = -1
	} else {
		c.Expires = expires
	}
	http.SetCookie(w, c)
}

func (c *CorePlugin) loginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectCrossSiteForm(w, r) {
			return
		}
		if !guardAuthLimit(c.loginLimit, w, r) {
			return
		}
		email, password, isForm, ok := decodeAuthCredentials(w, r, c.mgr.canonicalizeEmail)
		if !ok {
			return
		}
		failLogin := func(userID, reason string, status int, why i18n.Phrase) {
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "login.failed",
				UserID: userID,
				Email:  email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"reason": reason},
			})
			if isForm {
				writeLoginFormError(w, r, status, why)
			} else {
				writeAuthError(w, r, status, why)
			}
		}
		if email == "" || password == "" {
			if isForm {
				writeLoginFormError(w, r, http.StatusBadRequest, reasonCredentialsRequired)
			} else {
				writeAuthError(w, r, http.StatusBadRequest, reasonCredentialsRequired)
			}
			return
		}

		if c.loginLimitAccount != nil {
			key := "account:" + email
			allowed, retry := c.loginLimitAccount.AllowContext(r.Context(), key)
			if !allowed {
				w.Header().Set("Retry-After", ratelimit.RetryAfterSeconds(retry))
				if isForm {
					writeLoginFormError(w, r, http.StatusTooManyRequests, reasonRateLimit)
				} else {
					writeAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
				}
				return
			}
		}

		store := c.mgr.UserStore()
		if store == nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUserStoreNotConfigured)
			return
		}

		user, hash, err := store.FindByEmail(r.Context(), email)
		if err != nil {
			_ = verifyPassword(password, dummyHashFor(DefaultHasher))
			failLogin("", "bad_credentials", http.StatusUnauthorized, reasonInvalidCredentials)
			return
		}
		if !verifyPassword(password, hash) {
			failLogin(user.GetID(), "bad_credentials", http.StatusUnauthorized, reasonInvalidCredentials)
			return
		}

		sess, pendingTwoFA, err := c.mgr.MintSession(r.Context(), user.GetID(), c.mgr.Config().SessionTTL)
		if err != nil && !errors.Is(err, ErrTwoFactorEnforcement) {
			writeAuthError(w, r, http.StatusInternalServerError, reasonSessionCreateFailed)
			return
		}
		if err != nil {
			failLogin(user.GetID(), "twofa_failclosed", http.StatusInternalServerError, reasonTwoFactorUnavailable)
			return
		}
		if !credentialUnchanged(r.Context(), store, email, user.GetID(), hash) {
			if err := c.mgr.SessionStore().Delete(r.Context(), sess.Token); err != nil {
				slog.Warn("auth: dropping a session minted under a stale password failed",
					"user_hash", hashedIdentifier(user.GetID()), "err", err)
			}
			failLogin(user.GetID(), "credential_changed", http.StatusUnauthorized, reasonInvalidCredentials)
			return
		}
		if pendingTwoFA {
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "login.pending_2fa",
				UserID: user.GetID(),
				Email:  email,
				Remote: remoteHost(r),
			})
		} else {
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "login.succeeded",
				UserID: user.GetID(),
				Email:  email,
				Remote: remoteHost(r),
			})
		}
		cfg := c.mgr.Config()
		mintSessionCookie(w, cfg, sess.Token, sess.ExpiresAt)

		if isForm {
			if pendingTwoFA {
				http.Redirect(w, r, safeRedirectURL(c.twoFactorURL), http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
			return
		}

		resp := map[string]any{
			"user": map[string]any{
				"id":    user.GetID(),
				"email": user.GetEmail(),
				"roles": user.GetRoles(),
			},
		}

		if c.mgr.JWT() != nil && !pendingTwoFA && !cfg.CookieOnly {
			token, err := c.mgr.JWT().generateToken(user, hash)
			if err == nil {
				resp["token"] = token
			}
		}
		if pendingTwoFA {
			resp["two_factor_required"] = true
		}

		writeAuthJSON(w, r, http.StatusOK, resp)
	}
}

func credentialUnchanged(ctx context.Context, store UserStore, email, userID, hash string) bool {
	row, now, err := store.FindByEmail(ctx, email)
	if err != nil || row == nil || row.GetID() != userID {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(now), []byte(hash)) == 1
}

func (c *CorePlugin) logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rejectCrossSiteForm(w, r) {
			return
		}
		cfg := c.mgr.Config()
		revokeFailed := false
		for _, token := range sessionCookieCandidates(r, cfg.SessionCookie) {
			sess, gerr := c.mgr.SessionStore().Get(r.Context(), token)
			if err := c.mgr.SessionStore().Delete(r.Context(), token); err != nil {
				log.Printf("auth: logout: revoke session: %v", err)
				revokeFailed = true
				continue
			}
			if gerr == nil {
				c.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:   "session.revoked",
					UserID: sess.UserID,
					Remote: remoteHost(r),
					Meta:   map[string]string{"reason": "logout"},
				})
			}
		}
		if revokeFailed {
			writeAuthError(w, r, http.StatusInternalServerError, reasonSignOutFailed)
			return
		}
		mintSessionCookie(w, cfg, "", time.Time{})
		if c.clearSiteData != "" {
			w.Header().Set("Clear-Site-Data", c.clearSiteData)
		}
		if isFormRequest(r) {
			http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (c *CorePlugin) meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := c.mgr.Config()
		candidates := sessionCookieCandidates(r, cfg.SessionCookie)
		if len(candidates) == 0 {
			writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
			return
		}
		var sess *Session
		pending := false
		for _, token := range candidates {
			s, err := c.mgr.SessionStore().Get(r.Context(), token)
			if err != nil || s == nil {
				continue
			}
			if s.PendingTwoFactor {
				pending = true
				continue
			}
			sess = s
			break
		}
		if sess == nil {
			if pending {
				writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
				return
			}
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidSession)
			return
		}

		resp := map[string]any{
			"userId":    sess.UserID,
			"expiresAt": sess.ExpiresAt,
		}

		if c.mgr.UserStore() != nil {
			user, err := c.mgr.UserStore().FindByID(r.Context(), sess.UserID)
			if errors.Is(err, ErrUserNotFound) {
				writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidSession)
				return
			}
			if err == nil {
				resp["user"] = map[string]any{
					"id":    user.GetID(),
					"email": user.GetEmail(),
					"roles": user.GetRoles(),
				}
			}
		}

		writeAuthJSON(w, r, http.StatusOK, resp)
	}
}

func (c *CorePlugin) registerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r = asRegisterForm(r)
		if rejectCrossSiteForm(w, r) {
			return
		}
		if !guardAuthLimit(c.registerLimit, w, r) {
			return
		}
		email, password, isForm, ok := decodeAuthCredentials(w, r, c.mgr.canonicalizeEmail)
		if !ok {
			return
		}
		if email == "" || password == "" {
			if isForm {
				writeFormAuthError(w, r, http.StatusBadRequest, reasonCredentialsRequired)
			} else {
				writeAuthError(w, r, http.StatusBadRequest, reasonCredentialsRequired)
			}
			return
		}

		store := c.mgr.UserStore()
		if store == nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUserStoreNotConfigured)
			return
		}

		roles := c.mgr.DefaultRoles()

		if err := ValidatePasswordStrength(password); err != nil {
			if isForm {
				writeFormAuthError(w, r, http.StatusBadRequest, reasonPasswordTooShort)
			} else {
				writeAuthError(w, r, http.StatusBadRequest, reasonPasswordTooShort)
			}
			return
		}

		hash, err := HashPassword(password)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonPasswordHashingFailed)
			return
		}

		user, err := store.CreateUser(r.Context(), email, hash, roles)
		taken := errors.Is(err, ErrEmailTaken)
		if err != nil && !taken {
			writeAuthError(w, r, http.StatusInternalServerError, reasonRegistrationFailed)
			return
		}

		if taken {
			ev := SecurityEvent{
				Kind:   "register.duplicate",
				Email:  email,
				Remote: remoteHost(r),
			}
			if holder, _, ferr := store.FindByEmail(r.Context(), email); ferr == nil && holder != nil {
				ev.UserID = holder.GetID()
				c.deliverRegisterDuplicateNotice(r, holder.GetEmail())
			}
			c.mgr.emitSecurity(r.Context(), ev)
		} else {
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "register.succeeded",
				UserID: user.GetID(),
				Email:  email,
				Remote: remoteHost(r),
			})
		}

		if isForm {
			http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
			return
		}
		writeAuthJSON(w, r, http.StatusAccepted, map[string]any{"accepted": true}, skipCredentialHeaders)
	}
}

const duplicateRegisterNoticeBody = "Someone tried to create an account with this email address. " +
	"An account already exists. If this was you, sign in or reset your password; " +
	"if not, you can ignore this message."

func (c *CorePlugin) deliverRegisterDuplicateNotice(r *http.Request, holderEmail string) {
	if holderEmail == "" {
		return
	}
	sender := c.mgr.Config().RegisterEmailSender
	if sender == nil {
		return
	}
	c.notices.enqueue(r.Context(), holderEmail, scrubbedNoticeSend("core",
		"register duplicate-notice sender panicked", "register duplicate-notice send failed",
		holderEmail, sender, duplicateRegisterNoticeBody))
}

func writeAuthError(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase, params ...map[string]any) {
	writeAuthErrorFields(w, r, status, reason, nil, params...)
}

func refuseAuth(w http.ResponseWriter, r *http.Request, isForm bool, status int, reason i18n.Phrase) {
	if isForm {
		writeFormAuthError(w, r, status, reason)
		return
	}
	writeAuthError(w, r, status, reason)
}

func writeAuthErrorFields(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase, fields map[string]string, params ...map[string]any) {
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	body := map[string]any{
		"error":   reason.Text(requestContext(r), params...),
		"success": false,
		"code":    status,
		"reason":  reason.Code(),
	}
	if len(fields) > 0 {
		body["fields"] = fields
	}
	writeAuthJSON(w, r, status, body)
}

type authJSONOption uint8

const skipCredentialHeaders authJSONOption = 1

// writeAuthJSON marshals the body before sending the status and headers.
// An encoding error gets the standard 500 response.
func writeAuthJSON(w http.ResponseWriter, r *http.Request, status int, body any, options ...authJSONOption) {
	b, err := json.Marshal(body)
	if err != nil {
		slog.Warn("auth: encode response", "error", err)
		writeAuthError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	if !slices.Contains(options, skipCredentialHeaders) {
		writeCredentialHeaders(w)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
	w.Write([]byte("\n"))
}

func writeAuthErrorFor(w http.ResponseWriter, r *http.Request, status int, err error, frame i18n.Phrase) {
	if field, ok := errors.AsType[*tokenSpecFieldError](err); ok {
		reason, params := field.Phrased()
		writeAuthErrorFields(w, r, status, reason, map[string]string{field.field: reason.Text(requestContext(r), params)}, params)
		return
	}
	if p, ok := errors.AsType[i18n.Phrased](err); ok {
		reason, params := p.Phrased()
		writeAuthError(w, r, status, reason, params)
		return
	}
	writeAuthError(w, r, status, frame)
}

func requestContext(r *http.Request) context.Context {
	if r == nil {
		return context.Background()
	}
	return r.Context()
}

func invalidLinkBaseURL(base string) string {
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Sprintf("does not parse (%v)", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "needs an http or https scheme"
	}
	if u.Host == "" {
		return "has no host"
	}
	return ""
}
