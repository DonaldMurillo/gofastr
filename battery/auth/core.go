package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
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
	// notices sends the duplicate-registration notice off the timed
	// path, bounded and stopped with the manager (OnStop).
	notices *mailQueue
	// twoFactorURL is where an HTML form login of a 2FA account goes
	// (WithTwoFactorURL).
	twoFactorURL string
}

// CorePlugin drains its notice queue at shutdown.
var _ AuthPluginOnStop = (*CorePlugin)(nil)

// loginAccountScope is the per-account login limiter's Scope when the
// host sets none. The erasure matches its keys by it (erase.go).
const loginAccountScope = "login_account"

// NewCorePlugin creates the core auth plugin.
func NewCorePlugin() *CorePlugin {
	return &CorePlugin{notices: newMailQueue("core")}
}

// WithTwoFactorURL sets where an HTML form login goes when the account
// still owes its second factor: the app's page that posts the code to
// /2fa/challenge. Same-origin paths only. Init requires it when a 2FA
// plugin is registered, as the passkey, magic-link and OAuth plugins do:
// the session a form login mints for such an account is pending, and the
// login's `next` (or "/") would refuse it. A JSON login is unaffected; it
// answers two_factor_required.
func (c *CorePlugin) WithTwoFactorURL(path string) *CorePlugin {
	c.twoFactorURL = path
	return c
}

// OnStop cancels the duplicate-registration notices in flight and waits
// for the queue's workers; ctx bounds the wait.
func (c *CorePlugin) OnStop(ctx context.Context) error {
	return c.notices.stop(ctx)
}

// Name returns the plugin name.
func (c *CorePlugin) Name() string { return "core" }

// Init stores a reference to the AuthManager and constructs the optional
// login rate limiters from manager config.
func (c *CorePlugin) Init(mgr *AuthManager) error {
	c.mgr = mgr
	// Every plugin is registered before any Init runs, so a 2FA plugin
	// later in the list is already visible.
	if c.twoFactorURL == "" {
		for _, name := range mgr.order {
			if _, ok := mgr.plugins[name].(TwoFactorChecker); ok {
				return fmt.Errorf("auth: core plugin: %q handles two-factor sign-in, so set NewCorePlugin().WithTwoFactorURL to the page that finishes it: a form login of a 2FA account is pending until /2fa/challenge, and its next page would refuse it", name)
			}
		}
	}
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
		c.loginLimitAccount = newScopedRateLimiter(*cfg.LoginRateLimitPerAccount, loginAccountScope)
	}
	if cfg.RegisterRateLimit != nil {
		c.registerLimit = newScopedRateLimiter(*cfg.RegisterRateLimit, "register")
	}
	return nil
}

// RegisterRoutes mounts the core auth routes: login, logout, me,
// register, and password (a signed-in user's own password change).
func (c *CorePlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/login", c.loginHandler())
	r.Post(basePath+"/logout", c.logoutHandler())
	r.Get(basePath+"/me", c.meHandler())
	r.Post(basePath+"/register", c.registerHandler())
	r.Post(basePath+"/password", c.changePasswordHandler())
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
// The predicates are the repo's ONE cross-site form guard,
// core/handler.IsForgeableRequest + core/handler.IsCrossSiteRequest
// (Sec-Fetch-Site first; "same-site" falls through to the Origin-host
// compare, since a sibling subdomain is same-site yet still carries the
// SameSite cookie). This function owns only the auth battery's response
// shape. The gate is forgeability, NOT isFormRequest: it used to be the
// latter, which recognised only urlencoded and multipart, so a form with
// enctype="text/plain" (a CORS-simple type, no preflight) skipped the
// check entirely, and so did a bodyless fetch() that sends no
// Content-Type. On magic-link verify, whose credential comes from the URL
// rather than the body, that was a complete confirmation-step bypass:
// the attacker's own token, auto-submitted from their page, minted a
// session in the victim's browser.
func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if !handler.IsForgeableRequest(r) {
		return false
	}
	if handler.IsCrossSiteRequest(r) {
		writeFormAuthError(w, r, http.StatusForbidden, reasonCrossSiteRequest)
		return true
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
		writeFormAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
	} else {
		writeAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
	}
	return false
}

