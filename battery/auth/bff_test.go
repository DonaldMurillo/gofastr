package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestBFFPostureRejectsUntrustedOriginsAndJWTs(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	app.Router().Get("/api/check", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	app.Router().Post("/api/check", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := func(origin, authorization string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/check", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		return rec
	}

	if got := request("https://evil.example", "").Code; got != http.StatusForbidden {
		t.Fatalf("untrusted Origin status = %d, want 403", got)
	}
	allowed := request("https://app.example.com", "")
	if allowed.Code != http.StatusNoContent || allowed.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("allowed Origin response = %d, headers=%v", allowed.Code, allowed.Header())
	}
	if got := request("", "Bearer header.payload.signature").Code; got != http.StatusUnauthorized {
		t.Fatalf("JWT bearer status = %d, want 401", got)
	}
	if got := request("", "Bearer\theader.payload.signature").Code; got != http.StatusUnauthorized {
		t.Fatalf("tab-separated JWT bearer status = %d, want 401", got)
	}
	if got := request("", "Bearer gfsk_automation-token").Code; got != http.StatusNoContent {
		t.Fatalf("API token status = %d, want 204", got)
	}
	post := httptest.NewRequest(http.MethodPost, "/api/check", nil)
	post.Header.Set("Origin", "https://app.example.com")
	postRec := httptest.NewRecorder()
	app.Router().ServeHTTP(postRec, post)
	if postRec.Code != http.StatusForbidden {
		t.Fatalf("cookie mutation without CSRF status = %d, want 403", postRec.Code)
	}
	if !mgr.Config().CookieOnly || !mgr.Config().SessionSecure || mgr.Config().SessionCookie != "__Host-session" {
		t.Fatalf("BFF auth posture not applied: %+v", mgr.Config())
	}
}

func TestBFFPostureOnlyConsumesCORSPreflight(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	app.Router().Handle(http.MethodOptions, "/api/check", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(299)
	}))

	plain := httptest.NewRequest(http.MethodOptions, "/api/check", nil)
	plainRec := httptest.NewRecorder()
	app.Router().ServeHTTP(plainRec, plain)
	if plainRec.Code != 299 {
		t.Fatalf("application OPTIONS status = %d, want 299", plainRec.Code)
	}

	preflight := httptest.NewRequest(http.MethodOptions, "/api/check", nil)
	preflight.Header.Set("Origin", "https://app.example.com")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflightRec := httptest.NewRecorder()
	app.Router().ServeHTTP(preflightRec, preflight)
	if preflightRec.Code != http.StatusNoContent {
		t.Fatalf("CORS preflight status = %d, want 204", preflightRec.Code)
	}
}

