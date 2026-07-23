package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMethodNotAllowedCustomHandler verifies that a MethodNotAllowed
// handler registered via Router.MethodNotAllowed is dispatched instead
// of the bare http.Error text when a path exists but the request's
// method is not registered. This mirrors how Router.NotFound lets a
// caller customise the 404 path.
func TestMethodNotAllowedCustomHandler(t *testing.T) {
	r := New()
	r.Get("/items", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom-405", "yes")
		w.WriteHeader(http.StatusTeapot) // 418 — unmistakable
	}))

	req := httptest.NewRequest(http.MethodPost, "/items", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Fatalf("expected custom handler (418), got %d body=%q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Custom-405"); got != "yes" {
		t.Fatalf("custom MethodNotAllowed handler did not run; X-Custom-405=%q", got)
	}
}

// TestMethodNotAllowedKeepsAllowHeader verifies the router sets the
// RFC-compliant Allow header BEFORE dispatching to the custom handler,
// so the handler inherits it without having to recompute the allowed
// method set.
func TestMethodNotAllowedKeepsAllowHeader(t *testing.T) {
	r := New()
	r.Get("/things", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("custom"))
	}))

	req := httptest.NewRequest(http.MethodDelete, "/things", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if allow := w.Header().Get("Allow"); allow != "GET" {
		t.Fatalf("expected Allow: GET, got %q", allow)
	}
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

// TestMethodNotAllowedDefaultIsBare405 verifies that without a custom
// handler the router keeps its original bare-text 405 behaviour.
func TestMethodNotAllowedDefaultIsBare405(t *testing.T) {
	r := New()
	r.Get("/widgets", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	req := httptest.NewRequest(http.MethodPost, "/widgets", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET" {
		t.Fatalf("expected Allow: GET, got %q", allow)
	}
}

// TestGatedMethodsStillReturn404 is the security regression guard: a
// path whose only method is gated (disabled-module) must still return
// 404 — indistinguishable from a non-existent path — even when a
// MethodNotAllowed handler is installed. Without this, a disabled
// module's existence would leak through the custom 405 page.
func TestGatedMethodsStillReturn404(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool {
		return key != "GET /secret"
	})
	r.Get("/secret", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("MethodNotAllowed handler must NOT run for all-gated paths")
	}))

	// POST /secret → 404 (the only method, GET, is gated).
	req := httptest.NewRequest(http.MethodPost, "/secret", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (all methods gated), got %d", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "" {
		t.Fatalf("expected no Allow header for all-gated path, got %q", allow)
	}
}

// TestMethodNotAllowedSeesLateMiddleware verifies the middleware chain
// wraps the MethodNotAllowed handler at request time (mirrors the
// equivalent NotFound test).
func TestMethodNotAllowedSeesLateMiddleware(t *testing.T) {
	r := New()
	r.Get("/late", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-405-Late", "yes")
			next.ServeHTTP(w, req)
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/late", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if got := w.Header().Get("X-405-Late"); got != "yes" {
		t.Fatalf("late middleware did not wrap MethodNotAllowed; X-405-Late=%q", got)
	}
}

// TestDefaultFallbacksRunMiddleware verifies the DEFAULT 405/404
// responses (no custom handler installed) still pass through the
// middleware chain — CORS preflights on method-mismatched paths are the
// canonical consumer.
func TestDefaultFallbacksRunMiddleware(t *testing.T) {
	r := New()
	r.Get("/only-get", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Chain", "yes")
			next.ServeHTTP(w, req)
		})
	})

	post := httptest.NewRequest(http.MethodPost, "/only-get", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, post)
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("X-Chain") != "yes" {
		t.Fatalf("default 405: code=%d X-Chain=%q, want 405 with chain", w.Code, w.Header().Get("X-Chain"))
	}

	miss := httptest.NewRequest(http.MethodGet, "/nope", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, miss)
	if w.Code != http.StatusNotFound || w.Header().Get("X-Chain") != "yes" {
		t.Fatalf("default 404: code=%d X-Chain=%q, want 404 with chain", w.Code, w.Header().Get("X-Chain"))
	}
}
