package framework

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// liveReloadStatus opens the livereload stream on a's router with an
// already-cancelled context, so a live stream writes its ready event and
// returns at once.
func liveReloadStatus(t *testing.T, a *App) (int, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/__livereload", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// /__livereload parks a goroutine per EventSource with no seat and no
// cap. On a non-loopback dev bind any LAN peer could open as many as it
// liked, so Start withdraws it there, the way it withdraws the dev MCP.
func TestLiveReloadOffOnExposedBind(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV_LIVERELOAD", "")
	t.Setenv(dev.LiveReloadExposeEnv, "")

	loop := NewApp(WithoutDefaultMiddleware())
	loop.guardDevLiveReloadBind("localhost:8080")
	if code, body := liveReloadStatus(t, loop); code != http.StatusOK || !strings.Contains(body, "ready") {
		t.Fatalf("a loopback bind must keep livereload: %d %q", code, body)
	}

	exposed := NewApp(WithoutDefaultMiddleware())
	exposed.guardDevLiveReloadBind("0.0.0.0:8080")
	if code, _ := liveReloadStatus(t, exposed); code != http.StatusNotFound {
		t.Fatalf("livereload on an exposed bind answered %d, want 404", code)
	}

	t.Setenv(dev.LiveReloadExposeEnv, "1")
	optIn := NewApp(WithoutDefaultMiddleware())
	optIn.guardDevLiveReloadBind("0.0.0.0:8080")
	if code, _ := liveReloadStatus(t, optIn); code != http.StatusOK {
		t.Fatalf("the opt-in did not keep livereload: %d", code)
	}
}

// A host that served its own /__livereload kept it on an exposed bind,
// and Start still logged "livereload is off". The warning says the
// host's stream stays on, and the route keeps answering.
func TestExposedBindWarnsHostStream(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	t.Setenv(dev.LiveReloadExposeEnv, "")
	var logs bytes.Buffer
	a := NewApp(WithoutDefaultMiddleware(), WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	a.router.Get(dev.LiveReloadStreamURL, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	dev.RegisterLiveReload(a.router)
	a.guardDevLiveReloadBind("0.0.0.0:8080")
	if code, _ := liveReloadStatus(t, a); code != http.StatusTeapot {
		t.Fatalf("host stream answered %d, want it left serving", code)
	}
	out := logs.String()
	if strings.Contains(out, "livereload is off") || !strings.Contains(out, "stays on") {
		t.Fatalf("exposed bind with a host stream logged:\n%s", out)
	}
}
