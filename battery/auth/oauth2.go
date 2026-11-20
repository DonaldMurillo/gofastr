package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
)

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
	EmailVerified bool
}

// OAuth2Provider is one OAuth2 identity provider.
type OAuth2Provider interface {
	// Name returns the provider identifier (e.g. "google", "github").
	Name() string

	// AuthURL returns the URL to redirect the user to for authorisation.
	AuthURL(state string) string

	// ExchangeCode exchanges the authorisation code for an access token.
	ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error)

	// FetchUserInfo uses the access token to fetch the user's profile.
	FetchUserInfo(ctx context.Context, token string) (*OAuth2UserInfo, error)
}

// OAuth2PKCEProvider is a provider that sends an S256 PKCE challenge and verifier; the plugin prefers these methods.
type OAuth2PKCEProvider interface {
	// AuthURLWithChallenge is AuthURL plus code_challenge (S256).
	AuthURLWithChallenge(state, challenge string) string
	// ExchangeCodeWithVerifier is ExchangeCode plus code_verifier.
	ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*OAuth2Token, error)
}

// OAuth2Config holds the configuration for the OAuth2 plugin.
type OAuth2Config struct {
	// Providers is a map of provider name → provider implementation.
	Providers map[string]OAuth2Provider

	// RedirectURL is the base redirect URL for OAuth2 callbacks.
	RedirectURL string

	// StateSecret is the HMAC key used to sign OAuth2 state tokens.
	StateSecret string

	// TokenStore keeps each provider's tokens at sign-in for RefreshOAuthToken; nil keeps none.
	TokenStore OAuthTokenStore

	// TwoFactorURL is the same-origin page a sign-in of a 2FA account goes to; Init requires it next to a 2FA plugin.
	TwoFactorURL string
}

// OAuth2Plugin signs users in and links accounts through registered OAuth2 providers.
type OAuth2Plugin struct {
	mgr          *AuthManager
	providers    map[string]OAuth2Provider
	stateKey     []byte
	tokenStore   OAuthTokenStore
	twoFactorURL string

	noncesMu       sync.Mutex
	usedNonces     map[string]time.Time
	lastNonceSweep time.Time
}

const minStateSecretLen = 16

