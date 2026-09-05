package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
		// limiter below is deliberately NOT relaxed, it guards brute-
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

// rejectCrossSiteForm refuses a browser cross-site submission to an auth
// mutation endpoint and reports whether it wrote a response. Login CSRF
// needs no pre-existing cookie, an attacker's page can silently log the
// victim into an attacker-controlled account, so SameSite session
// cookies don't cover it. JSON bodies are exempt: a cross-site JSON POST
// needs a CORS preflight, which the framework never answers for these
// routes. Non-browser clients (curl, tests, native apps) send neither
// header and pass.
//
// The gate is isForgeableRequest, NOT isFormRequest. It used to be the
// latter, which recognised only urlencoded and multipart, so a form with
// enctype="text/plain" (a CORS-simple type, no preflight) skipped the
// check entirely, and so did a bodyless fetch() that sends no
// Content-Type. On magic-link verify, whose credential comes from the URL
// rather than the body, that was a complete confirmation-step bypass:
// the attacker's own token, auto-submitted from their page, minted a
// session in the victim's browser.
//
// Sec-Fetch-Site is the authoritative signal and is checked FIRST: every
// modern browser sends it, and a genuine cross-site attack POST carries
// "cross-site" regardless of the Origin value. The Origin fallback exists
// only for older clients that omit Fetch Metadata; there, a "null" Origin
// is NOT treated as an attack, because a legitimate top-level same-origin
// form navigation sends Origin: null (opaque origin) too, using null as
// the reject trigger would break normal browser logins.
func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !isForgeableRequest(r) {
		return false
	}
	// Primary: Fetch Metadata. Same-origin / none are safe; a cross-site
	// form POST (the CSRF shape) is refused outright. "same-site" is NOT
	// sufficient, a form on a sibling subdomain (evil.example.com →
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
	// attack, allow, matching a same-origin top-level form navigation.
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
//   - application/json: {"email":"…","password":"…"}, returns JSON
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
		email, password, isForm, ok := decodeAuthCredentials(w, r, c.mgr.canonicalizeEmail)
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

		// Per-account limit, keyed on the canonical email (decode already
		// canonicalized and composed-checked it; the literal reuse keeps
		// the limiter key and the lookup key the same string by
		// construction). Independent of the per-IP limit so an attacker
		// pivoting IPs cannot bypass. Apply BEFORE the user-store lookup
		// so an attacker can't probe account existence by measuring
		// per-account 429s either, every non-empty email gets the same
		// treatment.
		if c.loginLimitAccount != nil {
			key := "account:" + email
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
			// Verify against a dummy produced by the CONFIGURED hasher so
			// the not-found branch spends the same algorithm and cost a
			// real row does. Skipping the work leaks user existence by
			// timing; doing the WRONG algorithm's work leaks it just as
			// well, only more subtly.
			_ = CheckPassword(password, dummyHashFor(DefaultHasher))
			// Unknown user OR a transport error, record a failed login.
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

		// Mint the session through the manager so the second-factor
		// pending mark is applied by construction. Until /2fa/challenge
		// succeeds, only that endpoint accepts the cookie, meHandler
		// and any RequireAuth-protected route will refuse it. If the
		// pending mark can't be established, MintSession destroys the
		// session and we reject the login rather than issue a
		// password-only one.
		sess, pendingTwoFA, err := c.mgr.MintSession(r.Context(), user.GetID(), c.mgr.Config().SessionTTL)
		if err != nil && !errors.Is(err, ErrTwoFactorEnforcement) {
			writeAuthError(w, http.StatusInternalServerError, "session create failed")
			return
		}
		if err != nil {
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

		// Also return a JWT if configured, but never for a pending-2FA
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

		writeCredentialHeaders(w)
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
		// Revoke EVERY session-name cookie the client sent, not only the
		// first. A jar can hold duplicates at different scopes, and logout
		// must not leave a shadowed-but-valid session alive.
		// A failed Delete leaves a LIVE session while the response says
		// the user is signed out — the one outcome logout must never
		// produce, and worst on the shared machine the user is logging
		// out of. Clearing the cookie only hides the still-valid token
		// from this browser; anyone holding it keeps the session.
		//
		// The audit row is emitted only after the delete it describes
		// succeeds. Recording session.revoked for a session still in the
		// store makes the ledger say a revocation happened that did not.
		revokeFailed := false
		for _, token := range sessionCookieCandidates(r, cfg.SessionCookie) {
			// Capture the principal before deleting so the audit row
			// names who logged out. A Get failure (expired/invalid
			// cookie) yields no event, nothing to revoke of record.
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
			writeAuthError(w, http.StatusInternalServerError, "could not sign out; the session is still active")
			return
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
			http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// meHandler handles GET /auth/me, returns the current user.
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
			// Anything else, meHandler included, refuses them; remember we
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

		writeCredentialHeaders(w)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// registerHandler handles POST /auth/register, creates a new user.
// Accepts JSON or form-encoded bodies.
//
// ANTI-ENUMERATION (2026-09-04 red-probe round 3): the known-address
// and unknown-address answers are indistinguishable — 202
// {"accepted":true} on the JSON surface, a 303 to the post-register
// destination on the form surface, and no session cookie on either
// branch. Register used to answer 409 "email already registered" for a
// taken address, the one unauthenticated account-existence oracle left
// in the battery (forgot-password, login, and magic-link send all pin
// the opposite policy, and forgotHandler documents the uniform-response
// + equal-work contract this handler now mirrors). A known address
// creates nothing; instead the EXISTING holder is notified via
// AuthConfig.RegisterEmailSender, off the timed path. The password hash
// is computed BEFORE the branch, so both sides burn the same bcrypt
// work — the register analogue of forgot-password's
// burnUnknownBranchWork (CWE-208).
//
// BREAKING (round 3 decision): registration no longer auto-logs-in.
// The form path no longer sets the session cookie (a cookie minted on
// only one branch re-opens the oracle) and the JSON path returns 202
// without the created user object. Clients follow up with /auth/login.
func (c *CorePlugin) registerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cross-site rejection first, a 403'd request must not burn the
		// victim's per-IP budget (see loginHandler). Then the per-IP
		// throttle: unthrottled registration is account-table flooding +
		// email bombing once the duplicate-notice sender is wired.
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
		// /auth/register is anonymous. See decodeAuthCredentials for
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

		// Hash BEFORE the taken/unknown branch: bcrypt is the dominant
		// per-request cost, so neither branch is cheap to clock.
		hash, err := HashPassword(password)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "password hashing failed")
			return
		}

		user, err := store.CreateUser(r.Context(), email, hash, roles)
		taken := errors.Is(err, ErrEmailTaken)
		if err != nil && !taken {
			// A store/transport failure is not the collision sentinel.
			// The old handler answered 409 "email already registered"
			// here, reporting an account that may not exist; a real
			// failure stays a 500 and stays visible.
			writeAuthError(w, http.StatusInternalServerError, "registration failed")
			return
		}

		if taken {
			// Known address: create nothing, tell the holder, and
			// leave the operator a trail. The audit event carries the
			// HOLDER's id (never echoed to the caller); the notice is
			// delivered off the timed path.
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
			// Registration succeeded, record before the form/JSON
			// branch so both paths produce the event.
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "register.succeeded",
				UserID: user.GetID(),
				Email:  email,
				Remote: remoteHost(r),
			})
		}

		// ONE uniform answer for both branches.
		if isForm {
			// No session cookie: minting one here would re-open the
			// oracle the uniform response just closed (and on the
			// taken branch there is no user to mint for). The caller
			// lands on the destination logged-out and signs in.
			http.Redirect(w, r, successRedirect(w, r, "/"), http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
	}
}

