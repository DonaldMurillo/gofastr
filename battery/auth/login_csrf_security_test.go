package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: login/register form posts are refused when the browser says
// they came from another site. Login CSRF needs no pre-existing cookie —
// an attacker's page silently logs the victim into an attacker-controlled
// account, capturing anything the victim then does. SameSite cookies
// don't cover it, so the endpoints check Origin/Sec-Fetch-Site.
func setupLoginCSRF(t *testing.T) *router.Router {
	t.Helper()
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{JWTSecret: "k", SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: userStore, AllowInMemoryStores: true})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	hash, _ := HashPassword("password123")
	user := &BasicUser{ID: "u-1", Email: "alice@example.com", Roles: []string{"user"}}
	userStore.users["alice@example.com"] = &storeEntry{user: user, hash: hash}
	userStore.byID[user.ID] = userStore.users["alice@example.com"]
	r := router.New()
	mgr.RegisterRoutes(r)
	return r
}

func postAuthForm(r *router.Router, path string, hdr map[string]string) *httptest.ResponseRecorder {
	vals := url.Values{"email": {"alice@example.com"}, "password": {"password123"}}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "app.example"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLoginFormCrossOriginRejected(t *testing.T) {
	r := setupLoginCSRF(t)
	w := postAuthForm(r, "/auth/login", map[string]string{"Origin": "https://evil.example"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin form login should 403, got %d (body=%s)", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" && c.Value != "" {
			t.Fatal("cross-origin login minted a session cookie")
		}
	}
}

func TestLoginFormSameOriginAllowed(t *testing.T) {
	r := setupLoginCSRF(t)
	w := postAuthForm(r, "/auth/login", map[string]string{"Origin": "https://app.example"})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("same-origin form login should succeed with 303, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestLoginFormNoOriginAllowed(t *testing.T) {
	// Non-browser clients (curl, tests) send no Origin — must pass.
	r := setupLoginCSRF(t)
	w := postAuthForm(r, "/auth/login", nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("form login without Origin should succeed with 303, got %d", w.Code)
	}
}

func TestRegisterFormCrossOriginRejected(t *testing.T) {
	r := setupLoginCSRF(t)
	vals := url.Values{"email": {"new@example.com"}, "password": {"long-enough-password-123"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(vals.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "app.example"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin form register should 403, got %d", w.Code)
	}
}

func TestLoginFormCrossSiteFetchMetadataRejected(t *testing.T) {
	// Some agents send Sec-Fetch-Site without a usable Origin.
	r := setupLoginCSRF(t)
	w := postAuthForm(r, "/auth/login", map[string]string{"Sec-Fetch-Site": "cross-site"})
	if w.Code != http.StatusForbidden {
		t.Fatalf("Sec-Fetch-Site: cross-site form login should 403, got %d", w.Code)
	}
}

// Real browsers send Origin: null on a top-level same-origin form
// navigation (opaque origin), with Sec-Fetch-Site: same-origin. That is
// a LEGITIMATE login and must be allowed — Fetch Metadata is authoritative.
func TestLoginFormNullOriginSameSiteAllowed(t *testing.T) {
	r := setupLoginCSRF(t)
	w := postAuthForm(r, "/auth/login", map[string]string{
		"Origin":         "null",
		"Sec-Fetch-Site": "same-origin",
	})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("null-origin same-origin form login should succeed with 303, got %d (body=%s)", w.Code, w.Body.String())
	}
}
