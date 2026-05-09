package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofastr/gofastr/core/router"
)

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
