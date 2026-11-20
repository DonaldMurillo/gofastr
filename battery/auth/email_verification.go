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

// EmailSender sends one transactional mail.
type EmailSender interface {
	Send(ctx context.Context, to, body string) error
}

// EmailVerifier marks a user's address verified.
type EmailVerifier interface {
	MarkEmailVerified(ctx context.Context, userID string) error
}


// EmailVerificationConfig configures the plugin.
type EmailVerificationConfig struct {
	// BaseURL is the application URL used to construct the verification link.
	BaseURL string
	// TokenTTL is the verification link's lifetime. Default 24h.
	TokenTTL time.Duration
	// EmailSender mails the link; nil answers 503 unless DevMode is set.
	EmailSender EmailSender
	// BodyTemplate builds the mail body from the link; nil sends the link alone.
	BodyTemplate func(ctx context.Context, url string) string
	// TokenStore keeps verification tokens; nil is an in-memory store, refused in production.
	TokenStore MagicLinkTokenStore
	// DevMode logs hashes of the address and token when EmailSender is nil.
	DevMode bool
	// RateLimit is the per-IP limit on send-verification; nil means 10 a minute with a 15-minute block.
	RateLimit *RateLimiterConfig
}

// EmailVerificationPlugin wires:
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
	r = r.Group("", browserAccountOnly)
	r.Post(basePath+"/send-verification", http.HandlerFunc(p.sendHandler))
	r.Get(basePath+"/verify-email", http.HandlerFunc(p.verifyHandler))
}

func (p *EmailVerificationPlugin) sendHandler(w http.ResponseWriter, r *http.Request) {
	if rejectCrossSiteForm(w, r) {
		return
	}
	if p.limit != nil && !p.limit.guard(w, r) {
		return
	}
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
		return
	}
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
		writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoEmailVerifier)
		return
	}
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		if handler.NeedsSameSiteHop(r) {
			if target, ok := sameOriginTarget(r); ok {
				writeSameSiteHop(w, r, p.mgr.config.Pages, handler.SameSiteHopTarget(r, target), verifyHopTitle)
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
	p.mgr.emitSecurity(r.Context(), SecurityEvent{
		Kind:   "email.verified",
		UserID: userID,
		Remote: remoteHost(r),
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"verified": true})
}
