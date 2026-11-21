package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// MagicLinkEmailSender sends the magic-link email to the user.
type MagicLinkEmailSender interface {
	SendMagicLink(ctx context.Context, email, magicLinkURL string) error
}

// MagicLinkConfig configures the MagicLinkPlugin.
type MagicLinkConfig struct {
	// TokenTTL is how long the magic link remains valid (default: 15 minutes).
	TokenTTL time.Duration

	// EmailSender sends the magic link email. If nil, the URL is logged instead (dev mode).
	EmailSender MagicLinkEmailSender

	// TokenStore keeps pending tokens; nil is an in-memory store, refused in production.
	TokenStore MagicLinkTokenStore

	// BodyTemplate builds the mail body from the link; nil sends the link alone.
	BodyTemplate func(ctx context.Context, url string) string

	// BaseURL is the origin of the emailed link.
	BaseURL string

	// OnSuccessURL is the URL to redirect to after successful login (default: "/").
	OnSuccessURL string

	// TwoFactorURL is the same-origin page a sign-in of a 2FA account goes to; Init requires it next to a 2FA plugin.
	TwoFactorURL string

	// RateLimit is the per-IP limit on send; nil means 10 a minute with a 15-minute block.
	RateLimit *RateLimiterConfig
	// VerifyRateLimit is the per-IP limit on verify; nil means 30 a minute with a 5-minute block.
	VerifyRateLimit *RateLimiterConfig

	// DevMode lets send run without an EmailSender, logging hashes only; otherwise send answers 503.
	DevMode bool
}

func (c *MagicLinkConfig) defaults() {
	if c.TokenTTL <= 0 {
		c.TokenTTL = 15 * time.Minute
	}
	if c.OnSuccessURL == "" {
		c.OnSuccessURL = "/"
	}
}

// MagicLinkTokenStore is the interface for persisting and consuming magic-link tokens.
type MagicLinkTokenStore interface {
	// CreateToken generates a new token for the given email, valid for ttl.
	CreateToken(ctx context.Context, email string, ttl time.Duration) (token string, err error)

	// RedeemToken atomically consumes the token and returns the associated email.
	RedeemToken(ctx context.Context, token string) (email string, err error)

	// Cleanup removes expired tokens and returns the count purged.
	Cleanup(ctx context.Context) (int, error)
}

// MagicLinkTokenPeeker reads a token's payload without consuming it.
type MagicLinkTokenPeeker interface {
	PeekToken(ctx context.Context, token string) (email string, err error)
}

// ErrTokenNotFound is returned when a token is unknown, already consumed, or expired.
var ErrTokenNotFound = errors.New("auth: magic-link token not found or expired")

type magicLinkEntry struct {
	email     string
	expiresAt time.Time
}

// MemoryMagicLinkTokenStore is a goroutine-safe in-memory MagicLinkTokenStore.
type MemoryMagicLinkTokenStore struct {
	mu        sync.RWMutex
	tokens    map[string]*magicLinkEntry
	lastSweep time.Time
}

const magicLinkSweepInterval = 15 * time.Minute

// NewMemoryMagicLinkTokenStore returns a fresh in-memory token store.
func NewMemoryMagicLinkTokenStore() *MemoryMagicLinkTokenStore {
	return &MemoryMagicLinkTokenStore{tokens: make(map[string]*magicLinkEntry)}
}

// CreateToken stores email under a new random token valid for ttl.
func (m *MemoryMagicLinkTokenStore) CreateToken(_ context.Context, email string, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(b)

	now := time.Now()
	m.mu.Lock()
	m.tokens[token] = &magicLinkEntry{
		email:     email,
		expiresAt: now.Add(ttl),
	}
	if now.Sub(m.lastSweep) >= magicLinkSweepInterval {
		purgeMatching(m.tokens, func(e *magicLinkEntry) bool { return !now.Before(e.expiresAt) })
		m.lastSweep = now
	}
	m.mu.Unlock()

	return token, nil
}

// RedeemToken atomically consumes a token. Returns the associated email.
func (m *MemoryMagicLinkTokenStore) RedeemToken(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.tokens[token]
	if !ok {
		return "", ErrTokenNotFound
	}
	delete(m.tokens, token)

	if !time.Now().Before(entry.expiresAt) {
		return "", ErrTokenNotFound
	}
	return entry.email, nil
}

