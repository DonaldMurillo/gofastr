package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// PasswordSetter is the optional UserStore extension used by the
// PasswordResetPlugin to persist a new bcrypt hash for an existing
// user. The store implementation is responsible for validating the user
// exists; a nil error means the password was updated.
type PasswordSetter interface {
	SetPassword(ctx context.Context, userID, hashedPassword string) error
}

// ─── Password Reset Plugin ──────────────────────────────────────────────────

// PasswordResetConfig configures the plugin.
type PasswordResetConfig struct {
	// BaseURL is the application URL used to construct the reset link
	// emailed to the user (e.g. "https://app.example.com").
	BaseURL string

	// TokenTTL is the reset link's lifetime. Default: 1h. Short by design,
	// reset tokens are a transient secret and short lifetimes limit the
	// damage if logs / referer headers leak them.
	TokenTTL time.Duration

	// EmailSender sends the reset email. If nil, DevMode must be set or
	// /auth/forgot-password fails closed (it still returns 200 to avoid
	// account enumeration, but no email is sent).
	EmailSender EmailSender

	// TokenStore persists pending reset tokens. Defaults to in-memory (does
	// not survive restart / scale across replicas), set a durable store
	// (e.g. NewSQLMagicLinkTokenStore(db)) in production.
	TokenStore MagicLinkTokenStore

	// BodyTemplate, when non-nil, transforms the reset URL into the
	// full email body before EmailSender.Send is called. nil means
	// "send the URL as the entire body" (the historical behavior).
	BodyTemplate func(url string) string

	// DevMode logs the reset URL when EmailSender is nil. NEVER enable in
	// production, anyone with log read access then resets arbitrary
	// passwords. The log entry uses hashed identifiers to limit exposure,
	// but the URL itself is the secret.
	DevMode bool

	// RateLimit applies a per-IP limit to both endpoints. It defaults to
	// 10 attempts/min with a 15-minute block (the register floor);
	// loosen by passing a config with a large MaxAttempts.
	RateLimit *RateLimiterConfig
}

// PasswordResetPlugin wires:
//   - POST /auth/forgot-password (unauthenticated; takes {email}; sends a
//     reset link if the email exists; ALWAYS returns 200 to avoid leaking
//     account existence).
//   - POST /auth/reset-password (takes {token, password}; verifies the
//     token; updates the user's password).
type PasswordResetPlugin struct {
	cfg             PasswordResetConfig
	mgr             *AuthManager
	store           MagicLinkTokenStore
	limit           *RateLimiter
	deliveryQueue   chan passwordResetDelivery
	deliveryMu      sync.Mutex
	deliveryStarted bool
	deliveryStopped bool
	deliveryCtx     context.Context
	deliveryCancel  context.CancelFunc
	deliveryWG      sync.WaitGroup
}

type passwordResetDelivery struct {
	ctx      context.Context
	to       string
	resetURL string
}

// PasswordResetPlugin participates in AuthManager shutdown to drain its
// asynchronous email workers.
var _ AuthPluginOnStop = (*PasswordResetPlugin)(nil)

const (
	passwordResetDeliveryQueueCapacity = 1024
	passwordResetDeliveryWorkers       = 8
	passwordResetDeliveryTimeout       = 30 * time.Second
)

