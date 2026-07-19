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
	"log/slog"
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
	// EmailVerified reports whether the provider has VERIFIED the email.
	// The OAuth callback uses this to decide whether an email match may
	// bind to an existing local account: an unverified email must NEVER
	// match. Defaults to false — a provider that cannot assert
	// verification must not be treated as if it had.
	EmailVerified bool
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

// Init stores a reference to the AuthManager and fails closed when the
// configured UserStore is not a durable OAuth link store.
//
// A non-linker store means the OAuth callback cannot bind identity to
// (provider, provider_id) — it would have to fall back to email-only
// matching, which lets an IdP emitting an unverified email sign in as an
// existing local account. That fallback is removed; production must refuse
// to boot rather than silently expose the takeover.
//
// DevMode and AllowInMemoryStores keep the no-linker path reachable so
// the rest of the OAuth plumbing (redirect, state token, callback errors)
// stays unit-testable without a SQLite/Postgres backing store. The path
// logs a WARN — `resolveOAuthUser` itself returns errOAuthNoLinker rather
// than silently downgrading, so a DevMode test that actually drives the
// callback gets a loud failure, not a silent email-trust login.
func (p *OAuth2Plugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	if mgr.UserStore() == nil {
		// No user store at all — the callback already 500s on this. No
		// opinion here; let the existing nil-check fire at request time.
		return nil
	}
	if _, ok := mgr.UserStore().(OAuthLinker); !ok {
		cfg := mgr.Config()
		if !cfg.DevMode && !cfg.AllowInMemoryStores {
			return fmt.Errorf("auth: OAuth login requires a durable " +
				"(provider, provider_id) → user_id link store — the " +
				"configured UserStore does not implement OAuthLinker, so an " +
				"IdP emitting an unverified email could sign in as an " +
				"existing account. Use auth.NewEntityUserStore(...) (now a " +
				"linker) or set AuthConfig.AllowInMemoryStores: true for " +
				"local dev")
		}
		slog.Default().Warn("auth: OAuth2 plugin is running without a durable " +
			"OAuthLinker store — production must use one (e.g. " +
			"auth.NewEntityUserStore); an IdP emitting an unverified email " +
			"could otherwise sign in as an existing account")
	}
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
	r.Get(basePath+"/oauth/{provider}/link", p.linkHandler())
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

		state, err := p.generateState(providerName, "")
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "state generation failed")
			return
		}

		http.Redirect(w, r, provider.AuthURL(state), http.StatusFound)
	}
}

// linkHandler starts the AUTHENTICATED link-account flow: the logged-in user
// binds an additional OAuth provider. It requires a valid session, encodes
// that user's id into the (HMAC-signed) state, and redirects to the provider.
// The callback then links the returned provider identity to this proven user
// — bypassing the email-collision 409 that protects the unauthenticated path,
// because the user has proven ownership of BOTH the account (session) and the
// provider (the OAuth round-trip). Mounted at GET {basePath}/oauth/{provider}/link.
func (p *OAuth2Plugin) linkHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := router.Param(r, "provider")
		provider, ok := p.providers[providerName]
		if !ok {
			writeAuthError(w, http.StatusBadRequest, "unknown oauth provider")
			return
		}
		userID, ok := p.requireSessionUserID(w, r)
		if !ok {
			return // requireSessionUserID already wrote the 401/403
		}
		state, err := p.generateState(providerName, userID)
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "state generation failed")
			return
		}
		http.Redirect(w, r, provider.AuthURL(state), http.StatusFound)
	}
}

