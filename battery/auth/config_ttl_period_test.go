package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// A negative SessionTTL is a sign or unit error. defaults() used to read
// it as unset and substitute seven days, so login worked with the wrong
// lifetime instead of failing the way auth.md says it does.
func TestNegativeSessionTTLRefused(t *testing.T) {
	m := New(AuthConfig{DevMode: true, SessionTTL: -time.Hour})
	err := m.Init(nil)
	if err == nil || !strings.Contains(err.Error(), "SessionTTL") {
		t.Fatalf("Init with SessionTTL -1h: err = %v, want a SessionTTL refusal", err)
	}
	if got := New(AuthConfig{DevMode: true}).Config().SessionTTL; got != 7*24*time.Hour {
		t.Fatalf("zero SessionTTL defaulted to %v, want 7 days", got)
	}
}

// The enrolment URL told the authenticator app period=30 whatever
// TwoFAConfig.Period said, while verification used the configured
// period, so with Period 60 every code the app showed was refused.
func TestEnrollURLCarriesPeriod(t *testing.T) {
	userStore := newMemoryUserStore()
	mgr := New(AuthConfig{
		JWTSecret:           "test-secret",
		AllowInMemoryStores: true,
		SessionCookie:       "session_id",
		SessionTTL:          time.Hour,
		UserStore:           userStore,
	})
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{Store: NewMemoryTwoFAStore(), Period: 60}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	seedUser(t, userStore, "alice@test.com", "testpass")
	r := mountRoutes(mgr)

	login := httptest.NewRequest("POST", "/auth/login", strings.NewReader(`{"email":"alice@test.com","password":"testpass"}`))
	login.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	r.ServeHTTP(lw, login)
	var cookie string
	for _, c := range lw.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c.Value
		}
	}
	if cookie == "" {
		t.Fatalf("login: %d %s", lw.Code, lw.Body.String())
	}

	req := httptest.NewRequest("POST", "/auth/2fa/enroll", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(resp.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := u.Query().Get("period"); got != "60" {
		t.Fatalf("otpauth period = %q, want 60 (url %s)", got, resp.URL)
	}
	if got := u.Query().Get("digits"); got != "6" {
		t.Fatalf("otpauth digits = %q, want 6", got)
	}
}
