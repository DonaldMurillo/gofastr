package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

func TestLeakEndpointRequiresAuth(t *testing.T) {
	app := NewApp()
	app.registerDebugEndpoints()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.debug/goroutineleak", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /.debug/goroutineleak = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("missing Cache-Control: no-store")
	}
}

func TestLeakEndpointServesProfile(t *testing.T) {
	app := NewApp()
	app.registerDebugEndpoints()

	req := httptest.NewRequest(http.MethodGet, "/.debug/goroutineleak", nil)
	req = req.WithContext(handler.SetUser(req.Context(), "tester"))
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authed /.debug/goroutineleak = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "goroutineleak") {
		t.Fatalf("profile output missing header line: %q", rec.Body.String()[:min(200, rec.Body.Len())])
	}
}

func TestLeakToolReportsHealthyZero(t *testing.T) {
	app := NewApp()
	out, err := app.toolGoroutineLeaks(t.Context(), nil)
	if err != nil {
		t.Fatalf("toolGoroutineLeaks: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("tool returned %T, want map", out)
	}
	if _, ok := m["count"].(int); !ok {
		t.Fatalf("count is %T, want int", m["count"])
	}
	if _, ok := m["stacks"].(string); !ok {
		t.Fatalf("stacks is %T, want string", m["stacks"])
	}
}

// A full app start/serve/stop cycle must not abandon workers. Leaked
// goroutines never un-leak, so the profile count must not grow across
// the cycle — the assertion that keeps SSE/outbox/battery teardown
// honest as the framework evolves.
func TestAppCycleLeaksNoGoroutines(t *testing.T) {
	before := goroutineLeakCount()
	if before < 0 {
		t.Skip("goroutineleak profile unavailable")
	}
	app := NewApp()
	addr, stop := startOnRandomPort(t, app)
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("get healthz: %v", err)
	}
	resp.Body.Close()
	stop()
	if after := goroutineLeakCount(); after > before {
		t.Fatalf("app cycle leaked %d goroutine(s): before=%d after=%d — hit /.debug/goroutineleak for stacks", after-before, before, after)
	}
}
