//go:build red

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Open finding from the 2026-11-23 security hunt. webhook delivery wraps
// every client in netguard.GuardRoundTripper so a subscriber URL cannot
// reach a private address; OIDC's defaultOAuthHTTPClient is only
// oidcNoRedirect(&http.Client{Timeout: 10s}). validateOIDCEndpoint
// accepts any https host — including 127.0.0.1 and link-local — so a
// discovery document that names a private jwks_uri (or token_endpoint /
// userinfo_endpoint) makes the app server dial into the host network on
// login. Confirmed: ExchangeCode fetched https://127.0.0.1/… and a bare
// TCP accept also saw the dial when TLS later failed.
//
// Property: OIDC discovery-driven fetches refuse private / loopback /
// link-local dial targets the way webhook SSRF does.
// Surfaces: oidc_jwks.go::jwksCache.fetch, oidc.go::fetchDiscovery,
// ExchangeCode's token POST, fetchUserinfo — all share the unguarded
// client from NewOIDCProvider.
func TestRedOIDCPrivateJWKSRefused(t *testing.T) {
	var hits atomic.Int32
	meta := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[]}`))
	}))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	meta.Listener = ln
	meta.StartTLS()
	defer meta.Close()

	var srvURL string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 srvURL,
				"authorization_endpoint": srvURL + "/authorize",
				"token_endpoint":         srvURL + "/token",
				"jwks_uri":               meta.URL + "/latest/meta-data/",
			})
		case "/token":
			hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
			payload, _ := json.Marshal(map[string]any{
				"iss": srvURL, "sub": "u", "aud": "c",
				"exp": time.Now().Add(time.Hour).Unix(),
			})
			tok := base64.RawURLEncoding.EncodeToString(hdr) + "." +
				base64.RawURLEncoding.EncodeToString(payload) + ".sig"
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "a", "token_type": "Bearer", "id_token": tok,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	p, err := NewOIDCProvider(OIDCConfig{
		Issuer: srvURL, ClientID: "c", ClientSecret: "s",
		RedirectURL: "https://app.example/cb", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.ExchangeCode(context.Background(), "code")
	if hits.Load() != 0 {
		t.Fatalf("SECURITY: [oidc-ssrf] JWKS fetch reached private listener %s (%d hit(s)). "+
			"validateOIDCEndpoint allows any https host and defaultOAuthHTTPClient has no "+
			"netguard dial Control, so a discovery jwks_uri (or token/userinfo endpoint) on "+
			"127.0.0.1 / link-local / RFC1918 makes login SSRF into the host network. "+
			"Fix: wrap the OIDC/OAuth client with netguard.GuardRoundTripper (as webhook does) "+
			"and/or refuse private hosts in validateOIDCEndpoint.",
			meta.URL, hits.Load())
	}
}
