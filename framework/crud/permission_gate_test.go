package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// setupPermissionedHandler builds a CrudHandler over a "docs" table whose
// EntityConfig declares per-operation RBAC permissions.
func setupPermissionedHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE docs (id TEXT PRIMARY KEY, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("docs", entity.EntityConfig{
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
		Access: entity.AccessControl{
			Read:   "docs:read",
			Create: "docs:write",
			Update: "docs:write",
			Delete: "docs:delete",
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// grantReq returns a request whose context carries a RolePolicy granting the
// given permissions to the role "member", with the user holding that role —
// the shape access.Middleware would install.
func grantReq(r *http.Request, perms ...access.Permission) *http.Request {
	policy := access.NewRolePolicy()
	policy.Grant("member", perms...)
	ctx := access.WithPolicy(r.Context(), policy)
	ctx = access.WithRoles(ctx, []string{"member"})
	return r.WithContext(ctx)
}

// TestPermissionGate_ListDeniedWithoutPermission pins the secure behaviour:
// an entity declaring Access.Read refuses a request whose context lacks the
// permission with 403. Before B2 there was no permission check in auto-CRUD at
// all — every authenticated user got full CRUD.
func TestPermissionGate_ListDeniedWithoutPermission(t *testing.T) {
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('d1','secret')`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/docs", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("List without permission = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("denied List leaked the row: %s", rec.Body.String())
	}
}

func TestPermissionGate_ListAllowedWithPermission(t *testing.T) {
	ch, db := setupPermissionedHandler(t)
	if _, err := db.Exec(`INSERT INTO docs (id, body) VALUES ('d1','hello')`); err != nil {
		t.Fatal(err)
	}

	req := grantReq(httptest.NewRequest(http.MethodGet, "/api/docs", nil), "docs:read")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("List with docs:read = %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
}

// TestPermissionGate_WrongPermissionDenied confirms a permission for a
// different op does not unlock writes: docs:read does not authorize Create.
func TestPermissionGate_WrongPermissionDenied(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)

	req := grantReq(httptest.NewRequest(http.MethodPost, "/api/docs",
		strings.NewReader(`{"body":"x"}`)), "docs:read")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("Create with only docs:read = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
}

func TestPermissionGate_CreateAllowedWithWrite(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)

	req := grantReq(httptest.NewRequest(http.MethodPost, "/api/docs",
		strings.NewReader(`{"body":"x"}`)), "docs:write")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Create with docs:write = %d, want 201. body=%s", rec.Code, rec.Body.String())
	}
}

// TestPermissionGate_EventsDeniedNoPerm pins the SSE RBAC fix: the live
// _events feed is a read surface and must enforce Access.Read like List/Get.
// Without the gate, a user lacking docs:read gets 403 on GET /docs but a live
// stream of all writes on /docs/_events. Uses an already-cancelled context so
// that if the gate were bypassed the SSE loop exits immediately (no hang)
// rather than the test passing by timeout.
func TestPermissionGate_EventsDeniedNoPerm(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)
	ch.Events = event.NewEventBus()

	// Authenticated (clears the baseline auth check) but WITHOUT docs:read, so
	// the request reaches the permission gate. Cancelled ctx so a bypassed
	// gate exits the SSE loop immediately rather than hanging the test.
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/api/docs/_events", nil), "u1")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	rec := httptest.NewRecorder()
	ch.EventStream()(rec, req.WithContext(ctx))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("_events as authenticated-no-perm = %d, want 403. body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "subscribed") {
		t.Errorf("denied _events still opened the stream: %s", rec.Body.String())
	}
}

// TestPermissionGate_EventsAllowedWithPerm confirms the gate lets a permitted
// reader through (stream opens; cancelled ctx ends it promptly).
func TestPermissionGate_EventsAllowedWithPerm(t *testing.T) {
	ch, _ := setupPermissionedHandler(t)
	ch.Events = event.NewEventBus()

	// Authenticated AND holding docs:read. grantReq installs policy+roles;
	// withTestUser adds the handler user for the baseline check.
	req := grantReq(withTestUser(httptest.NewRequest(http.MethodGet, "/api/docs/_events", nil), "u1"), "docs:read")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	rec := httptest.NewRecorder()
	ch.EventStream()(rec, req.WithContext(ctx))

	if rec.Code == http.StatusForbidden {
		t.Fatalf("_events with docs:read = 403, want stream opened. body=%s", rec.Body.String())
	}
}
