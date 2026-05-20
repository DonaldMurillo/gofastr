package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Tests in this file pin the SHAPE of App's middleware chain — what
// defaults are present, how Use composes with them, and how the
// chain wraps explicit routes / Mountables / NotFound. Logger and
// plugin-lifecycle behaviour live in their own files (app_logger_test.go,
// plugin_lifecycle_test.go).

// TestDefaultMiddlewareWrapsExplicitRoutes pins the bug fixed in
// "fix(framework,router): apply default middleware in NewApp;..." —
// router.Router wraps handlers at registration time, so default
// middleware must be committed in NewApp BEFORE any user route is
// added. If applyDefaultMiddleware ever drifts back into Start, this
// test catches it: routes registered between NewApp and Start get the
// chain applied.
func TestDefaultMiddlewareWrapsExplicitRoutes(t *testing.T) {
	app := NewApp()
	app.Router.Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(app.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/probe")
	if err != nil {
		t.Fatalf("GET /probe: %v", err)
	}
	defer resp.Body.Close()

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("expected X-Request-Id header on a default-middleware route")
	}
}

// TestDefaultMiddlewareWrapsNotFoundHandler pins the second half of the
// router fix: NotFound handlers must run through the same middleware as
// matched routes. UI page rendering goes through NotFound (uihost mounts
// a catch-all), so a regression here means UI pages would silently lose
// security headers and request IDs.
func TestDefaultMiddlewareWrapsNotFoundHandler(t *testing.T) {
	app := NewApp()
	app.Router.NotFound(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("catch-all"))
	}))

	srv := httptest.NewServer(app.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/nothing-registered-here")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options on NotFound route = %q, want DENY", got)
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("X-Request-Id should attach to NotFound responses too")
	}
}

// TestWithoutDefaultMiddlewareSuppressesChain ensures the opt-out
// AppOption actually skips the default chain.
func TestWithoutDefaultMiddlewareSuppressesChain(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router.Get("/bare", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(app.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/bare")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-Frame-Options"); got != "" {
		t.Errorf("expected no X-Frame-Options header, got %q", got)
	}
	if resp.Header.Get("X-Request-Id") != "" {
		t.Error("expected no X-Request-Id when defaults are disabled")
	}
}

// TestUseDoesNotDisableDefaults pins that App.Use is additive — calling
// Use does not silently strip the default middleware chain (which was
// the old behavior and a real footgun).
func TestUseDoesNotDisableDefaults(t *testing.T) {
	app := NewApp()
	app.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-User-Mw", "yes")
			next.ServeHTTP(w, r)
		})
	})
	app.Router.Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	srv := httptest.NewServer(app.Router)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/probe")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("X-User-Mw"); got != "yes" {
		t.Errorf("user middleware did not fire; X-User-Mw = %q", got)
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("default security headers stripped by App.Use; X-Frame-Options = %q", got)
	}
	if resp.Header.Get("X-Request-Id") == "" {
		t.Error("default RequestID stripped by App.Use")
	}
}

// TestRequestTimeoutOverride pins that AppConfig.RequestTimeout is
// honored by the default middleware chain.
func TestRequestTimeoutOverride(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{RequestTimeout: 50 * time.Millisecond}))
	app.Router.Get("/slow", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	srv := httptest.NewServer(app.Router)
	defer srv.Close()
	start := time.Now()
	resp, err := http.Get(srv.URL + "/slow")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	dur := time.Since(start)
	if dur > 500*time.Millisecond {
		t.Fatalf("request took %v, expected ~50ms timeout to kick in", dur)
	}
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504 Gateway Timeout", resp.StatusCode)
	}
}

// stubMountable is a Mountable that records whether its Mount method was
// called and registers a probe route the test can hit.
type stubMountable struct {
	mounted bool
}

func (s *stubMountable) Mount(r *router.Router) {
	s.mounted = true
	r.Get("/mounted-probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

// TestMountRegistersImmediately pins the lifecycle fix that lets tests
// hit Mountable routes via httptest.NewServer(app.Router) without calling
// Start. Mount is fluent and the route is reachable instantly.
func TestMountRegistersImmediately(t *testing.T) {
	app := NewApp()
	m := &stubMountable{}
	app.Mount(m)
	if !m.mounted {
		t.Fatal("Mountable.Mount should be called from App.Mount, not deferred")
	}

	srv := httptest.NewServer(app.Router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/mounted-probe")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// TestShutdownIsSafeBeforeStart guards against a nil dereference on the
// "build & exit early" path some CLI flows take.
func TestShutdownIsSafeBeforeStart(t *testing.T) {
	app := NewApp()
	if err := app.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown before Start should be a no-op, got %v", err)
	}
}
