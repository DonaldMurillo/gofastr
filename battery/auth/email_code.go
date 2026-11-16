package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/ratelimit"
)

// EmailCodeSender mails a one-time sign-in code to an address.
// Implementations handle templating, delivery, and retries.
type EmailCodeSender interface {
	SendSignInCode(ctx context.Context, email, code string) error
}

// EmailCodeConfig configures the EmailCodePlugin.
type EmailCodeConfig struct {
	// CodeTTL is how long a code stays valid (default: 10 minutes).
	CodeTTL time.Duration

	// EmailSender mails the code. Without it, send answers 503 unless
	// DevMode is set.
	EmailSender EmailCodeSender

	// TokenStore persists pending challenges. It is the store the link
	// plugins use, and it must implement MagicLinkTokenPeeker (both
	// built-in stores do). Defaults to an in-memory store, which
	// production mode refuses unless AuthConfig.AllowInMemoryStores is
	// set: a challenge minted on one replica is unknown on another.
	TokenStore MagicLinkTokenStore

	// OnSuccessURL is where a form sign-in is redirected, and the
	// "redirect" a JSON one is told (default: "/").
	OnSuccessURL string

	// TwoFactorURL is where a sign-in goes when the account still owes
	// its second factor. Same-origin paths only. Init requires it when a
	// 2FA plugin is registered.
	TwoFactorURL string

	// RateLimit is the per-IP limit on /email-code/send (default: 10 a
	// minute, 15-minute block). The per-address mail cooldown is
	// AuthConfig.MailAddressRateLimit, shared with the link plugins.
	RateLimit *RateLimiterConfig

	// VerifyRateLimit is the per-IP limit on /email-code/verify
	// (default: 30 a minute, 5-minute block).
	VerifyRateLimit *RateLimiterConfig

	// AddressGuessLimit caps code guesses per address, across every
	// challenge for it (default: 10 an hour, 1-hour block). With five
	// guesses per challenge and one mail per address per cooldown, it is
	// what bounds a slow guesser with many IPs.
	AddressGuessLimit *RateLimiterConfig

	// DevMode lets send run without an EmailSender: it logs hashes of
	// the address and code, never the code itself.
	DevMode bool
}

func (c *EmailCodeConfig) defaults() {
	if c.CodeTTL <= 0 {
		c.CodeTTL = 10 * time.Minute
	}
	if c.OnSuccessURL == "" {
		c.OnSuccessURL = "/"
	}
	if c.RateLimit == nil {
		c.RateLimit = &RateLimiterConfig{MaxAttempts: 10, Window: time.Minute, BlockDuration: 15 * time.Minute}
	}
	if c.VerifyRateLimit == nil {
		c.VerifyRateLimit = &RateLimiterConfig{MaxAttempts: 30, Window: time.Minute, BlockDuration: 5 * time.Minute}
	}
	if c.AddressGuessLimit == nil {
		c.AddressGuessLimit = &RateLimiterConfig{MaxAttempts: 10, Window: time.Hour, BlockDuration: time.Hour}
	}
}

// emailCodeDigits is the code's length. Eight digits, not six: a code
// is typed, so it can be guessed, and the address budget allows about
// 240 guesses a day.
const emailCodeDigits = 8

// emailCodeChallengeGuesses is how many codes one challenge accepts
// before it is dropped.
const emailCodeChallengeGuesses = 5

// The challenge cookie. __Host- in production, as the OAuth state
// cookie; a __Host- cookie without Secure is refused, so dev drops it.
const (
	emailCodeCookie    = "__Host-gofastr_email_code"
	emailCodeCookieDev = "gofastr_email_code"
)

// EmailCodePlugin signs a user in with a one-time code mailed to their
// address. The code is bound to the browser that asked for it: send
// sets a challenge cookie, and verify takes the code only alongside it.
type EmailCodePlugin struct {
	config      EmailCodeConfig
	mgr         *AuthManager
	tokenStore  MagicLinkTokenStore
	peeker      MagicLinkTokenPeeker
	codeKey     []byte
	sendLimit   *RateLimiter
	verifyLimit *RateLimiter
	challenge   *RateLimiter
	address     *RateLimiter
}

