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
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// MagicLinkEmailSender sends the magic-link email to the user.
// Implementations handle templating, delivery, and retries.
type MagicLinkEmailSender interface {
	SendMagicLink(ctx context.Context, email, magicLinkURL string) error
}

// MagicLinkConfig configures the MagicLinkPlugin.
type MagicLinkConfig struct {
	// TokenLength is the number of random bytes in the token (default: 32).
	TokenLength int

	// TokenTTL is how long the magic link remains valid (default: 15 minutes).
	TokenTTL time.Duration

	// EmailSender sends the magic link email. If nil, the URL is logged instead (dev mode).
	EmailSender MagicLinkEmailSender

	// TokenStore persists pending tokens. Defaults to an in-memory store,
	// which does NOT survive restarts or scale across replicas, set a
	// durable store (e.g. NewSQLMagicLinkTokenStore(db)) in production.
	TokenStore MagicLinkTokenStore

	// BodyTemplate, when non-nil, transforms the magic-link URL into
	// the full email body before EmailSender.SendMagicLink is called.
	// nil means "send the URL as the entire body" (historical behavior).
	// Note: MagicLinkEmailSender's interface still takes (to, link).
	// Implementations decide whether to wrap the link in HTML; this
	// hook is the bridge for ones that just blast the body verbatim.
	// ctx is the request's, so it carries the reader's locale: render
	// the body from your phrases with it (i18n.Phrase.Text).
	BodyTemplate func(ctx context.Context, url string) string

	// BaseURL is the application base URL used to construct magic links
	// (e.g. "http://localhost:8080").
	BaseURL string

	// OnSuccessURL is the URL to redirect to after successful login (default: "/").
	OnSuccessURL string

	// RateLimit applies a per-IP rate limit to /auth/magic-link/send. It
	// defaults to 10 attempts/min with a 15-minute block (the register
	// floor): send is an unauthenticated email-dispatch primitive, every
	// request mints a token and mails a link. Loosen by passing a config
	// with a large MaxAttempts.
	RateLimit *RateLimiterConfig

	// VerifyRateLimit applies a per-IP rate limit to
	// /auth/magic-link/verify. It defaults to 30 attempts/min with a
	// 5-minute block (the login per-IP floor): verify redeems a secret
	// credential and mints a session. The token is 256-bit random, so
	// this shapes redemption traffic rather than blocking a practical
	// brute force. Same loosening rule as RateLimit.
	VerifyRateLimit *RateLimiterConfig

	// The confirmation screen a link opens comes from AuthConfig.Pages
	// (the plain built-in document when that is nil).

	// DevMode permits the plugin to operate without an EmailSender by
	// logging the magic-link URL to stdout. Without this flag, a nil
	// EmailSender causes /auth/magic-link/send to return 503, refusing
	// to silently log live tokens to production logs (which would let
	// anyone with log read access take over arbitrary accounts).
	DevMode bool
}

func (c *MagicLinkConfig) defaults() {
	if c.TokenLength <= 0 {
		c.TokenLength = 32
	}
	if c.TokenTTL <= 0 {
		c.TokenTTL = 15 * time.Minute
	}
	if c.OnSuccessURL == "" {
		c.OnSuccessURL = "/"
	}
}

// MagicLinkTokenStore is the interface for persisting and consuming magic-link tokens.
// Implementations must ensure RedeemToken is atomic: a token can only be consumed once.
type MagicLinkTokenStore interface {
	// CreateToken generates a new token for the given email, valid for ttl.
	CreateToken(ctx context.Context, email string, ttl time.Duration) (token string, err error)

	// RedeemToken atomically consumes the token and returns the associated email.
	// Must return an error if the token is unknown, already redeemed, or expired.
	RedeemToken(ctx context.Context, token string) (email string, err error)

	// Cleanup removes expired tokens and returns the count purged.
	Cleanup(ctx context.Context) (int, error)
}

// MagicLinkTokenPeeker is the optional MagicLinkTokenStore extension
// that reads a token's email WITHOUT consuming it.
//
// The confirmation page needs it: following a magic link renders "sign
// in as x@y?" before anything is redeemed, and naming the account is the
// whole point, it is how a victim recognises that the link they were
// sent belongs to someone else. A store that does not implement this
// still gets a confirmation step, just an unnamed one.
type MagicLinkTokenPeeker interface {
	PeekToken(ctx context.Context, token string) (email string, err error)
}

