package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// multiTenantIdP serves the fake IdP under the path a Microsoft-style
// multi-tenant issuer uses: configured as {server}/common/v2.0, its
// discovery document names {fake issuer}/{tenantid}/v2.0.
func multiTenantIdP(t *testing.T) (f *fakeIdP, issuer string) {
	t.Helper()
	f = newFakeIdP(t)
	inner := f.server.Config.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/common/v2.0")
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	f.discoveryIssuer = srv.URL + "/{tenantid}/v2.0"
	return f, srv.URL + "/common/v2.0"
}

func tenantProvider(t *testing.T, f *fakeIdP, issuer string, tenants ...string) *OIDCProvider {
	t.Helper()
	p, err := NewOIDCProvider(OIDCConfig{
		Issuer: issuer, ClientID: f.clientID, ClientSecret: f.clientSecret,
		RedirectURL: "https://app.example.com/cb", HTTPClient: newOIDCTestHTTPClient(t),
		Tenants: tenants,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOIDCMultiTenantIssuer(t *testing.T) {
	f, issuer := multiTenantIdP(t)
	base := strings.TrimSuffix(issuer, "/common/v2.0")
	tokenFor := func(iss, tid string) {
		c := standardIDClaims(iss, f.clientID)
		if tid != "" {
			c["tid"] = tid
		}
		f.claims = c
	}

	tokenFor(base+"/t-111/v2.0", "t-111")
	if _, err := tenantProvider(t, f, issuer).ExchangeCode(ctxBg(), "code"); err == nil || !strings.Contains(err.Error(), "discovery issuer") {
		t.Fatalf("without Tenants the template issuer must fail discovery: %v", err)
	}
	if _, err := tenantProvider(t, f, issuer, "t-111").ExchangeCode(ctxBg(), "code"); err != nil {
		t.Fatalf("listed tenant refused: %v", err)
	}
	if _, err := tenantProvider(t, f, issuer, "*").ExchangeCode(ctxBg(), "code"); err != nil {
		t.Fatalf("\"*\" refused a tenant: %v", err)
	}
	if _, err := tenantProvider(t, f, issuer, "t-222").ExchangeCode(ctxBg(), "code"); err == nil || !strings.Contains(err.Error(), "not admitted") {
		t.Fatalf("an unlisted tenant signed in: %v", err)
	}

	for name, c := range map[string][2]string{
		"tid differs from the iss tenant": {base + "/t-111/v2.0", "t-999"},
		"no tid":                          {base + "/t-111/v2.0", ""},
		"another host":                    {"https://evil.example/t-111/v2.0", "t-111"},
		"tid with a slash":                {base + "/t-1/x/v2.0", "t-1/x"},
		"the alias itself":                {issuer, ""},
	} {
		tokenFor(c[0], c[1])
		if _, err := tenantProvider(t, f, issuer, "*").ExchangeCode(ctxBg(), "code"); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}
}

// A template on another host, or one that is not the configured issuer
// with one segment open, still fails discovery with Tenants set.
func TestOIDCTenantTemplateMustFitTheIssuer(t *testing.T) {
	for template, want := range map[string]bool{
		"https://login.example/{tenantid}/v2.0":            true,
		"https://login.example/{tenantid}/v2.0/":           true,
		"https://evil.example/{tenantid}/v2.0":             false,
		"https://login.example/{tenantid}/v1.0":            false,
		"https://login.example/{tenantid}/{tenantid}/v2.0": false,
		"https://login.example/common/v2.0":                false,
	} {
		if got := tenantTemplateFits(template, "https://login.example/common/v2.0"); got != want {
			t.Errorf("tenantTemplateFits(%q) = %v, want %v", template, got, want)
		}
	}
	if tenantTemplateFits("https://login.example/{tenantid}/v2.0", "https://login.example/a/b/v2.0") {
		t.Error("a two-segment alias fit the template")
	}
}
