package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestWrapNotFound_DelegatesToPreviousHandler pins the composition contract:
// WrapNotFound must see genuine misses, claim what it claims, and delegate
// everything else to the handler that was installed before it — replacing it
// outright (the regression this guards against) would swallow the previous
// handler's behavior for every non-claimed miss.
func TestWrapNotFound_DelegatesToPreviousHandler(t *testing.T) {
	r := New()
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Previous-NotFound", "yes")
		w.WriteHeader(http.StatusNotFound)
	}))
	r.WrapNotFound(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if strings.HasPrefix(req.URL.Path, "/claimed/") {
				w.WriteHeader(http.StatusTeapot)
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/claimed/x", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("claimed miss: code %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Header().Get("X-Previous-NotFound") != "" {
		t.Fatal("claimed miss must not reach the previous handler")
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/other", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delegated miss: code %d, want 404", rec.Code)
	}
	if rec.Header().Get("X-Previous-NotFound") != "yes" {
		t.Fatal("non-claimed miss must reach the previously installed NotFound handler")
	}
}

// TestWrapNotFound_NoPreviousHandlerUsesPlain404 pins the no-NotFound case:
// next is a plain 404, so wrapping a router that never installed one adds
// the wrapper's behavior without inventing a fall-through that answers
// anything but 404.
func TestWrapNotFound_NoPreviousHandlerUsesPlain404(t *testing.T) {
	r := New()
	r.Get("/real", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r.WrapNotFound(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/claimed" {
				w.WriteHeader(http.StatusTeapot)
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/claimed", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("claimed miss: code %d, want %d", rec.Code, http.StatusTeapot)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nothing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unclaimed miss with no prior NotFound: code %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "404") {
		t.Fatalf("plain-404 next must emit the standard 404 body, got %q", rec.Body.String())
	}
}

// TestWrapNotFound_MiddlewareRunsOnce pins the load-bearing part of the
// composition: delegation must NOT re-run the middleware chain. Running it
// twice would double-log, double request IDs, and double security-header
// work on every miss the wrapper delegates.
func TestWrapNotFound_MiddlewareRunsOnce(t *testing.T) {
	r := New()
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	var hits int
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			hits++
			w.Header().Add("X-Chain-Run", "1")
			next.ServeHTTP(w, req)
		})
	})
	r.WrapNotFound(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/claimed" {
				w.WriteHeader(http.StatusTeapot)
				return
			}
			next.ServeHTTP(w, req)
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/delegated", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delegated miss: code %d, want 404", rec.Code)
	}
	if hits != 1 {
		t.Fatalf("middleware ran %d times on a delegated miss, want exactly 1", hits)
	}
	if got := len(rec.Header().Values("X-Chain-Run")); got != 1 {
		t.Fatalf("X-Chain-Run appeared %d times, want 1", got)
	}

	// The claimed path also runs the chain exactly once (the wrapper is the
	// NotFound raw handler; the chain wraps it, not bypasses it).
	hits = 0
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/claimed", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("claimed miss: code %d, want %d", rec.Code, http.StatusTeapot)
	}
	if hits != 1 {
		t.Fatalf("middleware ran %d times on a claimed miss, want exactly 1", hits)
	}
}

// TestWrapNotFound_Leaves405AndMatchedRoutesAlone pins the dispatch scope:
// the wrapper sees genuine misses only. A method mismatch on an existing
// path goes to MethodNotAllowed (never the wrapper), and a matched route is
// served normally.
func TestWrapNotFound_Leaves405AndMatchedRoutesAlone(t *testing.T) {
	r := New()
	r.Get("/thing", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r.NotFound(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("custom-nf"))
	}))
	r.WrapNotFound(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})
	})

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/thing", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("matched route: code %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/thing", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method mismatch: code %d, want 405 (must bypass the wrapper)", rec.Code)
	}
}