// ErrTokenNotFound is returned when a token is unknown, already consumed, or expired.
var ErrTokenNotFound = errors.New("auth: magic-link token not found or expired")

// magicLinkEntry is one stored token.
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

// magicLinkSweepInterval bounds how long abandoned in-memory and SQL token
// rows stay after they expire when later token mints continue.
const magicLinkSweepInterval = 15 * time.Minute

// NewMemoryMagicLinkTokenStore returns a fresh in-memory token store.
func NewMemoryMagicLinkTokenStore() *MemoryMagicLinkTokenStore {
	return &MemoryMagicLinkTokenStore{tokens: make(map[string]*magicLinkEntry)}
}

// CreateToken generates a cryptographically random hex-encoded token, stores it
// with the given email and TTL, and returns the token string.
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
// Returns ErrTokenNotFound if the token is unknown or expired.
func (m *MemoryMagicLinkTokenStore) RedeemToken(_ context.Context, token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.tokens[token]
	if !ok {
		return "", ErrTokenNotFound
	}
	// Always delete, single-use regardless of expiry
	delete(m.tokens, token)

	if !time.Now().Before(entry.expiresAt) {
		return "", ErrTokenNotFound
	}
	return entry.email, nil
}

// PeekToken reads a token's email without consuming it. Implements
// [MagicLinkTokenPeeker] for the confirmation page.
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
// Implements [MagicLinkTokenPurger].
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

