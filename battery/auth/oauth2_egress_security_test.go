package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Pins that the built-in Google/GitHub provider fetches never follow a
// redirect (the request carrying client_secret / code / refresh token /
// bearer header must never arrive at an origin other than the one the host
// configured), found by the 2026-09-04 red-probe round; fixed by building
// defaultOAuthHTTPClient through oidcNoRedirect so every provider client
// answers 3xx as final responses.
// Family: F2 Outbound fetch allow-list (redirect re-check on credential-bearing provider fetches)
// Property: a credential-bearing provider fetch never follows a redirect — the
// request (client_secret, authorization code, refresh token, bearer header)
// must never arrive at an origin other than the one the host configured.
// Surfaces: oauth2.go::GoogleProvider.ExchangeCode, GoogleProvider.RefreshToken,
// GoogleProvider.FetchUserInfo, GitHubProvider.ExchangeCode,
// GitHubProvider.RefreshToken, GitHubProvider.FetchUserInfo (+
// GitHubProvider.fetchPrimaryEmail, reached from FetchUserInfo). All six share
// defaultOAuthHTTPClient; oidc.go::oidcNoRedirect guards the OIDC provider
// (and now the shared default client too).

// TestProviderFetchRefusesRedirect serves a 307 at each built-in provider's
// configured endpoint and asserts the redirect target is never reached.
func TestProviderFetchRefusesRedirect(t *testing.T) {
	var hits atomic.Int32
	var redirectedBody atomic.Value // string; the POST body that arrived at the target
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			redirectedBody.Store(string(b))
		}
		// Answer every field any of the decoders want so a followed
		// redirect completes "successfully" — the hit count is the
		// oracle, not the call's result.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"t","refresh_token":"r","expires_in":3600,` +
			`"id":"1","email":"a@b.test","login":"a","name":"A","picture":"p"}`))
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 307 preserves method + body: the credential-bearing POST is
		// re-sent verbatim at the target. 301/302/303 (bodyless GET)
		// are the same leak shape with a weaker body.
		http.Redirect(w, r, target.URL+"/catch", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	ctx := context.Background()
	g := NewGoogleProvider("cid-test", "client-secret-value", "https://app.example.com/cb")
	g.tokenEndpoint = origin.URL
	g.userInfoEndpoint = origin.URL
	gh := NewGitHubProvider("cid-test", "client-secret-value", "https://app.example.com/cb")
	gh.tokenEndpoint = origin.URL
	gh.userInfoEndpoint = origin.URL

	surfaces := []struct {
		name string
		call func() error
	}{
		{"GoogleProvider.ExchangeCode", func() error { _, err := g.ExchangeCode(ctx, "code-live-xyz"); return err }},
		{"GoogleProvider.RefreshToken", func() error { _, err := g.RefreshToken(ctx, "refresh-live-xyz"); return err }},
		{"GoogleProvider.FetchUserInfo", func() error { _, err := g.FetchUserInfo(ctx, "bearer-live-xyz"); return err }},
		{"GitHubProvider.ExchangeCode", func() error { _, err := gh.ExchangeCode(ctx, "code-live-xyz"); return err }},
		{"GitHubProvider.RefreshToken", func() error { _, err := gh.RefreshToken(ctx, "refresh-live-xyz"); return err }},
		{"GitHubProvider.FetchUserInfo", func() error { _, err := gh.FetchUserInfo(ctx, "bearer-live-xyz"); return err }},
	}
	for _, s := range surfaces {
		before := hits.Load()
		_ = s.call() // result irrelevant: the fetch must stop at the origin's 3xx
		if got := hits.Load() - before; got != 0 {
			body, _ := redirectedBody.Load().(string)
			t.Errorf("SECURITY: [oauth-redirect] %s followed a 307 off the configured provider origin: "+
				"the request reached the redirect target (%d hit(s), POST body %q) — the credential-bearing "+
				"fetch must refuse redirects exactly as TestOIDCSec_TokenRedirectKeepsSecret pins for the OIDC provider",
				s.name, got, body)
		}
		redirectedBody.Store("")
	}
}

// Pins that every built-in provider response decode is size-bounded at
// 1 MiB, found by the 2026-09-04 red-probe round; fixed by wrapping each
// resp.Body decode in io.LimitReader(resp.Body, oauthProviderMaxBody),
// matching the OIDC fetch convention.
// Family: F2 Outbound fetch allow-list (response size bound on provider fetches)
// Property: every provider response decode is size-bounded — a provider
// endpoint must not be able to stream an unbounded body into a decoder that
// buffers to completion.
// Surfaces: oauth2.go::GoogleProvider.ExchangeCode, GoogleProvider.RefreshToken,
// GoogleProvider.FetchUserInfo, GitHubProvider.ExchangeCode,
// GitHubProvider.RefreshToken, GitHubProvider.FetchUserInfo,
// GitHubProvider.fetchPrimaryEmail — all decode through
// io.LimitReader(resp.Body, oauthProviderMaxBody); the OIDC fetches carry
// their own 1 MiB caps (oidc_jwks.go jwksMaxBody, oidc.go fetchDiscovery /
// ExchangeCode / fetchUserinfo).

// TestProviderResponseBodiesCapped serves a fully valid >1 MiB JSON document
// at each provider endpoint and asserts the fetch refuses it instead of
// buffering the whole body. With the 1 MiB LimitReader the decode fails
// (truncated JSON).
func TestProviderResponseBodiesCapped(t *testing.T) {
	// 2 MiB of padding inside a JSON string: a capped reader truncates
	// mid-string and the decode errors; an uncapped decoder buffers all
	// of it and succeeds.
	big := `{"access_token":"` + strings.Repeat("A", 2<<20) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(big))
	}))
	defer srv.Close()

	ctx := context.Background()
	g := NewGoogleProvider("cid", "cs", "https://app.example.com/cb")
	g.tokenEndpoint = srv.URL
	g.userInfoEndpoint = srv.URL
	gh := NewGitHubProvider("cid", "cs", "https://app.example.com/cb")
	gh.tokenEndpoint = srv.URL
	gh.userInfoEndpoint = srv.URL

	surfaces := []struct {
		name string
		call func() error
	}{
		{"GoogleProvider.ExchangeCode", func() error { _, err := g.ExchangeCode(ctx, "c"); return err }},
		{"GoogleProvider.RefreshToken", func() error { _, err := g.RefreshToken(ctx, "r"); return err }},
		{"GoogleProvider.FetchUserInfo", func() error { _, err := g.FetchUserInfo(ctx, "t"); return err }},
		{"GitHubProvider.ExchangeCode", func() error { _, err := gh.ExchangeCode(ctx, "c"); return err }},
		{"GitHubProvider.RefreshToken", func() error { _, err := gh.RefreshToken(ctx, "r"); return err }},
		{"GitHubProvider.FetchUserInfo", func() error { _, err := gh.FetchUserInfo(ctx, "t"); return err }},
	}
	for _, s := range surfaces {
		if err := s.call(); err == nil {
			t.Errorf("SECURITY: [oauth-bodycap] %s consumed a >1 MiB provider response without error: "+
				"the body is decoded from resp.Body with no LimitReader, unlike every OIDC fetch "+
				"(jwksMaxBody / fetchDiscovery / ExchangeCode / fetchUserinfo, all 1<<20)", s.name)
		}
	}
}
