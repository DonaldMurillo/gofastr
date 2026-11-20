package auth

import (
	"errors"
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
		// Each iss is the template filled with its tid: only the
		// character guard refuses these.
		"tid with a query mark":    {base + "/t-1?x/v2.0", "t-1?x"},
		"tid with a fragment mark": {base + "/t-1#x/v2.0", "t-1#x"},
		"tid with an open brace":   {base + "/t-1{x/v2.0", "t-1{x"},
		"tid with a close brace":   {base + "/t-1}x/v2.0", "t-1}x"},
		"the alias itself":         {issuer, ""},
		// Only the empty-tenant guard refuses this one: the template
		// with an empty slot is exactly the iss.
		"empty tid, empty slot": {base + "//v2.0", ""},
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
		// The slot is a whole path segment, never part of one or of
		// the host.
		"https://login.example/org-{tenantid}/v2.0": false,
		"https://login.example/{tenantid}v2.0":      false,
		"https://{tenantid}/v2.0":                   false,
	} {
		if got := tenantTemplateFits(template, "https://login.example/common/v2.0"); got != want {
			t.Errorf("tenantTemplateFits(%q) = %v, want %v", template, got, want)
		}
	}
	if tenantTemplateFits("https://login.example/{tenantid}/v2.0", "https://login.example/a/b/v2.0") {
		t.Error("a two-segment alias fit the template")
	}
	if tenantTemplateFits("https://login.example/org-{tenantid}/v2.0", "https://login.example/org-common/v2.0") {
		t.Error("a template slot inside a segment fit")
	}
	if tenantTemplateFits("https://login.example/{tenantid}-x/v2.0", "https://login.example/common-x/v2.0") {
		t.Error("a template slot at the start of a segment fit")
	}
	if tenantTemplateFits("https://{tenantid}/v2.0", "https://common/v2.0") {
		t.Error("a template slot that is the whole host fit")
	}
	if tenantTemplateFits("https://{tenantid}.example/v2.0", "https://common.example/v2.0") {
		t.Error("a template slot in the host fit")
	}
}

// Tenants set against an issuer whose discovery names an exact issuer
// fails discovery: the allowlist would otherwise go unread.
func TestOIDCTenantsRefuseAnExactIssuer(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = standardIDClaims(f.issuer, f.clientID)
	f.claims["tid"] = "t-111"
	_, err := tenantProvider(t, f, f.issuer, "t-222").ExchangeCode(ctxBg(), "code")
	if err == nil || !strings.Contains(err.Error(), "{tenantid} template") {
		t.Fatalf("Tenants with an exact issuer: %v, want a discovery refusal", err)
	}
}

// The provider keeps its own copy of the allowlist.
func TestOIDCTenantsAreCopied(t *testing.T) {
	f, issuer := multiTenantIdP(t)
	tenants := []string{"t-111"}
	p := tenantProvider(t, f, issuer, tenants...)
	tenants[0] = "t-222"
	base := strings.TrimSuffix(issuer, "/common/v2.0")
	f.claims = standardIDClaims(base+"/t-222/v2.0", f.clientID)
	f.claims["tid"] = "t-222"
	if _, err := p.ExchangeCode(ctxBg(), "code"); err == nil {
		t.Fatal("a caller's later write to its slice admitted another tenant")
	}
}

// A key in the tenant-independent set that names an issuer signs only
// for that issuer, or for its own tenant when it names the template.
func TestOIDCTenantKeyIssuerScope(t *testing.T) {
	f, issuer := multiTenantIdP(t)
	base := strings.TrimSuffix(issuer, "/common/v2.0")
	for name, c := range map[string]struct {
		keyIssuer string
		ok        bool
	}{
		"no issuer member":          {"", true},
		"template issuer":           {base + "/{tenantid}/v2.0", true},
		"this tenant":               {base + "/t-111/v2.0", true},
		"another tenant":            {base + "/t-999/v2.0", false},
		"another issuer's template": {"https://evil.example/{tenantid}/v2.0", false},
	} {
		f.jwksFn = func(int) []map[string]any {
			k := rsaJWKMap(f.rsaKID, &f.rsaKey.PublicKey)
			if c.keyIssuer != "" {
				k["issuer"] = c.keyIssuer
			}
			return []map[string]any{k}
		}
		f.claims = standardIDClaims(base+"/t-111/v2.0", f.clientID)
		f.claims["tid"] = "t-111"
		_, err := tenantProvider(t, f, issuer, "*").ExchangeCode(ctxBg(), "code")
		if (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", name, err, c.ok)
		}
	}
}

