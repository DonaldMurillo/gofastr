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