func TestBFFPostureSuppressesLoginJWT(t *testing.T) {
	store := newMemoryUserStore()
	mgr := New(AuthConfig{DevMode: true, JWTSecret: "test-secret", UserStore: store})
	_ = framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	seedUser(t, store, "alice@test.com", "hunter22")
	r := mountRoutes(mgr)
	body, _ := json.Marshal(map[string]string{"email": "alice@test.com", "password": "hunter22"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"token"`) {
		t.Fatalf("cookie-only login leaked a JWT: %s", rec.Body.String())
	}
}

func TestBFFPostureRequiresExactOrigin(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	defer func() {
		if recover() == nil {
			t.Fatal("wildcard AllowedOrigin should panic")
		}
	}()
	_ = WithBFFPosture(mgr, BFFPostureConfig{AllowedOrigins: []string{"*"}})
}

func TestBFFPostureFollowsAppAPIPrefixRegardlessOfOptionOrder(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts func(*AuthManager) []framework.AppOption
	}{
		{
			name: "prefix before posture",
			opts: func(mgr *AuthManager) []framework.AppOption {
				return []framework.AppOption{
					framework.WithAPIPrefix("/api/v1"),
					WithBFFPosture(mgr, BFFPostureConfig{AllowedOrigins: []string{"https://app.example.com"}}),
				}
			},
		},
		{
			name: "posture before prefix",
			opts: func(mgr *AuthManager) []framework.AppOption {
				return []framework.AppOption{
					WithBFFPosture(mgr, BFFPostureConfig{AllowedOrigins: []string{"https://app.example.com"}}),
					framework.WithAPIPrefix("/api/v1"),
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
			app := framework.NewApp(tc.opts(mgr)...)
			app.Router().Get("/api/v1/check", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/check", nil)
			req.Header.Set("Origin", "https://evil.example")
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("custom API prefix was not protected: status = %d, want 403", rec.Code)
			}
		})
	}
}

func TestBFFPostureSignOutFormPassesLogoutOriginGuard(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mgr.RegisterRoutes(app.Router())
	app.Router().Get("/account", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(ui.SignOut(ui.SignOutConfig{Ctx: r.Context(), Next: "/"})))
	}))

	pageReq := httptest.NewRequest(http.MethodGet, "/account", nil)
	pageRec := httptest.NewRecorder()
	app.Router().ServeHTTP(pageRec, pageReq)
	csrfCookie := findResponseCookie(pageRec.Result(), "__Host-"+CSRFCookieName)
	if csrfCookie == nil {
		t.Fatal("account page did not mint the secure CSRF cookie")
	}
	body := pageRec.Body.String()
	if !strings.Contains(body, `data-fui-comp="ui-sign-out"`) {
		t.Fatalf("account page omitted SignOut: %s", body)
	}
	form := url.Values{"next": {"/"}}
	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", strings.NewReader(form.Encode()))
	logoutReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logoutReq.AddCookie(csrfCookie)
	logoutRec := httptest.NewRecorder()
	app.Router().ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusSeeOther {
		t.Fatalf("SignOut through BFF middleware = %d, want 303: %s", logoutRec.Code, logoutRec.Body.String())
	}
}

func TestBFFPostureLogoutExemptionStaysSameOriginAndExact(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mgr.RegisterRoutes(app.Router())
	app.Router().Post("/auth/logout-all", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	crossSite := httptest.NewRequest(http.MethodPost, "/auth/logout", strings.NewReader("next=%2F"))
	crossSite.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSite.Header.Set("Origin", "https://evil.example")
	crossSiteRec := httptest.NewRecorder()
	app.Router().ServeHTTP(crossSiteRec, crossSite)
	if crossSiteRec.Code != http.StatusForbidden {
		t.Fatalf("cross-site logout status = %d, want 403", crossSiteRec.Code)
	}

	sibling := httptest.NewRequest(http.MethodPost, "/auth/logout-all", nil)
	siblingRec := httptest.NewRecorder()
	app.Router().ServeHTTP(siblingRec, sibling)
	if siblingRec.Code != http.StatusForbidden {
		t.Fatalf("logout sibling without CSRF status = %d, want 403", siblingRec.Code)
	}
}

func findResponseCookie(resp *http.Response, name string) *http.Cookie {
	for _, cookie := range resp.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func TestBFFPreflightWorksWithoutOPTIONSRoute(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	// CRUD registration adds GET/POST/PUT/PATCH/DELETE but never OPTIONS —
	// the preflight must be answered by the guard on the 405 fallback path.
	app.Router().Get("/api/posts", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	pre := httptest.NewRequest(http.MethodOptions, "/api/posts", nil)
	pre.Header.Set("Origin", "https://app.example.com")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, pre)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight on OPTIONS-less route = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("preflight missing CORS headers: %v", rec.Header())
	}
}

func TestBFFNoAPIPrefixGuardsWholeApp(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	// No WithAPIPrefix: entities mount bare, so /posts must still answer
	// to the origin allowlist and the bearer-JWT rejection.
	app.Router().Get("/posts", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	evil := httptest.NewRequest(http.MethodGet, "/posts", nil)
	evil.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, evil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bare-mount untrusted Origin = %d, want 403", rec.Code)
	}

	jwt := httptest.NewRequest(http.MethodGet, "/posts", nil)
	jwt.Header.Set("Authorization", "Bearer header.payload.signature")
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, jwt)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bare-mount bearer JWT = %d, want 401", rec.Code)
	}

	plain := httptest.NewRequest(http.MethodGet, "/posts", nil)
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, plain)
	if rec.Code != http.StatusOK {
		t.Fatalf("bare-mount plain navigation = %d, want 200", rec.Code)
	}
}

func TestLogoutRejectsSameSiteSiblingOrigin(t *testing.T) {
	mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore()})
	app := framework.NewApp(WithBFFPosture(mgr, BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	mgr.RegisterRoutes(app.Router())

	// A form on a sibling subdomain is Sec-Fetch-Site: same-site and the
	// SameSite session cookie IS attached — the Origin host comparison
	// must refuse the forced logout.
	sibling := httptest.NewRequest(http.MethodPost, "https://app.example.com/auth/logout", strings.NewReader("next=%2F"))
	sibling.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sibling.Header.Set("Sec-Fetch-Site", "same-site")
	sibling.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, sibling)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("same-site sibling logout = %d, want 403", rec.Code)
	}

	// The app's own origin (matching host) stays allowed.
	self := httptest.NewRequest(http.MethodPost, "https://app.example.com/auth/logout", strings.NewReader("next=%2F"))
	self.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	self.Header.Set("Sec-Fetch-Site", "same-origin")
	self.Header.Set("Origin", "https://app.example.com")
	rec = httptest.NewRecorder()
	app.Router().ServeHTTP(rec, self)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("same-origin logout unexpectedly rejected: %d", rec.Code)
	}
}
