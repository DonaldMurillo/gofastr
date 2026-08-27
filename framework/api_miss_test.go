package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// apiMissApp builds a started app whose NotFound fall-through is a marker
// handler, so a test can tell EXACTLY who answered a miss: the guard
// (problem+json, no marker) or the previously installed handler (marker).
// This mirrors the real wiring (a UI host installs its NotFound before
// Start wraps it) without depending on uihost behavior.
func apiMissApp(t *testing.T, opts ...AppOption) (*App, func()) {
	t.Helper()
	opts = append(opts, WithoutDefaultMiddleware())
	app := NewApp(opts...)
	app.Router().NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Previous-NotFound", "yes")
		w.WriteHeader(http.StatusNotFound)
	}))
	return startApp(t, app)
}

// TestAPIMissAnswersProblemJSON pins the api-json-error contract: ANY
// /api-prefixed path that matches no route answers 404
// application/problem+json whose body carries "status": 404 — never the
// previous (UI host) HTML fall-through.
func TestAPIMissAnswersProblemJSON(t *testing.T) {
	a, cleanup := apiMissApp(t, WithConfig(AppConfig{Name: "apimiss"}))
	defer cleanup()

	for _, path := range []string{"/api/posts/does-not-exist", "/api/nope", "/api"} {
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: code %d, want 404", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("GET %s: Content-Type %q, want application/problem+json", path, ct)
		}
		if rec.Header().Get("X-Previous-NotFound") != "" {
			t.Errorf("GET %s: reached the previous NotFound handler; the guard must claim API-namespace misses", path)
		}
		var doc map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Errorf("GET %s: body is not JSON: %v (%s)", path, err, rec.Body.String())
			continue
		}
		if status, ok := doc["status"].(float64); !ok || int(status) != 404 {
			t.Errorf("GET %s: problem status = %v, want 404", path, doc["status"])
		}
	}
}

// TestAPIMissDelegatesOutsideNamespace pins that the guard claims ONLY the
// API namespace: a miss outside it reaches the previously installed
// NotFound handler untouched.
func TestAPIMissDelegatesOutsideNamespace(t *testing.T) {
	a, cleanup := apiMissApp(t, WithConfig(AppConfig{Name: "apimiss"}))
	defer cleanup()

	for _, path := range []string{"/definitely-not-here", "/api-guide", "/apifoo"} {
		rec := httptest.NewRecorder()
		a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Header().Get("X-Previous-NotFound") != "yes" {
			t.Errorf("GET %s: did not reach the previous NotFound handler (code %d, CT %q)", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

// TestAPIMissPrefixComesFromConfig pins that the guarded namespace resolves
// from AppConfig.APIPrefix, never a hard-coded /api: under
// WithAPIPrefix("/v1") the guard moves with the config.
func TestAPIMissPrefixComesFromConfig(t *testing.T) {
	a, cleanup := apiMissApp(t, WithConfig(AppConfig{Name: "apimiss", APIPrefix: "/v1"}))
	defer cleanup()

	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/posts/does-not-exist", nil))
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/problem+json") {
		t.Fatalf("GET /v1/posts/does-not-exist: code %d CT %q, want 404 problem+json", rec.Code, rec.Header().Get("Content-Type"))
	}

	// /api/* is no longer the API namespace; those misses delegate.
	rec = httptest.NewRecorder()
	a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/posts/does-not-exist", nil))
	if rec.Header().Get("X-Previous-NotFound") != "yes" {
		t.Fatalf("GET /api/posts/does-not-exist under WithAPIPrefix(\"/v1\"): must delegate, got code %d CT %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}

// TestAPIMissDoesNotReflectHostilePathUnescaped pins the reflection guard:
// a hostile path reaches the problem document only through JSON encoding
// (encoder HTML-escaping), never as raw markup.
func TestAPIMissDoesNotReflectHostilePathUnescaped(t *testing.T) {
	a, cleanup := apiMissApp(t, WithConfig(AppConfig{Name: "apimiss"}))
	defer cleanup()

	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/<script>alert(1)</script>", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Fatalf("hostile path reflected unescaped: %s", rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not valid JSON after escaping: %v (%s)", err, rec.Body.String())
	}
}
