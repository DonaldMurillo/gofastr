package live_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

type previewRoleUser struct{ roles []string }

func (u previewRoleUser) GetRoles() []string { return u.roles }

func TestLiveDocsDebugRoutes(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-docs-debug")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	j := journal.NewMemory()
	config := world.New().App
	config.Name = "Preview docs"
	config.LLMMD = true
	config.DebugEndpoints = true
	entry, err := journal.NewEntry("config", time.Now(), journal.KindWorldEdit, journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: config})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.Append(entry); err != nil {
		t.Fatal(err)
	}
	l, err := live.New(j, func() *framework.App {
		return framework.NewApp(framework.WithDB(d))
	})
	if err != nil {
		t.Fatal(err)
	}
	request := func(ctx context.Context, path string, want int, contains string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		req.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		l.ServeHTTP(rec, req)
		if rec.Code != want || !strings.Contains(rec.Body.String(), contains) {
			t.Errorf("GET %s = %d %s; want %d containing %q", path, rec.Code, rec.Body, want, contains)
		}
	}
	bg := context.Background()
	member := handler.SetUser(bg, previewRoleUser{roles: []string{"member"}})
	admin := handler.SetUser(bg, previewRoleUser{roles: []string{"admin"}})
	request(bg, "/api/llm.md", http.StatusNotFound, "problem_no_route")
	request(bg, "/.debug/stats", http.StatusUnauthorized, "Unauthorized")
	apply := func(id string, op journal.Op, payload any) {
		t.Helper()
		e, err := journal.NewEntry(id, time.Now(), journal.KindWorldEdit, op, payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.Apply(e); err != nil {
			t.Fatal(err)
		}
	}
	apply("entity", journal.OpAddEntity, journal.AddEntityPayload{Entity: &world.Entity{
		Name: "posts", Fields: []world.Field{{Name: "title", Type: "string"}},
	}})
	checkEnabled := func() {
		t.Helper()
		for _, path := range []string{"/api/posts/llm.md", "/api/llm.md"} {
			request(bg, path, http.StatusUnauthorized, "Unauthorized")
			request(member, path, http.StatusOK, "posts")
			request(admin, path, http.StatusOK, "posts")
		}
		for _, path := range []string{"/.debug/stats", "/.debug/goroutineleak"} {
			request(bg, path, http.StatusUnauthorized, "Unauthorized")
			request(member, path, http.StatusForbidden, "Forbidden")
			if path == "/.debug/stats" {
				request(admin, path, http.StatusOK, `"pid"`)
			} else if pprof.Lookup("goroutineleak") == nil {
				request(admin, path, http.StatusNotImplemented, "Not Implemented")
			} else {
				request(admin, path, http.StatusOK, "goroutineleak profile:")
			}
		}
	}
	checkEnabled()
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}
	checkEnabled()
	config.LLMMD = false
	config.DebugEndpoints = false
	apply("off", journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: config})
	request(admin, "/api/posts/llm.md", http.StatusNotFound, "not_found")
	request(admin, "/api/llm.md", http.StatusNotFound, "problem_no_route")
	request(admin, "/.debug/stats", http.StatusNotFound, "problem_no_route")
	config.LLMMD = true
	config.DebugEndpoints = true
	apply("on", journal.OpSetAppConfig, journal.SetAppConfigPayload{Config: config})
	checkEnabled()
}
