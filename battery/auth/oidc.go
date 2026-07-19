package auth

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ─── OIDC (generic OpenID Connect provider) ─────────────────────────────────
//
// OIDCProvider implements OAuth2Provider for any OpenID Connect-compliant
// identity provider (Keycloak, Authentik, Authelia, Zitadel, Entra ID, Okta,
// …) using OIDC discovery + JWKS. It is a SEPARATE verifier from the HS256
// JWT verifier in token.go: app-issued session JWTs stay HS256-only by design,
// and only third-party id_tokens are validated here against the IdP's published
// asymmetric keys.
//
// id_token verification is the security core — see oidc_jwks.go. Signature
// algorithms are pinned to RS256/ES256; "none", HS256 and alg-confusion
// attacks are rejected before any key lookup.

// OIDCClaimsMapping maps id_token/userinfo claim names to the fields
// OAuth2UserInfo exposes. Zero-value fields fall back to the OIDC standard
// claims (sub, email, name, picture).
type OIDCClaimsMapping struct {
	IDClaim     string
	EmailClaim  string
	NameClaim   string
	AvatarClaim string
	// EmailVerifiedClaim names the boolean claim that asserts the email
	// has been verified by the IdP. Defaults to "email_verified" per
	// OIDC Core §5.1. Some IdPs emit the value as the string "true"
	// rather than a JSON bool; both shapes are accepted (the string is
	// matched case-insensitively). When the claim is absent or unparseable,
	// EmailVerified is set to false — an unverifiable email must never
	// bind to an existing local account.
	EmailVerifiedClaim string
}

// OIDCConfig configures an OIDCProvider.
type OIDCConfig struct {
	// Issuer is the IdP issuer identifier, e.g.
	// "https://keycloak.example/realms/myrealm". Required. Must be an https://
	// URL; http:// is accepted only for localhost/127.0.0.1/::1 (local IdPs
	// and tests).
	Issuer string
	// ClientID is the OAuth client_id registered at the IdP. Required.
	ClientID string
	// ClientSecret is the confidential-client secret. Required.
	ClientSecret string
	// RedirectURL is the app's callback URL. Required.
	RedirectURL string
	// ProviderName is returned by Name() and set as OAuth2UserInfo.Provider.
	// Defaults to "oidc".
	ProviderName string
	// Scopes requested at the authorization endpoint. Defaults to
	// ["openid","email","profile"].
	Scopes []string
	// Claims maps id_token/userinfo claims to OAuth2UserInfo fields.
	Claims OIDCClaimsMapping
	// HTTPClient overrides the default 10s-deadline client. If nil,
	// defaultOAuthHTTPClient is used.
	HTTPClient *http.Client
	// JWKSCacheTTL is how long a fetched JWKS is trusted before refresh.
	// Defaults to 1h.
	JWKSCacheTTL time.Duration
}

// OIDCProvider implements OAuth2Provider for a generic OIDC IdP.
type OIDCProvider struct {
	cfg        OIDCConfig
	name       string
	scopes     []string
	httpClient *http.Client

	// discovery is cached forever after first success; a process restart picks
	// up IdP endpoint moves. Guarded by mu; failures are NOT cached, so a
	// transient IdP outage remains retryable on the next call.
	mu        sync.Mutex
	discovery *oidcDiscovery

	jwks *jwksCache

	// verified id_token claims cached by access token so FetchUserInfo doesn't
	// re-fetch. Bounded (cap 1024); evicts oldest by insert order.
	claims *orderedCache
}

