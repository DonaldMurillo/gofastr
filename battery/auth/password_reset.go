package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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

	// TokenTTL is the reset link's lifetime. Default: 1h. Short by design —
	// reset tokens are a transient secret and short lifetimes limit the
	// damage if logs / referer headers leak them.
	TokenTTL time.Duration

	// EmailSender sends the reset email. If nil, DevMode must be set or
	// /auth/forgot-password fails closed (it still returns 200 to avoid
	// account enumeration, but no email is sent).
	EmailSender EmailSender

	// BodyTemplate, when non-nil, transforms the reset URL into the
	// full email body before EmailSender.Send is called. nil means
	// "send the URL as the entire body" (the historical behavior).
	BodyTemplate func(url string) string

	// DevMode logs the reset URL when EmailSender is nil. NEVER enable in
	// production — anyone with log read access then resets arbitrary
	// passwords. The log entry uses hashed identifiers to limit exposure,
	// but the URL itself is the secret.
	DevMode bool

	// RateLimit, when non-nil, applies a per-IP limit to both endpoints.
	// Strongly recommended in production.
	RateLimit *RateLimiterConfig
}

// PasswordResetPlugin wires:
//   - POST /auth/forgot-password (unauthenticated; takes {email}; sends a
//     reset link if the email exists; ALWAYS returns 200 to avoid leaking
//     account existence).
//   - POST /auth/reset-password (takes {token, password}; verifies the
//     token; updates the user's password).
type PasswordResetPlugin struct {
	cfg   PasswordResetConfig
	mgr   *AuthManager
	store MagicLinkTokenStore
	limit *RateLimiter
}

// NewPasswordResetPlugin builds the plugin with sensible defaults.
func NewPasswordResetPlugin(cfg PasswordResetConfig) *PasswordResetPlugin {
	if cfg.TokenTTL <= 0 {
		cfg.TokenTTL = time.Hour
	}
	p := &PasswordResetPlugin{
		cfg:   cfg,
		store: NewMemoryMagicLinkTokenStore(),
	}
	if cfg.RateLimit != nil {
		p.limit = NewRateLimiter(*cfg.RateLimit)
	}
	return p
}

func (p *PasswordResetPlugin) Name() string { return "password-reset" }

func (p *PasswordResetPlugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	return nil
}

func (p *PasswordResetPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/forgot-password", http.HandlerFunc(p.forgotHandler))
	r.Post(basePath+"/reset-password", http.HandlerFunc(p.resetHandler))
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

	// ALWAYS return 200 — even when email is empty or unknown — so the
	// response can't be used to enumerate registered accounts.
	defer func() {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}()

	if body.Email == "" {
		return
	}
	store := p.mgr.UserStore()
	if store == nil {
		return
	}
	user, _, err := store.FindByEmail(r.Context(), body.Email)
	if err != nil {
		// Either ErrUserNotFound (don't leak) or transport error (don't
		// leak either — operator monitors for transport failures via
		// metrics, not via the user-facing response).
		return
	}

	tok, err := p.store.CreateToken(r.Context(), user.GetID(), p.cfg.TokenTTL)
	if err != nil {
		return
	}
	resetURL := fmt.Sprintf("%s%s/reset-password?token=%s",
		p.cfg.BaseURL, p.mgr.Config().BasePath, tok)

	switch {
	case p.cfg.EmailSender != nil:
		emailBody := resetURL
		if p.cfg.BodyTemplate != nil {
			emailBody = p.cfg.BodyTemplate(resetURL)
		}
		_ = p.cfg.EmailSender.Send(r.Context(), user.GetEmail(), emailBody)
	case p.cfg.DevMode:
		// SECURITY: do not log the live reset URL. The URL embeds the
		// raw token, which is a takeover credential — anyone with read
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

	userID, err := p.store.RedeemToken(r.Context(), body.Token)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

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
	if err := setter.SetPassword(r.Context(), userID, hash); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			writeAuthError(w, http.StatusNotFound, "user not found")
			return
		}
		writeAuthError(w, http.StatusInternalServerError, "set password failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
}
