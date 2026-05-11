package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func csrfHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCSRF_GetSetsCookie(t *testing.T) {
	h := CSRF(CSRFConfig{})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "csrf_token=") {
		t.Fatalf("expected csrf_token cookie set, got %q", w.Header().Get("Set-Cookie"))
	}
}

func TestCSRF_PostBlockedWithoutToken(t *testing.T) {
	h := CSRF(CSRFConfig{})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestCSRF_PostAllowedWithMatchingHeader(t *testing.T) {
	h := CSRF(CSRFConfig{})(csrfHandler())
	tok := "secret-csrf-value"
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	r.Header.Set("X-CSRF-Token", tok)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with matching token, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCSRF_PostBlockedOnMismatch(t *testing.T) {
	h := CSRF(CSRFConfig{})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: "a"})
	r.Header.Set("X-CSRF-Token", "b")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on token mismatch, got %d", w.Code)
	}
}

func TestCSRF_SkipPredicateBypasses(t *testing.T) {
	h := CSRF(CSRFConfig{Skip: SkipBearerAuth()})(csrfHandler())
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Authorization", "Bearer xyz")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected bearer-authed POST to bypass CSRF, got %d", w.Code)
	}
}
