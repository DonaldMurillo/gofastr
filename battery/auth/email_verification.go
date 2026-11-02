package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// EmailSender is the lightweight interface this package uses for any
// outbound transactional email (verification, password reset). Concrete
// implementations wrap SMTP, SES, Postmark, etc. The body is intentionally
// "anything you want", these plugins build the URL and let the caller
// decide on templating.
type EmailSender interface {
	Send(ctx context.Context, to, body string) error
}

// EmailVerifier is the optional UserStore extension used by the
// EmailVerificationPlugin to mark a user's email as verified.
type EmailVerifier interface {
	MarkEmailVerified(ctx context.Context, userID string) error
}

// ─── Email Verification Plugin ──────────────────────────────────────────────

// EmailVerificationConfig configures the plugin.
type EmailVerificationConfig struct {
	// BaseURL is the application URL used to construct the verification link.
	BaseURL string
	// TokenTTL is the verification link's lifetime. Default 24h.
	TokenTTL time.Duration
	// EmailSender sends the verification message. If nil, DevMode must
	// be set or send-verification fails closed (503).
	EmailSender EmailSender
	// BodyTemplate, when non-nil, transforms the verification URL into
	// the full email body before EmailSender.Send is called. nil means
	// "send the URL as the entire body" (the historical behavior).
	// ctx is the request's, so it carries the reader's locale: render
	// the body from your phrases with it (i18n.Phrase.Text).
	BodyTemplate func(ctx context.Context, url string) string
	// TokenStore persists pending verification tokens. Defaults to in-memory
	// (does not survive restart / scale), set a durable store
	// (e.g. NewSQLMagicLinkTokenStore(db)) in production.
	TokenStore MagicLinkTokenStore
	// DevMode logs the verification URL when EmailSender is nil. NEVER
	// enable in production, anyone with log read access then takes
	// over arbitrary accounts.
	DevMode bool
	// RateLimit applies a per-IP limit to send-verification. It defaults
	// to 10 attempts/min with a 15-minute block (the register floor);
	// loosen by passing a config with a large MaxAttempts.
	RateLimit *RateLimiterConfig
}

// EmailVerificationPlugin wires:
//   - POST /auth/send-verification (authenticated; sends a token to the
//     current user's email).
//   - GET  /auth/verify-email?token=... (consumes a token, marks the
//     user verified; the request must carry that user's own session).
type EmailVerificationPlugin struct {
	cfg   EmailVerificationConfig
	mgr   *AuthManager
	store MagicLinkTokenStore // reusing the magic-link token shape
	limit *RateLimiter
}

// NewEmailVerificationPlugin builds the plugin with sensible defaults.
func NewEmailVerificationPlugin(cfg EmailVerificationConfig) *EmailVerificationPlugin {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = 24 * time.Hour
	}
	store := cfg.TokenStore
	if store == nil {
		store = NewMemoryMagicLinkTokenStore()
	}
	// Default per-IP throttle, the register floor. The endpoint is
	// session-gated, but each request mints a 24h takeover-credential
	// URL and dispatches mail, so one compromised session must not be an
	// unbounded mail primitive. Opt out by passing a config with a large
	// MaxAttempts.
	if cfg.RateLimit == nil {
		cfg.RateLimit = &RateLimiterConfig{
			MaxAttempts:   10,
			Window:        time.Minute,
			BlockDuration: 15 * time.Minute,
		}
	}
	p := &EmailVerificationPlugin{
		cfg:   cfg,
		store: store,
		limit: newScopedRateLimiter(*cfg.RateLimit, "email_verification"),
	}
	return p
}

func (p *EmailVerificationPlugin) Name() string { return "email-verification" }

func (p *EmailVerificationPlugin) Init(mgr *AuthManager) error {
	if why := invalidLinkBaseURL(p.cfg.BaseURL); why != "" {
		return fmt.Errorf("auth: email-verification plugin: BaseURL %q %s", p.cfg.BaseURL, why)
	}
	if p.cfg.TokenStore == nil {
		if err := refuseInMemoryTokenStore(mgr, "email-verification"); err != nil {
			return err
		}
	}
	p.mgr = mgr
	return nil
}

func (p *EmailVerificationPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/send-verification", http.HandlerFunc(p.sendHandler))
	r.Get(basePath+"/verify-email", http.HandlerFunc(p.verifyHandler))
}