// requireSessionUserID resolves the current authenticated user's id from the
// session cookie, mirroring AccountsPlugin.requireUserID. Returns ("", false)
// (after writing the error) when there is no valid, fully-authenticated
// session — a pending-2FA session cannot initiate a provider link.
func (p *OAuth2Plugin) requireSessionUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	cfg := p.mgr.Config()
	cookie, err := r.Cookie(cfg.SessionCookie)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "no session")
		return "", false
	}
	sess, err := p.mgr.SessionStore().Get(r.Context(), cookie.Value)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "invalid session")
		return "", false
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, http.StatusForbidden, "two-factor verification required")
		return "", false
	}
	if sess.UserID == "" {
		writeAuthError(w, http.StatusUnauthorized, "no session")
		return "", false
	}
	return sess.UserID, true
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

		// Validate state. linkUserID is non-empty only for the authenticated
		// link-account flow (linkHandler bound it into the signed state).
		stateParam := r.URL.Query().Get("state")
		linkUserID, ok := p.validateAndConsumeState(stateParam, providerName)
		if !ok {
			writeAuthError(w, http.StatusBadRequest, "invalid or expired state")
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			writeAuthError(w, http.StatusBadRequest, "missing authorisation code")
			return
		}

		// Exchange the authorization code for a token. The HMAC state token
		// (validated above) is what binds the callback to this authorization
		// request; the confidential client's secret protects the exchange.
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

		// Bind the durable (provider, provider_id) link namespace to the
		// STATE-VALIDATED registry key — NOT the provider-returned Name().
		// Two providers can share a Name() (e.g. two OIDC issuers both
		// defaulting to "oidc"); keying links on that returned name would let
		// issuer B's subject "123" resolve to issuer A's link and take over
		// that account. providerName came from the route and was HMAC-state
		// validated above, so it is the authoritative per-registration identity.
		info.Provider = providerName

		// Find or create user
		userStore := p.mgr.UserStore()
		if userStore == nil {
			writeAuthError(w, http.StatusInternalServerError, "user store not configured")
			return
		}

		// Authenticated link-account flow: the signed state carries a
		// logged-in user id, so bind the returned provider identity to that
		// PROVEN user instead of running the login decision table. This is the
		// ONLY path that may link a provider whose email matches an existing
		// password account — the user has proven ownership of both the account
		// (session) and the provider (the OAuth round-trip).
		if linkUserID != "" {
			// The session must STILL belong to the user the flow was initiated
			// for (defends against completing a link-state under a different
			// login, or after logout).
			currentID, ok := p.requireSessionUserID(w, r)
			if !ok {
				return // 401/403 already written
			}
			if currentID != linkUserID {
				writeAuthError(w, http.StatusForbidden, "session does not match the link request")
				return
			}
			linker, ok := userStore.(OAuthLinker)
			if !ok {
				writeAuthError(w, http.StatusInternalServerError, "user store does not support linking")
				return
			}
			// Never steal a provider identity already bound to a DIFFERENT user.
			owner, ferr := linker.FindByOAuth(r.Context(), info.Provider, info.ID)
			switch {
			case ferr != nil && !errors.Is(ferr, ErrUserNotFound):
				writeAuthError(w, http.StatusInternalServerError, "link lookup failed")
				return
			case ferr == nil && owner != nil && owner.GetID() != linkUserID:
				p.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:   "oauth.refused",
					UserID: linkUserID,
					Email:  info.Email,
					Remote: remoteHost(r),
					Meta:   map[string]string{"provider": providerName, "reason": "provider_already_linked"},
				})
				writeAuthError(w, http.StatusConflict, "this provider account is already linked to a different user")
				return
			}
			if err := linkOAuthPreferEnriched(r.Context(), userStore, linker, linkUserID, info); err != nil {
				writeAuthError(w, http.StatusInternalServerError, "link failed")
				return
			}
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "oauth.linked",
				UserID: linkUserID,
				Email:  info.Email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"provider": providerName, "flow": "authenticated_link"},
			})
			// The user already has a session — just return them to the app.
			http.Redirect(w, r, "/", http.StatusFound)
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
			case errors.Is(err, errOAuthNoLinker):
				// Unreachable in production (Init fails closed). Surfaces in
				// DevMode only; treat as a server misconfiguration.
				writeAuthError(w, http.StatusInternalServerError,
					"OAuth login is not configured on this server")
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

// errOAuthEmailCollision means: a local account with this email already
// exists, the IdP asserts the email is verified, and that account has a
// real password. Refuse rather than silently link — the user must prove
// ownership of the local account first (log in with their password) and
// link the provider from /auth/accounts. This is what stops an IdP emitting
// a verified-but-attacker-controlled email from taking over an existing
// password account.
var errOAuthEmailCollision = errors.New("oauth: email collision with pre-existing account")

// errOAuthLookupFailed means a transient/transport error happened during
// the (provider, provider_id), email, or password lookup.
var errOAuthLookupFailed = errors.New("oauth: lookup failed")

