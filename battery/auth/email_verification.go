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
// "anything you want" — these plugins build the URL and let the caller
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
	// (does not survive restart / scale) — set a durable store
	// (e.g. NewSQLMagicLinkTokenStore(db)) in production.
	TokenStore MagicLinkTokenStore
	// DevMode logs the verification URL when EmailSender is nil. NEVER
	// enable in production — anyone with log read access then takes
	// over arbitrary accounts.
	DevMode bool
	// RateLimit, when non-nil, applies a per-IP limit to send-verification.
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
	p := &EmailVerificationPlugin{
		cfg:   cfg,
		store: store,
	}
	if cfg.RateLimit != nil {
		p.limit = NewRateLimiter(*cfg.RateLimit)
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
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid session")
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

	tok, err := p.store.CreateToken(r.Context(), user.GetID(), p.cfg.TokenTTL)
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
		// the raw token, which is a takeover credential — anyone with
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
	userID, err := p.store.RedeemToken(r.Context(), tok)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	verifier, ok := p.mgr.UserStore().(EmailVerifier)
	if !ok {
		// The store doesn't expose MarkEmailVerified — refuse rather
		// than silently no-op; the operator wired the wrong store.
		writeAuthError(w, http.StatusInternalServerError,
			"user store does not implement EmailVerifier")
		return
	}
	if err := verifier.MarkEmailVerified(r.Context(), userID); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "mark verified failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"verified": true})
}