// NewPasswordResetPlugin builds the plugin with sensible defaults.
func NewPasswordResetPlugin(cfg PasswordResetConfig) *PasswordResetPlugin {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = time.Hour
	}
	store := cfg.TokenStore
	if store == nil {
		store = NewMemoryMagicLinkTokenStore()
	}
	// Default per-IP throttle shared by both endpoints, the register
	// floor: forgot-password is an unauthenticated email-dispatch
	// primitive (every known-email request mints a token and sends mail)
	// and reset-password is a secret-checking surface. Opt out by passing
	// a config with a large MaxAttempts.
	if cfg.RateLimit == nil {
		cfg.RateLimit = &RateLimiterConfig{
			MaxAttempts:   10,
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
	p := &PasswordResetPlugin{
		cfg:           cfg,
		store:         store,
		limit:         newScopedRateLimiter(*cfg.RateLimit, "password_reset"),
		deliveryQueue: make(chan passwordResetDelivery, passwordResetDeliveryQueueCapacity),
	}
	return p
}

func (p *PasswordResetPlugin) Name() string { return "password-reset" }

func (p *PasswordResetPlugin) Init(mgr *AuthManager) error {
	if why := invalidLinkBaseURL(p.cfg.BaseURL); why != "" {
		return fmt.Errorf("auth: password-reset plugin: BaseURL %q %s", p.cfg.BaseURL, why)
	}
	p.mgr = mgr
	return nil
}

func (p *PasswordResetPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/forgot-password", http.HandlerFunc(p.forgotHandler))
	r.Post(basePath+"/reset-password", http.HandlerFunc(p.resetHandler))
}

// burnUnknownBranchWork spends, for an address with no account, the same
// store work the known branch spends. No mail is sent: an address that
// never registered here must not receive anything, which rules out the
// usual "notify anyway" answer to the timing question.
//
// What remains asymmetric is the send, and it is kept off the timed path
// (see deliverResetEmail) so the client cannot clock it. The decoy is bound
// to a payload no user id can equal, so it is inert even if someone fishes
// it out of the store.
func (p *PasswordResetPlugin) burnUnknownBranchWork(r *http.Request) {
	if _, err := createPurposeToken(r.Context(), p.store, purposeReset, "\x00no-account", p.cfg.TokenTTL); err != nil {
		slog.Warn("password-reset decoy token mint failed",
			"plugin", "password-reset", "err", err)
	}
}

func (p *PasswordResetPlugin) forgotHandler(w http.ResponseWriter, r *http.Request) {
	if p.limit != nil && !p.limit.guard(w, r) {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	// Canonicalize BEFORE the uniform-response defer is armed: the 400
	// for a decomposed (non-NFC) spelling depends only on the input's
	// form, never on whether the account exists, so it opens no oracle —
	// but it must not fall through the deferred uniform write below,
	// which would double-write the response. #270: lookups and token
	// payloads use canonical identity.
	{
		ce, cerr := p.mgr.canonicalizeEmail(body.Email)
		if cerr != nil {
			writeAuthError(w, http.StatusBadRequest, errComposedEmailMessage)
			return
		}
		body.Email = ce
	}

	// ALWAYS return 200, even when email is empty or unknown, so the
	// response can't be used to enumerate registered accounts.
	// The response is written before any delivery runs (see the switch
	// below): the client's clock must not be able to tell the branches
	// apart, and delivery is the one asymmetry policy will not let us
	// remove — an address with no account here gets nothing.
	defer func() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}()

	if !emailWithinLimit(w, body.Email) {
		return
	}
	store := p.mgr.UserStore()
	if store == nil {
		return
	}
	user, _, err := store.FindByEmail(r.Context(), body.Email)
	if err != nil {
		// Either ErrUserNotFound (don't leak) or transport error (don't
		// leak either, operator monitors for transport failures via
		// metrics, not via the user-facing response).
		// Unknown email OR transport error, record the request anyway so
		// probing is visible in the audit trail. UserID stays empty
		// (anti-enumeration); the response is identical either way.
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "password.reset_requested",
			Email:  body.Email,
			Remote: remoteHost(r),
			Meta:   map[string]string{"known": "false"},
		})
		// The uniform 200 is only half the defence. This branch used to
		// return here having done nothing, while the known branch minted
		// a token and sent an email — so the two answered in visibly
		// different times and account existence stayed readable by clock
		// (CWE-208). Both branches now do the same work.
		//
		// Nothing is mailed: an address with no account here must not
		// receive anything from us. Parity is bought with the store work
		// instead, and the known branch's send is moved off the timed
		// path so the remaining difference is not observable either.
		p.burnUnknownBranchWork(r)
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "password.reset_requested",
		UserID: user.GetID(),
		Email:  body.Email,
		Remote: remoteHost(r),
		Meta:   map[string]string{"known": "true"},
	})

	tok, err := createPurposeToken(r.Context(), p.store, purposeReset, user.GetID(), p.cfg.TokenTTL)
	if err != nil {
		return
	}
	resetURL := fmt.Sprintf("%s%s/reset-password?token=%s",
		p.cfg.BaseURL, p.mgr.Config().BasePath, tok)

	switch {
	case p.cfg.EmailSender != nil:
		// Keep the email provider and host-supplied body template off the
		// request path. Otherwise the known-account branch takes as long as
		// delivery while the unknown-account branch returns after token-store
		// work, making the uniform response a timing oracle.
		p.queueResetEmail(r.Context(), user.GetEmail(), resetURL)
	case p.cfg.DevMode:
		// SECURITY: do not log the live reset URL. The URL embeds the
		// raw token, which is a takeover credential, anyone with read
		// access to dev logs could replay it. email_hash + token_hash give
		// enough signal to correlate with the rendered email body.
		slog.Info("password-reset dev",
			"plugin", "password-reset",
			"email_hash", hashedIdentifier(user.GetEmail()),
			"token_hash", hashedIdentifier(tok))
	default:
		// Operator hasn't wired email; refuse silently (status is still 200
		// to preserve no-enumeration). This IS a known footgun: in this
		// posture, the password-reset flow is non-functional in production
		// without anyone noticing. Document it.
	}
}