// errOAuthNoLinker means the configured UserStore does not implement
// OAuthLinker. Production Init fails closed before this is reachable; it
// surfaces in DevMode / AllowInMemoryStores only, where the no-linker path
// is kept reachable so the rest of the OAuth plumbing (redirect, state,
// callback errors) stays testable. resolveOAuthUser itself refuses to fall
// back to email-trust: an unverified IdP email must never sign in as an
// existing account.
var errOAuthNoLinker = errors.New("oauth: UserStore does not implement OAuthLinker")

// resolveOAuthUser implements the safe OAuth-callback resolution policy.
//
// A linker is required: (provider, provider_id) is the only identity
// assertion the IdP makes that survives an email change. Email matching is
// allowed only as a migration step, and ONLY when the IdP asserts the
// email is verified AND the existing account has no password — a password
// account is never silently bound to an OAuth identity.
//
// Decision table (mirrors the documented contract):
//
//  1. FindByOAuth(provider, providerID) hit → LOGIN as the linked user
//     (linked=false). Profile refresh is best-effort.
//  2. FindByEmail(email) hit AND info.EmailVerified:
//     a. existing account has a password → errOAuthEmailCollision
//     (the user must log in with their password and link from
//     /auth/accounts — protects a local credential from IdP-email
//     takeover).
//     b. existing account is passwordless → AUTO-LINK + LOGIN
//     (linked=true). Safe migration: the account was created by a prior
//     OAuth login; a verified email re-binds the same identity.
//  3. Otherwise (no link, no email match, OR unverified email match):
//     create a new passwordless user and link the (provider, providerID).
//     A concurrent create that wins the link PK is authoritative; the
//     just-created user is left as an orphan (best-effort ignore).
//
// CRITICAL: an unverified email NEVER binds to an existing account. It
// falls through to step 3 as if the email didn't match at all — the core
// takeover regression.
func (p *OAuth2Plugin) resolveOAuthUser(ctx context.Context, store UserStore, info *OAuth2UserInfo) (User, bool, error) {
	linker, hasLinker := store.(OAuthLinker)
	if !hasLinker {
		// Unreachable in production: Init fails closed without a linker.
		// We do NOT fall back to email-trust — the legacy branch is gone.
		return nil, false, errOAuthNoLinker
	}

	// Step 1: existing (provider, provider_id) link → login.
	if found, err := linker.FindByOAuth(ctx, info.Provider, info.ID); err == nil {
		// Best-effort profile refresh; a failure here does not block login.
		_ = linkOAuthPreferEnriched(ctx, store, linker, found.GetID(), info)
		return found, false, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, false, fmt.Errorf("%w: FindByOAuth: %v", errOAuthLookupFailed, err)
	}

	// Step 2: email-collision / migration handling.
	existing, _, err := store.FindByEmail(ctx, info.Email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, false, fmt.Errorf("%w: FindByEmail: %v", errOAuthLookupFailed, err)
	}
	if err == nil {
		// Email matches an existing local account. The decision now rides
		// entirely on EmailVerified + HasPassword.
		if info.EmailVerified {
			hasPw := true // conservative default when PasswordChecker is absent
			if checker, ok := store.(PasswordChecker); ok {
				hp, herr := checker.HasPassword(ctx, existing.GetID())
				if herr != nil {
					// Fail closed: a checker error means we can't tell
					// password from passwordless, and the safe answer is
					// "refuse to silently bind."
					return nil, false, fmt.Errorf("%w: HasPassword: %v", errOAuthLookupFailed, herr)
				}
				hasPw = hp
			}
			if hasPw {
				// Refuse. The user must log in with their password and
				// link the provider from settings.
				return nil, false, errOAuthEmailCollision
			}
			// AUTO-LINK. Safe migration: a passwordless account was
			// created by a prior OAuth login; a verified email re-binds
			// the same identity. Re-read the link afterward and return the
			// user the (provider, provider_id) PK actually bound — two
			// concurrent callbacks for the same external identity with
			// different email matches must both resolve to the PK winner, not
			// to whichever account each one matched by email.
			if linkErr := linkOAuthPreferEnriched(ctx, store, linker, existing.GetID(), info); linkErr != nil {
				return nil, false, fmt.Errorf("%w: LinkOAuth: %v", errOAuthLookupFailed, linkErr)
			}
			owner, oerr := p.authoritativeOAuthOwner(ctx, linker, info)
			if oerr != nil {
				return nil, false, oerr
			}
			return owner, true, nil
		}
		// !info.EmailVerified: an unverified email MUST NOT bind to the
		// existing account. Fall through to step 3 (create a new,
		// distinct user) — this is the core takeover regression: an
		// attacker who controls an unverified IdP email gets a fresh
		// account, NOT the victim's existing one.
	}

	// Step 3: create a new passwordless user, then link.
	var newUser User
	var createErr error
	if creator, ok := store.(OAuthUserCreator); ok {
		newUser, createErr = creator.CreateUserNoPassword(ctx, info.Email, p.mgr.DefaultRoles())
	} else {
		newUser, createErr = store.CreateUser(ctx, info.Email, passwordPlaceholderHash, p.mgr.DefaultRoles())
	}
	if createErr != nil {
		return nil, false, createErr
	}
	if linkErr := linkOAuthPreferEnriched(ctx, store, linker, newUser.GetID(), info); linkErr != nil {
		return nil, false, fmt.Errorf("%w: LinkOAuth: %v", errOAuthLookupFailed, linkErr)
	}

	// Re-resolve via the link table. If a concurrent create raced and won
	// the (provider, provider_id) PK — possible when two callbacks for the
	// same external identity land on different replicas — that winner is
	// authoritative and we return it; the just-created user is left as an
	// orphan (we do NOT delete it: cascading-delete/audit/retry hazards
	// outweigh an unlinked row, and the email-unique constraint means a
	// future callback follows the verified-email path). We FAIL CLOSED on a
	// re-read error rather than trust newUser — a transient lookup failure
	// after another callback won must never authenticate an unlinked account.
	owner, oerr := p.authoritativeOAuthOwner(ctx, linker, info)
	if oerr != nil {
		return nil, false, oerr
	}
	return owner, true, nil
}

