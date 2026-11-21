package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// fakeApple is the OIDC fake set up the way Apple answers: the id_token
// says email_verified as the string "true", and the client secret it is
// sent must be an ES256 JWT the team's key signed.
type fakeApple struct {
	*fakeIdP
	key *ecdsa.PrivateKey
	pem []byte
}

func newFakeApple(t *testing.T) *fakeApple {
	t.Helper()
	f := newFakeIdP(t)
	f.clientID = "com.example.web"
	f.omitUserinfoEp = true // Apple has no userinfo endpoint
	key := mustECKey(t)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeApple{fakeIdP: f, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})}
}

func (f *fakeApple) provider(t *testing.T) *AppleProvider {
	t.Helper()
	p, err := NewAppleProvider(AppleConfig{
		ClientID:    f.clientID,
		TeamID:      "TEAM123456",
		KeyID:       "KEY1234567",
		PrivateKey:  f.pem,
		RedirectURL: "https://app.example.com/auth/oauth/apple/callback",
		Issuer:      f.issuer,
		HTTPClient:  newOIDCTestHTTPClient(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fakeApple) appleClaims(email string) map[string]any {
	c := standardIDClaims(f.issuer, f.clientID)
	delete(c, "name")
	delete(c, "picture")
	c["sub"] = "001234.abcdef.0987"
	c["email"] = email
	c["email_verified"] = "true"
	return c
}

// checkClientSecret verifies the secret the last exchange sent the way
// Apple does: signature by the team key, then the claims.
func (f *fakeApple) checkClientSecret(t *testing.T) {
	t.Helper()
	f.tokenMu.Lock()
	form := f.tokenForm
	f.tokenMu.Unlock()
	if form.Get("client_id") != f.clientID {
		t.Fatalf("client_id = %q", form.Get("client_id"))
	}
	parts := strings.Split(form.Get("client_secret"), ".")
	if len(parts) != 3 {
		t.Fatalf("client_secret is not a JWT: %q", form.Get("client_secret"))
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if len(sig) != 64 || !ecdsa.Verify(&f.key.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("client_secret is not signed by the team key")
	}
	var header map[string]string
	var claims map[string]any
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(hb, &header)
	_ = json.Unmarshal(cb, &claims)
	if header["alg"] != "ES256" || header["kid"] != "KEY1234567" {
		t.Fatalf("header: %v", header)
	}
	if claims["iss"] != "TEAM123456" || claims["sub"] != f.clientID || claims["aud"] != f.issuer {
		t.Fatalf("claims: %v", claims)
	}
	iat, _ := claims["iat"].(float64)
	exp, _ := claims["exp"].(float64)
	if exp <= iat || exp-iat > (6*30*24*time.Hour).Seconds() {
		t.Fatalf("iat %v exp %v", iat, exp)
	}
}

func TestApple_AuthURLAsksForFormPost(t *testing.T) {
	f := newFakeApple(t)
	u, err := url.Parse(f.provider(t).AuthURLWithChallenge("st", "ch"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("response_mode") != "form_post" || q.Get("scope") != "openid email" ||
		q.Get("client_id") != f.clientID || q.Get("state") != "st" || q.Get("code_challenge") != "ch" {
		t.Fatalf("auth URL query: %v", q)
	}
}

func TestApple_ExchangeSignsClientSecretAndReadsStringVerified(t *testing.T) {
	f := newFakeApple(t)
	f.claims = f.appleClaims("kim@privaterelay.appleid.com")
	p := f.provider(t)
	tok, err := p.ExchangeCodeWithVerifier(context.Background(), "the-code", "the-verifier")
	if err != nil {
		t.Fatal(err)
	}
	f.checkClientSecret(t)
	info, err := p.FetchUserInfo(context.Background(), tok.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if info.Provider != "apple" || info.ID != "001234.abcdef.0987" || info.Email != "kim@privaterelay.appleid.com" || !info.EmailVerified {
		t.Fatalf("info: %+v", info)
	}
}

func TestApple_ConfigRefusals(t *testing.T) {
	f := newFakeApple(t)
	rsaDER, _ := x509.MarshalPKCS8PrivateKey(mustRSAKey(t, 2048))
	rsaPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rsaDER})
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	p384DER, _ := x509.MarshalPKCS8PrivateKey(p384)
	p384PEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: p384DER})
	junkPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not pkcs8")})
	good := AppleConfig{ClientID: "c", TeamID: "t", KeyID: "k", PrivateKey: f.pem, RedirectURL: "https://x/cb"}
	for name, mut := range map[string]func(*AppleConfig){
		"no client":   func(c *AppleConfig) { c.ClientID = "" },
		"no team":     func(c *AppleConfig) { c.TeamID = "" },
		"no key id":   func(c *AppleConfig) { c.KeyID = "" },
		"no redirect": func(c *AppleConfig) { c.RedirectURL = "" },
		"not pem":     func(c *AppleConfig) { c.PrivateKey = []byte("hello") },
		"rsa key":     func(c *AppleConfig) { c.PrivateKey = rsaPEM },
		"p-384 key":   func(c *AppleConfig) { c.PrivateKey = p384PEM },
	} {
		c := good
		mut(&c)
		if _, err := NewAppleProvider(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := NewAppleProvider(good); err != nil {
		t.Fatalf("good config: %v", err)
	}
	junk := good
	junk.PrivateKey = junkPEM
	if _, err := NewAppleProvider(junk); err == nil || !strings.Contains(err.Error(), "PKCS #8") {
		t.Fatalf("PEM around bytes that are not PKCS #8: %v", err)
	}
}

// The whole browser trip: start, Apple's cross-site form POST to the
// callback, the hop page, and the GET it loads, which signs in.
func TestApple_FormPostCallbackSignsIn(t *testing.T) {
	f := newFakeApple(t)
	f.claims = f.appleClaims("lee@example.com")
	mgr, users := newOAuth2Manager(t, f.provider(t))
	r := mountOAuth2Routes(mgr)

	start := httptest.NewRecorder()
	r.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/oauth/apple", nil))
	loc, _ := url.Parse(start.Header().Get("Location"))
	state := loc.Query().Get("state")
	stateCookie := cookieNamed(start.Result().Cookies(), oauthStateCookie)
	if state == "" || stateCookie == nil || loc.Query().Get("response_mode") != "form_post" {
		t.Fatalf("start: %d %q", start.Code, start.Header().Get("Location"))
	}

	// Apple's POST: cross-site, and the Lax state cookie stays behind.
	post := httptest.NewRequest(http.MethodPost, "/auth/oauth/apple/callback",
		strings.NewReader(url.Values{"code": {"the-code"}, "state": {state}, "user": {`{"name":{}}`}}.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "cross-site")
	post.Header.Set("Origin", f.issuer)
	hop := httptest.NewRecorder()
	r.ServeHTTP(hop, post)
	if hop.Code != http.StatusOK {
		t.Fatalf("form post: %d %s", hop.Code, hop.Body)
	}
	if cookieNamed(hop.Result().Cookies(), "session_id") != nil {
		t.Fatal("the POST itself must sign in nobody")
	}
	refresh := hop.Header().Get("Refresh")
	target, ok := strings.CutPrefix(refresh, "0;url=")
	if !ok || !strings.HasPrefix(target, "/auth/oauth/apple/callback?") {
		t.Fatalf("hop Refresh: %q", refresh)
	}
	tu, _ := url.Parse(target)
	if tu.Query().Get("code") != "the-code" || tu.Query().Get("state") != state || tu.Query().Has("user") {
		t.Fatalf("hop target query: %v", tu.Query())
	}

	// The page's own navigation: same-origin, so the state cookie rides it.
	done := httptest.NewRecorder()
	r.ServeHTTP(done, oauthCallbackReq(target, stateCookie.Value))
	if done.Code != http.StatusFound && done.Code != http.StatusSeeOther {
		t.Fatalf("GET callback: %d %s", done.Code, done.Body)
	}
	if cookieNamed(done.Result().Cookies(), "session_id") == nil {
		t.Fatal("no session after the GET callback")
	}
	f.checkClientSecret(t)
	if _, _, err := users.FindByEmail(context.Background(), "lee@example.com"); err != nil {
		t.Fatalf("account: %v", err)
	}
}

// The hop URL in another browser is the replayed-callback shape: no state
// cookie, so the GET refuses it, as it refuses any replayed callback.
func TestApple_FormPostHopDoesNotBindAnotherBrowser(t *testing.T) {
	f := newFakeApple(t)
	f.claims = f.appleClaims("mo@example.com")
	mgr, _ := newOAuth2Manager(t, f.provider(t))
	r := mountOAuth2Routes(mgr)
	start := httptest.NewRecorder()
	r.ServeHTTP(start, httptest.NewRequest(http.MethodGet, "/auth/oauth/apple", nil))
	loc, _ := url.Parse(start.Header().Get("Location"))

	post := httptest.NewRequest(http.MethodPost, "/auth/oauth/apple/callback",
		strings.NewReader(url.Values{"code": {"c"}, "state": {loc.Query().Get("state")}}.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Sec-Fetch-Site", "cross-site")
	hop := httptest.NewRecorder()
	r.ServeHTTP(hop, post)
	target := strings.TrimPrefix(hop.Header().Get("Refresh"), "0;url=")

	victim := httptest.NewRequest(http.MethodGet, target, nil)
	victim.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, victim)
	if w.Code != http.StatusBadRequest || cookieNamed(w.Result().Cookies(), "session_id") != nil {
		t.Fatalf("callback without the state cookie: %d", w.Code)
	}
}

// Only a provider that answers with a form post gets the POST route.
func TestOAuthFormPostRouteOnlyForFormPostProviders(t *testing.T) {
	mgr, _ := newOAuth2Manager(t, &mockProvider{name: "github"})
	r := mountOAuth2Routes(mgr)
	post := httptest.NewRequest(http.MethodPost, "/auth/oauth/github/callback", strings.NewReader("code=c&state=s"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, post)
	if w.Code != http.StatusBadRequest || w.Header().Get("Refresh") != "" {
		t.Fatalf("POST callback for a redirect provider: %d %q", w.Code, w.Header().Get("Refresh"))
	}
}

// Under WithBFFPosture, Apple's POST (cross-site Origin, no CSRF token)
// reaches the callback; any other auth POST still needs the token.
func TestBFFPostureLetsFormPostCallbackThrough(t *testing.T) {
	f := newFakeApple(t)
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	mgr.Use(NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"apple": f.provider(t)},
		StateSecret: "test-secret-key-state",
	}))
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{AllowedOrigins: []string{"https://app.example.com"}}))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRoutes(app.Router())
	app.Router().Post("/auth/oauth/apple/other", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	send := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("code=c&state=s"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", f.issuer)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)
		return w
	}
	if w := send("/auth/oauth/apple/callback"); w.Code != http.StatusOK || w.Header().Get("Refresh") == "" {
		t.Fatalf("Apple's POST under the BFF posture: %d %s", w.Code, w.Body)
	}
	if w := send("/auth/oauth/apple/other"); w.Code != http.StatusForbidden {
		t.Fatalf("another POST without a token: %d", w.Code)
	}
}

func TestIsFormPostCallback(t *testing.T) {
	for path, want := range map[string]bool{
		"/auth/oauth/apple/callback":     true,
		"/auth/oauth/apple/callback/x":   false,
		"/auth/oauth//callback":          false,
		"/auth/oauth/a/b/callback":       false,
		"/other/oauth/apple/callback":    false,
		"/auth/oauth/apple/link":         false,
		"/auth/oauth/apple/callbackish":  false,
		"/auth/oauth/apple/callback?x=1": false,
	} {
		if got := isFormPostCallback("/auth", path); got != want {
			t.Errorf("%q: %v", path, got)
		}
	}
}

// A host that mounts auth.CSRF itself gets the BFF exemptions from
// WithCSRFAuthRoutes: Apple's POST reaches the callback and logout
// passes, while any other POST still needs the token.
func TestCSRFAuthRoutesLetsFormPostCallbackThrough(t *testing.T) {
	f := newFakeApple(t)
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	mgr.Use(NewCorePlugin())
	mgr.Use(NewOAuth2Plugin(OAuth2Config{
		Providers:   map[string]OAuth2Provider{"apple": f.provider(t)},
		StateSecret: "test-secret-key-state",
	}))
	app := framework.NewApp()
	app.Use(CSRF(WithCSRFAuthRoutes(mgr)))
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	mgr.RegisterRoutes(app.Router())
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	app.Router().Post("/auth/oauth/apple/other", ok)
	app.Router().Post("/auth/logout-all", ok)
	send := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("code=c&state=s"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", f.issuer)
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		w := httptest.NewRecorder()
		app.Router().ServeHTTP(w, req)
		return w
	}
	if w := send("/auth/oauth/apple/callback"); w.Code != http.StatusOK || w.Header().Get("Refresh") == "" {
		t.Fatalf("Apple's POST under auth.CSRF(WithCSRFAuthRoutes): %d %s", w.Code, w.Body)
	}
	for _, path := range []string{"/auth/oauth/apple/other", "/auth/logout-all"} {
		if w := send(path); w.Code != http.StatusForbidden {
			t.Fatalf("%s without a token: %d, want 403", path, w.Code)
		}
	}
	victim, err := mgr.SessionStore().Create(context.Background(), "u-victim", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	forged := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	forged.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	forged.Header.Set("Sec-Fetch-Site", "cross-site")
	forged.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: victim.Token})
	fw := httptest.NewRecorder()
	app.Router().ServeHTTP(fw, forged)
	if fw.Code != http.StatusForbidden {
		t.Fatalf("cross-site logout under the exemption: %d, want 403", fw.Code)
	}
	if _, err := mgr.SessionStore().Get(context.Background(), victim.Token); err != nil {
		t.Fatalf("SECURITY: a cross-site form POST /auth/logout ended the victim's session: %v", err)
	}
	app.Router().Delete("/auth/logout", ok)
	del := httptest.NewRequest(http.MethodDelete, "/auth/logout", nil)
	dw := httptest.NewRecorder()
	app.Router().ServeHTTP(dw, del)
	if dw.Code != http.StatusForbidden {
		t.Fatalf("DELETE /auth/logout without a token: %d, want 403", dw.Code)
	}
	same := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	same.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, same)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("same-origin logout without a token: %d, want 303", w.Code)
	}
}
