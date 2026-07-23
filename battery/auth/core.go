package auth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// CorePlugin is the always-loaded auth plugin providing email/password
// authentication, session management, and JWT token support. It wraps
// the original battery/auth functionality into the plugin architecture.
//
// All existing auth handlers (login, logout, me) are here, reworked to
// use AuthManager's shared stores instead of receiving them as parameters.
type CorePlugin struct {
	mgr               *AuthManager
	loginLimit        *RateLimiter
	loginLimitAccount *RateLimiter
	registerLimit     *RateLimiter
}

// NewCorePlugin creates the core auth plugin.
func NewCorePlugin() *CorePlugin {
	return &CorePlugin{}
}

// Name returns the plugin name.
func (c *CorePlugin) Name() string { return "core" }

// Init stores a reference to the AuthManager and constructs the optional
// login rate limiters from manager config.
func (c *CorePlugin) Init(mgr *AuthManager) error {
	c.mgr = mgr
	cfg := mgr.Config()
	if cfg.LoginRateLimit != nil {
		// DevMode relaxes the per-IP login limiter so local
		// screenshot / verification tooling that hammers /auth/login
		// from localhost is not locked out (issue #71). The per-IP
		// flood throttle is the one that bites tooling; the per-account
		// limiter below is deliberately NOT relaxed — it guards brute-
		// force even in dev (pinned by TestAuthBypass_BruteForceNoLockout).
		ipCfg := *cfg.LoginRateLimit
		ipCfg.DevMode = cfg.DevMode
		c.loginLimit = newScopedRateLimiter(ipCfg, "login_ip")
	}
	if cfg.LoginRateLimitPerAccount != nil {
		c.loginLimitAccount = newScopedRateLimiter(*cfg.LoginRateLimitPerAccount, "login_account")
	}
	if cfg.RegisterRateLimit != nil {
		c.registerLimit = newScopedRateLimiter(*cfg.RegisterRateLimit, "register")
	}
	return nil
}

// RegisterRoutes mounts the core auth routes: login, logout, me, register.
func (c *CorePlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/login", c.loginHandler())
	r.Post(basePath+"/logout", c.logoutHandler())
	r.Get(basePath+"/me", c.meHandler())
	r.Post(basePath+"/register", c.registerHandler())
}

// rejectCrossSiteForm refuses a browser cross-site FORM submission to an
// auth mutation endpoint and reports whether it wrote a response. Login
// CSRF needs no pre-existing cookie — an attacker's page can silently log
// the victim into an attacker-controlled account — so SameSite session
// cookies don't cover it. JSON bodies are exempt: a cross-site JSON POST
// needs a CORS preflight, which the framework never answers for these
// routes. Non-browser clients (curl, tests, native apps) send neither
// header and pass.
//
// Sec-Fetch-Site is the authoritative signal and is checked FIRST: every
// modern browser sends it, and a genuine cross-site attack POST carries
// "cross-site" regardless of the Origin value. The Origin fallback exists
// only for older clients that omit Fetch Metadata; there, a "null" Origin
// is NOT treated as an attack, because a legitimate top-level same-origin
// form navigation sends Origin: null (opaque origin) too — using null as
// the reject trigger would break normal browser logins.
func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !isFormRequest(r) {
		return false
	}
	// Primary: Fetch Metadata. Same-origin / none are safe; a cross-site
	// form POST (the CSRF shape) is refused outright. "same-site" is NOT
	// sufficient — a form on a sibling subdomain (evil.example.com →
	// app.example.com) is same-site yet still carries the SameSite
	// cookie, so it falls through to the Origin-host comparison below.
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" {
		switch sfs {
		case "cross-site":
			writeFormAuthError(w, r, http.StatusForbidden, "cross_site_request")
			return true
		case "same-origin", "none":
			return false
		}
	}
	// Fallback for clients without Fetch Metadata: compare Origin host to
	// the request host. Absent or opaque ("null") Origin can't prove an
	// attack — allow, matching a same-origin top-level form navigation.
	if o := r.Header.Get("Origin"); o != "" && o != "null" {
		if u, err := url.Parse(o); err == nil && u.Host != "" && !strings.EqualFold(u.Host, r.Host) {
			writeFormAuthError(w, r, http.StatusForbidden, "cross_site_request")
			return true
		}
	}
	return false
}

// guardAuthLimit applies a per-IP limiter with the response shape matched
// to the request: browser form posts get the 303 error redirect (like every
// other form-path auth error), JSON clients the raw 429 body. A nil limiter
// allows everything.
func guardAuthLimit(rl *RateLimiter, w http.ResponseWriter, r *http.Request) bool {
	if rl == nil {
		return true
	}
	allowed, retry := rl.AllowContext(r.Context(), rl.clientIP(r))
	if allowed {
		return true
	}
	w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retry.Seconds()))
	if isFormRequest(r) {
		writeFormAuthError(w, r, http.StatusTooManyRequests, "rate_limit")
	} else {
		writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
	}
	return false
}