// NewOAuth2Plugin creates an OAuth2 plugin with the given configuration.
func NewOAuth2Plugin(cfg OAuth2Config) *OAuth2Plugin {
	p := &OAuth2Plugin{
		providers:    make(map[string]OAuth2Provider),
		usedNonces:   make(map[string]time.Time),
		tokenStore:   cfg.TokenStore,
		twoFactorURL: cfg.TwoFactorURL,
	}

	for _, key := range slices.Sorted(maps.Keys(cfg.Providers)) {
		mustValidProviderKey(key)
	}
	maps.Copy(p.providers, cfg.Providers)

	if cfg.StateSecret != "" {
		if len(cfg.StateSecret) < minStateSecretLen {
			panic(fmt.Sprintf(
				"oauth2: StateSecret must be at least %d bytes: state tokens are offline-brute-forceable from any redirect URL. Pass a real secret (openssl rand -base64 24) or leave it empty to mint a random per-process key",
				minStateSecretLen))
		}
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

// TokenStore returns OAuth2Config.TokenStore, or nil.
func (p *OAuth2Plugin) TokenStore() OAuthTokenStore { return p.tokenStore }

// Init refuses, outside DevMode and AllowInMemoryStores, a UserStore that is not an OAuthLinker.
func (p *OAuth2Plugin) Init(mgr *AuthManager) error {
	p.mgr = mgr
	if err := checkTwoFactorURL("oauth2", "OAuth2Config.TwoFactorURL", p.twoFactorURL); err != nil {
		return err
	}
	if p.twoFactorURL == "" {
		for _, name := range mgr.order {
			if _, ok := mgr.plugins[name].(TwoFactorChecker); ok {
				return fmt.Errorf("auth: oauth2 plugin: %q handles two-factor sign-in, so set OAuth2Config.TwoFactorURL to the page that finishes it: an OAuth sign-in of a 2FA account is pending until /2fa/challenge, and \"/\" would refuse it", name)
			}
		}
	}
	if mgr.UserStore() == nil {
		return nil
	}
	if _, ok := mgr.UserStore().(OAuthLinker); !ok {
		cfg := mgr.Config()
		if !cfg.DevMode && !cfg.AllowInMemoryStores {
			return fmt.Errorf("auth: OAuth login requires a durable " +
				"(provider, provider_id) → user_id link store: the " +
				"configured UserStore does not implement OAuthLinker, so an " +
				"IdP emitting an unverified email could sign in as an " +
				"existing account. Use auth.NewEntityUserStore(...) (now a " +
				"linker) or set AuthConfig.AllowInMemoryStores: true for " +
				"local dev")
		}
		slog.Default().Warn("auth: OAuth2 plugin is running without a durable " +
			"OAuthLinker store: production must use one (e.g. " +
			"auth.NewEntityUserStore); an IdP emitting an unverified email " +
			"could otherwise sign in as an existing account")
	}
	return nil
}

// RegisterProvider adds provider under name, panicking on a name that is not 1-64 ASCII letters, digits, '-' or '_'.
func (p *OAuth2Plugin) RegisterProvider(name string, provider OAuth2Provider) {
	mustValidProviderKey(name)
	p.providers[name] = provider
}

const maxProviderKeyLen = 64

func validProviderKey(key string) bool {
	if key == "" || len(key) > maxProviderKeyLen {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func mustValidProviderKey(key string) {
	if !validProviderKey(key) {
		panic(fmt.Sprintf("oauth2: provider key %q must be 1-%d ASCII letters, digits, '-' or '_': it is a route segment and a field of the dot-separated state token",
			key, maxProviderKeyLen))
	}
}

func (p *OAuth2Plugin) providerKeys() []string {
	return slices.Sorted(maps.Keys(p.providers))
}

// RegisterRoutes mounts the OAuth2 auth and callback routes.
func (p *OAuth2Plugin) RegisterRoutes(r *router.Router, basePath string) {
	r = r.Group("", browserAccountOnly)
	r.Get(basePath+"/oauth/{provider}", p.redirectHandler())
	r.Get(basePath+"/oauth/{provider}/link", p.linkHandler())
	r.Get(basePath+"/oauth/{provider}/callback", p.callbackHandler())
	r.Post(basePath+"/oauth/{provider}/callback", p.formPostCallbackHandler())
}

func (p *OAuth2Plugin) formPostCallbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp, ok := p.providers[router.Param(r, "provider")].(OAuth2FormPostProvider)
		if !ok || !fp.CallbackIsFormPost() {
			writeAuthError(w, r, http.StatusBadRequest, reasonUnknownOauthProvider)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		if !parseAuthFormOrRefuse(w, r) {
			return
		}
		q := url.Values{}
		for _, k := range []string{"code", "state", "error"} {
			if v := r.PostFormValue(k); v != "" {
				q.Set(k, v)
			}
		}
		target, ok := sameOriginTarget(&http.Request{URL: &url.URL{Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: q.Encode()}})
		if !ok {
			writeAuthError(w, r, http.StatusBadRequest, reasonUnknownOauthProvider)
			return
		}
		writeSameSiteHop(w, r, p.mgr.config.Pages, handler.SameSiteHopTarget(r, target), oauthSignInTitle)
	}
}

const (
	oauthStateCookie    = "__Host-gofastr_oauth_state"
	oauthStateCookieDev = "gofastr_oauth_state"
)

func (p *OAuth2Plugin) stateCookieName() string {
	if p.mgr.Config().SessionSecure {
		return oauthStateCookie
	}
	return oauthStateCookieDev
}

func (p *OAuth2Plugin) bindStateToBrowser(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     p.stateCookieName(),
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   p.mgr.Config().SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(stateTTL / time.Second),
	})
}

func (p *OAuth2Plugin) requireBoundBrowser(w http.ResponseWriter, r *http.Request, state string) bool {
	http.SetCookie(w, &http.Cookie{
		Name:     p.stateCookieName(),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   p.mgr.Config().SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	c, err := r.Cookie(p.stateCookieName())
	if err != nil || c.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) == 1
}

func (p *OAuth2Plugin) redirectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := router.Param(r, "provider")
		provider, ok := p.providers[providerName]
		if !ok {
			writeAuthError(w, r, http.StatusBadRequest, reasonUnknownOauthProvider)
			return
		}

		state, err := p.generateState(providerName, "")
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonStateGenerationFailed)
			return
		}
		p.bindStateToBrowser(w, state)

		http.Redirect(w, r, p.authURL(provider, state), http.StatusFound)
	}
}

func (p *OAuth2Plugin) linkHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := router.Param(r, "provider")
		provider, ok := p.providers[providerName]
		if !ok {
			writeAuthError(w, r, http.StatusBadRequest, reasonUnknownOauthProvider)
			return
		}
		userID, ok := p.requireSessionUserID(w, r)
		if !ok {
			return // requireSessionUserID already wrote the 401/403
		}
		state, err := p.generateState(providerName, userID)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonStateGenerationFailed)
			return
		}
		p.bindStateToBrowser(w, state)
		http.Redirect(w, r, p.authURL(provider, state), http.StatusFound)
	}
}

