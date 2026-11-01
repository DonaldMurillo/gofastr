package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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

// PasswordSwapper is the optional PasswordSetter extension a password
// change writes through. SwapPassword sets newHash only while the stored
// hash is still oldHash, in one statement, and reports whether it wrote.
// EntityUserStore implements it. A store without it gets a re-read just
// before the write, which leaves a short window a reset can still land in.
type PasswordSwapper interface {
	SwapPassword(ctx context.Context, userID, oldHash, newHash string) (bool, error)
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
	// ctx is the request's (for a reset, a detached copy that keeps its
	// values), so it carries the reader's locale: render the body from
	// your phrases with it (i18n.Phrase.Text).
	BodyTemplate func(ctx context.Context, url string) string

	// NoticeTemplate, when non-nil, builds the body of the email a
	// completed reset sends to the account's address. unlinked names the
	// sign-in providers the reset removed (a reset unlinks every OAuth
	// identity, so one planted from a stolen session cannot sign back
	// in). nil sends the default text from this package's phrases. The
	// notice goes through EmailSender and is skipped when it is nil.
	NoticeTemplate func(ctx context.Context, unlinked []string) string

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
	cfg      PasswordResetConfig
	mgr      *AuthManager
	store    MagicLinkTokenStore
	limit    *RateLimiter
	delivery *mailQueue
}

// PasswordResetPlugin participates in AuthManager shutdown to drain its
// asynchronous email workers.
var _ AuthPluginOnStop = (*PasswordResetPlugin)(nil)

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
		cfg:      cfg,
		store:    store,
		limit:    newScopedRateLimiter(*cfg.RateLimit, "password_reset"),
		delivery: newMailQueue("password-reset"),
	}
	return p
}

func (p *PasswordResetPlugin) Name() string { return "password-reset" }

