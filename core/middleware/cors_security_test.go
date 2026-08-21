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

// Every other ResponseWriter wrapper in this package forwards Flusher and
// Hijacker (metrics, logging, tracing and timeout each have a test pinning
// it). CORS's stripCredsWriter, installed on every non-preflight request
// when AllowedOrigins is ["*"], did not, so behind a wildcard CORS policy
// the SSE bus lost its Flusher and the WebSocket upgrade lost its Hijacker.
// The framework's SSE constructor type-asserts http.Flusher, so this is a
// hard failure, not degraded buffering.
func TestCORS_PreservesFlusherAndHijacker(t *testing.T) {
	var sawFlusher, sawHijacker bool
	h := CORS(CORSConfig{AllowedOrigins: []string{"*"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, sawFlusher = w.(http.Flusher)
			_, sawHijacker = w.(http.Hijacker)
		}))

	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if !sawFlusher {
		t.Error("Flusher not preserved through the wildcard-CORS wrapper — SSE cannot stream behind it")
	}
	if !sawHijacker {
		t.Error("Hijacker not preserved through the wildcard-CORS wrapper — WebSocket upgrade fails behind it")
	}
}

// The wrapper exists to stop a downstream handler pairing ACAO:* with
// credentials, which is the actual vulnerability. Preserving the interfaces
// must not weaken that.
func TestCORS_WildcardStillStripsCredentials(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"*"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.WriteHeader(200)
		}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("SECURITY: [cors] credentials header survived alongside ACAO:* (got %q)", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ACAO = %q, want *", got)
	}
}

// Flush must strip too, or a streaming handler that sets the header and
// flushes before WriteHeader slips it out.
func TestCORS_WildcardStripsCredentialsOnFlush(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"*"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("SECURITY: [cors] credentials header survived a pre-write Flush (got %q)", got)
	}
}

// Vary must be APPENDED. Set() clobbers a Vary an upstream middleware
// already wrote (compression sets Vary: Accept-Encoding), which makes a
// shared cache serve one encoding to clients that cannot read it.
func TestCORS_AppendsVaryInsteadOfClobbering(t *testing.T) {
	h := CORS(CORSConfig{AllowedOrigins: []string{"https://ok.example"}})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	rec := httptest.NewRecorder()
	rec.Header().Set("Vary", "Accept-Encoding")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://ok.example")
	h.ServeHTTP(rec, req)

	vary := rec.Header().Values("Vary")
	var sawEncoding, sawOrigin bool
	for _, v := range vary {
		if strings.Contains(v, "Accept-Encoding") {
			sawEncoding = true
		}
		if strings.Contains(v, "Origin") {
			sawOrigin = true
		}
	}
	if !sawEncoding {
		t.Errorf("CORS clobbered an existing Vary header; got %v", vary)
	}
	if !sawOrigin {
		t.Errorf("CORS did not vary on Origin; got %v", vary)
	}
}