func (p *OAuth2Plugin) linkSessionHolds(r *http.Request, userID string) bool {
	sess, err := p.mgr.requestSession(r, false)
	return err == nil && !sess.PendingTwoFactor && sess.UserID == userID
}

func (p *OAuth2Plugin) undoLink(ctx context.Context, store UserStore, userID, provider string) bool {
	unlinker, ok := store.(AccountUnlinker)
	if !ok {
		slog.Error("auth: a link written across a credential change cannot be undone: the user store does not implement AccountUnlinker",
			"user_hash", hashedIdentifier(userID))
		return false
	}
	undo, cancel := undoContext(ctx)
	defer cancel()
	if err := unlinker.UnlinkOAuth(undo, userID, provider); err != nil {
		slog.Error("auth: removing a link written across a credential change failed",
			"user_hash", hashedIdentifier(userID), "err", err)
		return false
	}
	return true
}

func (p *OAuth2Plugin) requireSessionUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	if rejectInProcessAccountOperation(w, r) {
		return "", false
	}
	sess, err := p.mgr.requestSession(r, false)
	if err != nil {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
		return "", false
	}
	if sess.PendingTwoFactor {
		writeAuthError(w, r, http.StatusForbidden, reason2faRequired)
		return "", false
	}
	if sess.UserID == "" {
		writeAuthError(w, r, http.StatusUnauthorized, reasonNoSession)
		return "", false
	}
	return sess.UserID, true
}

