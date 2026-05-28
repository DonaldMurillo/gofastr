package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreoa "github.com/DonaldMurillo/gofastr/core/openapi"
	"github.com/DonaldMurillo/gofastr/core/schema"
	crudpkg "github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	openapipkg "github.com/DonaldMurillo/gofastr/framework/openapi"
)

func securityExposureApp(t *testing.T) *App {
	t.Helper()
	app := NewApp()
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "secret", Type: schema.String},
		},
	}.WithTimestamps(false))
	return app
}

func startSecurityExposureApp(t *testing.T) (*App, func()) {
	t.Helper()
	app := securityExposureApp(t)
	done := make(chan error, 1)
	go func() {
		done <- app.Start("127.0.0.1:0")
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		app.serverMu.Lock()
		srv := app.server
		app.serverMu.Unlock()
		if srv != nil {
			return app, func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = app.Shutdown(shutdownCtx)
				<-done
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for app server to start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEntityLLMMDHandler_RequiresAuth(t *testing.T) {
	app := securityExposureApp(t)
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	crudpkg.LLMMDHandler(ent).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/posts/llm.md", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [llmmd] unauthenticated entity llm.md returned %d. Attack: schema disclosure without auth.", rec.Code)
	}
}

func TestRegistryLLMMDHandler_RequiresAuth(t *testing.T) {
	app := securityExposureApp(t)
	rec := httptest.NewRecorder()
	crudpkg.RegistryLLMMDHandler(app.Registry, "Test App").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/llm.md", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [llmmd] unauthenticated registry llm.md returned %d. Attack: global schema disclosure without auth.", rec.Code)
	}
}

func TestDebugStatsEndpoint_RequiresAuth(t *testing.T) {
	app := NewApp()
	app.registerDebugEndpoints()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.debug/stats", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [debug] unauthenticated /.debug/stats returned %d. Attack: runtime/process disclosure without auth.", rec.Code)
	}
}

func TestOpenAPISpecHandler_RequiresAuth(t *testing.T) {
	app := securityExposureApp(t)
	spec := openapipkg.EntityOpenAPI(app.Registry, "Test App", "1.0.0")

	rec := httptest.NewRecorder()
	coreoa.Handler(spec).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [openapi] unauthenticated spec returned %d. Attack: machine-readable API disclosure without auth.", rec.Code)
	}
}

func TestSwaggerUIHandler_RequiresAuth(t *testing.T) {
	app := securityExposureApp(t)
	spec := openapipkg.EntityOpenAPI(app.Registry, "Test App", "1.0.0")

	rec := httptest.NewRecorder()
	coreoa.SwaggerUIHandler(spec, "/api/docs").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [openapi] unauthenticated swagger UI returned %d. Attack: interactive API docs exposed without auth.", rec.Code)
	}
}

func TestOpenAPIRoute_SetsNoStore(t *testing.T) {
	app, cleanup := startSecurityExposureApp(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("SECURITY: [openapi] /openapi.json missing Cache-Control: no-store. Attack: shared-proxy/browser caching of machine-readable schema docs.")
	}
}

func TestSwaggerUIRoute_SetsNoStore(t *testing.T) {
	app, cleanup := startSecurityExposureApp(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/docs/", nil))
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("SECURITY: [openapi] /api/docs/ missing Cache-Control: no-store. Attack: shared-proxy/browser caching of interactive API documentation.")
	}
}

func TestRegistryLLMMDRoute_SetsNoStore(t *testing.T) {
	app, cleanup := startSecurityExposureApp(t)
	defer cleanup()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/llm.md", nil))
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("SECURITY: [llmmd] /api/llm.md missing Cache-Control: no-store. Attack: shared-proxy/browser caching of LLM schema docs.")
	}
}

func TestDebugStatsEndpoint_DoesNotExposeProcessInternals(t *testing.T) {
	app := NewApp()
	app.registerDebugEndpoints()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.debug/stats", nil))
	if rec.Code == http.StatusOK {
		var payload map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["pid"] != nil || payload["goVersion"] != nil {
			t.Fatalf("SECURITY: [debug] debug stats exposed pid/goVersion to unauthenticated caller: %v", payload)
		}
	}
}

func TestDebugStatsEndpoint_SetsNoStore(t *testing.T) {
	app := NewApp()
	app.registerDebugEndpoints()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.debug/stats", nil))
	if !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("SECURITY: [debug] /.debug/stats missing Cache-Control: no-store. Attack: shared-proxy/browser caching of runtime/process diagnostics.")
	}
}

func TestStart_SetsHTTPServerTimeouts(t *testing.T) {
	app := NewApp()
	done := make(chan error, 1)
	go func() {
		done <- app.Start("127.0.0.1:0")
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		app.serverMu.Lock()
		srv := app.server
		app.serverMu.Unlock()
		if srv != nil {
			if srv.ReadHeaderTimeout == 0 || srv.ReadTimeout == 0 || srv.WriteTimeout == 0 || srv.IdleTimeout == 0 {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = app.Shutdown(shutdownCtx)
				<-done
				t.Fatalf("SECURITY: [server] http.Server started with zero timeouts: readHeader=%s read=%s write=%s idle=%s", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = app.Shutdown(shutdownCtx)
			<-done
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for app server to start")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