// authoritativeOAuthOwner re-reads the (provider, provider_id) link after a
// LinkOAuth and returns the user the durable PK actually bound — the source
// of truth when concurrent callbacks race for the same external identity. It
// FAILS CLOSED: a lookup error, or a link that is somehow absent immediately
// after a successful write, yields errOAuthLookupFailed rather than
// authenticating a user whose ownership we cannot prove.
func (p *OAuth2Plugin) authoritativeOAuthOwner(ctx context.Context, linker OAuthLinker, info *OAuth2UserInfo) (User, error) {
	owner, err := linker.FindByOAuth(ctx, info.Provider, info.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: FindByOAuth after link: %v", errOAuthLookupFailed, err)
	}
	if owner == nil {
		return nil, fmt.Errorf("%w: link absent immediately after write", errOAuthLookupFailed)
	}
	return owner, nil
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
//	base64(nonce) "." providerName "." expiryUnix "." base64(userID) "." base64(hmac)
//
// where hmac covers everything before the final separator. There is no
// per-call server state — the redirect endpoint cannot be turned into a
// memory-growth surface, and an in-flight OAuth flow survives a server
// restart between redirect and callback.
//
// userID is empty for an ordinary login flow. For the authenticated
// link-account flow it carries the id of the logged-in user the callback
// must link to — inside the HMAC payload, so a client can neither forge nor
// alter which account a provider gets bound to.
func (p *OAuth2Plugin) generateState(providerName, userID string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	nonceB64 := base64.RawURLEncoding.EncodeToString(nonce)
	expiryUnix := strconv.FormatInt(time.Now().Add(stateTTL).Unix(), 10)
	userB64 := base64.RawURLEncoding.EncodeToString([]byte(userID))
	payload := nonceB64 + "." + providerName + "." + expiryUnix + "." + userB64

	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payload + "." + sig, nil
}

// validateAndConsumeState verifies the HMAC, the expiry, the provider
// binding, and the nonce-replay window. Replay protection is enforced
// by recording the (verified) nonce in usedNonces until its embedded
// expiry passes.
// validateAndConsumeState verifies the HMAC, expiry, provider binding, and
// nonce-replay window, and returns the userID bound into the state (empty for
// an ordinary login flow; the logged-in user's id for the link-account flow).
// ok is false on any failure.
func (p *OAuth2Plugin) validateAndConsumeState(state, expectedProvider string) (userID string, ok bool) {
	parts := strings.SplitN(state, ".", 5)
	if len(parts) != 5 {
		return "", false
	}
	nonceB64, provider, expiryStr, userB64, sig := parts[0], parts[1], parts[2], parts[3], parts[4]
	if provider != expectedProvider {
		return "", false
	}

	// Verify HMAC over (nonce.provider.expiry.userID) before trusting any of
	// the other fields — including the expiry and the bound userID. HMAC-first
	// ordering keeps an attacker from getting the validate path to do work on
	// an unsigned token (also avoids leaking timing oracles via the expiry
	// compare) and, critically, means the link-flow userID cannot be forged.
	payload := nonceB64 + "." + provider + "." + expiryStr + "." + userB64
	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", false
	}

	expiryUnix, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return "", false
	}
	expiry := time.Unix(expiryUnix, 0)
	if time.Now().After(expiry) {
		return "", false
	}

	uidBytes, err := base64.RawURLEncoding.DecodeString(userB64)
	if err != nil {
		return "", false
	}

	// Replay protection: refuse if the nonce has already been consumed
	// within its TTL window. Add it on first sighting.
	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	if _, replayed := p.usedNonces[nonceB64]; replayed {
		return "", false
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
	return string(uidBytes), true
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
		ID            string `json:"id"`
		Email         string `json:"email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
		EmailVerified any    `json:"email_verified"`
		VerifiedEmail any    `json:"verified_email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	// Google asserts email_verified on its userinfo response (bool). Older
	// docs surface the same value as "verified_email" — accept either, the
	// newer name wins. When neither is present we default to false: a
	// missing assertion is not a verified email.
	emailVerified := parseGoogleEmailVerified(body.EmailVerified) ||
		parseGoogleEmailVerified(body.VerifiedEmail)

	return &OAuth2UserInfo{
		ID:            body.ID,
		Email:         body.Email,
		Name:          body.Name,
		AvatarURL:     body.Picture,
		Provider:      "google",
		EmailVerified: emailVerified,
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

	// GitHub's /user endpoint surfaces the user's PUBLIC email but does not
	// assert verification. The /user/emails endpoint (user:email scope,
	// already requested at AuthURL) lists emails with a verified flag we
	// can trust. Prefer it both for filling a hidden primary AND for
	// confirming the public email is verified.
	email := body.Email
	emailVerified := false
	if got, verified, err := g.fetchPrimaryEmail(ctx, token); err == nil && got != "" {
		email = got
		emailVerified = verified
	} else if email != "" {
		// Public email present but we couldn't reach /user/emails to confirm
		// verification — be conservative. An unverifiable email must never
		// bind to an existing local account.
		emailVerified = false
	}

	return &OAuth2UserInfo{
		ID:            fmt.Sprintf("%d", body.ID),
		Email:         email,
		Name:          body.Name,
		AvatarURL:     body.AvatarURL,
		Provider:      "github",
		EmailVerified: emailVerified,
	}, nil
}

// fetchPrimaryEmail queries GitHub /user/emails (requires user:email
// scope) and returns the address marked primary AND verified, plus a
// boolean that is true only when the returned email was confirmed
// verified by GitHub. The empty string + false when none exists.
func (g *GitHubProvider) fetchPrimaryEmail(ctx context.Context, token string) (string, bool, error) {
	// Derive /user/emails from userInfoEndpoint so test overrides work.
	emailsURL := g.userInfoEndpoint + "/emails"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, emailsURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("github: /user/emails returned %d", resp.StatusCode)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", false, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true, nil
		}
	}
	// Fall back to any verified email if none are marked primary.
	for _, e := range emails {
		if e.Verified {
			return e.Email, true, nil
		}
	}
	return "", false, nil
}

// parseGoogleEmailVerified coerces Google's `email_verified` /
// `verified_email` JSON value into a strict bool. Accepts a JSON bool or
// the strings "true"/"false" (case-insensitive). Anything else — including
// the field's absence — resolves to false: a missing assertion is not a
// verified email, and the OAuth callback must never bind an unverifiable
// email to an existing account.
func parseGoogleEmailVerified(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}