func (p *OAuth2Plugin) callbackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := router.Param(r, "provider")
		provider, ok := p.providers[providerName]
		if !ok {
			writeAuthError(w, r, http.StatusBadRequest, reasonUnknownOauthProvider)
			return
		}

		stateParam := r.URL.Query().Get("state")
		if p.sameSiteHop(w, r, stateParam, providerName) {
			return
		}
		if !p.requireBoundBrowser(w, r, stateParam) {
			writeAuthError(w, r, http.StatusBadRequest, reasonOauthWrongBrowser)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			writeAuthError(w, r, http.StatusBadRequest, reasonMissingAuthorisationCode)
			return
		}
		linkUserID, ok := p.validateAndConsumeState(stateParam, providerName)
		if !ok {
			writeAuthError(w, r, http.StatusBadRequest, reasonInvalidOrExpiredState)
			return
		}

		tok, err := p.exchangeCode(r.Context(), provider, code, stateParam)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, reasonCodeExchangeFailed)
			return
		}

		info, err := provider.FetchUserInfo(r.Context(), tok.AccessToken)
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonFailedToFetchUserInfo)
			return
		}
		canonical, err := p.mgr.canonicalizeEmail(info.Email)
		if err != nil {
			slog.Warn("oauth email refused",
				"plugin", "oauth2", "email_hash", hashedIdentifier(info.Email), "err", err)
			writeAuthErrorFor(w, r, http.StatusBadRequest, err, reasonComposedEmail)
			return
		}
		info.Email = canonical
		if !emailWithinLimit(w, r, info.Email) {
			return
		}

		info.Provider = providerName

		userStore := p.mgr.UserStore()
		if userStore == nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonUserStoreNotConfigured)
			return
		}

		if linkUserID != "" {
			currentID, ok := p.requireSessionUserID(w, r)
			if !ok {
				return // 401/403 already written
			}
			if currentID != linkUserID {
				writeAuthError(w, r, http.StatusForbidden, reasonLinkSessionMismatch)
				return
			}
			snap, err := p.mgr.snapshotCredential(r.Context(), linkUserID)
			if err != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
				return
			}
			linker, ok := userStore.(OAuthLinker)
			if !ok {
				writeAuthError(w, r, http.StatusInternalServerError, reasonStoreNoLinking)
				return
			}
			owner, ferr := linker.FindByOAuth(r.Context(), info.Provider, info.ID)
			switch {
			case ferr != nil && !errors.Is(ferr, ErrUserNotFound):
				writeAuthError(w, r, http.StatusInternalServerError, reasonLinkLookupFailed)
				return
			case ferr == nil && owner != nil && owner.GetID() != linkUserID:
				p.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:   "oauth.refused",
					UserID: linkUserID,
					Email:  info.Email,
					Remote: remoteHost(r),
					Meta:   map[string]string{"provider": providerName, "reason": "provider_already_linked"},
				})
				writeAuthError(w, r, http.StatusConflict, reasonProviderLinkedElsewhere)
				return
			}
			if err := linkOAuthPreferEnriched(r.Context(), userStore, linker, linkUserID, info); err != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reasonLinkFailed)
				return
			}
			bound, berr := p.authoritativeOAuthOwner(r.Context(), linker, info)
			if berr != nil {
				writeAuthError(w, r, http.StatusInternalServerError, reasonLinkLookupFailed)
				return
			}
			if bound.GetID() != linkUserID {
				p.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:   "oauth.refused",
					UserID: linkUserID,
					Email:  info.Email,
					Remote: remoteHost(r),
					Meta:   map[string]string{"provider": providerName, "reason": "provider_already_linked"},
				})
				writeAuthError(w, r, http.StatusConflict, reasonProviderLinkedElsewhere)
				return
			}
			p.saveProviderTokens(r.Context(), linkUserID, providerName, tok)
			if !p.linkSessionHolds(r, linkUserID) || snap.moved(r.Context(), p.mgr) {
				tokensGone := p.dropProviderTokenRow(r.Context(), linkUserID, providerName)
				if !p.undoLink(r.Context(), userStore, linkUserID, info.Provider) || !tokensGone {
					writeAuthError(w, r, http.StatusInternalServerError, reasonLinkFailed)
					return
				}
				writeAuthError(w, r, http.StatusUnauthorized, reasonInvalidSession)
				return
			}
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "oauth.linked",
				UserID: linkUserID,
				Email:  info.Email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"provider": providerName, "flow": "authenticated_link"},
			})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		user, linked, err := p.resolveOAuthUser(r.Context(), userStore, info)
		if err != nil {
			var collision *oauthEmailCollision
			switch {
			case errors.As(err, &collision):
				p.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:   "oauth.refused",
					UserID: collision.userID,
					Email:  info.Email,
					Meta:   map[string]string{"provider": providerName, "reason": "link_conflict"},
				})
				writeAuthError(w, r, http.StatusConflict, reasonEmailTaken)
			case errors.Is(err, errOAuthNoEmail):
				p.mgr.emitSecurity(r.Context(), SecurityEvent{
					Kind:   "oauth.refused",
					Remote: remoteHost(r),
					Meta:   map[string]string{"provider": providerName, "reason": "no_email"},
				})
				writeAuthError(w, r, http.StatusBadRequest, reasonOauthNoEmail)
			case errors.Is(err, errOAuthLookupFailed):
				writeAuthError(w, r, http.StatusInternalServerError, reasonUserLookupFailed)
			case errors.Is(err, errOAuthNoLinker):
				writeAuthError(w, r, http.StatusInternalServerError, reasonOauthNotConfigured)
			default:
				writeAuthError(w, r, http.StatusConflict, reasonCouldNotCreateUser)
			}
			return
		}

		cfg := p.mgr.Config()
		sess, pending, err := p.mgr.MintSession(r.Context(), user.GetID(), cfg.SessionTTL)
		if errors.Is(err, ErrTwoFactorEnforcement) {
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind: "login.failed", UserID: user.GetID(), Email: info.Email, Remote: remoteHost(r),
				Meta: map[string]string{"reason": "twofa_failclosed", "via": "oauth", "provider": providerName},
			})
			writeAuthError(w, r, http.StatusInternalServerError, reasonTwoFactorUnavailable)
			return
		}
		if err != nil {
			writeAuthError(w, r, http.StatusInternalServerError, reasonSessionCreateFailed)
			return
		}
		p.saveProviderTokens(r.Context(), user.GetID(), providerName, tok)
		if !p.linkStillNames(r.Context(), userStore, info, user.GetID()) {
			undo, cancel := undoContext(r.Context())
			delErr := p.mgr.SessionStore().Delete(undo, sess.Token)
			cancel()
			if delErr != nil {
				slog.Warn("auth: dropping a session minted under a removed OAuth link failed",
					"user_hash", hashedIdentifier(user.GetID()), "err", delErr)
			}
			p.dropProviderTokenRow(r.Context(), user.GetID(), providerName)
			p.mgr.emitSecurity(r.Context(), SecurityEvent{
				Kind:   "oauth.refused",
				UserID: user.GetID(),
				Email:  info.Email,
				Remote: remoteHost(r),
				Meta:   map[string]string{"provider": providerName, "reason": "link_removed"},
			})
			writeAuthError(w, r, http.StatusUnauthorized, reasonOauthLinkRemoved)
			return
		}
		kind := "oauth.login"
		if linked {
			kind = "oauth.linked"
		}
		p.mgr.emitSecurity(r.Context(), SecurityEvent{
			Kind: kind, UserID: user.GetID(), Email: info.Email, Remote: remoteHost(r),
			Meta: map[string]string{"provider": providerName},
		})
		mintSessionCookie(w, cfg, sess.Token, sess.ExpiresAt)

		target := "/"
		if pending {
			target = safeRedirectURL(p.twoFactorURL)
		}
		if handler.NeedsSameSiteHop(r) {
			writeSameSiteHop(w, r, p.mgr.config.Pages, target, oauthSignInTitle)
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func (p *OAuth2Plugin) sameSiteHop(w http.ResponseWriter, r *http.Request, state, providerName string) bool {
	if !handler.NeedsSameSiteHop(r) {
		return false
	}
	if _, err := p.mgr.requestSession(r, false); err == nil {
		return false
	}
	c, err := r.Cookie(p.stateCookieName())
	if err != nil || c.Value == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		return false
	}
	userID, nonce, _, ok := p.verifyState(state, providerName)
	if !ok || userID == "" || !p.stateUnspent(nonce) {
		return false
	}
	target, ok := sameOriginTarget(r)
	if !ok {
		return false
	}
	writeSameSiteHop(w, r, p.mgr.config.Pages, handler.SameSiteHopTarget(r, target), oauthHopTitle)
	return true
}

