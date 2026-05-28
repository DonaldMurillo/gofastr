package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCORS_SanitizesConfigTokens locks the contract that CRLF/NUL bytes
// in CORSConfig.AllowedMethods or AllowedHeaders never reach the wire.
// Even config-time tokens are sometimes built from env/template data
// and a header-split there would smuggle arbitrary response headers.
func TestCORS_SanitizesConfigTokens(t *testing.T) {
	mw := CORS(CORSConfig{
		AllowedOrigins: []string{"https://good.example"},
		AllowedMethods: []string{"GET", "POST\r\nX-Forged: 1"},
		AllowedHeaders: []string{"Content-Type", "X-Bad\nSet-Cookie: x=1"},
	})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://good.example")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	for _, name := range []string{"Access-Control-Allow-Methods", "Access-Control-Allow-Headers"} {
		if got := rec.Header().Get(name); strings.ContainsAny(got, "\r\n\x00\x7f") {
			t.Fatalf("CORS %s reflected unsanitized bytes: %q", name, got)
		}
		if got := rec.Header().Get("X-Forged"); got != "" {
			t.Fatalf("CORS smuggled X-Forged header into response: %q", got)
		}
	}
}

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
