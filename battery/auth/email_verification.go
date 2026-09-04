package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

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
	BodyTemplate func(url string) string
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
//     user verified).
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
	cfg := p.mgr.Config()
	cookie, err := r.Cookie(cfg.SessionCookie)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "no session")
		return
	}
	sess, err := p.mgr.SessionStore().Get(r.Context(), cookie.Value)
	if err != nil || sess == nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid session")
		return
	}
	// A pending-2FA session has proven the password and nothing else.
	// Its four siblings (2fa enroll / verify / disable / backup-codes)
	// all refuse one; this handler did not, leaving a half-authenticated
	// session able to drive verification-email sends at someone else's
	// address.
	if sess.PendingTwoFactor {
		writeAuthError(w, http.StatusForbidden, "two-factor verification required")
		return
	}
	store := p.mgr.UserStore()
	if store == nil {
		writeAuthError(w, http.StatusInternalServerError, "user store not configured")
		return
	}
	user, err := store.FindByID(r.Context(), sess.UserID)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}

	tok, err := createPurposeToken(r.Context(), p.store, purposeVerify, user.GetID(), p.cfg.TokenTTL)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "token create failed")
		return
	}

	verifyURL := fmt.Sprintf("%s%s/verify-email?token=%s",
		p.cfg.BaseURL, p.mgr.Config().BasePath, tok)

	switch {
	case p.cfg.EmailSender != nil:
		emailBody := verifyURL
		if p.cfg.BodyTemplate != nil {
			emailBody = p.cfg.BodyTemplate(verifyURL)
		}
		if err := p.cfg.EmailSender.Send(r.Context(), user.GetEmail(), emailBody); err != nil {
			writeAuthError(w, http.StatusInternalServerError, "email send failed")
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
		writeAuthError(w, http.StatusServiceUnavailable, "email delivery not configured")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
}

func (p *EmailVerificationPlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		writeAuthError(w, http.StatusBadRequest, "token required")
		return
	}
	userID, err := redeemPurposeToken(r.Context(), p.store, purposeVerify, tok)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	verifier, ok := p.mgr.UserStore().(EmailVerifier)
	if !ok {
		// The store doesn't expose MarkEmailVerified, refuse rather
		// than silently no-op; the operator wired the wrong store.
		writeAuthError(w, http.StatusInternalServerError,
			"user store does not implement EmailVerifier")
		return
	}
	if err := verifier.MarkEmailVerified(r.Context(), userID); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "mark verified failed")
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