func sameOriginTarget(r *http.Request) (string, bool) {
	path := r.URL.EscapedPath()
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", false
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	return path, true
}

var errOAuthEmailCollision = errors.New("oauth: email collision with pre-existing account")

type oauthEmailCollision struct{ userID string }

func (e *oauthEmailCollision) Error() string { return errOAuthEmailCollision.Error() }
func (e *oauthEmailCollision) Unwrap() error { return errOAuthEmailCollision }

var errOAuthNoEmail = errors.New("oauth: provider asserted no email")

func (p *OAuth2Plugin) saveProviderTokens(ctx context.Context, userID, providerKey string, tok *OAuth2Token) {
	if p.tokenStore == nil || tok == nil {
		return
	}
	if err := p.tokenStore.Save(ctx, OAuthTokenRecord{
		UserID:       userID,
		Provider:     providerKey,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		Expiry:       tok.Expiry,
	}); err != nil {
		slog.Warn("auth: storing provider tokens failed; refresh unavailable until the next sign-in",
			"plugin", "oauth2", "user_hash", hashedIdentifier(userID), "err", err)
	}
}

func (p *OAuth2Plugin) dropProviderTokenRow(ctx context.Context, userID, providerKey string) bool {
	if p.tokenStore == nil {
		return true
	}
	undo, cancel := undoContext(ctx)
	defer cancel()
	if err := p.tokenStore.Delete(undo, userID, providerKey); err != nil {
		slog.Error("auth: dropping provider tokens stored under a refused OAuth callback failed",
			"plugin", "oauth2", "user_hash", hashedIdentifier(userID), "err", err)
		return false
	}
	return true
}

func (p *OAuth2Plugin) pkceVerifier(state string) string {
	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte("pkce|" + state))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (p *OAuth2Plugin) authURL(provider OAuth2Provider, state string) string {
	if pp, ok := provider.(OAuth2PKCEProvider); ok {
		return pp.AuthURLWithChallenge(state, pkceChallenge(p.pkceVerifier(state)))
	}
	return provider.AuthURL(state)
}

func (p *OAuth2Plugin) exchangeCode(ctx context.Context, provider OAuth2Provider, code, state string) (*OAuth2Token, error) {
	if pp, ok := provider.(OAuth2PKCEProvider); ok {
		return pp.ExchangeCodeWithVerifier(ctx, code, p.pkceVerifier(state))
	}
	return provider.ExchangeCode(ctx, code)
}

var errOAuthLookupFailed = errors.New("oauth: lookup failed")

var errOAuthNoLinker = errors.New("oauth: UserStore does not implement OAuthLinker")