// PeekToken returns a live token's payload without consuming it.
func (m *MemoryMagicLinkTokenStore) PeekToken(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.tokens[token]
	if !ok || !time.Now().Before(entry.expiresAt) {
		return "", ErrTokenNotFound
	}
	return entry.email, nil
}

// DeleteTokensForPayload removes every unredeemed token carrying payload.
func (m *MemoryMagicLinkTokenStore) DeleteTokensForPayload(_ context.Context, payload string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return purgeMatching(m.tokens, func(e *magicLinkEntry) bool { return e.email == payload }), nil
}

// Cleanup removes all expired tokens and returns the count purged.
func (m *MemoryMagicLinkTokenStore) Cleanup(_ context.Context) (int, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	return purgeMatching(m.tokens, func(e *magicLinkEntry) bool { return !now.Before(e.expiresAt) }), nil
}

// MagicLinkPlugin serves passwordless sign-in by emailed link.
type MagicLinkPlugin struct {
	config      MagicLinkConfig
	mgr         *AuthManager
	tokenStore  MagicLinkTokenStore
	sendLimit   *RateLimiter
	verifyLimit *RateLimiter
	basePath    string

	lifecycleMu  sync.Mutex
	started      bool
	stopped      bool
	stopCh       chan struct{}
	reaperCtx    context.Context
	reaperCancel context.CancelFunc
	reaperDone   chan struct{}
}

// NewMagicLinkPlugin creates a new magic-link plugin with the given config.
func NewMagicLinkPlugin(config MagicLinkConfig) *MagicLinkPlugin {
	config.defaults()
	store := config.TokenStore
	if store == nil {
		store = NewMemoryMagicLinkTokenStore()
	}
	if config.RateLimit == nil {
		config.RateLimit = &RateLimiterConfig{
			MaxAttempts:   10,
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
	if config.VerifyRateLimit == nil {
		config.VerifyRateLimit = &RateLimiterConfig{
			MaxAttempts:   30,
			Window:        time.Minute,
			BlockDuration: 5 * time.Minute,
		}
	}
	p := &MagicLinkPlugin{
		config:      config,
		tokenStore:  store,
		sendLimit:   newScopedRateLimiter(*config.RateLimit, "magiclink_send"),
		verifyLimit: newScopedRateLimiter(*config.VerifyRateLimit, "magiclink_verify"),
	}
	return p
}

// Name returns the plugin identifier.
func (p *MagicLinkPlugin) Name() string { return "magic-link" }

// Init checks BaseURL, the token store and TwoFactorURL.
func (p *MagicLinkPlugin) Init(mgr *AuthManager) error {
	if why := invalidLinkBaseURL(p.config.BaseURL); why != "" {
		return fmt.Errorf("auth: magic-link plugin: BaseURL %q %s", p.config.BaseURL, why)
	}
	if p.config.TokenStore == nil {
		if err := refuseInMemoryTokenStore(mgr, "magic-link"); err != nil {
			return err
		}
	}
	if err := checkTwoFactorURL("magic-link", "MagicLinkConfig.TwoFactorURL", p.config.TwoFactorURL); err != nil {
		return err
	}
	if err := requireTwoFactorURL(mgr, "magic-link", "MagicLinkConfig.TwoFactorURL", p.config.TwoFactorURL, "a magic-link sign-in", "OnSuccessURL"); err != nil {
		return err
	}
	p.mgr = mgr
	return nil
}

func allowAddress(ctx context.Context, rl *RateLimiter, email string) bool {
	if rl == nil {
		return true
	}
	allowed, _, err := rl.Admit(ctx, "address:"+sha256hex(email))
	if err != nil {
		slog.Warn("auth: per-address mail limiter store error: not sending", "err", err)
	}
	return allowed
}

func refuseInMemoryTokenStore(mgr *AuthManager, plugin string) error {
	cfg := mgr.Config()
	if cfg.DevMode || cfg.AllowInMemoryStores {
		return nil
	}
	return fmt.Errorf("auth: %s plugin: production mode refuses the in-memory token store: a link minted on one replica is invalid on another and a restart drops every pending link; set its TokenStore (e.g. auth.NewSQLMagicLinkTokenStore(db)), or set AuthConfig.AllowInMemoryStores: true to acknowledge a single-node deployment", plugin)
}

// RegisterRoutes mounts the magic-link send and verify endpoints.
func (p *MagicLinkPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r = r.Group("", browserAccountOnly)
	p.basePath = basePath
	r.Post(basePath+"/magic-link/send", http.HandlerFunc(p.sendHandler))
	r.Get(basePath+"/magic-link/verify", http.HandlerFunc(p.confirmHandler))
	r.Post(basePath+"/magic-link/verify", http.HandlerFunc(p.verifyHandler))
}

// OnStart starts the reaper that calls the token store's Cleanup.
func (p *MagicLinkPlugin) OnStart(ctx context.Context) error {
	p.lifecycleMu.Lock()
	if p.stopped {
		p.lifecycleMu.Unlock()
		return errors.New("auth: magic-link plugin cannot start after it has stopped")
	}
	if p.started {
		p.lifecycleMu.Unlock()
		return nil
	}

	interval := p.config.TokenTTL
	if interval < time.Minute {
		interval = time.Minute
	}
	stopCh := make(chan struct{})
	reaperCtx, reaperCancel := context.WithCancel(ctx)
	reaperDone := make(chan struct{})
	p.stopCh = stopCh
	p.reaperCtx = reaperCtx
	p.reaperCancel = reaperCancel
	p.reaperDone = reaperDone
	p.started = true
	p.lifecycleMu.Unlock()

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		defer close(reaperDone)
		for {
			select {
			case <-ticker.C:
				p.reapExpiredTokensContext(reaperCtx)
			case <-stopCh:
				return
			case <-reaperCtx.Done():
				return
			}
		}
	}()
	return nil
}

func (p *MagicLinkPlugin) reapExpiredTokens() {
	p.reapExpiredTokensContext(context.Background())
}

func (p *MagicLinkPlugin) reapExpiredTokensContext(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("magic-link: token store cleanup panicked",
				"plugin", "magic-link", "panic", textsafe.Recovered(rec))
		}
	}()
	if n, err := p.tokenStore.Cleanup(ctx); err != nil {
		slog.Warn("magic-link: token store cleanup failed",
			"plugin", "magic-link", "err", err.Error())
	} else if n > 0 {
		slog.Debug("magic-link: reaped expired tokens",
			"plugin", "magic-link", "count", n)
	}
}

