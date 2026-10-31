package auth

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// TestOIDCTokenAuthMethod: a provider whose discovery lists only
// client_secret_basic gets the secret in an Authorization header (each
// half form-encoded, RFC 6749 §2.3.1), never in the body. A provider that
// omits the list or accepts client_secret_post keeps the body form, and
// TokenAuthMethod overrides discovery.
func TestOIDCTokenAuthMethod(t *testing.T) {
	for _, tc := range []struct {
		name      string
		methods   []string
		override  string
		wantBasic bool
	}{
		{"absent list posts", nil, "", false},
		{"post listed posts", []string{"client_secret_basic", "client_secret_post"}, "", false},
		{"basic only uses basic", []string{"client_secret_basic"}, "", true},
		{"override forces basic", nil, OIDCAuthClientSecretBasic, true},
		{"override forces post", []string{"client_secret_basic"}, OIDCAuthClientSecretPost, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIdP(t)
			f.clientID = "app client"
			f.clientSecret = "s3:cr/et+%"
			f.authMethods = tc.methods
			p, err := NewOIDCProvider(OIDCConfig{
				Issuer:          f.issuer,
				ClientID:        f.clientID,
				ClientSecret:    f.clientSecret,
				RedirectURL:     "https://app.example.com/cb",
				HTTPClient:      newOIDCTestHTTPClient(t),
				TokenAuthMethod: tc.override,
			})
			if err != nil {
				t.Fatalf("NewOIDCProvider: %v", err)
			}
			if _, err := p.ExchangeCode(context.Background(), "the-code"); err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			f.tokenMu.Lock()
			form, hasHdr, basic := f.tokenForm, f.tokenHasHdr, f.tokenBasic
			f.tokenMu.Unlock()
			if !tc.wantBasic {
				if hasHdr || form.Get("client_secret") != f.clientSecret || form.Get("client_id") != f.clientID {
					t.Fatalf("want client_secret_post; header=%v form=%v", hasHdr, form)
				}
				return
			}
			if form.Has("client_secret") {
				t.Fatalf("client_secret_basic also sent the secret in the body: %v", form)
			}
			id, _ := url.QueryUnescape(basic[0])
			secret, _ := url.QueryUnescape(basic[1])
			if !hasHdr || id != f.clientID || secret != f.clientSecret {
				t.Fatalf("Authorization carried %q:%q (decoded %q:%q), want %q:%q", basic[0], basic[1], id, secret, f.clientID, f.clientSecret)
			}
		})
	}
}

// TestOIDCTokenAuthMethodRefusals: an unknown TokenAuthMethod fails
// construction, and a provider listing neither secret method fails the
// exchange before the secret leaves the process.
func TestOIDCTokenAuthMethodRefusals(t *testing.T) {
	f := newFakeIdP(t)
	if _, err := NewOIDCProvider(OIDCConfig{
		Issuer: f.issuer, ClientID: f.clientID, ClientSecret: f.clientSecret,
		RedirectURL: "https://app.example.com/cb", TokenAuthMethod: "private_key_jwt",
	}); err == nil {
		t.Fatal("NewOIDCProvider accepted TokenAuthMethod private_key_jwt")
	}

	f.authMethods = []string{"private_key_jwt"}
	p := newTestProvider(t, f)
	_, err := p.ExchangeCode(context.Background(), "the-code")
	if err == nil || !strings.Contains(err.Error(), "token_endpoint_auth_methods_supported") {
		t.Fatalf("ExchangeCode = %v, want a refusal naming the discovered methods", err)
	}
	f.tokenMu.Lock()
	defer f.tokenMu.Unlock()
	if f.tokenForm != nil {
		t.Fatal("the token endpoint was called with no method it supports")
	}
}