func (p *OAuth2Plugin) resolveOAuthUser(ctx context.Context, store UserStore, info *OAuth2UserInfo) (User, bool, error) {
	linker, hasLinker := store.(OAuthLinker)
	if !hasLinker {
		return nil, false, errOAuthNoLinker
	}

	if found, err := linker.FindByOAuth(ctx, info.Provider, info.ID); err == nil {
		if refresher, ok := store.(OAuthProfileRefresher); ok {
			_ = refresher.RefreshOAuthProfile(ctx, found.GetID(), info.Provider, info.ID, OAuthAccountProfile{
				Email:     info.Email,
				Name:      info.Name,
				AvatarURL: info.AvatarURL,
			})
		}
		if info.EmailVerified && info.Email != "" && found.GetEmail() == info.Email {
			markVerifiedIfUnproven(ctx, store, found.GetID())
		}
		return found, false, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, false, fmt.Errorf("%w: FindByOAuth: %v", errOAuthLookupFailed, err)
	}

	if info.Email == "" {
		return nil, false, errOAuthNoEmail
	}

	existing, _, err := store.FindByEmail(ctx, info.Email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, false, fmt.Errorf("%w: FindByEmail: %v", errOAuthLookupFailed, err)
	}
	if err == nil {
		if info.EmailVerified {
			hasPw := true // conservative default when PasswordChecker is absent
			if checker, ok := store.(PasswordChecker); ok {
				hp, herr := checker.HasPassword(ctx, existing.GetID())
				if herr != nil {
					return nil, false, fmt.Errorf("%w: HasPassword: %v", errOAuthLookupFailed, herr)
				}
				hasPw = hp
			}
			if hasPw {
				return nil, false, &oauthEmailCollision{userID: existing.GetID()}
			}
			proven := false // no checker: nothing says the address was proven
			if checker, ok := store.(EmailVerifiedChecker); ok {
				v, verr := checker.IsEmailVerified(ctx, existing.GetID())
				if verr != nil {
					return nil, false, fmt.Errorf("%w: IsEmailVerified: %v", errOAuthLookupFailed, verr)
				}
				proven = v
			}
			if !proven {
				return nil, false, &oauthEmailCollision{userID: existing.GetID()}
			}
			if linkErr := linkOAuthPreferEnriched(ctx, store, linker, existing.GetID(), info); linkErr != nil {
				return nil, false, fmt.Errorf("%w: LinkOAuth: %v", errOAuthLookupFailed, linkErr)
			}
			owner, oerr := p.authoritativeOAuthOwner(ctx, linker, info)
			if oerr != nil {
				return nil, false, oerr
			}
			return owner, true, nil
		}
	}

	var newUser User
	var createErr error
	if creator, ok := store.(OAuthUserCreator); ok {
		newUser, createErr = creator.CreateUserNoPassword(ctx, info.Email, p.mgr.DefaultRoles())
	} else {
		newUser, createErr = store.CreateUser(ctx, info.Email, placeholderPasswordHash(), p.mgr.DefaultRoles())
	}
	if createErr != nil {
		return nil, false, createErr
	}
	if linkErr := linkOAuthPreferEnriched(ctx, store, linker, newUser.GetID(), info); linkErr != nil {
		return nil, false, fmt.Errorf("%w: LinkOAuth: %v", errOAuthLookupFailed, linkErr)
	}

	owner, oerr := p.authoritativeOAuthOwner(ctx, linker, info)
	if oerr != nil {
		return nil, false, oerr
	}
	if info.EmailVerified && owner.GetID() == newUser.GetID() {
		markVerifiedIfUnproven(ctx, store, owner.GetID())
	}
	return owner, true, nil
}

func markVerifiedIfUnproven(ctx context.Context, store UserStore, userID string) {
	verifier, ok := store.(EmailVerifier)
	if !ok {
		return
	}
	if checker, ok := store.(EmailVerifiedChecker); ok {
		if v, err := checker.IsEmailVerified(ctx, userID); err == nil && v {
			return
		}
	}
	if err := verifier.MarkEmailVerified(ctx, userID); err != nil {
		slog.Warn("oauth could not record a verified email",
			"plugin", "oauth2", "user_hash", hashedIdentifier(userID), "err", err)
	}
}

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

func (p *OAuth2Plugin) linkStillNames(ctx context.Context, store UserStore, info *OAuth2UserInfo, userID string) bool {
	linker, ok := store.(OAuthLinker)
	if !ok {
		return false
	}
	u, err := linker.FindByOAuth(ctx, info.Provider, info.ID)
	return err == nil && u != nil && u.GetID() == userID
}

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

const stateTTL = 10 * time.Minute

const nonceGCThreshold = 4096

const (
	nonceGCSweepInterval = time.Minute
	nonceMaxEntries      = 1 << 16
	nonceEvictBatch      = nonceMaxEntries / 16
)

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