// Under a multi-tenant issuer the account is the tenant and the subject:
// one sub in two tenants is two accounts. An exact issuer keeps sub.
func TestOIDCMultiTenantIdentityCarriesTheTenant(t *testing.T) {
	f, issuer := multiTenantIdP(t)
	base := strings.TrimSuffix(issuer, "/common/v2.0")
	ids := map[string]string{}
	for _, tid := range []string{"t-111", "t-222"} {
		c := standardIDClaims(base+"/"+tid+"/v2.0", f.clientID)
		c["tid"] = tid
		f.claims = c
		p := tenantProvider(t, f, issuer, "*")
		tok, err := p.ExchangeCode(ctxBg(), "code")
		if err != nil {
			t.Fatalf("%s: %v", tid, err)
		}
		info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
		if err != nil {
			t.Fatalf("%s: %v", tid, err)
		}
		ids[tid] = info.ID
	}
	if ids["t-111"] != "t-111/user-123" || ids["t-222"] != "t-222/user-123" {
		t.Fatalf("multi-tenant ids %v, want the tenant before the subject", ids)
	}
	// Without verified claims (a restart), userinfo has no tenant.
	p := tenantProvider(t, f, issuer, "*")
	if _, err := p.ensureDiscovery(ctxBg()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.FetchUserInfo(ctxBg(), "unknown-token"); err == nil {
		t.Fatal("a multi-tenant sign-in from userinfo alone was accepted")
	}

	plain := newFakeIdP(t)
	pp := newTestProvider(t, plain)
	tok, err := pp.ExchangeCode(ctxBg(), "code")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := pp.FetchUserInfo(ctxBg(), tok.AccessToken); err != nil || info.ID != "user-123" {
		t.Fatalf("exact issuer id: %+v %v", info, err)
	}
}

// Under a multi-tenant issuer any tenant's admin sets its users' email,
// so an email is verified only when the signed token carries both
// xms_edov true and email_verified true for the kept address; userinfo
// never vouches for it.
func TestOIDCMultiTenantEmailNeedsDomainProof(t *testing.T) {
	f, issuer := multiTenantIdP(t)
	base := strings.TrimSuffix(issuer, "/common/v2.0")
	for name, c := range map[string]struct {
		claims map[string]any
		want   bool
	}{
		"email_verified alone":        {map[string]any{"email_verified": true}, false},
		"xms_edov false":              {map[string]any{"email_verified": true, "xms_edov": false}, false},
		"xms_edov as a string":        {map[string]any{"email_verified": true, "xms_edov": "true"}, false},
		"xms_edov true":               {map[string]any{"email_verified": true, "xms_edov": true}, true},
		"signed email_verified false": {map[string]any{"email_verified": false, "xms_edov": true}, false},
		// No signed flag: only userinfo says verified.
		"userinfo flag only": {map[string]any{"xms_edov": true}, false},
		"neither signed":     {map[string]any{}, false},
	} {
		claims := standardIDClaims(base+"/t-111/v2.0", f.clientID)
		claims["tid"] = "t-111"
		for k, v := range c.claims {
			claims[k] = v
		}
		f.claims = claims
		f.userinfo = map[string]any{"email_verified": true, "email": "alice@example.com"}
		p := tenantProvider(t, f, issuer, "*")
		tok, err := p.ExchangeCode(ctxBg(), "code")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.EmailVerified != c.want {
			t.Errorf("%s: EmailVerified = %v, want %v", name, info.EmailVerified, c.want)
		}
	}
}

// A tenant identity with no link never joins an account by its address,
// even a verified one that a verified passwordless account holds: the
// owner links it while signed in. Its existing link still signs in.
func TestOIDCTenantIdentityNeverEmailLinks(t *testing.T) {
	f, issuer := multiTenantIdP(t)
	base := strings.TrimSuffix(issuer, "/common/v2.0")
	f.claims = standardIDClaims(base+"/t-111/v2.0", f.clientID)
	f.claims["tid"] = "t-111"
	f.claims["email_verified"] = true
	f.claims["xms_edov"] = true
	p := tenantProvider(t, f, issuer, "*")
	tok, err := p.ExchangeCode(ctxBg(), "code")
	if err != nil {
		t.Fatal(err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil || !info.EmailVerified {
		t.Fatalf("fixture: %+v %v, want a verified tenant identity", info, err)
	}

	store := newMemoryUserStore()
	ctx := ctxBg()
	owner, err := store.CreateUserNoPassword(ctx, "alice@example.com", []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkEmailVerified(ctx, owner.GetID()); err != nil {
		t.Fatal(err)
	}
	plugin, _ := newResolveManager(t, store)
	var collision *oauthEmailCollision
	if _, _, err := plugin.resolveOAuthUser(ctx, store, info); !errors.As(err, &collision) || collision.userID != owner.GetID() {
		t.Fatalf("unlinked tenant identity: %v, want an email collision", err)
	}
	if err := store.LinkOAuth(ctx, owner.GetID(), info.Provider, info.ID); err != nil {
		t.Fatal(err)
	}
	if got, _, err := plugin.resolveOAuthUser(ctx, store, info); err != nil || got.GetID() != owner.GetID() {
		t.Fatalf("linked tenant identity: %v %v, want the owner", got, err)
	}
}