// OnStop stops the reaper.
func (p *MagicLinkPlugin) OnStop(ctx context.Context) error {
	p.lifecycleMu.Lock()
	if !p.stopped {
		p.stopped = true
		if p.started {
			close(p.stopCh)
			p.reaperCancel()
		}
	}
	done := p.reaperDone
	p.lifecycleMu.Unlock()

	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *MagicLinkPlugin) sendHandler(w http.ResponseWriter, r *http.Request) {
	if p.sendLimit != nil && !p.sendLimit.guard(w, r) {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	var cerr error
	body.Email, cerr = p.mgr.canonicalizeEmail(body.Email)
	if cerr != nil {
		writeAuthErrorFor(w, r, http.StatusBadRequest, cerr, reasonComposedEmail)
		return
	}
	if body.Email == "" {
		writeAuthError(w, r, http.StatusBadRequest, reasonEmailIsRequired)
		return
	}
	if !emailWithinLimit(w, r, body.Email) {
		return
	}

	requested := SecurityEvent{
		Kind:   "magiclink.requested",
		Email:  body.Email,
		Remote: remoteHost(r),
	}
	if store := p.mgr.UserStore(); store != nil {
		if u, _, err := store.FindByEmail(r.Context(), body.Email); err == nil && u != nil {
			requested.UserID = u.GetID()
		}
	}

	if !p.mgr.allowMailAddress(r.Context(), body.Email) {
		requested.Meta = map[string]string{"throttled": "address"}
		p.mgr.emitSecurity(r.Context(), requested)
		writeMagicLinkSent(w, r)
		return
	}

	token, err := createPurposeToken(r.Context(), p.tokenStore, purposeMagicLink, body.Email, p.config.TokenTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToCreateToken)
		return
	}

	magicLinkURL := fmt.Sprintf("%s%s/magic-link/verify?token=%s",
		p.config.BaseURL,
		p.mgr.Config().BasePath,
		token,
	)

	switch {
	case p.config.EmailSender != nil:
		linkBody := magicLinkURL
		if p.config.BodyTemplate != nil {
			linkBody = p.config.BodyTemplate(r.Context(), magicLinkURL)
		}
		if err := p.config.EmailSender.SendMagicLink(r.Context(), body.Email, linkBody); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToSendEmail)
			return
		}
	case p.config.DevMode:
		slog.Info("magic-link dev",
			"plugin", "magic-link",
			"email_hash", hashedIdentifier(body.Email),
			"token_hash", hashedIdentifier(token))
	default:
		writeAuthError(w, r, http.StatusServiceUnavailable, reasonMagicLinkDeliveryNotConfigured)
		return
	}

	p.mgr.emitSecurity(r.Context(), requested)
	writeMagicLinkSent(w, r)
}