func (p *OAuth2Plugin) validateAndConsumeState(state, expectedProvider string) (userID string, ok bool) {
	userID, nonceB64, expiry, ok := p.verifyState(state, expectedProvider)
	if !ok {
		return "", false
	}

	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	if _, replayed := p.usedNonces[nonceB64]; replayed {
		return "", false
	}
	now := time.Now()
	if len(p.usedNonces) >= nonceGCThreshold && now.Sub(p.lastNonceSweep) >= nonceGCSweepInterval {
		p.purgeExpiredNoncesLocked(now)
	}
	if len(p.usedNonces) >= nonceMaxEntries {
		p.evictEarliestNoncesLocked(nonceEvictBatch)
	}
	p.usedNonces[nonceB64] = expiry
	return userID, true
}

func (p *OAuth2Plugin) evictEarliestNoncesLocked(n int) {
	type entry struct {
		nonce  string
		expiry time.Time
	}
	all := make([]entry, 0, len(p.usedNonces))
	for nonce, exp := range p.usedNonces {
		all = append(all, entry{nonce, exp})
	}
	slices.SortFunc(all, func(a, b entry) int {
		if c := a.expiry.Compare(b.expiry); c != 0 {
			return c
		}
		return strings.Compare(a.nonce, b.nonce)
	})
	for _, e := range all[:min(n, len(all))] {
		delete(p.usedNonces, e.nonce)
	}
}

func (p *OAuth2Plugin) stateUnspent(nonceB64 string) bool {
	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	_, spent := p.usedNonces[nonceB64]
	return !spent
}

func (p *OAuth2Plugin) verifyState(state, expectedProvider string) (userID, nonceB64 string, expiry time.Time, ok bool) {
	parts := strings.SplitN(state, ".", 5)
	if len(parts) != 5 {
		return "", "", time.Time{}, false
	}
	nonceB64 = parts[0]
	provider, expiryStr, userB64, sig := parts[1], parts[2], parts[3], parts[4]
	if provider != expectedProvider {
		return "", "", time.Time{}, false
	}

	payload := nonceB64 + "." + provider + "." + expiryStr + "." + userB64
	mac := hmac.New(sha256.New, p.stateKey)
	mac.Write([]byte(payload))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return "", "", time.Time{}, false
	}

	expiryUnix, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return "", "", time.Time{}, false
	}
	expiry = time.Unix(expiryUnix, 0)
	if time.Now().After(expiry) {
		return "", "", time.Time{}, false
	}

	uidBytes, err := base64.RawURLEncoding.DecodeString(userB64)
	if err != nil {
		return "", "", time.Time{}, false
	}
	return string(uidBytes), nonceB64, expiry, true
}

func (p *OAuth2Plugin) purgeExpiredNonces() {
	p.noncesMu.Lock()
	defer p.noncesMu.Unlock()
	p.purgeExpiredNoncesLocked(time.Now())
}

func (p *OAuth2Plugin) purgeExpiredNoncesLocked(now time.Time) {
	for n, exp := range p.usedNonces {
		if !now.Before(exp) {
			delete(p.usedNonces, n)
		}
	}
	p.lastNonceSweep = now
}

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

var defaultOAuthHTTPClient = oidcNoRedirect(&http.Client{Timeout: 10 * time.Second})

const oauthProviderMaxBody = 1 << 20

const maxTokenLifetime = 10 * 365 * 24 * time.Hour

func tokenExpiry(expiresIn int64) time.Time {
	switch {
	case expiresIn > int64(maxTokenLifetime/time.Second):
		return time.Now().Add(maxTokenLifetime)
	case expiresIn > 0:
		return time.Now().Add(time.Duration(expiresIn) * time.Second)
	case expiresIn < 0:
		return time.Now()
	}
	return time.Time{}
}

// GoogleProvider implements OAuth2Provider for Google's OAuth2 endpoints.
type GoogleProvider struct {
	// DisablePKCE stops sending PKCE.
	DisablePKCE bool

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

func (g *GoogleProvider) AuthURL(state string) string { return g.AuthURLWithChallenge(state, "") }

// AuthURLWithChallenge implements OAuth2PKCEProvider.
func (g *GoogleProvider) AuthURLWithChallenge(state, challenge string) string {
	u, _ := url.Parse("https://accounts.google.com/o/oauth2/v2/auth")
	q := u.Query()
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirectURL)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	setPKCEChallenge(q, challenge, g.DisablePKCE)
	u.RawQuery = q.Encode()
	return u.String()
}

func (g *GoogleProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	return g.ExchangeCodeWithVerifier(ctx, code, "")
}