func (p *PasswordResetPlugin) Init(mgr *AuthManager) error {
	if why := invalidLinkBaseURL(p.cfg.BaseURL); why != "" {
		return fmt.Errorf("auth: password-reset plugin: BaseURL %q %s", p.cfg.BaseURL, why)
	}
	if p.cfg.TokenStore == nil {
		if err := refuseInMemoryTokenStore(mgr, "password-reset"); err != nil {
			return err
		}
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
			writeAuthError(w, r, http.StatusBadRequest, reasonComposedEmail)
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

	if !emailWithinLimit(w, r, body.Email) {
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
// response to provider latency, through the shared mailQueue: a bounded queue
// and a fixed worker count keep a flood from creating an unbounded number of
// goroutines, request cancellation is detached because the client has already
// received its response, and each provider call still gets a bounded deadline.
func (p *PasswordResetPlugin) queueResetEmail(requestCtx context.Context, to, resetURL string) {
	p.delivery.enqueue(requestCtx, to, func(ctx context.Context) {
		body := resetURL
		if p.cfg.BodyTemplate != nil {
			body = p.cfg.BodyTemplate(ctx, resetURL)
		}
		if err := p.cfg.EmailSender.Send(ctx, to, body); err != nil {
			// The client still receives the anti-enumeration 200. Log hashed
			// identifiers only: the URL embeds the takeover token.
			slog.Warn("password-reset email send failed",
				"plugin", "password-reset",
				"email_hash", hashedIdentifier(to),
				"err", err)
		}
	})
}

// OnStop cancels active deliveries, closes the queue, and waits for workers to
// exit. The caller's context bounds shutdown when a custom sender ignores
// cancellation.
func (p *PasswordResetPlugin) OnStop(ctx context.Context) error {
	return p.delivery.stop(ctx)
}

// dropLinks spends every outstanding reset link of the user, so none
// sets the password again after a reset or a change. A token store
// without MagicLinkTokenPurger keeps them, and that gap is logged.
func (p *PasswordResetPlugin) dropLinks(ctx context.Context, userID, reason, remote string) {
	n, supported, err := purgePurposeTokens(ctx, p.store, purposeReset, userID)
	switch {
	case err != nil:
		slog.Warn("password-reset sibling token purge failed",
			"plugin", "password-reset", "reason", reason, "user_hash", hashedIdentifier(userID), "err", err)
	case !supported:
		slog.Warn("password-reset could not drop sibling reset tokens: token store does not implement MagicLinkTokenPurger",
			"plugin", "password-reset", "reason", reason, "user_hash", hashedIdentifier(userID))
	case n > 0:
		p.mgr.emitSecurity(ctx, SecurityEvent{
			Kind:   "reset_tokens.purged",
			UserID: userID,
			Remote: remote,
			Meta:   map[string]string{"reason": reason, "count": strconv.Itoa(n)},
		})
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
		writeAuthError(w, r, http.StatusBadRequest, reasonTokenAndPasswordRequired)
		return
	}
	if len(body.Password) > 128 {
		writeAuthError(w, r, http.StatusBadRequest, reasonPasswordTooLong)
		return
	}

	// Validate everything that can fail BEFORE consuming the single-use token.
	// RedeemToken atomically deletes the token, so any failure after it strands
	// the user, they'd have to restart the whole forgot-password flow for a new
	// emailed token. The token is only burned once the inputs are known-good and
	// immediately before SetPassword.
	setter, ok := p.mgr.UserStore().(PasswordSetter)
	if !ok {
		writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoPasswordSetter)
		return
	}

	if err := ValidatePasswordStrength(body.Password); err != nil {
		writeAuthError(w, r, http.StatusBadRequest, reasonPasswordTooShort)
		return
	}

	hash, err := HashPassword(body.Password)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonHashFailed)
		return
	}

	userID, err := redeemPurposeToken(r.Context(), p.store, purposeReset, body.Token)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
		return
	}
	if err := setter.SetPassword(r.Context(), userID, hash); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			writeAuthError(w, r, http.StatusNotFound, reasonUserNotFound)
			return
		}
		writeAuthError(w, r, http.StatusInternalServerError, reasonSetPasswordFailed)
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
	p.dropLinks(r.Context(), userID, "password_reset", remoteHost(r))
	// Passkeys go before sessions, as an account claim orders them. A
	// passkey sign-in re-reads its key after minting; with the revoke
	// first, one that minted after it and re-read before the purge kept
	// its session. Purged first, a later mint finds the key gone and an
	// earlier one is caught by the revoke.
	// One added from a stolen session signs in again after the reset
	// otherwise.
	//
	// A failed purge does not stop the ones after it: returning on the
	// passkey error left every session, magic link and API token live
	// behind a reset that answered 500. Each purge runs, and any failure
	// answers 500 once they all have; a fresh reset link retries safely.
	incomplete := false
	if n, err := p.mgr.purgeUserPasskeys(r.Context(), userID); err != nil {
		slog.Warn("password-reset passkey removal failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	} else if n > 0 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "passkey.deleted",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"reason": "password_reset", "count": strconv.Itoa(n)},
		})
	}
	// A verified account keeps no IdP identity either: one linked from a
	// stolen session signs the attacker back in after the owner's reset.
	// The notice below names what was removed, so the owner can link a
	// provider of their own again. Unlinked before the session revoke, the
	// same order as the passkeys: an IdP sign-in that lands mid-unlink
	// mints a session the revoke then catches.
	var unlinked []string
	lister, unlinker, err := oauthUnlinkTools(p.mgr.UserStore())
	if err == nil {
		unlinked, err = p.mgr.unlinkAllOAuth(r.Context(), userID, lister, unlinker)
	}
	for _, provider := range unlinked {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "oauth.unlinked",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"provider": provider, "reason": "password_reset"},
		})
	}
	if err != nil {
		slog.Warn("password-reset OAuth unlink failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	}
	// A failed purge left the attacker's live cookie working behind a
	// reset that answered success.
	if err := p.mgr.revokeUserSessions(r.Context(), userID, "password_reset", remoteHost(r)); err != nil {
		incomplete = true
	}
	// A magic link mailed before the reset signs in like a session does.
	if err := p.mgr.dropMagicLinks(r.Context(), userID, "password_reset", remoteHost(r)); err != nil {
		slog.Warn("password-reset magic-link purge failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	}
	// API tokens are credentials too: one minted by whoever held the
	// account before the reset kept working after it.
	if n, supported, err := p.mgr.revokeUserAPITokens(r.Context(), userID); err != nil {
		slog.Warn("password-reset API token revocation failed",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		incomplete = true
	} else if supported && n > 0 {
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind:   "token.revoked",
			UserID: userID,
			Remote: remoteHost(r),
			Meta:   map[string]string{"reason": "password_reset", "count": strconv.Itoa(n)},
		})
	}
	if incomplete {
		writeAuthError(w, r, http.StatusInternalServerError, reasonResetIncomplete)
		return
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
			writeAuthError(w, r, http.StatusInternalServerError, reasonResetIncomplete)
			return
		}
	}

	p.queueResetNotice(r.Context(), userID, unlinked)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
}

// queueResetNotice mails the account's address that its password was reset
// and which sign-in providers the reset unlinked. It is skipped when no
// EmailSender is set, and a lookup or send failure is logged, never
// returned: the reset itself has already succeeded.
func (p *PasswordResetPlugin) queueResetNotice(requestCtx context.Context, userID string, unlinked []string) {
	if p.cfg.EmailSender == nil {
		return
	}
	user, err := p.mgr.UserStore().FindByID(requestCtx, userID)
	if err != nil || user == nil || user.GetEmail() == "" {
		slog.Warn("password-reset notice skipped: no address for the account",
			"plugin", "password-reset", "user_hash", hashedIdentifier(userID), "err", err)
		return
	}
	to := user.GetEmail()
	p.delivery.enqueue(requestCtx, to, func(ctx context.Context) {
		var body string
		if p.cfg.NoticeTemplate != nil {
			body = p.cfg.NoticeTemplate(ctx, unlinked)
		} else {
			body = defaultResetNotice(ctx, unlinked)
		}
		if err := p.cfg.EmailSender.Send(ctx, to, body); err != nil {
			slog.Warn("password-reset notice send failed",
				"plugin", "password-reset", "email_hash", hashedIdentifier(to), "err", err)
		}
	})
}

// defaultResetNotice is the notice body when NoticeTemplate is nil.
func defaultResetNotice(ctx context.Context, unlinked []string) string {
	parts := []string{resetNoticeDone.Text(ctx)}
	if len(unlinked) > 0 {
		parts = append(parts, resetNoticeUnlinked.Text(ctx, map[string]any{"providers": strings.Join(unlinked, ", ")}))
	}
	parts = append(parts, resetNoticeWarning.Text(ctx))
	return strings.Join(parts, "\n\n")
}
