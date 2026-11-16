package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// The feed fails closed on its hook-scope check: a Before hook that errors,
// or a record that names no row, withholds the event instead of sending it
// unchecked.
func TestEventHookScopeFailsClosed(t *testing.T) {
	ddl := `CREATE TABLE posts (id TEXT PRIMARY KEY, status TEXT);`
	cfg := makeEntityConfig("posts", "posts", "", []schema.Field{{Name: "status", Type: schema.String}})
	cfg.Exposure = &entity.ExposureConfig{Public: true}
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "posts", []map[string]any{{"id": "p1", "status": "published"}})
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/posts/_events", UserID: "reader"})
	ev := func(rec map[string]any) event.Event {
		return event.Event{Type: event.EntityUpdated, Data: map[string]any{eventKeyEntity: "posts", eventKeyRecord: rec}}
	}

	failing := hook.NewHookRegistry()
	failing.RegisterHook(hook.BeforeList, func(context.Context, any) error { return errors.New("scope store down") })
	ch.Hooks = failing
	if !ch.eventHookScopeHides(req, ev(map[string]any{"id": "p1", "status": "published"})) {
		t.Error("a BeforeList hook error let the event through")
	}

	ch.Hooks = editorScopeHooks()
	if !ch.eventHookScopeHides(req, ev(map[string]any{"status": "published"})) {
		t.Error("a record with no key was delivered without a scope check")
	}
	if ch.eventHookScopeHides(req, ev(map[string]any{"id": "p1", "status": "published"})) {
		t.Error("a published row the hooks allow was withheld")
	}
}

// The idle re-check floor holds for a handler whose field was set directly,
// not only through WithEventStreamReauth.
func TestEventStreamReauthFloorOnField(t *testing.T) {
	ch := &CrudHandler{EventStreamReauth: 500 * time.Millisecond}
	if got := ch.eventStreamReauthInterval(); got != 30*time.Second {
		t.Fatalf("sub-second field = %v, want the 30s default", got)
	}
}

// A multi-tenant entity's feed refuses a caller with no tenant: the
// per-event tenant filter would otherwise match every tenant's writes.
func TestEventStreamRequiresTenant(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, tenantItemsConfig(), tenantItemsDDL)
	ch.Events = event.NewEventBus()
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items/_events", UserID: "u1"})
	ctx, cancel := context.WithTimeout(req.Context(), 300*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	ch.EventStream()(rec, req.WithContext(ctx))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stream with no tenant = %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
}