func (p *EmailVerificationPlugin) sendHandler(w http.ResponseWriter, r *http.Request) {
	// A bodyless POST is CORS-simple, so a cross-site page can auto-submit
	// it with the victim's session cookie riding along — the same
	// Fetch-Metadata gate every sibling form-mutable handler applies. Each
	// forged POST mints and mails a live 24h takeover-credential URL, so
	// the mail-spam primitive is worth refusing even session-gated.
	if rejectCrossSiteForm(w, r) {
		return
	}
	if p.limit != nil && !p.limit.guard(w, r) {
		return
	}
	// Bound to the context principal when there is one (see
	// AuthManager.requestSession).
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
		return
	}
	// A pending-2FA session has proven the password and nothing else.
	// Its four siblings (2fa enroll / verify / disable / backup-codes)
	// all refuse one; this handler did not, leaving a half-authenticated
	// session able to drive verification-email sends at someone else's
	// address.
	if sess.PendingTwoFactor {
		writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
		return
	}
	store := p.mgr.UserStore()
	if store == nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserStoreNotConfigured)
		return
	}
	user, err := store.FindByID(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
		return
	}

	tok, err := createPurposeToken(r.Context(), p.store, purposeVerify, user.GetID(), p.cfg.TokenTTL)
	if err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonTokenCreateFailed)
		return
	}

	verifyURL := fmt.Sprintf("%s%s/verify-email?token=%s",
		p.cfg.BaseURL, p.mgr.Config().BasePath, tok)

	switch {
	case p.cfg.EmailSender != nil:
		emailBody := verifyURL
		if p.cfg.BodyTemplate != nil {
			emailBody = p.cfg.BodyTemplate(r.Context(), verifyURL)
		}
		if err := p.cfg.EmailSender.Send(r.Context(), user.GetEmail(), emailBody); err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonEmailSendFailed)
			return
		}
	case p.cfg.DevMode:
		// SECURITY: do not log the live verification URL. The URL embeds
		// the raw token, which is a takeover credential, anyone with
		// read access to dev logs could replay it. email_hash +
		// token_hash give enough signal to correlate with the rendered
		// email body.
		slog.Info("email-verification dev",
			"plugin", "email-verification",
			"email_hash", hashedIdentifier(user.GetEmail()),
			"token_hash", hashedIdentifier(tok))
	default:
		writeAuthError(w, r, http.StatusServiceUnavailable, reasonEmailDeliveryNotConfigured)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
}

func (p *EmailVerificationPlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		writeAuthError(w, r, http.StatusBadRequest, reasonTokenRequired)
		return
	}
	verifier, ok := p.mgr.UserStore().(EmailVerifier)
	if !ok {
		// The store doesn't expose MarkEmailVerified, refuse rather
		// than silently no-op; the operator wired the wrong store.
		writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoEmailVerifier)
		return
	}
	// The link proves the mailbox only to the account that asked for it.
	// Whoever registered an address can send this mail to its real owner;
	// a click from the owner's browser, or a mail scanner's prefetch,
	// must not mark the registrant's account verified, or a magic link
	// would no longer claim it and a verified IdP login would link into
	// it. So the request must carry a full session of the token's user.
	// The session check runs before the redeem, so a sessionless prefetch
	// leaves the token for the real click.
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		// A link clicked in webmail is a cross-site navigation, and the
		// browser withholds the SameSite=Strict session cookie on it.
		// Answer it with a page from this origin that loads the same
		// link again: that navigation starts on this site, so the cookie
		// rides it. The hop redeems nothing, and its follow-up is
		// same-origin, so a browser with no session gets this 401 then.
		if handler.IsCrossSiteRequest(r) {
			if target, ok := sameOriginTarget(r); ok {
				writeSameSiteHop(w, r, p.mgr.config.Pages, target, verifyHopTitle)
				return
			}
		}
		writeAuthError(w, r, http.StatusUnauthorized, reasonVerifySignIn)
		return
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
		return
	}
	// A link opened from another account's session stays redeemable for
	// its owner. Stores without a peek fall back to the check after the
	// redeem below.
	if peeker, ok := p.store.(MagicLinkTokenPeeker); ok {
		raw, err := peeker.PeekToken(r.Context(), tok)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
			return
		}
		if owner, ok := peekPurposePayload(raw, purposeVerify); ok && owner != sess.UserID {
			writeAuthError(w, r, http.StatusForbidden, reasonVerifyOtherAccount)
			return
		}
	}
	userID, err := redeemPurposeToken(r.Context(), p.store, purposeVerify, tok)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidToken)
		return
	}
	if userID != sess.UserID {
		writeAuthError(w, r, http.StatusForbidden, reasonVerifyOtherAccount)
		return
	}
	if err := verifier.MarkEmailVerified(r.Context(), userID); err != nil {
		writeAuthError(w, r, http.StatusInternalServerError, reasonMarkVerifiedFailed)
		return
	}
	// The trust-bit flip gates OAuth account binding (an unverified email
	// never links), so the trail must answer "who verified this address,
	// when" — the same reason oauth.linked is recorded.
	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "email.verified",
		UserID: userID,
		Remote: remoteHost(r),
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"verified": true})
}