// MagicLinkPlugin implements AuthPlugin and AuthPluginRoutes for passwordless
// magic-link authentication.
type MagicLinkPlugin struct {
	config      MagicLinkConfig
	mgr         *AuthManager
	tokenStore  MagicLinkTokenStore
	sendLimit   *RateLimiter
	verifyLimit *RateLimiter
	// basePath is captured at RegisterRoutes so the confirmation page's
	// form can post back to the same path the link was mounted under.
	basePath string

	// lifecycleMu protects the optional token reaper's one-start/one-stop
	// lifecycle when the manager receives repeated lifecycle calls.
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
	// Both endpoints carry default per-IP floors (opt-out like the
	// login/register ones): send is an unauthenticated mail primitive
	// (10/min, the register floor) and verify redeems a secret and mints
	// a session (30/min, the login per-IP floor).
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

// Init stores a reference to the AuthManager and validates the
// configured link origin: BaseURL builds the emailed sign-in URL, so a
// scheme-mis-shaped value fails here rather than shipping in a
// credential-bearing link.
func (p *MagicLinkPlugin) Init(mgr *AuthManager) error {
	if why := invalidLinkBaseURL(p.config.BaseURL); why != "" {
		return fmt.Errorf("auth: magic-link plugin: BaseURL %q %s", p.config.BaseURL, why)
	}
	if p.config.TokenStore == nil {
		if err := refuseInMemoryTokenStore(mgr, "magic-link"); err != nil {
			return err
		}
	}
	p.mgr = mgr
	return nil
}

// refuseInMemoryTokenStore is the production gate for the link plugins
// (magic link, password reset, email verification) running on the
// MemoryMagicLinkTokenStore they mint when no TokenStore is set: a link
// minted on one replica is invalid on the next, and a restart drops every
// pending link. Same posture as the session and 2FA stores.
func refuseInMemoryTokenStore(mgr *AuthManager, plugin string) error {
	cfg := mgr.Config()
	if cfg.DevMode || cfg.AllowInMemoryStores {
		return nil
	}
	return fmt.Errorf("auth: %s plugin: production mode refuses the in-memory token store: a link minted on one replica is invalid on another and a restart drops every pending link; set its TokenStore (e.g. auth.NewSQLMagicLinkTokenStore(db)), or set AuthConfig.AllowInMemoryStores: true to acknowledge a single-node deployment", plugin)
}

// RegisterRoutes mounts the magic-link send and verify endpoints.
func (p *MagicLinkPlugin) RegisterRoutes(r *router.Router, basePath string) {
	p.basePath = basePath
	r.Post(basePath+"/magic-link/send", http.HandlerFunc(p.sendHandler))
	// GET renders the confirmation screen; POST is what actually signs
	// in. Splitting them is what stops a link the attacker generated
	// from signing a victim into the attacker's account on click.
	r.Get(basePath+"/magic-link/verify", http.HandlerFunc(p.confirmHandler))
	r.Post(basePath+"/magic-link/verify", http.HandlerFunc(p.verifyHandler))
}

// OnStart launches the token-store reaper: a ticker calling Cleanup so
// unredeemed rows (anonymous-minted by /magic-link/send and the sibling
// password-reset / email-verification flows sharing the store) do not
// accumulate until the disk fills. The SQL store also sweeps lazily on
// its mint path; this ticker is the belt to that braces and the ONLY
// reaper a memory-backed store gets.
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

// reapExpiredTokens runs one Cleanup pass under a recover guard: tokenStore
// is a host-installable interface, so a panic in a custom implementation
// would otherwise unwind the reaper goroutine and crash the process
// (recovercallback).
func (p *MagicLinkPlugin) reapExpiredTokens() {
	p.reapExpiredTokensContext(context.Background())
}

func (p *MagicLinkPlugin) reapExpiredTokensContext(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			// Scrub before the log (textsafe.Recovered): a panicking
			// host store can carry control/bidi bytes in the value.
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

// OnStop ends the reaper goroutine. It implements AuthPluginOnStop so the
// manager stops it as part of application shutdown.
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

// sendHandler handles POST {basePath}/magic-link/send.
// Accepts {"email":"..."}, creates a token, and dispatches the magic link email.
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
	// Canonicalize at ingestion (#270): the email rides inside the
	// token and comes back out at consume time, so canonical-in means
	// the eventual FindByEmail/CreateUser see canonical too. A
	// decomposed (non-NFC) spelling is refused: it could otherwise
	// mint a token for a twin identity of the same mailbox.
	var cerr error
	body.Email, cerr = p.mgr.canonicalizeEmail(body.Email)
	if cerr != nil {
		writeAuthError(w, r, http.StatusBadRequest, reasonComposedEmail)
		return
	}
	if body.Email == "" {
		writeAuthError(w, r, http.StatusBadRequest, reasonEmailIsRequired)
		return
	}
	// The address rides inside the token and is handed to the sender, so
	// an oversized one is carried all the way into delivery unless it is
	// refused here. Length is independent of whether the account exists,
	// so this opens no enumeration oracle.
	if !emailWithinLimit(w, r, body.Email) {
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
		// Dev mode opt-in: log the link instead of sending. NEVER do this
		// in production, anyone with log read access could grab the
		// token and take over the account.
		// Hash the sensitive bits so the log is greppable for debugging
		// but doesn't expose the live token or full email to anyone with
		// log read access.
		// SECURITY: do not log the live magic-link URL. The URL embeds the
		// raw token, which is a takeover credential, anyone with read
		// access to dev logs could replay it. email_hash + token_hash give
		// enough signal to correlate with the rendered email body.
		slog.Info("magic-link dev",
			"plugin", "magic-link",
			"email_hash", hashedIdentifier(body.Email),
			"token_hash", hashedIdentifier(token))
	default:
		// Fail-closed. The operator forgot to wire EmailSender and didn't
		// opt into DevMode, better a 503 than silently leaking tokens.
		writeAuthError(w, r, http.StatusServiceUnavailable, reasonMagicLinkDeliveryNotConfigured)
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "magiclink.requested",
		Email:  body.Email,
		Remote: remoteHost(r),
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"message": reasonMagicLinkSent.Text(r.Context()),
		"success": true,
	})
}

// ConfirmPageData is what Pages.MagicLinkConfirm renders.
type ConfirmPageData struct {
	// Email is the account the link signs into, or "" when the token
	// store cannot peek without consuming. Showing it is the point: it
	// is how a user notices the link was not meant for them.
	Email string

	// Token must be POSTed back to Action in a field named "token".
	Token string

	// Action is the path the confirmation form submits to.
	Action string

	// CSRFField is a ready-to-embed hidden input carrying the CSRF
	// token, or "" when the route is not behind CSRF middleware.
	//
	// A Pages implementation MUST render this inside its form. Without it,
	// with auth.CSRF() mounted (WithBFFPosture does this app-wide), the
	// only button on the confirmation page returns 403 and passwordless
	// sign-in is unreachable.
	CSRFField template.HTML

	// Words are the screen's words in the caller's locale.
	Words ConfirmWords
}

