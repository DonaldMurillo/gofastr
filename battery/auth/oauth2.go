package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// ─── Data types ─────────────────────────────────────────────────────────────

// OAuth2Token holds the token information returned by an OAuth2 provider.
type OAuth2Token struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// OAuth2UserInfo holds the user profile fetched from an OAuth2 provider.
type OAuth2UserInfo struct {
	ID        string
	Email     string
	Name      string
	AvatarURL string
	Provider  string
}

// ─── Provider interface ─────────────────────────────────────────────────────

// OAuth2Provider is the interface each OAuth2 provider (Google, GitHub, etc.)
// must implement. Providers are registered on the OAuth2Plugin via
// RegisterProvider or passed in through OAuth2Config.
type OAuth2Provider interface {
	// Name returns the provider identifier (e.g. "google", "github").
	Name() string

	// AuthURL returns the URL to redirect the user to for authorisation.
	// The state parameter is included for CSRF protection.
	AuthURL(state string) string

	// ExchangeCode exchanges the authorisation code for an access token.
	ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error)

	// FetchUserInfo uses the access token to fetch the user's profile.
	FetchUserInfo(ctx context.Context, token string) (*OAuth2UserInfo, error)
}

// ─── Config ─────────────────────────────────────────────────────────────────

// OAuth2Config holds the configuration for the OAuth2 plugin.
type OAuth2Config struct {
	// Providers is a map of provider name → provider implementation.
	Providers map[string]OAuth2Provider

	// RedirectURL is the base redirect URL for OAuth2 callbacks.
	// The provider-specific path is appended automatically.
	RedirectURL string

	// StateSecret is the HMAC key used to sign OAuth2 state tokens.
	// If empty, a random key is generated at startup (suitable for
	// single-instance deployments only).
	StateSecret string

	// TokenStore, when set, persists each provider's access/refresh token
	// at login so that calls made on the user's behalf can recover after
	// the (short-lived) access token expires — see RefreshOAuthToken /
	// ValidOAuthToken. Opt-in: when nil, OAuth login behaves exactly as
	// before and the provider's refresh token is discarded.
	TokenStore OAuthTokenStore
}

// ─── Plugin ─────────────────────────────────────────────────────────────────

// OAuth2Plugin implements AuthPlugin and AuthPluginRoutes to provide
// OAuth2-based authentication via configurable providers.
//
// State is stateless: the token itself carries (nonce, provider,
// expiryUnix, hmac). The plugin never persists per-redirect state,
// so server restarts mid-flow don't invalidate in-flight logins and
// the redirect endpoint can't be used as a memory-pressure DoS
// surface. Replay protection comes from a bounded usedNonces map
// that's only touched on the (much rarer) successful callback path.
type OAuth2Plugin struct {
	mgr        *AuthManager
	providers  map[string]OAuth2Provider
	stateKey   []byte
	tokenStore OAuthTokenStore

	// usedNonces dedup callbacks against replay. Keyed by the random
	// nonce embedded in the state token; values are the token's
	// signed expiry so periodic GC can drop entries past TTL. Only
	// validateAndConsumeState mutates this map.
	noncesMu   sync.Mutex
	usedNonces map[string]time.Time
}

// NewOAuth2Plugin creates an OAuth2 plugin with the given configuration.
func NewOAuth2Plugin(cfg OAuth2Config) *OAuth2Plugin {
	p := &OAuth2Plugin{
		providers:  make(map[string]OAuth2Provider),
		usedNonces: make(map[string]time.Time),
		tokenStore: cfg.TokenStore,
	}

	// Copy providers from config
	for name, provider := range cfg.Providers {
		p.providers[name] = provider
	}

	// State signing key
	if cfg.StateSecret != "" {
		p.stateKey = []byte(cfg.StateSecret)
	} else {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			panic("oauth2: failed to generate state key: " + err.Error())
		}
		p.stateKey = key
	}

	return p
}

// Name returns the plugin identifier.
func (p *OAuth2Plugin) Name() string { return "oauth2" }

// Init stores a reference to the AuthManager.
func (p *OAuth2Plugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	return nil
}

// RegisterProvider adds a provider to the plugin's registry.
func (p *OAuth2Plugin) RegisterProvider(name string, provider OAuth2Provider) {
	p.providers[name] = provider
}

// ─── Routes ─────────────────────────────────────────────────────────────────