// NewEmailCodePlugin creates the emailed-code sign-in plugin.
func NewEmailCodePlugin(config EmailCodeConfig) *EmailCodePlugin {
	config.defaults()
	store := config.TokenStore
	if store == nil {
		store = NewMemoryMagicLinkTokenStore()
	}
	return &EmailCodePlugin{
		config:      config,
		tokenStore:  store,
		sendLimit:   newScopedRateLimiter(*config.RateLimit, "emailcode_send"),
		verifyLimit: newScopedRateLimiter(*config.VerifyRateLimit, "emailcode_verify"),
		challenge: newScopedRateLimiter(RateLimiterConfig{
			MaxAttempts: emailCodeChallengeGuesses, Window: config.CodeTTL, BlockDuration: config.CodeTTL,
		}, "emailcode_challenge"),
		address: newScopedRateLimiter(*config.AddressGuessLimit, "emailcode_address"),
	}
}

// Name returns the plugin identifier.
func (p *EmailCodePlugin) Name() string { return "email-code" }

// Init checks the store and the two-factor page, and derives the code
// key from JWTSecret, which every replica shares.
func (p *EmailCodePlugin) Init(mgr *AuthManager) error {
	peeker, ok := p.tokenStore.(MagicLinkTokenPeeker)
	if !ok {
		return errors.New("auth: email-code plugin: TokenStore must implement MagicLinkTokenPeeker: verify reads a challenge's address to meter guesses before anything is consumed")
	}
	if p.config.TokenStore == nil {
		if err := refuseInMemoryTokenStore(mgr, "email-code"); err != nil {
			return err
		}
	}
	if err := checkTwoFactorURL("email-code", "EmailCodeConfig.TwoFactorURL", p.config.TwoFactorURL); err != nil {
		return err
	}
	if p.config.TwoFactorURL == "" {
		for _, name := range mgr.order {
			if _, ok := mgr.plugins[name].(TwoFactorChecker); ok {
				return fmt.Errorf("auth: email-code plugin: %q handles two-factor sign-in, so set EmailCodeConfig.TwoFactorURL to the page that finishes it: an emailed-code sign-in of a 2FA account is pending until /2fa/challenge, and OnSuccessURL would refuse it", name)
			}
		}
	}
	secret := mgr.Config().JWTSecret
	if secret == "" {
		return errors.New("auth: email-code plugin: AuthConfig.JWTSecret is empty: the code is derived from it so every replica can check it")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("gofastr/auth/email-code/v1"))
	p.codeKey = mac.Sum(nil)
	p.peeker = peeker
	p.mgr = mgr
	return nil
}

// RegisterRoutes mounts send and verify.
func (p *EmailCodePlugin) RegisterRoutes(r *router.Router, basePath string) {
	r = r.Group("", browserAccountOnly)
	r.Post(basePath+"/email-code/send", http.HandlerFunc(p.sendHandler))
	r.Post(basePath+"/email-code/verify", http.HandlerFunc(p.verifyHandler))
}

// codeFor is the code a challenge token stands for. It is derived, not
// stored, so the stored payload is the bare address and a password
// reset's purge by address reaches every outstanding challenge.
func (p *EmailCodePlugin) codeFor(token string) string {
	mac := hmac.New(sha256.New, p.codeKey)
	mac.Write([]byte(token))
	n := binary.BigEndian.Uint64(mac.Sum(nil)[:8]) % 100_000_000
	return fmt.Sprintf("%0*d", emailCodeDigits, n)
}

func (p *EmailCodePlugin) cookieName() string {
	if p.mgr.Config().SessionSecure {
		return emailCodeCookie
	}
	return emailCodeCookieDev
}

func (p *EmailCodePlugin) setChallengeCookie(w http.ResponseWriter, token string) {
	c := &http.Cookie{
		Name:     p.cookieName(),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   p.mgr.Config().SessionSecure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(p.config.CodeTTL / time.Second),
	}
	if token == "" {
		c.MaxAge = -1
		c.Expires = time.Unix(0, 0)
	}
	http.SetCookie(w, c)
}

// readField reads one field from a JSON or form body.
func readField(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	if isFormRequest(r) {
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		if !parseAuthFormOrRefuse(w, r) {
			return "", false
		}
		return r.PostFormValue(name), true
	}
	var body struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return "", false
	}
	if name == "code" {
		return body.Code, true
	}
	return body.Email, true
}

