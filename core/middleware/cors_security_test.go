package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_RejectedOriginOmitsAllowMethods(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://good.example"}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "" {
		t.Fatalf("SECURITY: [cors] rejected origin received Access-Control-Allow-Methods=%q. Attack: permissive CORS metadata leakage.", got)
	}
}

func TestCORS_RejectedOriginOmitsAllowHeaders(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://good.example"}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "" {
		t.Fatalf("SECURITY: [cors] rejected origin received Access-Control-Allow-Headers=%q. Attack: permissive CORS metadata leakage.", got)
	}
}

func TestCORS_RejectedOriginPreflightReturnsForbidden(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://good.example"}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("SECURITY: [cors] rejected-origin preflight returned %d, want 403. Attack: preflight appears to succeed for blocked origins.", rec.Code)
	}
}

func TestCORS_EmptyAllowedOriginsDenyAllWithoutExtraHeaders(t *testing.T) {
	h := CORS(CORSConfig{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://any.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" ||
		rec.Header().Get("Access-Control-Allow-Methods") != "" ||
		rec.Header().Get("Access-Control-Allow-Headers") != "" {
		t.Fatalf("SECURITY: [cors] empty AllowedOrigins still emitted CORS headers: %#v", rec.Header())
	}
}

func TestCORS_WildcardStripsCredentialsHeader(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"*"}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("SECURITY: [cors] wildcard ACAO response kept Access-Control-Allow-Credentials=%q. Attack: invalid wildcard+credentials CORS config survives.", got)
	}
}
