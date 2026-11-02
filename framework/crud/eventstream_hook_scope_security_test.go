package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// editorScopeHooks is the documented "Hide drafts from non-editors" read
// scope (hooks-and-transactions.md), on both read chains: every caller but
// the user "editor" reads published rows only.
func editorScopeHooks() *hook.HookRegistry {
	isEditor := func(r *http.Request) bool {
		u, _ := handler.GetUser(r.Context())
		tu, _ := u.(*testUser)
		return tu != nil && tu.id == "editor"
	}
	hooks := hook.NewHookRegistry()
	hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		if p := data.(*hook.ListPayload); !isEditor(p.Request) {
			p.AddWhere("status = $1", "published")
		}
		return nil
	})
	hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		if p := data.(*hook.GetPayload); !isEditor(p.Request) {
			p.AddWhere("status = $1", "published")
		}
		return nil
	})
	return hooks
}

// subscribe opens ch's _events stream for user and returns a stop func that
// closes it and hands back everything it wrote.
func subscribeEvents(t *testing.T, ch *CrudHandler, user string) func() string {
	t.Helper()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts/_events", UserID: user})
	sctx, cancel := context.WithCancel(req.Context())
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { ch.EventStream()(rec, req.WithContext(sctx)); close(done) }()
	time.Sleep(50 * time.Millisecond)
	return func() string {
		cancel()
		<-done
		return rec.Body.String()
	}
}

// The _events feed honoured only the declared ReadScope, so a caller a
// BeforeList/BeforeGet hook confines to published rows received the full
// record of every draft the moment it was written, and the id of every
// hidden row deleted. List, Get, ?include= and the write responses all
// honour that hook (2026-11-01 bug bash). Delivery now runs the
// subscriber's own Before chains against the stored row.
func TestEventStreamHonoursBeforeHookScope(t *testing.T) {
	ddl := `CREATE TABLE posts (id TEXT PRIMARY KEY, status TEXT, body TEXT);`
	cfg := makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "body", Type: schema.String},
	})
	cfg.Exposure = &entity.ExposureConfig{Public: true}
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	ch.Events = event.NewEventBus()
	ch.Hooks = editorScopeHooks()
	seedRows(t, db, "posts", []map[string]any{
		{"id": "draft-1", "status": "draft", "body": "secret draft"},
		{"id": "pub-1", "status": "published", "body": "visible post"},
	})

	// Precondition: the list route hides the draft from a reader.
	lr := httptest.NewRecorder()
	ch.List()(lr, makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts", UserID: "reader"}))
	if strings.Contains(lr.Body.String(), "secret draft") || !strings.Contains(lr.Body.String(), "visible post") {
		t.Fatalf("precondition: list = %s", lr.Body.String())
	}

	reader := subscribeEvents(t, ch, "reader")
	editor := subscribeEvents(t, ch, "editor")
	ch.EmitEvent(context.Background(), event.EntityUpdated,
		map[string]any{"id": "draft-1", "status": "draft", "body": "secret draft"})
	ch.EmitEvent(context.Background(), event.EntityUpdated,
		map[string]any{"id": "pub-1", "status": "published", "body": "visible post"})
	// A hard delete leaves no row to check: a restricted subscriber is not
	// told, an unrestricted one is.
	ch.EmitEvent(context.Background(), event.EntityDeleted, map[string]any{"id": "gone-hidden"})
	time.Sleep(200 * time.Millisecond)
	readerBody, editorBody := reader(), editor()

	if strings.Contains(readerBody, "secret draft") {
		t.Errorf("SECURITY: _events delivered a row the BeforeList/BeforeGet scope hides:\n%s", readerBody)
	}
	if strings.Contains(readerBody, "gone-hidden") {
		t.Errorf("SECURITY: _events told a restricted subscriber a row it cannot know of was deleted:\n%s", readerBody)
	}
	if !strings.Contains(readerBody, "visible post") {
		t.Errorf("the published row was withheld from the reader:\n%s", readerBody)
	}
	for _, want := range []string{"secret draft", "visible post", "gone-hidden"} {
		if !strings.Contains(editorBody, want) {
			t.Errorf("the editor, whom the hooks do not restrict, missed %q:\n%s", want, editorBody)
		}
	}
}

// A soft delete leaves the row in place, so its delete event is checked
// against the stored row like any other: the reader hears of the published
// row's delete, not of the draft's.
func TestEventStreamSoftDeleteHonoursBeforeHookScope(t *testing.T) {
	ddl := `CREATE TABLE posts (id TEXT PRIMARY KEY, status TEXT, body TEXT, deleted_at TIMESTAMP);`
	cfg := makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "body", Type: schema.String},
	})
	cfg.Exposure = &entity.ExposureConfig{Public: true}
	cfg.Scope.SoftDelete = true
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	ch.Events = event.NewEventBus()
	ch.Hooks = editorScopeHooks()
	seedRows(t, db, "posts", []map[string]any{
		{"id": "draft-1", "status": "draft", "body": "d", "deleted_at": "2026-11-01 00:00:00"},
		{"id": "pub-1", "status": "published", "body": "p", "deleted_at": "2026-11-01 00:00:00"},
	})

	reader := subscribeEvents(t, ch, "reader")
	ch.EmitEvent(context.Background(), event.EntityDeleted, map[string]any{"id": "draft-1"})
	ch.EmitEvent(context.Background(), event.EntityDeleted, map[string]any{"id": "pub-1"})
	time.Sleep(200 * time.Millisecond)
	body := reader()
	if strings.Contains(body, "draft-1") {
		t.Errorf("SECURITY: the soft delete of a hook-hidden row reached a restricted subscriber:\n%s", body)
	}
	if !strings.Contains(body, "pub-1") {
		t.Errorf("the soft delete of a visible row was withheld:\n%s", body)
	}
}
