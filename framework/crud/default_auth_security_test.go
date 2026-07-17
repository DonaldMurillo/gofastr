package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// setupPlainHandler builds a CrudHandler over a "notes" table whose
// EntityConfig declares NEITHER OwnerField NOR Access — the exact shape
// issue #65 was filed against: a blueprint entity with no `access:` block
// and no per-user scoping.
func setupPlainHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE notes (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// TestAnonymousCreateRejectedByDefault pins issue #65: an entity with no
// OwnerField and no Access block must refuse an anonymous POST — before the
// fix, Create() returned 201 and persisted the row for anyone.
func TestAnonymousCreateRejectedByDefault(t *testing.T) {
	ch, db := setupPlainHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/notes", strings.NewReader(`{"title":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous Create = %d, want 401/403. body=%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notes`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("anonymous Create persisted a row despite the rejection: count=%d", count)
	}
}

// TestAnonymousListRejectedByDefault pins the read half of #65: List is
// also open by default before the fix.
func TestAnonymousListRejectedByDefault(t *testing.T) {
	ch, _ := setupPlainHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/notes", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous List = %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
}

// TestAuthenticatedCreateAllowedByDefault: once a session/user is present
// in context (the way SessionMiddleware installs one), an entity with no
// OwnerField/Access still allows the write — the default gate requires
// authentication, not a specific permission.
func TestAuthenticatedCreateAllowedByDefault(t *testing.T) {
	ch, _ := setupPlainHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/notes", strings.NewReader(`{"title":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("authenticated Create = %d, want 201. body=%s", rec.Code, rec.Body.String())
	}
}

// TestPublicEntityOpensReadAndWrite pins the `Public` opt-out contract:
// it is a full, deliberate opt-out (a public contact form, a blog's
// comments) — every operation is open to anonymous callers, not a
// partial "reads only" relaxation. Entities that want public reads but
// gated writes use a declared Access block instead (blank Access.Read +
// a real Access.Create permission).
func TestPublicEntityOpensReadAndWrite(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Public: true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	listReq := httptest.NewRequest(http.MethodGet, "/api/posts", nil)
	listRec := httptest.NewRecorder()
	ch.List()(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("anonymous List on public entity = %d, want 200. body=%s", listRec.Code, listRec.Body.String())
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/posts", strings.NewReader(`{"title":"hi"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	ch.Create()(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("anonymous Create on public entity = %d, want 201. body=%s", createRec.Code, createRec.Body.String())
	}
}

// TestOwnerFieldUnaffectedByDefaultGate: entities that already declare
// OwnerField keep their existing contract untouched — RequireOwner alone
// governs them, same as before #65.
func TestOwnerFieldUnaffectedByDefaultGate(t *testing.T) {
	ch := setupOwnerCreateInProcHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/notes", strings.NewReader(`{"title":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous Create on OwnerField entity = %d, want 401 (unchanged contract). body=%s", rec.Code, rec.Body.String())
	}
}

// TestAccessEntityUnaffectedByDefaultGate: entities that already declare an
// Access block keep RBAC as the sole gate — no additional baseline-session
// requirement layered on top (matches "as today").
func TestAccessEntityUnaffectedByDefaultGate(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)

	// No session, no policy at all — must still be refused (fail-closed),
	// but by the existing permission gate, not a new baseline check with a
	// different error shape.
	req := httptest.NewRequest(http.MethodPost, "/api/docs", strings.NewReader(`{"body":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("anonymous Create on Access-declared entity = %d, want 403 (RBAC gate). body=%s", rec.Code, rec.Body.String())
	}
}

// TestSSEStricterThanBlankReadAccess pins the EventStream baseline: a
// declared Access block with a BLANK Read permission means "public static
// reads", but the live feed must still demand a session — otherwise an
// anonymous subscriber turns the public list endpoint into a real-time
// scrape of every write. Only Public (the full opt-out) opens the stream.
func TestSSEStricterThanBlankReadAccess(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("sse_posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Access: entity.AccessControl{Create: "posts:create"}, // Read stays blank: public static reads
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Events = event.NewEventBus()

	// Bounded context so a missing gate fails via ctx deadline instead of
	// hanging in the SSE loop (same pattern as TestEventStream_AnonymousIsRejected).
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/posts/_events", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	ch.EventStream()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous SSE on blank-Read Access entity = %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
}