// confirmHandler handles GET {basePath}/magic-link/verify?token=xxx.
//
// It renders a confirmation screen and DOES NOT sign anyone in. The link
// is itself the credential, so an attacker can request one for their own
// account and get a victim to open it, a GET that minted a session put
// the victim's browser into the attacker's account silently, and
// everything they did next landed there. A GET is also invisible to
// rejectCrossSiteForm.
//
// The usual browser-binding fixes do not fit this flow: a binding cookie
// breaks cross-device links (request on a laptop, open on a phone), and
// refusing Sec-Fetch-Site: cross-site breaks every webmail client. A
// confirmation step keeps both working and still makes signing in a
// deliberate act.
func (p *MagicLinkPlugin) confirmHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeAuthError(w, r, http.StatusUnauthorized, reasonTokenIsRequired)
		return
	}
	// Peek, never redeem: the token has to survive for the POST, and a
	// GET that consumed it would let a prefetcher or a link-scanner burn
	// the user's link before they ever saw the page.
	email := ""
	if peeker, ok := p.tokenStore.(MagicLinkTokenPeeker); ok {
		got, err := peeker.PeekToken(r.Context(), token)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
			return
		}
		// A token from another flow reads as unknown here rather than
		// naming an account: the confirmation page would otherwise
		// display a reset token's userID as the address to sign in as.
		addr, ok := peekPurposePayload(got, purposeMagicLink)
		if !ok {
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
			return
		}
		email = addr
	}

	// Read the token from the context first: this GET may be the very
	// request that mints the CSRF cookie, in which case it is not yet in
	// r.Cookies(). Fall back to the cookie for a route whose middleware
	// stashed nothing on ctx.
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