// refuse answers in the request's shape: a form post is sent back to
// its page with ?error=, a JSON caller gets the envelope.
func (p *EmailCodePlugin) refuse(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase) {
	if isFormRequest(r) {
		writeFormAuthError(w, r, status, reason)
		return
	}
	writeAuthError(w, r, status, reason)
}

// sendHandler handles POST {basePath}/email-code/send with an "email"
// field. Every address gets the same answer, mailed or not.
func (p *EmailCodePlugin) sendHandler(w http.ResponseWriter, r *http.Request) {
	if !guardAuthLimit(p.sendLimit, w, r) {
		return
	}
	// A cross-site page must not plant its own challenge in this
	// browser: the code it then gets the user to type would be its own.
	if rejectCrossSiteForm(w, r) {
		return
	}
	raw, ok := readField(w, r, "email")
	if !ok {
		return
	}
	email, err := p.mgr.canonicalizeEmail(raw)
	if err != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, err, reasonComposedEmail)
		return
	}
	if email == "" {
		p.refuse(w, r, http.StatusBadRequest, reasonEmailIsRequired)
		return
	}
	if !emailWithinLimit(w, r, email) {
		return
	}

	requested := SecurityEvent{Kind: "emailcode.requested", Email: email, Remote: remoteHost(r)}
	if store := p.mgr.UserStore(); store != nil {
		if u, _, err := store.FindByEmail(r.Context(), email); err == nil && u != nil {
			requested.UserID = u.GetID()
		}
	}

	if !p.mgr.allowMailAddress(r.Context(), email) {
		// Same answer as a mailed code, cookie included, so the cooldown
		// says nothing about the address. The decoy is stored nowhere.
		decoy := make([]byte, 32)
		if _, err := rand.Read(decoy); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToCreateToken)
			return
		}
		requested.Meta = map[string]string{"throttled": "address"}
		p.mgr.emitSecurity(r.Context(), requested)
		p.setChallengeCookie(w, hex.EncodeToString(decoy))
		p.writeSent(w, r)
		return
	}

	if p.config.EmailSender == nil && !p.config.DevMode {
		writeAuthError(w, r, http.StatusServiceUnavailable, reasonEmailCodeDeliveryNotConfigured)
		return
	}
	token, err := createPurposeToken(r.Context(), p.tokenStore, purposeEmailCode, email, p.config.CodeTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToCreateToken)
		return
	}
	code := p.codeFor(token)
	if p.config.EmailSender != nil {
		if err := p.config.EmailSender.SendSignInCode(r.Context(), email, code); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToSendEmail)
			return
		}
	} else {
		// The code is a credential: the log carries hashes only, as the
		// magic link's dev line does.
		slog.Info("email-code dev",
			"plugin", "email-code",
			"email_hash", hashedIdentifier(email),
			"code_hash", hashedIdentifier(code))
	}
	p.mgr.emitSecurity(r.Context(), requested)
	p.setChallengeCookie(w, token)
	p.writeSent(w, r)
}