// RegisterRoutes mounts the OAuth2 auth and callback routes.
func (p *OAuth2Plugin) RegisterRoutes(r *router.Router, basePath string) {
	r.Get(basePath+"/oauth/{provider}", p.redirectHandler())
	r.Get(basePath+"/oauth/{provider}/callback", p.callbackHandler())
}

// ─── Redirect handler ──────────────────────────────────────────────────────

func (p *OAuth2Plugin) redirectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := router.Param(r, "provider")
		provider, ok := p.providers[providerName]
		if !ok {
			writeAuthError(w, http.StatusBadRequest, "unknown oauth provider")
			return
		}

		state, err := p.generateState(providerName)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "state generation failed")
			return
		}

		http.Redirect(w, r, provider.AuthURL(state), http.StatusFound)
	}
}

// ─── Callback handler ──────────────────────────────────────────────────────

func (p *OAuth2Plugin) callbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := router.Param(r, "provider")
		provider, ok := p.providers[providerName]
		if !ok {
			writeAuthError(w, http.StatusBadRequest, "unknown oauth provider")
			return
		}

		// Validate state
		stateParam := r.URL.Query().Get("state")
		if !p.validateAndConsumeState(stateParam, providerName) {
			writeAuthError(w, http.StatusBadRequest, "invalid or expired state")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			writeAuthError(w, http.StatusBadRequest, "missing authorisation code")
			return
		}

		// Exchange code for token
		tok, err := provider.ExchangeCode(r.Context(), code)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "code exchange failed")
			return
		}

		// Fetch user info
		info, err := provider.FetchUserInfo(r.Context(), tok.AccessToken)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "failed to fetch user info")
			return
		}

		// Find or create user
		userStore := p.mgr.UserStore()
		if userStore == nil {
			writeAuthError(w, http.StatusInternalServerError, "user store not configured")
			return
		}

		user, linked, err := p.resolveOAuthUser(r.Context(), userStore, info)
		if err != nil {
			switch {
			case errors.Is(err, errOAuthEmailCollision):
				p.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:  "oauth.refused",
					Email: info.Email,
					Meta:  map[string]string{"provider": providerName, "reason": "link_conflict"},
				})
				writeAuthError(w, http.StatusConflict,
					"an account with this email already exists — log in with your existing credentials and link from account settings")
			case errors.Is(err, errOAuthLookupFailed):
				writeAuthError(w, http.StatusInternalServerError, "user lookup failed")
			default:
				writeAuthError(w, http.StatusConflict, "could not create user")
			}
			return
		}

		if linked {
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "oauth.linked",
				UserID: user.GetID(),
				Email:  info.Email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"provider": providerName},
			})
		} else {
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "oauth.login",
				UserID: user.GetID(),
				Email:  info.Email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"provider": providerName},
			})
		}

		// Persist the provider tokens (incl. the refresh token) when a
		// token store is configured. Best-effort: a store failure must not
		// block the login itself — the user still gets a session, they just
		// lose transparent refresh until the next successful login.
		if p.tokenStore != nil {
			_ = p.tokenStore.Save(r.Context(), OAuthTokenRecord{
				UserID:       user.GetID(),
				Provider:     providerName,
				AccessToken:  tok.AccessToken,
				RefreshToken: tok.RefreshToken,
				Expiry:       tok.Expiry,
			})
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

		// Redirect to a sensible landing page
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

// ─── OAuth identity resolution ──────────────────────────────────────────────

// errOAuthEmailCollision means: a local account with this email already
// exists, but no OAuth link has been established. Refuse rather than
// silently log in — see the OAuthLinker doc for rationale.
var errOAuthEmailCollision = errors.New("oauth: email collision with pre-existing account")

// errOAuthLookupFailed means a transient/transport error happened during
// the (provider, provider_id) or email lookup.
var errOAuthLookupFailed = errors.New("oauth: lookup failed")

// resolveOAuthUser implements the safe OAuth-callback resolution policy.
//
// Order:
//  1. (linker) FindByOAuth(provider, providerID) — exact identity match.
//  2. (linker) FindByEmail. If a user already exists for this email AND
//     no provider link is on file, return errOAuthEmailCollision (the
//     handler responds 409 — silent linking is a takeover vector when
//     the IdP doesn't verify emails).
//  3. (no linker) FindByEmail. If found, log in (legacy behavior — only
//     use this with a UserStore you trust to never resolve unverified
//     emails to existing accounts).
//  4. Otherwise CreateUser, and LinkOAuth if supported.
func (p *OAuth2Plugin) resolveOAuthUser(ctx context.Context, store UserStore, info *OAuth2UserInfo) (User, bool, error) {
	linker, hasLinker := store.(OAuthLinker)

	if hasLinker {
		user, err := linker.FindByOAuth(ctx, info.Provider, info.ID)
		if err == nil {
			// Existing link: this is a login, not a new binding.
			return user, false, nil
		}
		if !errors.Is(err, ErrUserNotFound) {
			return nil, false, fmt.Errorf("%w: FindByOAuth: %v", errOAuthLookupFailed, err)
		}
		// Fall through to email + collision check.
	}

	existing, _, err := store.FindByEmail(ctx, info.Email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, false, fmt.Errorf("%w: FindByEmail: %v", errOAuthLookupFailed, err)
	}

	if err == nil {
		// Email matches an existing local account.
		if hasLinker {
			// Refuse to link silently. The user must prove ownership of
			// the local account first (log in via password) and link
			// from settings.
			return nil, false, errOAuthEmailCollision
		}
		// Legacy: trust the email match. Loud warning at construction
		// time would be ideal — for now, this path requires deliberate
		// opt-out by NOT implementing OAuthLinker.
		return existing, false, nil
	}

	// Auto-create. Prefer the OAuthUserCreator path so the store can
	// record password_set=false; that lets AccountsPlugin's unlink
	// guard distinguish "real password user" from "OAuth-only with
	// placeholder hash" later. Fall back to CreateUser+placeholder for
	// stores that don't opt in (still functional, just coarser unlink).
	var (
		user      User
		createErr error
	)
	if creator, ok := store.(OAuthUserCreator); ok {
		user, createErr = creator.CreateUserNoPassword(ctx, info.Email, []string{"user"})
	} else {
		user, createErr = store.CreateUser(ctx, info.Email, passwordPlaceholderHash, []string{"user"})
	}
	if createErr != nil {
		return nil, false, createErr
	}
	if hasLinker {
		linkErr := linkOAuthPreferEnriched(ctx, store, linker, user.GetID(), info)
		if linkErr != nil {
			// Record the failure but proceed; the next login will
			// re-resolve via email and try linking again.
			return nil, false, fmt.Errorf("%w: LinkOAuth: %v", errOAuthLookupFailed, linkErr)
		}
		// A new (provider, providerID) binding was persisted — the
		// distinguishing signal for the oauth.linked audit event.
		return user, true, nil
	}
	return user, false, nil
}

// linkOAuthPreferEnriched persists the (provider, providerID) bind plus
// the optional profile fields when the store opts in. Stores that only
// implement the minimal OAuthLinker get the legacy call and the extras
// are dropped — backwards-compatible.
func linkOAuthPreferEnriched(ctx context.Context, store UserStore, linker OAuthLinker, userID string, info *OAuth2UserInfo) error {
	if enriched, ok := store.(OAuthEnrichedLinker); ok {
		return enriched.LinkOAuthEnriched(ctx, userID, info.Provider, info.ID, OAuthAccountProfile{
			Email:     info.Email,
			Name:      info.Name,
			AvatarURL: info.AvatarURL,
		})
	}
	return linker.LinkOAuth(ctx, userID, info.Provider, info.ID)
}

// ─── State management ───────────────────────────────────────────────────────

const stateTTL = 10 * time.Minute

// nonceGCThreshold is the map size above which validateAndConsumeState
// scan-and-purges expired entries. Only the callback path mutates the
// nonces map, so unbounded growth requires successful callbacks — much
// harder to abuse than the redirect path under the old design.
const nonceGCThreshold = 4096

// generateState builds a stateless, HMAC-signed state token of the form
//
//	base64(nonce) "." providerName "." expiryUnix "." base64(hmac)
//
// where hmac covers everything before the final separator. There is no
// per-call server state — the redirect endpoint cannot be turned into a
// memory-growth surface, and an in-flight OAuth flow survives a server
// restart between redirect and callback.
func (p *OAuth2Plugin) generateState(providerName string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	nonceB64 := base64.RawURLEncoding.EncodeToString(nonce)
	expiryUnix := strconv.FormatInt(time.Now().Add(stateTTL).Unix(), 10)
	payload := nonceB64 + "." + providerName + "." + expiryUnix

	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payload + "." + sig, nil
}

// validateAndConsumeState verifies the HMAC, the expiry, the provider
// binding, and the nonce-replay window. Replay protection is enforced
// by recording the (verified) nonce in usedNonces until its embedded
// expiry passes.
func (p *OAuth2Plugin) validateAndConsumeState(state, expectedProvider string) bool {
	parts := strings.SplitN(state, ".", 4)
	if len(parts) != 4 {
		return false
	}
	nonceB64, provider, expiryStr, sig := parts[0], parts[1], parts[2], parts[3]
	if provider != expectedProvider {
		return false
	}

	// Verify HMAC over (nonce.provider.expiry) before trusting any of
	// the other fields — including the expiry. HMAC-first ordering
	// keeps an attacker from getting the validate path to do work on
	// an unsigned token (also avoids leaking timing oracles via the
	// expiry compare).
	payload := nonceB64 + "." + provider + "." + expiryStr
	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return false
	}

	expiryUnix, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return false
	}
	expiry := time.Unix(expiryUnix, 0)
	if time.Now().After(expiry) {
		return false
	}

	// Replay protection: refuse if the nonce has already been consumed
	// within its TTL window. Add it on first sighting.
	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	if _, replayed := p.usedNonces[nonceB64]; replayed {
		return false
	}
	if len(p.usedNonces) >= nonceGCThreshold {
		now := time.Now()
		for k, exp := range p.usedNonces {
			if now.After(exp) {
				delete(p.usedNonces, k)
			}
		}
	}
	p.usedNonces[nonceB64] = expiry
	return true
}