// mintSessionCookie writes the session cookie. It is the ONLY place in
// the battery that constructs the session cookie: the password-login
// mint, the logout clear, the magic-link verify mint, and the OAuth
// callback mint all go through it, so SameSite=Strict and the attribute
// set can never drift apart again (magic-link and OAuth shipped Lax for
// their whole lives because each hand-rolled its own literal).
//
// token == "" is the logout clear: an empty value with epoch Expires
// and MaxAge -1 retires the cookie in every browser.
//
// The OAuth STATE cookie (oauthStateCookie) is deliberately NOT this
// cookie and keeps its own Lax literals in oauth2.go: it must ride the
// provider's top-level redirect back to the callback, a trip the
// session cookie never makes.
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
		// failLogin records a failed-login security event (UserID may be
		// empty: anti-enumeration keeps "no such user" and "wrong
		// password" indistinguishable) and answers with the caller's
		// status/message pair, form or JSON per the request shape. It
		// replaces the three emit-and-respond blocks duplicated across
		// the FindByEmail-error, bad-password, and two-factor
		// fail-closed branches.
		failLogin := func(userID, reason string, status int, why i18n.Phrase) {
			c.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "login.failed",
				UserID: userID,
				Email:  email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"reason": reason},
			})
			if isForm {
				writeFormAuthError(w, r, status, why)
			} else {
				writeAuthError(w, r, status, why)
			}
		}
		if email == "" || password == "" {
			if isForm {
				writeFormAuthError(w, r, http.StatusBadRequest, reasonCredentialsRequired)
			} else {
				writeAuthError(w, r, http.StatusBadRequest, reasonCredentialsRequired)
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
					writeFormAuthError(w, r, http.StatusTooManyRequests, reasonRateLimit)
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
			// Verify against a dummy produced by the CONFIGURED hasher so
			// the not-found branch spends the same algorithm and cost a
			// real row does. Skipping the work leaks user existence by
			// timing; doing the WRONG algorithm's work leaks it just as
			// well, only more subtly.
			_ = verifyPassword(password, dummyHashFor(DefaultHasher))
			// Unknown user OR a transport error, record a failed login.
			// UserID stays empty (anti-enumeration: the event never
			// distinguishes "no such user" from "wrong password").
			failLogin("", "bad_credentials", http.StatusUnauthorized, reasonInvalidCredentials)
			return
		}
		if !verifyPassword(password, hash) {
			failLogin(user.GetID(), "bad_credentials", http.StatusUnauthorized, reasonInvalidCredentials)
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
			writeAuthError(w, r, http.StatusInternalServerError, reasonSessionCreateFailed)
			return
		}
		if err != nil {
			failLogin(user.GetID(), "twofa_failclosed", http.StatusInternalServerError, reasonTwoFactorUnavailable)
			return
		}
		// A reset, change or account claim that finished while the hash
		// above was being verified purged this user's sessions before
		// this one existed. Re-read the credential now the session is
		// stored: a purge from here on reaches it, and a hash that has
		// already moved means the password just checked is no longer the
		// account's, so the session goes and the login fails like a
		// wrong password.
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
			// A pending session is refused everywhere but /2fa/challenge,
			// so the browser goes to the page that posts the code. Init made
			// sure twoFactorURL is set whenever a 2FA plugin can make one
			// pending.
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

		// Also return a JWT if configured, but never for a pending-2FA
		// login. The JWT is stateless: handing it out here would let a
		// password-only caller skip the second factor entirely on any
		// JWT-authenticated route.
		if c.mgr.JWT() != nil && !pendingTwoFA && !cfg.CookieOnly {
			// Bound to the hash just verified, not a fresh read: a
			// reset landing after the check leaves this token dead.
			token, err := c.mgr.JWT().generateToken(user, hash)
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

// credentialUnchanged reports whether email still resolves to userID with
// the password hash hash. A lookup failure counts as changed: the caller
// is about to hand out a credential and fails closed.
func credentialUnchanged(ctx context.Context, store UserStore, email, userID, hash string) bool {
	row, now, err := store.FindByEmail(ctx, email)
	if err != nil || row == nil || row.GetID() != userID {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(now), []byte(hash)) == 1
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
			writeAuthError(w, r, http.StatusInternalServerError, reasonSignOutFailed)
			return
		}
		mintSessionCookie(w, cfg, "", time.Time{})
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
				writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
				return
			}
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidSession)
			return
		}

		// Try to look up the user for richer response
		resp := map[string]any{
			"userId":    sess.UserID,
			"expiresAt": sess.ExpiresAt,
		}

		if c.mgr.UserStore() != nil {
			user, err := c.mgr.UserStore().FindByID(r.Context(), sess.UserID)
			// A session whose account is gone (deleted, or trashed by a
			// soft delete) is not a signed-in user: the session middleware
			// already resolves it to anonymous, and /me must agree.
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

		// SECURITY: roles are server-assigned, never client-controlled.
		// /auth/register is anonymous. See decodeAuthCredentials for
		// the rationale. Role elevation is a separate admin-gated flow.
		// The values come from AuthConfig.DefaultRoles (operator
		// configuration, fallback ["user"]); any client-supplied roles
		// key on the request body is ignored.
		roles := c.mgr.DefaultRoles()

		if err := ValidatePasswordStrength(password); err != nil {
			if isForm {
				writeFormAuthError(w, r, http.StatusBadRequest, reasonPasswordTooShort)
			} else {
				writeAuthError(w, r, http.StatusBadRequest, reasonPasswordTooShort)
			}
			return
		}

		// Hash BEFORE the taken/unknown branch: bcrypt is the dominant
		// per-request cost, so neither branch is cheap to clock.
		hash, err := HashPassword(password)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonPasswordHashingFailed)
			return
		}

		user, err := store.CreateUser(r.Context(), email, hash, roles)
		taken := errors.Is(err, ErrEmailTaken)
		if err != nil && !taken {
			// A store/transport failure is not the collision sentinel.
			// The old handler answered 409 "email already registered"
			// here, reporting an account that may not exist; a real
			// failure stays a 500 and stays visible.
			writeAuthError(w, r, http.StatusInternalServerError, reasonRegistrationFailed)
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
// the timed path: the send is queued once the uniform response is
// already decided, so the client cannot clock the known/unknown branches
// apart — the same posture forgotHandler documents for its delivery. The
// queue (mailQueue) is the one password reset uses: a fixed worker pool,
// a bounded queue that drops rather than blocks, a deadline on every
// send, and OnStop cancelling what is in flight. A stalled provider costs
// at most the pool, not a goroutine per duplicate registration. The
// sender is host code, so its panic is recovered and logged scrubbed (the
// recovercallback contract); the notice body carries no secret.
func (c *CorePlugin) deliverRegisterDuplicateNotice(r *http.Request, holderEmail string) {
	if holderEmail == "" {
		return
	}
	sender := c.mgr.Config().RegisterEmailSender
	if sender == nil {
		return
	}
	c.notices.enqueue(r.Context(), holderEmail, func(ctx context.Context) {
		defer func() {
			if p := recover(); p != nil {
				// Scrub before the log (textsafe.Recovered): the panic
				// value is host-sender state, control/bidi bytes in it
				// must not reach the operator's tail raw.
				slog.Warn("register duplicate-notice sender panicked",
					"plugin", "core", "email_hash", hashedIdentifier(holderEmail), "panic", textsafe.Recovered(p))
			}
		}()
		if err := sender.Send(ctx, holderEmail, duplicateRegisterNoticeBody); err != nil {
			slog.Warn("register duplicate-notice send failed",
				"plugin", "core", "email_hash", hashedIdentifier(holderEmail), "err", err)
		}
	})
}

// writeAuthError is the shared error helper: it emits the canonical flat
// envelope {"error","success","code","reason"} with Content-Type
// application/json, the shape framework/crud/crud.go's writeJSONError
// uses and the generated SDKs and sdkdocs document. error is the reason
// in the request's locale, reason its stable code. battery/auth keeps a
// local copy because batteries may not import framework/crud.
func writeAuthError(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase, params ...map[string]any) {
	writeCredentialHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error":   reason.Text(requestContext(r), params...),
		"success": false,
		"code":    status,
		"reason":  reason.Code(),
	})
}