func writeMagicLinkSent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message": reasonMagicLinkSent.Text(r.Context()),
		"success": true,
	})
}

// ConfirmPageData is what Pages.MagicLinkConfirm renders.
type ConfirmPageData struct {
	// Email is the account the link signs into, or "" when the store cannot peek.
	Email string

	// Token must be POSTed back to Action in a field named "token".
	Token string

	// Action is the path the confirmation form submits to.
	Action string

	// CSRFField is the hidden CSRF input, or ""; a Pages form must render it.
	CSRFField template.HTML

	// Words are the screen's words in the caller's locale.
	Words ConfirmWords
}

func (p *MagicLinkPlugin) confirmHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeAuthError(w, r, http.StatusUnauthorized, reasonTokenIsRequired)
		return
	}
	email := ""
	if peeker, ok := p.tokenStore.(MagicLinkTokenPeeker); ok {
		got, err := peeker.PeekToken(r.Context(), token)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
			return
		}
		addr, ok := peekPurposePayload(got, purposeMagicLink)
		if !ok {
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
			return
		}
		email = addr
	}

	csrfField := CSRFInputFromCtx(r.Context())
	if csrfField == "" {
		csrfField = CSRFInputHTML(r)
	}
	writeConfirmPage(w, r, p.mgr.config.Pages, ConfirmPageData{
		Email:     email,
		Token:     token,
		Action:    p.basePath + "/magic-link/verify",
		CSRFField: csrfField,
		Words:     confirmWords(r.Context(), email),
	})
}

func (p *MagicLinkPlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	if p.verifyLimit != nil && !p.verifyLimit.guard(w, r) {
		return
	}
	if rejectCrossSiteForm(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if !parseAuthFormOrRefuse(w, r) {
		return
	}
	token := r.PostFormValue("token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		writeAuthError(w, r, http.StatusUnauthorized, reasonTokenIsRequired)
		return
	}

	email, err := redeemPurposeToken(r.Context(), p.tokenStore, purposeMagicLink, token)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
		return
	}

	user, sess, pending, refused := p.mgr.signInMailboxOwner(r.Context(), r, email, "magic_link")
	if refused != nil {
		writeAuthError(w, r, refused.status(), refused.reason)
		return
	}
	cfg := p.mgr.Config()
	mintSessionCookie(w, cfg, sess.Token, sess.ExpiresAt)

	consumed := SecurityEvent{
		Kind:   "magiclink.consumed",
		UserID: user.GetID(),
		Email:  email,
		Remote: remoteHost(r),
	}
	if pending {
		consumed.Meta = map[string]string{"two_factor": "pending"}
	}
	p.mgr.emitSecurity(r.Context(), consumed)

	target := p.config.OnSuccessURL
	if pending {
		target = p.config.TwoFactorURL
	}
	http.Redirect(w, r, safeRedirectURL(target), http.StatusFound)
}

func safeRedirectURL(u string) string {
	if handler.IsSafeRelativePath(u) {
		return u
	}
	return "/"
}

func checkTwoFactorURL(plugin, option, value string) error {
	if value == "" || handler.IsSafeRelativePath(value) {
		return nil
	}
	return fmt.Errorf("auth: %s plugin: %s %q is not a same-origin path (one leading \"/\", no scheme or host), so a two-factor sign-in would be sent to \"/\", which refuses it", plugin, option, value)
}

func requireTwoFactorURL(mgr *AuthManager, plugin, option, value, signIn, fallback string) error {
	if value != "" {
		return nil
	}
	for _, name := range mgr.order {
		if _, ok := mgr.plugins[name].(TwoFactorChecker); ok {
			return fmt.Errorf("auth: %s plugin: %q handles two-factor sign-in, so set %s to the page that finishes it: %s of a 2FA account is pending until /2fa/challenge, and %s would refuse it", plugin, name, option, signIn, fallback)
		}
	}
	return nil
}