// queueResetEmail schedules delivery without tying the uniform forgot-password
// response to provider latency. The bounded queue and fixed worker count keep a
// flood from creating an unbounded number of goroutines. Request cancellation
// is detached because the client has already received its response; each
// provider call still gets a bounded deadline.
func (p *PasswordResetPlugin) queueResetEmail(requestCtx context.Context, to, resetURL string) {
	delivery := passwordResetDelivery{
		ctx:      context.WithoutCancel(requestCtx),
		to:       to,
		resetURL: resetURL,
	}

	p.deliveryMu.Lock()
	if p.deliveryStopped {
		p.deliveryMu.Unlock()
		slog.Warn("password-reset email delivery stopped",
			"plugin", "password-reset",
			"email_hash", hashedIdentifier(to))
		return
	}
	if !p.deliveryStarted {
		p.deliveryStarted = true
		p.deliveryCtx, p.deliveryCancel = context.WithCancel(context.Background())
		for i := 0; i < passwordResetDeliveryWorkers; i++ {
			p.deliveryWG.Add(1)
			go p.runResetEmailWorker()
		}
	}
	queued := false
	select {
	case p.deliveryQueue <- delivery:
		queued = true
	default:
	}
	p.deliveryMu.Unlock()

	if !queued {
		slog.Warn("password-reset email delivery queue full",
			"plugin", "password-reset",
			"email_hash", hashedIdentifier(to))
	}
}