// loginHandler handles POST /auth/login. Accepts either:
//   - application/json: {"email":"…","password":"…"} — returns JSON
//     {"user":{…},"token":"…"} with 200.
//   - application/x-www-form-urlencoded / multipart/form-data: same
//     fields, returns 303 See Other to the post-login destination
//     (?next= override or "/" fallback) with the session cookie set.
//
// The runtime's form interceptor honours the 303 Location header so
// browser flows navigate after login.
func (c *CorePlugin) loginHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cross-site rejection runs BEFORE the limiter: a 403'd request
		// must not count against the victim's per-IP budget, or a
		// malicious page could lock a visitor out of their own login by
		// firing hidden cross-site posts.
		if rejectCrossSiteForm(w, r) {
			return
		}
		if !guardAuthLimit(c.loginLimit, w, r) {
			return
		}
		email, password, isForm, ok := decodeAuthCredentials(w, r)
		if !ok {
			return
		}
		if email == "" || password == "" {
			if isForm {
				writeFormAuthError(w, r, http.StatusBadRequest, "credentials_required")
			} else {
				writeAuthError(w, http.StatusBadRequest, "email and password required")
			}
			return
		}

		// Per-account limit, keyed on the lower-cased email. Independent
		// of the per-IP limit so an attacker pivoting IPs cannot bypass.
		// Apply BEFORE the user-store lookup so an attacker can't probe
		// account existence by measuring per-account 429s either —
		// every non-empty email gets the same treatment.
		if c.loginLimitAccount != nil {
			key := "account:" + strings.ToLower(strings.TrimSpace(email))
			allowed, retry := c.loginLimitAccount.AllowContext(r.Context(), key)
			if !allowed {
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", retry.Seconds()))
				if isForm {
					writeFormAuthError(w, r, http.StatusTooManyRequests, "rate_limit")
				} else {
					writeAuthError(w, http.StatusTooManyRequests, "rate limit exceeded")
				}
				return
			}
		}

		store := c.mgr.UserStore()
		if store == nil {
			writeAuthError(w, http.StatusInternalServerError, "user store not configured")
			return
		}

		user, hash, err := store.FindByEmail(r.Context(), email)
		if err != nil {
			// Run a dummy bcrypt against the package-level dummy hash so
			// the response time matches the existing-user path. Skipping
			// bcrypt here leaks user existence via timing.
			_ = CheckPassword(password, dummyBcryptHash)
			// Unknown user OR a transport error — record a failed login.
			// UserID stays empty (anti-enumeration: the event never
			// distinguishes "no such user" from "wrong password").
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "login.failed",
				Email:  email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"reason": "bad_credentials"},
			})
			if isForm {
				writeFormAuthError(w, r, http.StatusUnauthorized, "invalid_credentials")
			} else {
				writeAuthError(w, http.StatusUnauthorized, "invalid credentials")
			}
			return
		}
		if !CheckPassword(password, hash) {
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "login.failed",
				UserID: user.GetID(),
				Email:  email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"reason": "bad_credentials"},
			})
			if isForm {
				writeFormAuthError(w, r, http.StatusUnauthorized, "invalid_credentials")
			} else {
				writeAuthError(w, http.StatusUnauthorized, "invalid credentials")
			}
			return
		}

		// Create session
		sess, err := c.mgr.SessionStore().Create(r.Context(), user.GetID(), c.mgr.Config().SessionTTL)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "session create failed")
			return
		}

		// If any plugin gates logins on a second factor and the user has
		// it enabled, mark this session as pending. Until /2fa/challenge
		// succeeds, only that endpoint accepts the cookie — meHandler
		// and any RequireAuth-protected route will refuse it. If the
		// pending mark can't be established, destroy the session and
		// reject the login rather than issue a password-only session.
		pendingTwoFA, err := c.markPendingIfTwoFactorEnabled(r, sess.Token, user.GetID())
		if err != nil {
			_ = c.mgr.SessionStore().Delete(r.Context(), sess.Token)
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "login.failed",
				UserID: user.GetID(),
				Email:  email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"reason": "twofa_failclosed"},
			})
			if isForm {
				writeFormAuthError(w, r, http.StatusInternalServerError, "two_factor_unavailable")
			} else {
				writeAuthError(w, http.StatusInternalServerError, "two-factor enforcement unavailable")
			}
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
		http.SetCookie(w, &http.Cookie{
			Name:     cfg.SessionCookie,
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   cfg.SessionSecure,
			SameSite: http.SameSiteStrictMode,
			Expires:  sess.ExpiresAt,
		})

		if isForm {
			http.Redirect(w, r, successRedirect(r, "/"), http.StatusSeeOther)
			return
		}

		resp := map[string]any{
			"user": map[string]any{
				"id":    user.GetID(),
				"email": user.GetEmail(),
				"roles": user.GetRoles(),
			},
		}

		// Also return a JWT if configured — but never for a pending-2FA
		// login. The JWT is stateless: handing it out here would let a
		// password-only caller skip the second factor entirely on any
		// JWT-authenticated route.
		if c.mgr.JWT() != nil && !pendingTwoFA && !cfg.CookieOnly {
			token, err := c.mgr.JWT().GenerateToken(user)
			if err == nil {
				resp["token"] = token
			}
		}
		if pendingTwoFA {
			resp["two_factor_required"] = true
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// logoutHandler handles POST /auth/logout. For form requests, redirects
// to ?next= (or "/") with the session cookie cleared. JSON requests get
// 204 No Content.
func (c *CorePlugin) logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Forced logout is the nuisance sibling of login CSRF; the same
		// origin check closes it for free.
		if rejectCrossSiteForm(w, r) {
			return
		}
		cfg := c.mgr.Config()
		// Revoke EVERY session-name cookie the client sent, not just the
		// first — a jar can hold duplicates at different scopes, and logout
		// must not leave a shadowed-but-valid session alive.
		for _, token := range sessionCookieCandidates(r, cfg.SessionCookie) {
			// Capture the principal before deleting so the audit row
			// names who logged out. A Get failure (expired/invalid
			// cookie) yields no event — nothing to revoke of record.
			if sess, gerr := c.mgr.SessionStore().Get(r.Context(), token); gerr == nil {
				c.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:   "session.revoked",
					UserID: sess.UserID,
					Remote: remoteHost(r),
					Meta:   map[string]string{"reason": "logout"},
				})
			}
			_ = c.mgr.SessionStore().Delete(r.Context(), token)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     cfg.SessionCookie,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   cfg.SessionSecure,
			SameSite: http.SameSiteStrictMode,
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
		})
		if isFormRequest(r) {
			http.Redirect(w, r, successRedirect(r, "/"), http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// markPendingIfTwoFactorEnabled queries any registered TwoFactorChecker
// plugins and, if any reports the user has 2FA enabled, marks the new
// session as pending — a default-deny posture so missing the
// /2fa/challenge call doesn't leave the session fully privileged.
//
// Fail-closed contract: if 2FA state can't be determined (checker error)
// or the pending mark can't be established (store doesn't implement
// SessionPendingMarker, or the mark call fails), it returns an error and
// the caller must reject the login. Anything else silently downgrades
// 2FA-enrolled accounts to password-only auth.
func (c *CorePlugin) markPendingIfTwoFactorEnabled(r *http.Request, sessionToken, userID string) (pending bool, err error) {
	for _, name := range c.mgr.order {
		checker, ok := c.mgr.plugins[name].(TwoFactorChecker)
		if !ok {
			continue
		}
		enabled, err := checker.HasTwoFactorEnabled(r.Context(), userID)
		if err != nil {
			slog.Default().Warn("auth: two-factor state lookup failed; rejecting login (fail-closed)",
				"plugin", name, "error", err)
			return false, fmt.Errorf("two-factor state lookup (%s): %w", name, err)
		}
		if !enabled {
			continue
		}
		marker, ok := c.mgr.SessionStore().(SessionPendingMarker)
		if !ok {
			slog.Default().Warn("auth: user has two-factor enabled but the session store does not implement SessionPendingMarker; rejecting login (fail-closed)",
				"plugin", name, "store", fmt.Sprintf("%T", c.mgr.SessionStore()))
			return false, fmt.Errorf("session store %T cannot mark a session pending two-factor", c.mgr.SessionStore())
		}
		if err := marker.MarkPendingTwoFactor(r.Context(), sessionToken); err != nil {
			slog.Default().Warn("auth: marking session pending two-factor failed; rejecting login (fail-closed)",
				"plugin", name, "error", err)
			return false, fmt.Errorf("mark pending two-factor: %w", err)
		}
		return true, nil
	}
	return false, nil
}

// meHandler handles GET /auth/me — returns the current user.
func (c *CorePlugin) meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := c.mgr.Config()
		// Try every session-name cookie the client sent (duplicates at
		// different scopes shadow each other; the first valid one wins).
		candidates := sessionCookieCandidates(r, cfg.SessionCookie)
		if len(candidates) == 0 {
			writeAuthError(w, http.StatusUnauthorized, "no session")
			return
		}
		var sess *Session
		pending := false
		for _, token := range candidates {
			s, err := c.mgr.SessionStore().Get(r.Context(), token)
			if err != nil || s == nil {
				continue
			}
			// Pending-2FA sessions are usable ONLY for /auth/2fa/challenge.
			// Anything else — meHandler included — refuses them; remember we
			// saw one so the error names the real state.
			if s.PendingTwoFactor {
				pending = true
				continue
			}
			sess = s
			break
		}
		if sess == nil {
			if pending {
				writeAuthError(w, http.StatusForbidden, "two-factor verification required")
				return
			}
			writeAuthError(w, http.StatusUnauthorized, "invalid session")
			return
		}

		// Try to look up the user for richer response
		resp := map[string]any{
			"userId":    sess.UserID,
			"expiresAt": sess.ExpiresAt,
		}

		if c.mgr.UserStore() != nil {
			if user, err := c.mgr.UserStore().FindByID(r.Context(), sess.UserID); err == nil {
				resp["user"] = map[string]any{
					"id":    user.GetID(),
					"email": user.GetEmail(),
					"roles": user.GetRoles(),
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// registerHandler handles POST /auth/register — creates a new user.
// Accepts JSON or form-encoded bodies. Form requests get a 303 redirect
// to the post-register destination (?next= override or "/") with the
// session cookie set after auto-login.
func (c *CorePlugin) registerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cross-site rejection first — a 403'd request must not burn the
		// victim's per-IP budget (see loginHandler). Then the per-IP
		// throttle: unthrottled registration is account-table flooding +
		// email bombing once verification mail is wired.
		if rejectCrossSiteForm(w, r) {
			return
		}
		if !guardAuthLimit(c.registerLimit, w, r) {
			return
		}
		email, password, isForm, ok := decodeAuthCredentials(w, r)
		if !ok {
			return
		}
		if email == "" || password == "" {
			if isForm {
				writeFormAuthError(w, r, http.StatusBadRequest, "credentials_required")
			} else {
				writeAuthError(w, http.StatusBadRequest, "email and password required")
			}
			return
		}

		store := c.mgr.UserStore()
		if store == nil {
			writeAuthError(w, http.StatusInternalServerError, "user store not configured")
			return
		}

		// SECURITY: roles are server-assigned, never client-controlled.
		// /auth/register is anonymous — see decodeAuthCredentials for
		// the rationale. Role elevation is a separate admin-gated flow.
		// The values come from AuthConfig.DefaultRoles (operator
		// configuration, fallback ["user"]); any client-supplied roles
		// key on the request body is ignored.
		roles := c.mgr.DefaultRoles()

		if err := ValidatePasswordStrength(password); err != nil {
			if isForm {
				writeFormAuthError(w, r, http.StatusBadRequest, "weak_password")
			} else {
				writeAuthError(w, http.StatusBadRequest, "password must be at least 8 characters")
			}
			return
		}

		hash, err := HashPassword(password)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "password hashing failed")
			return
		}

		user, err := store.CreateUser(r.Context(), email, hash, roles)
		if err != nil {
			if isForm {
				writeFormAuthError(w, r, http.StatusConflict, "email_taken")
			} else {
				writeAuthError(w, http.StatusConflict, "email already registered")
			}
			return
		}

		// Registration succeeded — record before the form/JSON branch so
		// both paths produce the event.
		c.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "register.succeeded",
			UserID: user.GetID(),
			Email:  email,
			Remote: remoteHost(r),
		})

		// Form path: auto-login + cookie + 303 redirect.
		if isForm {
			sess, err := c.mgr.SessionStore().Create(r.Context(), user.GetID(), c.mgr.Config().SessionTTL)
			if err != nil {
				writeAuthError(w, http.StatusInternalServerError, "session create failed")
				return
			}
			cfg := c.mgr.Config()
			http.SetCookie(w, &http.Cookie{
				Name:     cfg.SessionCookie,
				Value:    sess.Token,
				Path:     "/",
				HttpOnly: true,
				Secure:   cfg.SessionSecure,
				SameSite: http.SameSiteStrictMode,
				Expires:  sess.ExpiresAt,
			})
			http.Redirect(w, r, successRedirect(r, "/"), http.StatusSeeOther)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"user": map[string]any{
				"id":    user.GetID(),
				"email": user.GetEmail(),
				"roles": user.GetRoles(),
			},
		})
	}
}

// writeAuthError is the shared error helper: it emits the canonical flat
// envelope {"error","success","code"} with Content-Type application/json —
// the shape framework/crud/crud.go's writeJSONError uses and the generated
// SDKs and sdkdocs document. battery/auth keeps a local copy because
// batteries may not import framework/crud.
func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   msg,
		"success": false,
		"code":    status,
	})
}