// duplicateRegisterNoticeBody is mailed to the existing holder when an
// anonymous caller registers with their address. It deliberately
// carries no URL and no token: the recipient needs no credential, and
// the notice must not be usable as a phish vector.
const duplicateRegisterNoticeBody = "Someone tried to create an account with this email address. " +
	"An account already exists. If this was you, sign in or reset your password; " +
	"if not, you can ignore this message."

// deliverRegisterDuplicateNotice mails the existing account holder off
// the timed path: the goroutine is started once the uniform response is
// already decided, so the client cannot clock the known/unknown
// branches apart — the same posture forgotHandler documents for its
// delivery. context.WithoutCancel keeps the send alive after the
// request context is torn down. The sender is a host-supplied
// interface called from our goroutine, so the call runs under a
// recover guard: a panicking Send becomes a WARN, not a process kill
// (the recovercallback contract).
func (c *CorePlugin) deliverRegisterDuplicateNotice(r *http.Request, holderEmail string) {
	if holderEmail == "" {
		return
	}
	sender := c.mgr.Config().RegisterEmailSender
	if sender == nil {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	go func() {
		defer func() {
			if p := recover(); p != nil {
				slog.Warn("register duplicate-notice sender panicked",
					"plugin", "core", "email_hash", hashedIdentifier(holderEmail), "panic", fmt.Sprint(p))
			}
		}()
		if err := sender.Send(ctx, holderEmail, duplicateRegisterNoticeBody); err != nil {
			slog.Warn("register duplicate-notice send failed",
				"plugin", "core", "email_hash", hashedIdentifier(holderEmail), "err", err)
		}
	}()
}

// writeAuthError is the shared error helper: it emits the canonical flat
// envelope {"error","success","code"} with Content-Type application/json,
// the shape framework/crud/crud.go's writeJSONError uses and the generated
// SDKs and sdkdocs document. battery/auth keeps a local copy because
// batteries may not import framework/crud.
func writeAuthError(w http.ResponseWriter, status int, msg string) {
	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   msg,
		"success": false,
		"code":    status,
	})
}
