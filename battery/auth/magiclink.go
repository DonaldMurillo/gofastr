package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
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
	// which does NOT survive restarts or scale across replicas — set a
	// durable store (e.g. NewSQLMagicLinkTokenStore(db)) in production.
	TokenStore MagicLinkTokenStore

	// BodyTemplate, when non-nil, transforms the magic-link URL into
	// the full email body before EmailSender.SendMagicLink is called.
	// nil means "send the URL as the entire body" (historical behavior).
	// Note: MagicLinkEmailSender's interface still takes (to, link)
	// — implementations decide whether to wrap the link in HTML; this
	// hook is the bridge for ones that just blast the body verbatim.
	BodyTemplate func(url string) string

	// BaseURL is the application base URL used to construct magic links
	// (e.g. "http://localhost:8080").
	BaseURL string

	// OnSuccessURL is the URL to redirect to after successful login (default: "/").
	OnSuccessURL string

	// RateLimit, when non-nil, applies a per-IP rate limit to
	// /auth/magic-link/send. Without this, an attacker can spam any
	// recipient address. nil disables (not recommended).
	RateLimit *RateLimiterConfig

	// DevMode permits the plugin to operate without an EmailSender by
	// logging the magic-link URL to stdout. Without this flag, a nil
	// EmailSender causes /auth/magic-link/send to return 503 — refusing
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

// ErrTokenNotFound is returned when a token is unknown, already consumed, or expired.
var ErrTokenNotFound = errors.New("auth: magic-link token not found or expired")

// magicLinkEntry is one stored token.
type magicLinkEntry struct {
	email     string
	expiresAt time.Time
}

// MemoryMagicLinkTokenStore is a goroutine-safe in-memory MagicLinkTokenStore.
type MemoryMagicLinkTokenStore struct {
	mu     sync.RWMutex
	tokens map[string]*magicLinkEntry
}

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

	m.mu.Lock()
	m.tokens[token] = &magicLinkEntry{
		email:     email,
		expiresAt: time.Now().Add(ttl),
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
	// Always delete — single-use regardless of expiry
	delete(m.tokens, token)

	if time.Now().After(entry.expiresAt) {
		return "", ErrTokenNotFound
	}
	return entry.email, nil
}

// Cleanup removes all expired tokens and returns the count purged.
func (m *MemoryMagicLinkTokenStore) Cleanup(_ context.Context) (int, error) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()

	n := 0
	for tok, entry := range m.tokens {
		if now.After(entry.expiresAt) {
			delete(m.tokens, tok)
			n++
		}
	}
	return n, nil
}

// MagicLinkPlugin implements AuthPlugin and AuthPluginRoutes for passwordless
// magic-link authentication.
type MagicLinkPlugin struct {
	config     MagicLinkConfig
	mgr        *AuthManager
	tokenStore MagicLinkTokenStore
	sendLimit  *RateLimiter
}

// NewMagicLinkPlugin creates a new magic-link plugin with the given config.
func NewMagicLinkPlugin(config MagicLinkConfig) *MagicLinkPlugin {
	config.defaults()
	store := config.TokenStore
	if store == nil {
		store = NewMemoryMagicLinkTokenStore()
	}
	p := &MagicLinkPlugin{
		config:     config,
		tokenStore: store,
	}
	if config.RateLimit != nil {
		p.sendLimit = NewRateLimiter(*config.RateLimit)
	}
	return p
}

// Name returns the plugin identifier.
func (p *MagicLinkPlugin) Name() string { return "magic-link" }

// Init stores a reference to the AuthManager.
func (p *MagicLinkPlugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	return nil
}

// RegisterRoutes mounts the magic-link send and verify endpoints.
func (p *MagicLinkPlugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Post(basePath+"/magic-link/send", http.HandlerFunc(p.sendHandler))
	r.Get(basePath+"/magic-link/verify", http.HandlerFunc(p.verifyHandler))
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
	if body.Email == "" {
		writeAuthError(w, http.StatusBadRequest, "email is required")
		return
	}

	token, err := p.tokenStore.CreateToken(r.Context(), body.Email, p.config.TokenTTL)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "failed to create token")
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
			linkBody = p.config.BodyTemplate(magicLinkURL)
		}
		if err := p.config.EmailSender.SendMagicLink(r.Context(), body.Email, linkBody); err != nil {
			writeAuthError(w, http.StatusInternalServerError, "failed to send email")
			return
		}
	case p.config.DevMode:
		// Dev mode opt-in: log the link instead of sending. NEVER do this
		// in production — anyone with log read access could grab the
		// token and take over the account.
		// Hash the sensitive bits so the log is greppable for debugging
		// but doesn't expose the live token or full email to anyone with
		// log read access.
		// SECURITY: do not log the live magic-link URL. The URL embeds the
		// raw token, which is a takeover credential — anyone with read
		// access to dev logs could replay it. email_hash + token_hash give
		// enough signal to correlate with the rendered email body.
		slog.Info("magic-link dev",
			"plugin", "magic-link",
			"email_hash", hashedIdentifier(body.Email),
			"token_hash", hashedIdentifier(token))
	default:
		// Fail-closed. The operator forgot to wire EmailSender and didn't
		// opt into DevMode — better a 503 than silently leaking tokens.
		writeAuthError(w, http.StatusServiceUnavailable, "magic link delivery not configured")
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
		"message": "magic link sent",
		"success": true,
	})
}

// verifyHandler handles GET {basePath}/magic-link/verify?token=xxx.
// Redeems the token, finds or creates the user, creates a session,
// sets the session cookie, and redirects to OnSuccessURL.
func (p *MagicLinkPlugin) verifyHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		writeAuthError(w, http.StatusUnauthorized, "token is required")
		return
	}

	email, err := p.tokenStore.RedeemToken(r.Context(), token)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	// Find or create user
	userStore := p.mgr.UserStore()
	if userStore == nil {
		writeAuthError(w, http.StatusInternalServerError, "user store not configured")
		return
	}

	user, _, findErr := userStore.FindByEmail(r.Context(), email)
	if findErr != nil && !errors.Is(findErr, ErrUserNotFound) {
		// Any non-NotFound error is a transport / DB failure. We must
		// NOT silently auto-create a user — that would mask the failure
		// AND let an attacker provoke account creation by tripping a
		// transient lookup error.
		writeAuthError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	if findErr != nil {
		// User doesn't exist — auto-create. Prefer the OAuthUserCreator
		// path so password_set=false is recorded for the unlink guard;
		// fall back to CreateUser+placeholder for stores that don't opt
		// in. Either way, magic-link users never authenticate via
		// password and the placeholder bcrypt step is reused.
		var err error
		if creator, ok := userStore.(OAuthUserCreator); ok {
			user, err = creator.CreateUserNoPassword(r.Context(), email, []string{"user"})
		} else {
			user, err = userStore.CreateUser(r.Context(), email, passwordPlaceholderHash, []string{"user"})
		}
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
	}

	// Create session
	cfg := p.mgr.Config()
	sess, err := p.mgr.SessionStore().Create(r.Context(), user.GetID(), cfg.SessionTTL)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "session create failed")
		return
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     cfg.SessionCookie,
		Value:    sess.Token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})

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
// a same-origin path. If the URL is not a relative path starting with '/',
// it falls back to "/".
func safeRedirectURL(u string) string {
	if u == "" {
		return "/"
	}
	// Must start with / and NOT be a protocol-relative URL (//evil.com)
	if len(u) > 0 && u[0] == '/' && !(len(u) > 1 && u[1] == '/') {
		return u
	}
	return "/"
}
