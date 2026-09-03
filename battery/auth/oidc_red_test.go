//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: when the configured issuer is https, every endpoint the discovery
// document advertises (token_endpoint, jwks_uri, userinfo_endpoint,
// authorization_endpoint) must itself be https; the loopback-http exemption is
// issuer-only.
// Surfaces: oidc.go:NewOIDCProvider, oidc.go:fetchDiscovery,
// oidc.go:ExchangeCode, oidc.go:FetchUserInfo (fetchUserinfo),
// oidc_jwks.go:jwksCache.fetchLocked.
// Finding: NewOIDCProvider enforces the scheme on cfg.Issuer only and
// fetchDiscovery checks the issuer claim and endpoint presence but never the
// endpoints' schemes, so ExchangeCode POSTs client_id+client_secret to a
// plain-http token_endpoint and the JWKS fetch GETs a plain-http jwks_uri;
// both cleartext servers below are dialed today, the token exchange even
// succeeds end to end.
// Fix direction: validate every advertised endpoint's scheme in fetchDiscovery
// (https, or plain http only under the same literal-loopback exemption the
// issuer gets) and refuse the document otherwise, before any dial.

package auth

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOIDCRedRejectsHTTPEndpoints(t *testing.T) {
	key := mustRSAKey(t, 2048)

	var tokenDials, jwksDials atomic.Int32
	issuerURL := "" // set once the https issuer is up (fakeIdP's own pattern)

	// Plain-http token endpoint. Records the dial and answers with a fully
	// valid token response so today's code also proceeds to the JWKS fetch:
	// the client secret rides this POST body in cleartext.
	httpToken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenDials.Add(1)
		headerJSON := mustJSONMarshal(t, map[string]any{"alg": "RS256", "kid": "red-1", "typ": "JWT"})
		claimsJSON := mustJSONMarshal(t, standardIDClaims(issuerURL, "test-client"))
		signingInput := []byte(b64u(headerJSON) + "." + b64u(claimsJSON))
		writeJSON(t, w, map[string]any{
			"access_token": "red-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     buildCompact(headerJSON, claimsJSON, signRS256(t, key, signingInput)),
		})
	}))
	t.Cleanup(httpToken.Close)

	// Plain-http JWKS. A network MITM on this fetch substitutes the keys
	// that vouch for every id_token.
	httpJWKS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwksDials.Add(1)
		writeJSON(t, w, map[string]any{"keys": []map[string]any{rsaJWKMap("red-1", &key.PublicKey)}})
	}))
	t.Cleanup(httpJWKS.Close)

	// https issuer whose discovery document advertises the two http endpoints.
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"issuer":                 issuerURL,
			"authorization_endpoint": issuerURL + "/authorize",
			"token_endpoint":         httpToken.URL + "/token",
			"jwks_uri":               httpJWKS.URL + "/jwks",
			"userinfo_endpoint":      issuerURL + "/userinfo",
		})
	})
	httpsIssuer := httptest.NewTLSServer(mux)
	t.Cleanup(httpsIssuer.Close)
	issuerURL = httpsIssuer.URL

	p, err := NewOIDCProvider(OIDCConfig{
		Issuer:       issuerURL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://app.example.com/cb",
		ProviderName: "redtest",
		HTTPClient:   httpsIssuer.Client(),
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	tok, exErr := p.ExchangeCode(ctxBg(), "any-code")

	// The hard assertions: the cleartext endpoints must never be dialed.
	if n := tokenDials.Load(); n != 0 {
		t.Errorf("client_id+client_secret were POSTed to the plain-http token_endpoint %d time(s); an https issuer advertising an http token_endpoint must be refused before any dial", n)
	}
	if n := jwksDials.Load(); n != 0 {
		t.Errorf("signing keys were fetched from the plain-http jwks_uri %d time(s); a MITM on that path substitutes the keys that vouch for id_tokens", n)
	}
	if exErr == nil {
		t.Errorf("ExchangeCode must fail when discovery advertises http endpoints, got success (token returned: %v)", tok != nil)
	}
}

// RED TEST — open finding, 2026-09-02 adversarial pass round 2 (tests-only;
// no fix applied). Pins the userinfo_endpoint half of the scheme finding
// above: FetchUserInfo dials the advertised endpoint with the bearer access
// token, so an http userinfo_endpoint on an https issuer leaks the token (and
// lets a network MITM supply the identity claims) in cleartext.
// Property: an https issuer's discovery document may not advertise an http
// userinfo_endpoint; the document must be refused before any dial.
// Surfaces: oidc.go:fetchDiscovery (no endpoint-scheme validation),
// oidc.go:FetchUserInfo → oidc.go:fetchUserinfo.
// Finding: discovery advertising http:// userinfo_endpoint is accepted, and
// FetchUserInfo (no cached claims path) GETs it with the bearer token.
// Fix direction: same as TestOIDCRedRejectsHTTPEndpoints — validate every
// advertised endpoint's scheme in fetchDiscovery and refuse the document
// before any dial.
func TestOIDCRedRejectsHTTPUserinfo(t *testing.T) {
	var userinfoDials atomic.Int32

	// Plain-http userinfo endpoint. Records the dial and answers a fully
	// valid userinfo document so today's code would succeed end to end:
	// the bearer access token rides this request in cleartext.
	httpUserinfo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userinfoDials.Add(1)
		writeJSON(t, w, map[string]any{
			"sub":            "red-sub",
			"email":          "red@example.com",
			"email_verified": true,
		})
	}))
	t.Cleanup(httpUserinfo.Close)

	// https issuer whose ONLY http endpoint is the userinfo_endpoint, so
	// the finding is isolated from the token/JWKS surfaces pinned above.
	issuerURL := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"issuer":                 issuerURL,
			"authorization_endpoint": issuerURL + "/authorize",
			"token_endpoint":         issuerURL + "/token",
			"jwks_uri":               issuerURL + "/jwks",
			"userinfo_endpoint":      httpUserinfo.URL + "/userinfo",
		})
	})
	httpsIssuer := httptest.NewTLSServer(mux)
	t.Cleanup(httpsIssuer.Close)
	issuerURL = httpsIssuer.URL

	p, err := NewOIDCProvider(OIDCConfig{
		Issuer:       issuerURL,
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "https://app.example.com/cb",
		ProviderName: "redtest",
		HTTPClient:   httpsIssuer.Client(),
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	// No ExchangeCode first: the access token has no cached id_token
	// claims, so FetchUserInfo must build identity from userinfo — the
	// exact path that trusts the advertised endpoint.
	ui, uiErr := p.FetchUserInfo(ctxBg(), "red-access-token")

	// The hard assertions: the cleartext endpoint must never be dialed.
	if n := userinfoDials.Load(); n != 0 {
		t.Errorf("userinfo was fetched over plain http %d time(s) with the bearer access token; an https issuer advertising an http userinfo_endpoint must be refused before any dial", n)
	}
	if uiErr == nil {
		t.Errorf("FetchUserInfo must fail when discovery advertises an http userinfo_endpoint, got success (info=%v)", ui != nil)
	}
}
