package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: /auth/register must carry a default per-IP throttle like
// /auth/login does. Unthrottled registration is account-table flooding
// and (once an EmailSender is wired) an email-bombing primitive.
func TestRegisterRateLimitedByDefault(t *testing.T) {
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{JWTSecret: "k", SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: userStore, AllowInMemoryStores: true})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	sawTooMany := false
	for i := 0; i < 60; i++ {
		body, _ := json.Marshal(map[string]string{
			"email":    fmt.Sprintf("user%d@example.com", i),
			"password": "long-enough-password-123",
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "203.0.113.9:4444" // one IP hammering
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			sawTooMany = true
			break
		}
	}
	if !sawTooMany {
		t.Fatal("60 registrations from one IP never hit 429 — /auth/register has no default rate limit")
	}
}

// Property: cross-site posts are rejected BEFORE the limiter counts them.
// Otherwise a malicious page fires hidden cross-site posts from the
// victim's browser and locks the victim's own IP out of register/login.
func TestCrossSitePostsDontBurnRateBudget(t *testing.T) {
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{JWTSecret: "k", SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: userStore, AllowInMemoryStores: true})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	for i := 0; i < 50; i++ {
		vals := url.Values{"email": {"victim@example.com"}, "password": {"long-enough-password-123"}}
		req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(vals.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://evil.example")
		req.Host = "app.example"
		req.RemoteAddr = "203.0.113.9:4444"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-site post %d = %d, want 403", i, w.Code)
		}
	}

	// The victim's own registration from the same IP still goes through.
	body, _ := json.Marshal(map[string]string{"email": "victim@example.com", "password": "long-enough-password-123"})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:4444"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code == http.StatusTooManyRequests {
		t.Fatal("SECURITY: 403'd cross-site posts consumed the victim's per-IP rate budget")
	}
}

// A browser form submission that hits the per-IP limit gets the form-aware
// 303 error redirect, not a raw JSON 429 page.
func TestRegisterFormRateLimit303(t *testing.T) {
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{JWTSecret: "k", SessionTTL: time.Hour, SessionCookie: "session_id", UserStore: userStore, AllowInMemoryStores: true})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	r := router.New()
	mgr.RegisterRoutes(r)

	sawRedirect := false
	for i := 0; i < 60; i++ {
		vals := url.Values{"email": {fmt.Sprintf("user%d@example.com", i)}, "password": {"long-enough-password-123"}}
		req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(vals.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", "http://app.example/signup")
		req.Host = "app.example"
		req.RemoteAddr = "203.0.113.7:4444"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("form register got raw 429 at attempt %d — want the 303 error redirect", i)
		}
		if w.Code == http.StatusSeeOther && strings.Contains(w.Header().Get("Location"), "error=rate_limit") {
			sawRedirect = true
			break
		}
	}
	if !sawRedirect {
		t.Fatal("60 form registrations never produced the rate-limit error redirect")
	}
}
