package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
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
		c.loginLimit = NewRateLimiter(*cfg.LoginRateLimit)
	}
	if cfg.LoginRateLimitPerAccount != nil {
		c.loginLimitAccount = NewRateLimiter(*cfg.LoginRateLimitPerAccount)
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
		if c.loginLimit != nil && !c.loginLimit.guard(w, r) {
			return
		}
		email, password, _, isForm, ok := decodeAuthCredentials(w, r)
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
			allowed, retry := c.loginLimitAccount.Allow(key)
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
			if isForm {
				writeFormAuthError(w, r, http.StatusUnauthorized, "invalid_credentials")
			} else {
				writeAuthError(w, http.StatusUnauthorized, "invalid credentials")
			}
			return
		}
		if !CheckPassword(password, hash) {
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
		// and any RequireAuth-protected route will refuse it.
		c.markPendingIfTwoFactorEnabled(r, sess.Token, user.GetID())

		cfg := c.mgr.Config()
		http.SetCookie(w, &http.Cookie{
			Name:     cfg.SessionCookie,
			Value:    sess.Token,
			Path:     "/",
			HttpOnly: true,
			Secure:   cfg.SessionSecure,
			SameSite: http.SameSiteLaxMode,
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

		// Also return a JWT if configured
		if c.mgr.JWT() != nil {
			token, err := c.mgr.JWT().GenerateToken(user)
			if err == nil {
				resp["token"] = token
			}
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
		cfg := c.mgr.Config()
		if cookie, err := r.Cookie(cfg.SessionCookie); err == nil {
			_ = c.mgr.SessionStore().Delete(r.Context(), cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     cfg.SessionCookie,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   cfg.SessionSecure,
			SameSite: http.SameSiteLaxMode,
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
func (c *CorePlugin) markPendingIfTwoFactorEnabled(r *http.Request, sessionToken, userID string) {
	for _, name := range c.mgr.order {
		checker, ok := c.mgr.plugins[name].(TwoFactorChecker)
		if !ok {
			continue
		}
		enabled, err := checker.HasTwoFactorEnabled(r.Context(), userID)
		if err != nil || !enabled {
			continue
		}
		if marker, ok := c.mgr.SessionStore().(SessionPendingMarker); ok {
			_ = marker.MarkPendingTwoFactor(r.Context(), sessionToken)
		}
		return
	}
}

// meHandler handles GET /auth/me — returns the current user.
func (c *CorePlugin) meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := c.mgr.Config()
		cookie, err := r.Cookie(cfg.SessionCookie)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "no session")
			return
		}
		sess, err := c.mgr.SessionStore().Get(r.Context(), cookie.Value)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "invalid session")
			return
		}
		// Pending-2FA sessions are usable ONLY for /auth/2fa/challenge.
		// Anything else — meHandler included — refuses them.
		if sess.PendingTwoFactor {
			writeAuthError(w, http.StatusForbidden, "two-factor verification required")
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
		email, password, roles, isForm, ok := decodeAuthCredentials(w, r)
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

		if len(roles) == 0 {
			roles = []string{"user"}
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
				SameSite: http.SameSiteLaxMode,
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

// writeAuthError is the shared error response helper (kept from original).
func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   msg,
		"success": false,
	})
}