// verifyHandler handles POST {basePath}/magic-link/verify.
//
// Redeems the token, finds or creates the user, mints a session, sets
// the session cookie, and redirects to OnSuccessURL. Reached only from
// the confirmation page confirmHandler renders.
func (p *MagicLinkPlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	if p.verifyLimit != nil && !p.verifyLimit.guard(w, r) {
		return
	}
	// The attacker holds the token, so they could auto-submit this form
	// from their own page and skip the confirmation entirely. Same
	// Fetch-Metadata gate the password form uses.
	if rejectCrossSiteForm(w, r) {
		return
	}
	// Cap the body before any form parse: FormValue/ParseForm would
	// otherwise read up to the stdlib's 10 MiB floor into memory.
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
	if err := r.ParseForm(); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeAuthError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return
		}
		writeAuthError(w, r, http.StatusBadRequest, i18n.ReasonInvalidForm)
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

	// Find or create user
	userStore := p.mgr.UserStore()
	if userStore == nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserStoreNotConfigured)
		return
	}

	user, hash, findErr := userStore.FindByEmail(r.Context(), email)
	if findErr != nil && !errors.Is(findErr, ErrUserNotFound) {
		// Any non-NotFound error is a transport / DB failure. We must
		// NOT silently auto-create a user, that would mask the failure
		// AND let an attacker provoke account creation by tripping a
		// transient lookup error.
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
		return
	}
	created, claimed := false, false
	if findErr != nil {
		// User doesn't exist, auto-create. Prefer the OAuthUserCreator
		// path so password_set=false is recorded for the unlink guard;
		// fall back to CreateUser+placeholder for stores that don't opt
		// in. Either way, magic-link users never authenticate via
		// password and the placeholder bcrypt step is reused.
		var err error
		if creator, ok := userStore.(OAuthUserCreator); ok {
			user, err = creator.CreateUserNoPassword(r.Context(), email, p.mgr.DefaultRoles())
		} else {
			user, err = userStore.CreateUser(r.Context(), email, passwordPlaceholderHash, p.mgr.DefaultRoles())
		}
		if errors.Is(err, ErrEmailTaken) {
			// A concurrent redeem for the same address created it
			// between the lookup and the create. This token is spent,
			// so take the existing-account path below instead of
			// answering 500.
			user, hash, err = userStore.FindByEmail(r.Context(), email)
			if err != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
				return
			}
		} else if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToCreateUser)
			return
		} else {
			created = true
		}
	}
	if created {
		// The redeemed token proved the mailbox. A failure leaves the
		// account unverified and the next magic link claims it, which
		// is a no-op on an account nothing else has touched.
		if verifier, ok := userStore.(EmailVerifier); ok {
			if err := verifier.MarkEmailVerified(r.Context(), user.GetID()); err != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToMarkEmailVerified)
				return
			}
		}
	} else {
		// The address may have been registered by someone who never
		// proved it, with a password and sessions of their own. The
		// redeemed token is the mailbox owner's proof, so an unverified
		// account is claimed: everything the registrant attached is
		// evicted before the owner's session is minted. A store that
		// cannot say whether the address was proven is refused rather
		// than guessed at either way: "verified" signs the owner into
		// the squatter's live account, "unverified" wipes a real
		// owner's password on every magic link.
		checker, ok := userStore.(EmailVerifiedChecker)
		if !ok {
			writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoVerifiedChecker)
			return
		}
		verified, err := checker.IsEmailVerified(r.Context(), user.GetID())
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
			return
		}
		if !verified {
			if err := p.mgr.claimAccount(r.Context(), user.GetID(), "magic_link", remoteHost(r), true); err != nil {
				slog.Warn("magic-link account claim failed",
					"plugin", "magic-link", "user_hash", hashedIdentifier(user.GetID()), "err", err)
				writeAuthError(w, r, http.StatusInternalServerError, reasonAccountClaimFailed)
				return
			}
			claimed = true
		}
	}
	if created || claimed {
		// This request wrote the credential itself; the one to hold the
		// session to is what it wrote.
		if _, hash, err = userStore.FindByEmail(r.Context(), email); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
			return
		}
	}

	// Mint through the manager, not SessionStore().Create: MintSession is
	// what applies the second-factor pending mark. Creating the session
	// directly here left PendingTwoFactor=false for a 2FA-enrolled user,
	// so a magic link alone produced a fully-privileged session that
	// could then disable the factor.
	cfg := p.mgr.Config()
	sess, _, err := p.mgr.MintSession(r.Context(), user.GetID(), cfg.SessionTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonSessionCreateFailed)
		return
	}
	// A password reset or change that finished after the link was spent
	// purged this user's sessions and tokens before this session existed.
	// Re-read the credential now the session is stored, as login does: a
	// purge from here on reaches it, and a credential that already moved
	// means the reset outranks the link.
	if !credentialUnchanged(r.Context(), userStore, email, user.GetID(), hash) {
		if err := p.mgr.SessionStore().Delete(r.Context(), sess.Token); err != nil {
			slog.Warn("auth: dropping a magic-link session minted under a stale credential failed",
				"user_hash", hashedIdentifier(user.GetID()), "err", err)
		}
		writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
		return
	}
	// Set session cookie through the one mint helper: SameSite=Strict,
	// the login mint's posture. The flow needed no cross-site return
	// trip for the SESSION cookie (the confirmation POST that mints it
	// is same-site), so Lax bought nothing and re-opened the CSRF
	// exposure Strict closed.
	mintSessionCookie(w, cfg, sess.Token, sess.ExpiresAt)

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "magiclink.consumed",
		UserID: user.GetID(),
		Email:  email,
		Remote: remoteHost(r),
	})

	http.Redirect(w, r, safeRedirectURL(p.config.OnSuccessURL), http.StatusFound)
}

// generateRandomPassword creates a hex-encoded random string of the given byte length.
func generateRandomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// safeRedirectURL prevents open-redirect attacks by ensuring the URL is
// a same-origin path, falling back to "/" when it is not. The grammar
// is handler.IsSafeRelativePath (the battery's one safe-relative validator):
// raw backslash, percent-encoded backslash/control bytes, and C0
// refusal included — browsers normalise '\' to '/' and decode
// percent-escapes before navigating, so anything weaker re-opens the
// cross-origin and header-injection shapes the form redirects already
// refuse.
func safeRedirectURL(u string) string {
	if handler.IsSafeRelativePath(u) {
		return u
	}
	return "/"
}