func (p *EmailCodePlugin) writeSent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if isFormRequest(r) {
		target := successRedirect(w, r, "")
		if target == "" {
			target = safeReferer(r)
		}
		if target == "" {
			target = "/"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	writeAuthJSON(w, r, http.StatusOK, map[string]any{
		"message": reasonEmailCodeSent.Text(r.Context()),
		"success": true,
	})
}

// normalizeCode drops the spaces and hyphens people type or paste
// between groups of digits.
func normalizeCode(s string) string {
	return strings.Map(func(c rune) rune {
		if c == ' ' || c == '-' {
			return -1
		}
		return c
	}, s)
}

// verifyHandler handles POST {basePath}/email-code/verify with a "code"
// field, alongside the challenge cookie send set.
func (p *EmailCodePlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	if !guardAuthLimit(p.verifyLimit, w, r) {
		return
	}
	// The attacker knows the code to their own address, so a cross-site
	// form could otherwise sign this browser into their account.
	if rejectCrossSiteForm(w, r) {
		return
	}
	raw, ok := readField(w, r, "code")
	if !ok {
		return
	}
	code := normalizeCode(raw)
	if code == "" {
		p.refuse(w, r, http.StatusBadRequest, reasonCodeRequired)
		return
	}
	cookie, err := r.Cookie(p.cookieName())
	if err != nil || cookie.Value == "" || len(cookie.Value) > 128 {
		p.refuse(w, r, http.StatusUnauthorized, reasonInvalidCode)
		return
	}
	token := cookie.Value

	// Peek, never redeem, until the code matches: a wrong guess must not
	// cost the user their challenge before its budget is spent.
	stored, err := p.peeker.PeekToken(r.Context(), token)
	if err != nil {
		p.refuse(w, r, http.StatusUnauthorized, reasonInvalidCode)
		return
	}
	email, ok := peekPurposePayload(stored, purposeEmailCode)
	if !ok {
		p.refuse(w, r, http.StatusUnauthorized, reasonInvalidCode)
		return
	}

	// Both budgets are spent before the code is compared. A store
	// error refuses the attempt: an outage is not a spent budget, and
	// it must not be an unmetered one either.
	if !p.spendGuess(w, r, p.challenge, "challenge:"+sha256hex(token), token, email) {
		return
	}
	if !p.spendGuess(w, r, p.address, "address:"+sha256hex(email), "", email) {
		return
	}

	want := p.codeFor(token)
	if len(code) != len(want) || subtle.ConstantTimeCompare([]byte(code), []byte(want)) != 1 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{Kind: "emailcode.failed", Email: email, Remote: remoteHost(r)})
		p.refuse(w, r, http.StatusUnauthorized, reasonInvalidCode)
		return
	}

	// Consume. A concurrent verify of the same code loses here.
	if _, err := redeemPurposeToken(r.Context(), p.tokenStore, purposeEmailCode, token); err != nil {
		p.refuse(w, r, http.StatusUnauthorized, reasonInvalidCode)
		return
	}
	p.setChallengeCookie(w, "")

	user, sess, pending, refused := p.mgr.signInMailboxOwner(r.Context(), r, email, "email_code")
	if refused != nil {
		p.refuse(w, r, refused.status(), refused.reason)
		return
	}
	mintSessionCookie(w, p.mgr.Config(), sess.Token, sess.ExpiresAt)
	consumed := SecurityEvent{Kind: "emailcode.consumed", UserID: user.GetID(), Email: email, Remote: remoteHost(r)}
	if pending {
		consumed.Meta = map[string]string{"two_factor": "pending"}
	}
	p.mgr.emitSecurity(r.Context(), consumed)

	target := p.config.OnSuccessURL
	if pending {
		target = p.config.TwoFactorURL
	}
	target = safeRedirectURL(target)
	if isFormRequest(r) {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	out := map[string]any{"redirect": target, "success": true}
	if pending {
		out["two_factor_required"] = true
	}
	writeAuthJSON(w, r, http.StatusOK, out)
}

// spendGuess spends one guess from rl under key and reports whether the
// caller may go on. A spent challenge budget drops the challenge (token
// set); a spent address budget only refuses.
func (p *EmailCodePlugin) spendGuess(w http.ResponseWriter, r *http.Request, rl *RateLimiter, key, token, email string) bool {
	allowed, retry, err := rl.Admit(r.Context(), key)
	if err != nil {
		slog.Warn("auth: email-code guess budget store error: failing closed", "err", err)
		w.Header().Set("Retry-After", ratelimit.RetryAfterSeconds(retry))
		writeAuthError(w, r, http.StatusServiceUnavailable, reasonTryAgainShortly)
		return false
	}
	if allowed {
		return true
	}
	if token != "" {
		if _, err := redeemPurposeToken(r.Context(), p.tokenStore, purposeEmailCode, token); err != nil && !errors.Is(err, ErrTokenNotFound) {
			slog.Warn("auth: dropping a spent email-code challenge failed", "err", err)
		}
		p.setChallengeCookie(w, "")
	}
	p.mgr.emitSecurity(r.Context(), SecurityEvent{Kind: "emailcode.locked", Email: email, Remote: remoteHost(r)})
	w.Header().Set("Retry-After", ratelimit.RetryAfterSeconds(retry))
	p.refuse(w, r, http.StatusTooManyRequests, reasonCodeAttemptsSpent)
	return false
}