// OnStop cancels active deliveries, closes the queue, and waits for workers to
// exit. The caller's context bounds shutdown when a custom sender ignores
// cancellation.
func (p *PasswordResetPlugin) OnStop(ctx context.Context) error {
	p.deliveryMu.Lock()
	if !p.deliveryStopped {
		p.deliveryStopped = true
		if p.deliveryStarted {
			p.deliveryCancel()
			close(p.deliveryQueue)
		}
	}
	started := p.deliveryStarted
	p.deliveryMu.Unlock()

	if !started {
		return nil
	}
	done := make(chan struct{})
	go func() {
		p.deliveryWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *PasswordResetPlugin) runResetEmailWorker() {
	defer p.deliveryWG.Done()
	for delivery := range p.deliveryQueue {
		if p.deliveryCtx.Err() == nil {
			p.sendResetEmail(delivery)
		}
	}
}

func (p *PasswordResetPlugin) sendResetEmail(delivery passwordResetDelivery) {
	defer func() {
		if recover() != nil {
			slog.Error("password-reset email delivery panicked",
				"plugin", "password-reset",
				"email_hash", hashedIdentifier(delivery.to))
		}
	}()

	ctx, cancel := context.WithTimeout(delivery.ctx, passwordResetDeliveryTimeout)
	stopCancel := context.AfterFunc(p.deliveryCtx, cancel)
	defer stopCancel()
	defer cancel()

	body := delivery.resetURL
	if p.cfg.BodyTemplate != nil {
		body = p.cfg.BodyTemplate(delivery.resetURL)
	}
	if err := p.cfg.EmailSender.Send(ctx, delivery.to, body); err != nil {
		// The client still receives the anti-enumeration 200. Log hashed
		// identifiers only: the URL embeds the takeover token.
		slog.Warn("password-reset email send failed",
			"plugin", "password-reset",
			"email_hash", hashedIdentifier(delivery.to),
			"err", err)
	}
}

func (p *PasswordResetPlugin) resetHandler(w http.ResponseWriter, r *http.Request) {
	if p.limit != nil && !p.limit.guard(w, r) {
		return
	}
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !decodeJSONLimited(w, r, &body) {
		return
	}
	if body.Token == "" || body.Password == "" {
		writeAuthError(w, http.StatusBadRequest, "token and password required")
		return
	}
	if len(body.Password) > 128 {
		writeAuthError(w, http.StatusBadRequest, "password too long")
		return
	}

	// Validate everything that can fail BEFORE consuming the single-use token.
	// RedeemToken atomically deletes the token, so any failure after it strands
	// the user, they'd have to restart the whole forgot-password flow for a new
	// emailed token. The token is only burned once the inputs are known-good and
	// immediately before SetPassword.
	setter, ok := p.mgr.UserStore().(PasswordSetter)
	if !ok {
		writeAuthError(w, http.StatusInternalServerError,
			"user store does not implement PasswordSetter")
		return
	}

	if err := ValidatePasswordStrength(body.Password); err != nil {
		writeAuthError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := HashPassword(body.Password)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "hash failed")
		return
	}

	userID, err := redeemPurposeToken(r.Context(), p.store, purposeReset, body.Token)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	if err := setter.SetPassword(r.Context(), userID, hash); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			writeAuthError(w, http.StatusNotFound, "user not found")
			return
		}
		writeAuthError(w, http.StatusInternalServerError, "set password failed")
		return
	}

	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "password.reset_completed",
		UserID: userID,
		Remote: remoteHost(r),
	})

	// Revoke every pre-existing session for this user. A credential that was
	// compromised before the reset must not retain access through an already-
	// issued cookie, the whole point of a reset is to lock the attacker out.
	// Stores that don't implement SessionUserPurger leave the window open; log
	// that so the gap is visible rather than silent.
	//
	// The user's OTHER outstanding reset links go first. Revoking sessions
	// but leaving those spendable means a link phished or read from the
	// mailbox an hour ago still sets the password again after the victim's
	// own reset completes, and the account returns to the attacker.
	if n, supported, err := purgePurposeTokens(r.Context(), p.store, purposeReset, userID); err != nil {
		slog.Warn("password-reset sibling token purge failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
	} else if !supported {
		slog.Warn("password-reset could not drop sibling reset tokens: token store does not implement MagicLinkTokenPurger",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID))
	} else if n > 0 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "reset_tokens.purged",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"reason": "password_reset", "count": strconv.Itoa(n)},
		})
	}
	if purger, ok := p.mgr.SessionStore().(SessionUserPurger); ok {
		if n, err := purger.DeleteByUser(r.Context(), userID); err != nil {
			slog.Warn("password-reset session revocation failed",
				"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		} else {
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "session.revoked",
				UserID: userID,
				Remote: remoteHost(r),
				Meta:   map[string]string{"reason": "password_reset", "count": strconv.Itoa(n)},
			})
		}
	} else {
		slog.Warn("password-reset could not revoke existing sessions: session store does not implement SessionUserPurger",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID))
	}
	// API tokens are credentials too: one minted by whoever held the
	// account before the reset kept working after it.
	if n, supported, err := p.mgr.revokeUserAPITokens(r.Context(), userID); err != nil {
		slog.Warn("password-reset API token revocation failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
	} else if supported && n > 0 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "token.revoked",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"reason": "password_reset", "count": strconv.Itoa(n)},
		})
	}
	// The reset link reached the mailbox, so it is the owner's proof. On an
	// account nobody had proven, someone else may have attached an IdP
	// identity; the claim evicts it (the password is already the owner's
	// new one, so it stays) and marks the address verified. Stores without
	// EmailVerifiedChecker do not track the state and are left as they are.
	// The claim runs after the password write on purpose: run first, a
	// failed write would leave the account verified under the old
	// password. A failed claim leaves it unverified, so the next mailbox
	// proof runs the claim again.
	if checker, ok := p.mgr.UserStore().(EmailVerifiedChecker); ok {
		verified, err := checker.IsEmailVerified(r.Context(), userID)
		if err == nil && !verified {
			err = p.mgr.claimAccount(r.Context(), userID, "password_reset", remoteHost(r), false)
		}
		if err != nil {
			slog.Warn("password-reset account claim failed",
				"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
			writeAuthError(w, http.StatusInternalServerError, "password updated, but securing the account failed: request another reset link")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
}