// NewOIDCProvider validates cfg and returns a provider WITHOUT performing any
// network I/O — discovery runs lazily on first use.
func NewOIDCProvider(cfg OIDCConfig) (*OIDCProvider, error) {
	if cfg.Issuer == "" {
		return nil, errors.New("oidc: Issuer is required")
	}
	if cfg.ClientID == "" {
		return nil, errors.New("oidc: ClientID is required")
	}
	if cfg.ClientSecret == "" {
		// Confidential-client only. The secret is the sole protection on the
		// code→token exchange (this provider sends no PKCE code_verifier). Do
		// NOT relax this to support public/SPA clients without first adding a
		// random per-request PKCE verifier bound via a cookie or store — a
		// secret-less exchange with no verifier is unprotected.
		return nil, errors.New("oidc: ClientSecret is required")
	}
	if cfg.RedirectURL == "" {
		return nil, errors.New("oidc: RedirectURL is required")
	}
	u, err := url.Parse(cfg.Issuer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errors.New("oidc: Issuer must be a valid absolute URL")
	}
	switch u.Scheme {
	case "https":
		// ok
	case "http":
		if !isLocalhostHost(u.Hostname()) {
			return nil, errors.New("oidc: Issuer must be https:// (http only allowed for localhost)")
		}
	default:
		return nil, errors.New("oidc: Issuer must be an https:// URL")
	}

	name := cfg.ProviderName
	if name == "" {
		name = "oidc"
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{"openid", "email", "profile"}
	}
	claims := cfg.Claims
	if claims.IDClaim == "" {
		claims.IDClaim = "sub"
	}
	if claims.EmailClaim == "" {
		claims.EmailClaim = "email"
	}
	if claims.NameClaim == "" {
		claims.NameClaim = "name"
	}
	if claims.AvatarClaim == "" {
		claims.AvatarClaim = "picture"
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = defaultOAuthHTTPClient
	}
	ttl := cfg.JWKSCacheTTL
	if ttl <= 0 {
		ttl = time.Hour
	}
	cfg.Claims = claims

	return &OIDCProvider{
		cfg:        cfg,
		name:       name,
		scopes:     scopes,
		httpClient: hc,
		jwks:       &jwksCache{httpClient: hc, ttl: ttl},
		claims:     newOrderedCache(1024),
	}, nil
}

// Name returns the provider identifier.
func (p *OIDCProvider) Name() string { return p.name }

// ─── Discovery ───────────────────────────────────────────────────────────────

// oidcDiscovery is the subset of the OpenID discovery document we consume.
type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// discoveryURL joins the well-known path to the issuer using exactly one slash,
// tolerating a single trailing slash on the issuer.
func discoveryURL(issuer string) string {
	return strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
}

// normalizeIssuer trims a single trailing slash for issuer comparison. OIDC
// §4.3 requires the document's issuer to match the configured one exactly.
func normalizeIssuer(s string) string {
	return strings.TrimSuffix(s, "/")
}

// isLocalhostHost reports whether host is a loopback address, permitting
// plain-http Issuers for local IdPs and tests.
func isLocalhostHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// ensureDiscovery returns the cached discovery document, fetching it on first
// use. The mutex serializes concurrent first-callers (no thundering herd) and
// caches only on success so a failed fetch stays retryable.
func (p *OIDCProvider) ensureDiscovery(ctx context.Context) (*oidcDiscovery, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovery != nil {
		return p.discovery, nil
	}
	d, err := p.fetchDiscovery(ctx)
	if err != nil {
		return nil, err
	}
	p.discovery = d
	return d, nil
}

func (p *OIDCProvider) fetchDiscovery(ctx context.Context) (*oidcDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL(p.cfg.Issuer), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var d oidcDiscovery
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	if normalizeIssuer(d.Issuer) != normalizeIssuer(p.cfg.Issuer) {
		// Issuer-spoofing guard (OIDC §4.3): a discovery document served from
		// one origin that claims a different issuer could impersonate the
		// configured IdP.
		return nil, errors.New("oidc: discovery issuer does not match configured issuer")
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return nil, errors.New("oidc: discovery document missing required endpoints")
	}
	return &d, nil
}

// ─── OAuth2Provider interface ────────────────────────────────────────────────