// purgeExpiredNonces is retained for explicit cleanup callers (e.g. a
// future ticker goroutine). The hot validate path inlines its own
// size-gated purge.
func (p *OAuth2Plugin) purgeExpiredNonces() {
	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	now := time.Now()
	for n, exp := range p.usedNonces {
		if now.After(exp) {
			delete(p.usedNonces, n)
		}
	}
}

// randomPassword generates a random alphanumeric string of the given length.
// Used as a placeholder hash input for OAuth-created users (they never log
// in via password). Panics on entropy failure — the auth system can't
// remain sound without crypto/rand.
func randomPassword(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("auth: crypto/rand failed: %v", err))
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// ─── Built-in providers ────────────────────────────────────────────────────

// defaultOAuthHTTPClient is the http.Client used by built-in OAuth providers
// when no override is supplied. The 10s deadline matches framework/auth/oauth.go.
// Without it, an IdP that hangs the connection pins one goroutine + two TCP
// fds per inflight callback for the full kernel socket timeout.
var defaultOAuthHTTPClient = &http.Client{Timeout: 10 * time.Second}

// GoogleProvider implements OAuth2Provider for Google's OAuth2 endpoints.
type GoogleProvider struct {
	clientID         string
	clientSecret     string
	redirectURL      string
	httpClient       *http.Client
	tokenEndpoint    string // override for tests; defaults to Google's URL
	userInfoEndpoint string // override for tests; defaults to Google's URL
}