// writeAuthErrorFor answers err with its own phrase when it carries one,
// and with frame otherwise: an unphrased error's text never reaches the
// caller.
func writeAuthErrorFor(w http.ResponseWriter, r *http.Request, status int, err error, frame i18n.Phrase) {
	if p, ok := errors.AsType[i18n.Phrased](err); ok {
		reason, params := p.Phrased()
		writeAuthError(w, r, status, reason, params)
		return
	}
	writeAuthError(w, r, status, frame)
}

// requestContext is r's context, or Background for a nil request.
func requestContext(r *http.Request) context.Context {
	if r == nil {
		return context.Background()
	}
	return r.Context()
}

// invalidLinkBaseURL reports why base cannot serve as the origin of an
// emailed credential link ("" when it can). The password-reset,
// email-verification, and magic-link plugins build user-facing reset /
// verify / sign-in URLs from a config-supplied BaseURL; each validates
// at Init and fails loudly next to the declaration, mirroring uihost
// strict mode's invalidSitemapBaseURL — the repo's contract that a
// config-supplied origin building user-facing URLs is scheme-checked at
// load. A mis-shaped value ("//evil.example", a bare host, "javascript:")
// otherwise ships silently inside an emailed takeover link. Empty passes:
// with no origin configured the plugins build relative links or none.
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