// AuthURL builds the authorization-endpoint redirect. No nonce is sent: this
// is the confidential-client authorization-code flow — the code is single-use
// and exchanged server-to-server with the client secret, and the OAuth2
// plugin's HMAC state token already binds the callback to this redirect. A
// nonce is only mandatory for the implicit/hybrid flow, where no server-side
// code exchange happens.
//
// AuthURL cannot return an error. If discovery has not yet run (or fails), it
// falls back to a best-effort "<issuer>/authorize?…" URL and lets the callback
// fail cleanly rather than send the user to a stale or wrong endpoint.
func (p *OIDCProvider) AuthURL(state string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint := strings.TrimSuffix(p.cfg.Issuer, "/") + "/authorize"
	if d, err := p.ensureDiscovery(ctx); err == nil {
		endpoint = d.AuthorizationEndpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		u = &url.URL{Path: "/"}
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("scope", strings.Join(p.scopes, " "))
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

// ExchangeCode trades the authorization code for tokens at the token endpoint,
// then fully verifies the id_token BEFORE returning. Verified claims are cached
// by access token so FetchUserInfo can map them without re-fetching.
func (p *OIDCProvider) ExchangeCode(ctx context.Context, code string) (*OAuth2Token, error) {
	d, err := p.ensureDiscovery(ctx)
	if err != nil {
		return nil, err
	}
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", p.cfg.RedirectURL)
	data.Set("client_id", p.cfg.ClientID)
	data.Set("client_secret", p.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		d.TokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: token exchange returned %d", resp.StatusCode)
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		IDToken      string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, err
	}
	if body.IDToken == "" {
		// scope includes openid, so an id_token is expected. Without it we
		// cannot establish identity — refuse rather than trust the (unsigned)
		// access token alone.
		return nil, errors.New("oidc: token response missing id_token")
	}
	claims, err := p.verifyIDToken(ctx, body.IDToken, d.JWKSURI)
	if err != nil {
		return nil, err
	}
	if body.AccessToken != "" {
		p.claims.put(body.AccessToken, claims)
	}
	return &OAuth2Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// FetchUserInfo maps the verified id_token claims (cached at ExchangeCode) into
// OAuth2UserInfo. If the mapped email is empty and the IdP exposes a
// userinfo_endpoint, it is fetched with the bearer token and only the missing
// email/name/avatar fields are merged in (the userinfo sub MUST match the
// id_token sub). With no cached claims, the info is built purely from userinfo.
func (p *OIDCProvider) FetchUserInfo(ctx context.Context, token string) (*OAuth2UserInfo, error) {
	if cached, ok := p.claims.get(token); ok {
		return p.userInfoFromClaims(ctx, token, cached)
	}
	// No verified claims for this token (e.g. process restart with a stale
	// access token). Fall back to userinfo if the IdP exposes one.
	d, err := p.ensureDiscovery(ctx)
	if err != nil {
		return nil, err
	}
	if d.UserinfoEndpoint == "" {
		return nil, errors.New("oidc: no verified claims for token and no userinfo endpoint")
	}
	ui, err := p.fetchUserinfo(ctx, token, d.UserinfoEndpoint)
	if err != nil {
		return nil, err
	}
	// With no id_token to anchor identity, ID must come from userinfo's sub.
	id := claimString(ui, "sub")
	if id == "" {
		return nil, errors.New("oidc: userinfo missing subject")
	}
	return &OAuth2UserInfo{
		ID:            id,
		Email:         claimString(ui, p.cfg.Claims.EmailClaim),
		Name:          claimString(ui, p.cfg.Claims.NameClaim),
		AvatarURL:     claimString(ui, p.cfg.Claims.AvatarClaim),
		Provider:      p.name,
		EmailVerified: parseEmailVerified(ui, p.cfg.Claims.EmailVerifiedClaim),
	}, nil
}

func (p *OIDCProvider) userInfoFromClaims(ctx context.Context, token string, claims map[string]interface{}) (*OAuth2UserInfo, error) {
	id := claimString(claims, p.cfg.Claims.IDClaim)
	email := claimString(claims, p.cfg.Claims.EmailClaim)
	name := claimString(claims, p.cfg.Claims.NameClaim)
	avatar := claimString(claims, p.cfg.Claims.AvatarClaim)
	emailVerified, verifiedPresent := parseEmailVerifiedClaim(claims, p.cfg.Claims.EmailVerifiedClaim)
	// Fetch userinfo when the id_token is missing the email OR the
	// email_verified claim. Either gap can block a legitimate verified-email
	// migration (a passwordless account that should auto-link), and the IdP
	// often surfaces the missing claim at the userinfo endpoint.
	if email == "" || !verifiedPresent {
		if d, err := p.ensureDiscovery(ctx); err == nil && d.UserinfoEndpoint != "" {
			if ui, err := p.fetchUserinfo(ctx, token, d.UserinfoEndpoint); err == nil {
				tokenSub := claimString(claims, "sub")
				uiSub := claimString(ui, "sub")
				// OIDC §5.3.2: the userinfo subject MUST equal the id_token
				// subject, else the response is about a different user.
				if tokenSub != "" && uiSub != "" && tokenSub != uiSub {
					return nil, errors.New("oidc: userinfo subject does not match id_token subject")
				}
				if email == "" {
					email = claimString(ui, p.cfg.Claims.EmailClaim)
				}
				if name == "" {
					name = claimString(ui, p.cfg.Claims.NameClaim)
				}
				if avatar == "" {
					avatar = claimString(ui, p.cfg.Claims.AvatarClaim)
				}
				// A signature-bound id_token email_verified ALWAYS wins — never
				// overwrite a signed `false` with an unsigned userinfo `true`.
				// Consult userinfo only when the id_token did NOT carry the claim.
				if !verifiedPresent {
					emailVerified, _ = parseEmailVerifiedClaim(ui, p.cfg.Claims.EmailVerifiedClaim)
				}
			}
		}
	}
	if id == "" {
		return nil, errors.New("oidc: id_token missing subject claim")
	}
	return &OAuth2UserInfo{
		ID:            id,
		Email:         email,
		Name:          name,
		AvatarURL:     avatar,
		Provider:      p.name,
		EmailVerified: emailVerified,
	}, nil
}

// parseEmailVerified reads the configured EmailVerifiedClaim from a claim
// map. Accepts a JSON bool, or the strings "true"/"false" (some IdPs emit
// the value as a string). Anything else — including the claim's absence —
// resolves to false. An unverifiable email must never bind to an account.
// parseEmailVerifiedClaim reads the configured email_verified claim, returning
// its boolean value AND whether the claim was PRESENT at all. Presence drives
// the OIDC userinfo fallback: a signature-bound id_token claim (even an
// explicit `false`) must never be overwritten by an unsigned userinfo `true`,
// so callers consult userinfo only when the id_token did not carry the claim.
func parseEmailVerifiedClaim(claims map[string]interface{}, claimName string) (value, present bool) {
	if len(claims) == 0 {
		return false, false
	}
	if claimName == "" {
		claimName = "email_verified"
	}
	raw, ok := claims[claimName]
	if !ok {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		return strings.EqualFold(v, "true"), true
	}
	// Present but an unrecognized type — treat as present + false: an
	// unparseable verification claim must never read as verified, and the
	// signed token having carried *something* means we do not re-fetch.
	return false, true
}

func parseEmailVerified(claims map[string]interface{}, claimName string) bool {
	v, _ := parseEmailVerifiedClaim(claims, claimName)
	return v
}

func (p *OIDCProvider) fetchUserinfo(ctx context.Context, token, endpoint string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: userinfo returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ─── verified-claims cache (bounded, FIFO eviction) ─────────────────────────

type orderedCache struct {
	mu  sync.Mutex
	cap int
	m   map[string]*list.Element
	l   *list.List
}

type claimsEntry struct {
	key    string
	claims map[string]interface{}
}

func newOrderedCache(cap int) *orderedCache {
	return &orderedCache{cap: cap, m: make(map[string]*list.Element), l: list.New()}
}

func (c *orderedCache) put(key string, claims map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.m[key]; ok {
		e.Value.(*claimsEntry).claims = claims
		return
	}
	e := c.l.PushBack(&claimsEntry{key: key, claims: claims})
	c.m[key] = e
	for c.l.Len() > c.cap {
		front := c.l.Front()
		if front == nil {
			break
		}
		c.l.Remove(front)
		delete(c.m, front.Value.(*claimsEntry).key)
	}
}

func (c *orderedCache) get(key string) (map[string]interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return nil, false
	}
	return e.Value.(*claimsEntry).claims, true
}

// ─── claim helpers ──────────────────────────────────────────────────────────

func claimString(claims map[string]interface{}, key string) string {
	if claims == nil {
		return ""
	}
	if v, ok := claims[key].(string); ok {
		return v
	}
	return ""
}

// verifyClaims enforces the id_token claim checks (iss/aud/exp/iat/sub). It is
// called by verifyIDToken AFTER the signature has been verified.
func (p *OIDCProvider) verifyClaims(claims map[string]interface{}) error {
	const leeway int64 = 60 // seconds — covers clock skew and IdP drift
	now := time.Now().Unix()

	if normalizeIssuer(claimString(claims, "iss")) != normalizeIssuer(p.cfg.Issuer) {
		return errors.New("oidc: id_token iss does not match issuer")
	}

	aud, ok := claims["aud"]
	if !ok {
		return errors.New("oidc: id_token missing aud")
	}
	auds := toStringSlice(aud)
	found := false
	for _, a := range auds {
		if a == p.cfg.ClientID {
			found = true
		}
	}
	if !found {
		return errors.New("oidc: id_token aud does not contain client_id")
	}
	// The authorized-party claim, when present, must name this client — OIDC
	// §3.1.3.7.3 states this unconditionally (not only for multi-audience
	// tokens). A multi-audience token additionally MUST carry azp at all.
	if azp := claimString(claims, "azp"); azp != "" {
		if azp != p.cfg.ClientID {
			return errors.New("oidc: id_token azp does not match client_id")
		}
	} else if len(auds) > 1 {
		return errors.New("oidc: id_token with multiple audiences must carry an azp equal to client_id")
	}

	expRaw, ok := claims["exp"]
	if !ok {
		return errors.New("oidc: id_token missing exp")
	}
	exp, err := toUnixTime(expRaw)
	if err != nil {
		return errors.New("oidc: id_token has invalid exp")
	}
	if now >= exp+leeway {
		return errors.New("oidc: id_token is expired")
	}

	// A not-before in the future MUST NOT be accepted (RFC 7519 §4.1.5).
	if nbfRaw, ok := claims["nbf"]; ok {
		nbf, err := toUnixTime(nbfRaw)
		if err != nil {
			return errors.New("oidc: id_token has invalid nbf")
		}
		if now+leeway < nbf {
			return errors.New("oidc: id_token is not yet valid (nbf)")
		}
	}

	if iatRaw, ok := claims["iat"]; ok {
		iat, err := toUnixTime(iatRaw)
		if err != nil {
			return errors.New("oidc: id_token has invalid iat")
		}
		if iat > now+leeway {
			return errors.New("oidc: id_token issued in the future")
		}
	}

	if claimString(claims, "sub") == "" {
		return errors.New("oidc: id_token missing sub")
	}
	return nil
}

func toStringSlice(v interface{}) []string {
	switch a := v.(type) {
	case string:
		return []string{a}
	case []interface{}:
		out := make([]string, 0, len(a))
		for _, x := range a {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func toUnixTime(v interface{}) (int64, error) {
	switch n := v.(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	}
	return 0, errors.New("oidc: claim is not a numeric time")
}