// NewGoogleProvider creates a Google OAuth2 provider.
func NewGoogleProvider(clientID, clientSecret, redirectURL string) *GoogleProvider {
	return &GoogleProvider{
		clientID:         clientID,
		clientSecret:     clientSecret,
		redirectURL:      redirectURL,
		httpClient:       defaultOAuthHTTPClient,
		tokenEndpoint:    "https://oauth2.googleapis.com/token",
		userInfoEndpoint: "https://www.googleapis.com/oauth2/v2/userinfo",
	}
}

func (g *GoogleProvider) Name() string { return "google" }

func (g *GoogleProvider) AuthURL(state string) string {
	u, _ := url.Parse("https://accounts.google.com/o/oauth2/v2/auth")
	q := u.Query()
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	// access_type=offline is required for Google to issue a refresh token;
	// prompt=consent forces re-issuing it even after the first grant so a
	// configured OAuthTokenStore always has something to refresh with.
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	u.RawQuery = q.Encode()
	return u.String()
}

func (g *GoogleProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	data := url.Values{}
	data.Set("code", code)
	data.Set("client_id", g.clientID)
	data.Set("client_secret", g.clientSecret)
	data.Set("redirect_uri", g.redirectURL)
	data.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.tokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google: token exchange returned %d", resp.StatusCode)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// RefreshToken exchanges a Google refresh token for a fresh access token.
// Google does not re-issue the refresh token on this grant, so the returned
// OAuth2Token.RefreshToken is normally empty — callers keep the stored one.
func (g *GoogleProvider) RefreshToken(ctx context.Context, refreshToken string) (*OAuth2Token, error) {
	data := url.Values{}
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", g.clientID)
	data.Set("client_secret", g.clientSecret)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.tokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google: token refresh returned %d", resp.StatusCode)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

func (g *GoogleProvider) FetchUserInfo(ctx context.Context, token string) (*OAuth2UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		g.userInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google: userinfo returned %d", resp.StatusCode)
	}

	var body struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	return &OAuth2UserInfo{
		ID:        body.ID,
		Email:     body.Email,
		Name:      body.Name,
		AvatarURL: body.Picture,
		Provider:  "google",
	}, nil
}

// GitHubProvider implements OAuth2Provider for GitHub's OAuth2 endpoints.
type GitHubProvider struct {
	clientID         string
	clientSecret     string
	redirectURL      string
	httpClient       *http.Client
	tokenEndpoint    string // override for tests
	userInfoEndpoint string // override for tests
}

// NewGitHubProvider creates a GitHub OAuth2 provider.
func NewGitHubProvider(clientID, clientSecret, redirectURL string) *GitHubProvider {
	return &GitHubProvider{
		clientID:         clientID,
		clientSecret:     clientSecret,
		redirectURL:      redirectURL,
		httpClient:       defaultOAuthHTTPClient,
		tokenEndpoint:    "https://github.com/login/oauth/access_token",
		userInfoEndpoint: "https://api.github.com/user",
	}
}

func (g *GitHubProvider) Name() string { return "github" }

func (g *GitHubProvider) AuthURL(state string) string {
	u, _ := url.Parse("https://github.com/login/oauth/authorize")
	q := u.Query()
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirectURL)
	q.Set("scope", "user:email")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

func (g *GitHubProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	data := url.Values{}
	data.Set("code", code)
	data.Set("client_id", g.clientID)
	data.Set("client_secret", g.clientSecret)
	data.Set("redirect_uri", g.redirectURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.tokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github: token exchange returned %d: %s", resp.StatusCode, body)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// RefreshToken exchanges a GitHub refresh token for a fresh access token.
// Only GitHub Apps (and OAuth apps with expiring tokens enabled) issue
// refresh tokens; classic OAuth-app tokens never expire and won't have one.
func (g *GitHubProvider) RefreshToken(ctx context.Context, refreshToken string) (*OAuth2Token, error) {
	data := url.Values{}
	data.Set("refresh_token", refreshToken)
	data.Set("client_id", g.clientID)
	data.Set("client_secret", g.clientSecret)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		g.tokenEndpoint,
		strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Don't fold the provider response body into the error: it flows to
		// caller logs, and keeping upstream bytes out of error strings matches
		// the Google path. Status code is enough to diagnose.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github: token refresh returned %d", resp.StatusCode)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

func (g *GitHubProvider) FetchUserInfo(ctx context.Context, token string) (*OAuth2UserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		g.userInfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: user info returned %d", resp.StatusCode)
	}

	var body struct {
		ID        int    `json:"id"`
		Login     string `json:"login"`
		Email     string `json:"email"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	email := body.Email
	if email == "" {
		// User has hidden their primary email on /user. The user:email
		// scope is already requested at AuthURL time, so we can fetch
		// /user/emails and pick the verified primary. Synthesizing
		// "<login>@github" — the previous fallback — produces a
		// non-routable address that breaks email-based recovery.
		if got, err := g.fetchPrimaryEmail(ctx, token); err == nil && got != "" {
			email = got
		}
	}

	return &OAuth2UserInfo{
		ID:        fmt.Sprintf("%d", body.ID),
		Email:     email,
		Name:      body.Name,
		AvatarURL: body.AvatarURL,
		Provider:  "github",
	}, nil
}

// fetchPrimaryEmail queries GitHub /user/emails (requires user:email
// scope) and returns the address marked primary AND verified, or
// the empty string if none exists.
func (g *GitHubProvider) fetchPrimaryEmail(ctx context.Context, token string) (string, error) {
	// Derive /user/emails from userInfoEndpoint so test overrides work.
	emailsURL := g.userInfoEndpoint + "/emails"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, emailsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github: /user/emails returned %d", resp.StatusCode)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	// Fall back to any verified email if none are marked primary.
	for _, e := range emails {
		if e.Verified {
			return e.Email, nil
		}
	}
	return "", nil
}