// ExchangeCodeWithVerifier implements OAuth2PKCEProvider.
func (g *GoogleProvider) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*OAuth2Token, error) {
	data := url.Values{}
	data.Set("code", code)
	setPKCEVerifier(data, verifier, g.DisablePKCE)
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, oauthProviderMaxBody)).Decode(&body); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       tokenExpiry(body.ExpiresIn),
	}, nil
}

// RefreshToken exchanges a Google refresh token for a fresh access token.
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, oauthProviderMaxBody)).Decode(&body); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       tokenExpiry(body.ExpiresIn),
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
	//gofastr:allow(floatnum) email_verified is read as a bool or string only
	if err := json.NewDecoder(io.LimitReader(resp.Body, oauthProviderMaxBody)).Decode(&body); err != nil {
		return nil, err
	}

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
	// DisablePKCE stops sending PKCE.
	DisablePKCE bool

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

func (g *GitHubProvider) AuthURL(state string) string { return g.AuthURLWithChallenge(state, "") }

// AuthURLWithChallenge implements OAuth2PKCEProvider.
func (g *GitHubProvider) AuthURLWithChallenge(state, challenge string) string {
	u, _ := url.Parse("https://github.com/login/oauth/authorize")
	q := u.Query()
	q.Set("client_id", g.clientID)
	q.Set("redirect_uri", g.redirectURL)
	q.Set("scope", "user:email")
	q.Set("state", state)
	setPKCEChallenge(q, challenge, g.DisablePKCE)
	u.RawQuery = q.Encode()
	return u.String()
}

func (g *GitHubProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	return g.ExchangeCodeWithVerifier(ctx, code, "")
}

// ExchangeCodeWithVerifier implements OAuth2PKCEProvider.
func (g *GitHubProvider) ExchangeCodeWithVerifier(ctx context.Context, code, verifier string) (*OAuth2Token, error) {
	data := url.Values{}
	data.Set("code", code)
	setPKCEVerifier(data, verifier, g.DisablePKCE)
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
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, oauthProviderMaxBody)).Decode(&body); err != nil {
		return nil, err
	}
	if err := githubTokenRefusal("token exchange", body.Error, body.AccessToken); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       tokenExpiry(body.ExpiresIn),
	}, nil
}

// RefreshToken exchanges a GitHub refresh token for a fresh access token.
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
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github: token refresh returned %d", resp.StatusCode)
	}

	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, oauthProviderMaxBody)).Decode(&body); err != nil {
		return nil, err
	}
	if err := githubTokenRefusal("token refresh", body.Error, body.AccessToken); err != nil {
		return nil, err
	}

	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       tokenExpiry(body.ExpiresIn),
	}, nil
}

func githubTokenRefusal(what, code, accessToken string) error {
	if code == "" && accessToken != "" {
		return nil
	}
	if code == "" {
		return fmt.Errorf("github: %s returned no access token", what)
	}
	plain := len(code) <= 64
	for i := 0; plain && i < len(code); i++ {
		c := code[i]
		plain = c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
	}
	if !plain {
		return fmt.Errorf("github: %s was refused", what)
	}
	return fmt.Errorf("github: %s was refused: %s", what, code)
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, oauthProviderMaxBody)).Decode(&body); err != nil {
		return nil, err
	}

	email := body.Email
	emailVerified := false
	if got, verified, err := g.fetchPrimaryEmail(ctx, token); err == nil && got != "" {
		email = got
		emailVerified = verified
	} else if email != "" {
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

func (g *GitHubProvider) fetchPrimaryEmail(ctx context.Context, token string) (string, bool, error) {
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
	if err := json.NewDecoder(io.LimitReader(resp.Body, oauthProviderMaxBody)).Decode(&emails); err != nil {
		return "", false, err
	}
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true, nil
		}
	}
	for _, e := range emails {
		if e.Verified {
			return e.Email, true, nil
		}
	}
	return "", false, nil
}

func parseGoogleEmailVerified(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

func setPKCEChallenge(q url.Values, challenge string, disabled bool) {
	if challenge == "" || disabled {
		return
	}
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
}

func setPKCEVerifier(form url.Values, verifier string, disabled bool) {
	if verifier == "" || disabled {
		return
	}
	form.Set("code_verifier", verifier)
}

var (
	_ OAuth2PKCEProvider = (*GoogleProvider)(nil)
	_ OAuth2PKCEProvider = (*GitHubProvider)(nil)
	_ OAuth2PKCEProvider = (*OIDCProvider)(nil)
)
