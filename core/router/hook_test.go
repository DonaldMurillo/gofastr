package router

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestRegisterHookCalledOnRoot(t *testing.T) {
	r := New()
	var mu sync.Mutex
	var got []string
	r.SetRegisterHook(func(method, pattern string) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, method+" "+pattern)
	})

	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	sub := r.Group("/api")
	sub.Post("/items", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	mu.Lock()
	defer mu.Unlock()
	want := []string{"GET /users/{id}", "POST /api/items"}
	if len(got) != len(want) {
		t.Fatalf("expected %d hook calls, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestRegisterHookNilIsNoop(t *testing.T) {
	r := New()
	r.SetRegisterHook(nil) // must not panic
	r.Get("/ok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	// Just reaching here without panicking is the assertion.
}

func TestRouteGateAllowsWhenTrue(t *testing.T) {
	r := New()
	r.SetRouteGate(func(pattern string) bool {
		return true
	})
	called := false
	r.Get("/allowed", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/allowed", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if !called {
		t.Fatal("handler not called when gate returned true")
	}
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRouteGateBlocksWhenFalse(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool {
		return key != "GET /blocked"
	})
	r.Get("/blocked", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not run when gate is false")
	}))
	r.Get("/open", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	// Blocked route → 404
	req := httptest.NewRequest(http.MethodGet, "/blocked", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("blocked route: expected 404, got %d", w.Code)
	}

	// Open route → 200
	req2 := httptest.NewRequest(http.MethodGet, "/open", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != 200 {
		t.Fatalf("open route: expected 200, got %d", w2.Code)
	}
}

func TestRouteGateNilIsNoop(t *testing.T) {
	r := New()
	r.SetRouteGate(nil) // must not block
	called := false
	r.Get("/ok", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if !called {
		t.Fatal("handler not called when gate is nil")
	}
}

func TestRouteGateBlocksBeforeMiddleware(t *testing.T) {
	r := New()
	mwRan := false
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			mwRan = true
			next.ServeHTTP(w, req)
		})
	})
	r.SetRouteGate(func(pattern string) bool { return false })
	r.Get("/blocked", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	req := httptest.NewRequest(http.MethodGet, "/blocked", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if mwRan {
		t.Fatal("middleware ran for a gated route — gate must fire before the chain")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// TestGateKeyedOnMethodAndPath verifies that two modules owning different
// methods on the same path are gated independently (H1 scenario a).
func TestGateKeyedOnMethodAndPath(t *testing.T) {
	r := New()
	// Gate blocks only "GET /shared" — POST /shared must pass.
	r.SetRouteGate(func(key string) bool {
		return key != "GET /shared"
	})
	r.Get("/shared", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("GET handler should not run — gated")
	}))
	r.Post("/shared", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(201)
	}))

	// GET → 404 (gated)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/shared", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /shared gated: expected 404, got %d", w.Code)
	}

	// POST → 201 (not gated)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/shared", nil))
	if w2.Code != 201 {
		t.Fatalf("POST /shared not gated: expected 201, got %d", w2.Code)
	}
}

// TestGateNonModuleMethodNot404 verifies that a module owning GET /hook
// being disabled does not 404 a non-module POST /hook (H1 scenario b).
func TestGateNonModuleMethodNot404(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool {
		// Only "GET /hook" is gated (module-owned).
		return key != "GET /hook"
	})
	r.Get("/hook", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("GET handler should not run — gated")
	}))
	r.Post("/hook", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	// POST /hook → 200 (not gated)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/hook", nil))
	if w.Code != 200 {
		t.Fatalf("POST /hook: expected 200, got %d", w.Code)
	}
}

// TestGateAllMethodsGatedIs404 verifies that when the only method on a
// path is gated, probing another method returns 404 (not 405 with Allow).
func TestGateAllMethodsGatedIs404(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool {
		return key != "GET /only"
	})
	r.Get("/only", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	// POST /only → 404 (only method is gated, path effectively non-existent)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/only", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (all methods gated), got %d", w.Code)
	}
	// No Allow header should be present.
	if allow := w.Header().Get("Allow"); allow != "" {
		t.Fatalf("expected no Allow header, got %q", allow)
	}
}

// TestGate405ExcludesGatedMethods verifies the Allow header lists only
// non-gated methods when some methods on a path are gated (M5).
func TestGate405ExcludesGatedMethods(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool {
		// "GET /hook" gated, "POST /hook" not.
		return key != "GET /hook"
	})
	r.Get("/hook", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	r.Post("/hook", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	// DELETE /hook → 405 with Allow: POST only (GET is gated)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/hook", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	allow := w.Header().Get("Allow")
	if allow != "POST" {
		t.Fatalf("expected Allow: POST, got %q", allow)
	}
}

// TestRouteGateConcurrentSafe exercises SetRouteGate concurrent with
// dispatch to verify no data race (M4).
func TestRouteGateConcurrentSafe(t *testing.T) {
	r := New()
	r.SetRouteGate(func(key string) bool { return true })
	r.Get("/hot", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			r.SetRouteGate(func(key string) bool { return true })
		}
	}()

	for i := 0; i < 200; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/hot", nil))
	}
	<-done
}
