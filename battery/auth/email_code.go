package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
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
type EmailCodeSender interface {
	SendSignInCode(ctx context.Context, email, code string) error
}

// EmailCodeConfig configures the EmailCodePlugin.
type EmailCodeConfig struct {
	// CodeTTL is how long a code stays valid (default: 10 minutes).
	CodeTTL time.Duration

	// EmailSender mails the code. Required unless DevMode is set.
	EmailSender EmailCodeSender

	// TokenStore persists pending challenges and must implement
	// MagicLinkTokenPeeker. Defaults to an in-memory store.
	TokenStore MagicLinkTokenStore

	// OnSuccessURL is where a form sign-in is redirected, and the
	// "redirect" a JSON one is told (default: "/").
	OnSuccessURL string

	// TwoFactorURL is where a sign-in of a 2FA account goes, a
	// same-origin path. Required next to a 2FA plugin.
	TwoFactorURL string

	// RateLimit is the per-IP limit on /email-code/send (default: 10 a
	// minute, 15-minute block).
	RateLimit *RateLimiterConfig

	// VerifyRateLimit is the per-IP limit on /email-code/verify
	// (default: 30 a minute, 5-minute block).
	VerifyRateLimit *RateLimiterConfig

	// AddressGuessLimit caps code guesses per address across all its
	// challenges (default: 10 an hour, 1-hour block).
	AddressGuessLimit *RateLimiterConfig

	// DevMode lets send run without an EmailSender, logging hashes only.
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

const (
	emailCodeDigits           = 8
	emailCodeChallengeGuesses = 5
)

const (
	emailCodeCookie    = "__Host-gofastr_email_code"
	emailCodeCookieDev = "gofastr_email_code"
)

// EmailCodePlugin signs a user in with a one-time code mailed to their
// address and typed into the browser that asked for it.
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

// Init checks the store and the two-factor page and derives the code key.
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

func (p *EmailCodePlugin) refuse(w http.ResponseWriter, r *http.Request, status int, reason i18n.Phrase) {
	if isFormRequest(r) {
		writeFormAuthError(w, r, status, reason)
		return
	}
	writeAuthError(w, r, status, reason)
}

func (p *EmailCodePlugin) sendHandler(w http.ResponseWriter, r *http.Request) {
	if !guardAuthLimit(p.sendLimit, w, r) {
		return
	}
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
		requested.Meta = map[string]string{"throttled": "address"}
		p.mgr.emitSecurity(r.Context(), requested)
		live, err := p.holdsLiveChallenge(r, email)
		if err != nil {
			slog.Warn("auth: email-code challenge lookup failed: keeping the browser's challenge", "err", err)
			writeAuthError(w, r, http.StatusServiceUnavailable, reasonTryAgainShortly)
			return
		}
		if !live {
			token, err := createPurposeToken(r.Context(), p.tokenStore, purposeEmailCode, email, p.config.CodeTTL)
			if err != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToCreateToken)
				return
			}
			p.setChallengeCookie(w, token)
		}
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
		slog.Info("email-code dev",
			"plugin", "email-code",
			"email_hash", hashedIdentifier(email),
			"code_hash", hashedIdentifier(code))
	}
	p.mgr.emitSecurity(r.Context(), requested)
	p.setChallengeCookie(w, token)
	p.writeSent(w, r)
}

func (p *EmailCodePlugin) holdsLiveChallenge(r *http.Request, email string) (bool, error) {
	c, err := r.Cookie(p.cookieName())
	if err != nil || c.Value == "" || len(c.Value) > 128 {
		return false, nil
	}
	raw, err := p.peeker.PeekToken(r.Context(), c.Value)
	if errors.Is(err, ErrTokenNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	addr, ok := peekPurposePayload(raw, purposeEmailCode)
	return ok && addr == email, nil
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

func normalizeCode(s string) string {
	return strings.Map(func(c rune) rune {
		if c == ' ' || c == '-' {
			return -1
		}
		return c
	}, s)
}

func (p *EmailCodePlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	if !guardAuthLimit(p.verifyLimit, w, r) {
		return
	}
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

	if !p.spendGuess(w, r, p.address, "address:"+sha256hex(email), "", email) {
		return
	}
	if !p.spendGuess(w, r, p.challenge, "challenge:"+sha256hex(token), token, email) {
		return
	}

	want := p.codeFor(token)
	if len(code) != len(want) || subtle.ConstantTimeCompare([]byte(code), []byte(want)) != 1 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{Kind: "emailcode.failed", Email: email, Remote: remoteHost(r)})
		p.refuse(w, r, http.StatusUnauthorized, reasonInvalidCode)
		return
	}

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
