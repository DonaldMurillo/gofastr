package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// OIDC email_verified claim → OAuth2UserInfo.EmailVerified. This is the
// signal resolveOAuthUser uses to decide whether an email match may bind
// to an existing local account. An IdP emitting email_verified=false (or
// omitting the claim) must surface as EmailVerified=false so the callback
// never treats the email as a verified identifier.

// TestOIDCSec_EmailVerified_True: the standard claim surfaces as
// EmailVerified=true. A drift here would silently downgrade every OIDC
// login to unverified, breaking the safe migration path (passwordless
// account + verified email → auto-link).
func TestOIDCSec_EmailVerified_True(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = cloneClaims(baseClaims(f))
	f.claims["email_verified"] = true
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if !info.EmailVerified {
		t.Fatalf("email_verified=true must surface as EmailVerified=true")
	}
}

// TestOIDCSec_EmailVerified_False: an explicit email_verified=false claim
// surfaces as EmailVerified=false. This is the takeover-prevention signal
// — resolveOAuthUser's unverified-email arm falls through to a fresh
// create rather than matching an existing account.
func TestOIDCSec_EmailVerified_False(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = cloneClaims(baseClaims(f))
	f.claims["email_verified"] = false
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.EmailVerified {
		t.Fatalf("email_verified=false must surface as EmailVerified=false")
	}
}

// TestOIDCSec_EmailVerified_MissingDefaultsFalse: an absent claim defaults
// to false. The OIDC spec does not require email_verified, so the safe
// default is "unverified" — a missing assertion is never a verified email.
func TestOIDCSec_EmailVerified_MissingDefaultsFalse(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = cloneClaims(baseClaims(f))
	delete(f.claims, "email_verified")
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.EmailVerified {
		t.Fatalf("absent email_verified must default to EmailVerified=false")
	}
}

// TestOIDCSec_EmailVerified_StringTrue: some IdPs (notably older Azure AD
// configurations) emit the value as the string "true" rather than a JSON
// bool. parseEmailVerified accepts both shapes; this test pins the
// string-true case so a regression doesn't silently treat a verified
// email as unverified (and refuse the safe migration path).
func TestOIDCSec_EmailVerified_StringTrue(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = cloneClaims(baseClaims(f))
	f.claims["email_verified"] = "true"
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if !info.EmailVerified {
		t.Fatalf(`email_verified="true" (string) must surface as EmailVerified=true`)
	}
}

// TestOIDCSec_EmailVerified_StringFalse: the string "false" must surface
// as EmailVerified=false, NOT be interpreted as truthy by virtue of being
// a non-empty string. A drift here would let an IdP downgrade verification
// to "any non-empty string counts as true".
func TestOIDCSec_EmailVerified_StringFalse(t *testing.T) {
	f := newFakeIdP(t)
	f.claims = cloneClaims(baseClaims(f))
	f.claims["email_verified"] = "false"
	p := newTestProvider(t, f)

	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if info.EmailVerified {
		t.Fatalf(`email_verified="false" (string) must surface as EmailVerified=false`)
	}
}

// TestOIDCSec_EmailVerified_CustomClaim: a host that maps the claim name
// (e.g. an IdP that puts verification under a non-standard key) gets the
// value from the configured claim. Without this, hosts on non-standard
// IdPs would silently lose the verification signal.
func TestOIDCSec_EmailVerified_CustomClaim(t *testing.T) {
	f := newFakeIdP(t)
	// Use a non-standard claim name and leave email_verified absent.
	f.claims = map[string]interface{}{
		"iss":                   f.issuer,
		"sub":                   "user-9",
		"aud":                   f.clientID,
		"exp":                   time.Now().Add(time.Hour).Unix(),
		"iat":                   time.Now().Unix(),
		"email":                 "bob@example.com",
		"verified_email_custom": true,
	}
	p, err := NewOIDCProvider(OIDCConfig{
		Issuer:       f.issuer,
		ClientID:     f.clientID,
		ClientSecret: f.clientSecret,
		RedirectURL:  "https://app.example.com/cb",
		ProviderName: "custom",
		Claims: OIDCClaimsMapping{
			EmailVerifiedClaim: "verified_email_custom",
		},
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	tok, err := p.ExchangeCode(ctxBg(), "any-code")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	info, err := p.FetchUserInfo(ctxBg(), tok.AccessToken)
	if err != nil {
		t.Fatalf("FetchUserInfo: %v", err)
	}
	if !info.EmailVerified {
		t.Fatalf("custom claim %q=true must surface as EmailVerified=true", "verified_email_custom")
	}
}

// TestOIDCSec_UnverifiedEmailCannotTakeoverPasswordAccount: the END-TO-END
// takeover regression. An OIDC id_token with email_verified=false for an
// email that already belongs to a password account must NOT bind to that
// account. resolveOAuthUser must treat the email as unmatched and refuse
// (the unique-email constraint then blocks the fresh-create too).
//
// This is the test that would catch a regression re-introducing
// email-trust on unverified OIDC emails — the original vulnerability.
func TestOIDCSec_UnverifiedEmailCannotTakeoverPasswordAccount(t *testing.T) {
	// Build a fake IdP whose id_token carries an UNVERIFIED email that
	// collides with the victim's existing account.
	f := newFakeIdP(t)
	f.claims = cloneClaims(baseClaims(f))
	f.claims["email"] = "victim@example.com"
	f.claims["email_verified"] = false
	oidcProv := newTestProvider(t, f)
	oidcProv.name = "testoidc"

	// Wire a real AuthManager + OAuth2Plugin + memoryUserStore (a linker
	// in the test build) and pre-seed a victim with a real password.
	store := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     store,
	})
	plugin := NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"oidc": oidcProv},
		StateSecret: "test-secret",
	})
	mgr.Use(plugin)
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	ctx := ctxBg()
	victim, err := store.CreateUser(ctx, "victim@example.com", "realhash", []string{"user"})
	if err != nil {
		t.Fatalf("seed victim: %v", err)
	}

	// Drive the OIDC callback. The IdP mints a real signed id_token with
	// email_verified=false.
	state, err := plugin.generateState("oidc", "")
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	cbURL := "/auth/oauth/oidc/callback?state=" + state + "&code=fakecode"
	req := httptest.NewRequest(http.MethodGet, cbURL, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// The callback must NOT succeed as a login. Either it errors (the
	// unique-email constraint blocks the fresh create) or it returns a
	// non-302. Either way, no session for the victim.
	if rec.Code == 302 {
		// Inspect the session cookie: if one was set, its user MUST NOT be
		// the victim.
		for _, c := range rec.Result().Cookies() {
			if c.Name != "session_id" {
				continue
			}
			sess, err := mgr.SessionStore().Get(ctx, c.Value)
			if err != nil {
				continue
			}
			if sess.UserID == victim.GetID() {
				t.Fatalf("TAKEOVER REGRESSION: unverified OIDC email signed in as victim %q",
					victim.GetID())
			}
		}
	}
	// And the victim must not have a link to the OIDC subject.
	accts, _ := store.ListAccounts(ctx, victim.GetID())
	if len(accts) != 0 {
		t.Fatalf("victim account gained an OAuth link from an unverified email callback: %+v", accts)
	}
}
