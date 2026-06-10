package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// startApp boots app on an ephemeral loopback port and returns it + a cleanup,
// mirroring startSecurityExposureApp.
func startApp(t *testing.T, app *App) (*App, func()) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		app.serverMu.Lock()
		srv := app.server
		app.serverMu.Unlock()
		if srv != nil {
			return app, func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = app.Shutdown(ctx)
				<-done
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func openapiApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	app := NewApp(opts...)
	app.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	return app
}

// TestOpenAPI_GatedByDefault confirms the default: /openapi.json requires auth.
func TestOpenAPI_GatedByDefault(t *testing.T) {
	app, cleanup := startApp(t, openapiApp(t))
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("default /openapi.json = %d, want 401", rec.Code)
	}
}

// TestOpenAPI_PublicWhenOptedIn confirms WithPublicOpenAPI serves the spec
// unauthenticated — the fix for the quickstart curl that always 401'd.
func TestOpenAPI_PublicWhenOptedIn(t *testing.T) {
	app, cleanup := startApp(t, openapiApp(t, WithPublicOpenAPI()))
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public /openapi.json = %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
}
