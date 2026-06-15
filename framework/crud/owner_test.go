package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// testUser implements the minimal user shape the owner extractor cares about.
type testUser struct{ id string }

func (u *testUser) GetID() string { return u.id }

// withTestUser stashes a user id in the request context the way auth
// middleware would.
func withTestUser(r *http.Request, id string) *http.Request {
	return r.WithContext(handler.SetUser(r.Context(), &testUser{id: id}))
}

// setupOwnerScopedHandler creates a CrudHandler over an in-memory sqlite
// "logs" table with an explicit user_id column and OwnerField wired.
func setupOwnerScopedHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE logs (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		notes TEXT
	)`); err != nil {
		t.Fatal(err)
	}

	ent := entity.Define("logs", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "notes", Type: schema.String},
		},
		OwnerField: "user_id",
	}.WithTimestamps(false))
	ent.SetDB(db)

	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	return ch, db
}

// seedRow inserts a row directly via SQL.
func seedRow(t *testing.T, db *sql.DB, id, userID, notes string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO logs (id, user_id, notes) VALUES (?, ?, ?)`,
		id, userID, notes,
	); err != nil {
		t.Fatal(err)
	}
}

// installOwnerExtractor wires framework/owner against a testUser stored on ctx.
func installOwnerExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		raw, ok := handler.GetUser(ctx)
		if !ok || raw == nil {
			return nil, false
		}
		if u, ok := raw.(*testUser); ok {
			return u.GetID(), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

func TestOwnerScope_ListExcludesOtherUsersRows(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice's row")
	seedRow(t, db, "log-b1", "bob", "bob's row")

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	req = withTestUser(req, "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if resp.Total != 1 {
		t.Fatalf("total = %d, want 1", resp.Total)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("data len = %d, want 1", len(resp.Data))
	}
	if resp.Data[0]["user_id"] != "alice" {
		t.Errorf("leaked row: %+v", resp.Data[0])
	}
}

func TestOwnerScope_GetByIdReturns404ForOtherUsersRow(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice secret")

	// Bob requests Alice's row by id — must 404, not 200.
	req := httptest.NewRequest(http.MethodGet, "/api/logs/log-a1", nil)
	req.SetPathValue("id", "log-a1")
	req = withTestUser(req, "bob")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user GET status = %d (want 404). body=%s", rec.Code, rec.Body.String())
	}
}

func TestOwnerScope_CreateAutoStampsOwner(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)

	body := strings.NewReader(`{"notes":"hello world"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/logs", body)
	req.Header.Set("Content-Type", "application/json")
	req = withTestUser(req, "carol")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["user_id"] != "carol" {
		t.Errorf("user_id not auto-stamped: %+v", got)
	}

	// Verify DB
	var uid string
	if err := db.QueryRow("SELECT user_id FROM logs WHERE id = ?", got["id"]).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if uid != "carol" {
		t.Errorf("stored user_id = %q, want carol", uid)
	}
}

// setupHiddenOwnerHandler mirrors setupOwnerScopedHandler but marks the owner
// column Hidden — the blueprint generator synthesizes the owner column this way
// so it never appears in generated forms/tables. The framework still manages it
// (InjectOwner stamps it; ApplyOwnerScope filters on it), so it MUST be
// persisted on create even though it's hidden from the API surface.
func setupHiddenOwnerHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		notes TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("logs", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Hidden: true},
			{Name: "notes", Type: schema.String},
		},
		OwnerField: "user_id",
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// Regression: a Hidden owner column must still be written on create. doCreate
// skips ReadOnly/Hidden fields, but the owner column is framework-managed and
// was silently dropped — leaving every row's owner blank, which made
// owner-scoping match nothing and a seeded admin's rows invisible.
func TestOwnerScope_HiddenOwnerColumnPersistedOnCreate(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupHiddenOwnerHandler(t)

	body := strings.NewReader(`{"notes":"hidden owner row"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/logs", body)
	req.Header.Set("Content-Type", "application/json")
	req = withTestUser(req, "carol")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// Hidden from the response surface...
	if _, leaked := got["user_id"]; leaked {
		t.Errorf("hidden owner column leaked into API response: %+v", got)
	}
	// ...but persisted to the DB so scoping works.
	var uid string
	if err := db.QueryRow("SELECT user_id FROM logs WHERE id = ?", got["id"]).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if uid != "carol" {
		t.Fatalf("stored user_id = %q, want carol — hidden owner column was dropped on insert", uid)
	}

	// And the owner scope now finds the row for its owner, nobody else.
	listReq := withTestUser(httptest.NewRequest(http.MethodGet, "/api/logs", nil), "carol")
	listRec := httptest.NewRecorder()
	ch.List()(listRec, listReq)
	var resp ListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Fatalf("owner sees %d rows, want 1", resp.Total)
	}
	otherReq := withTestUser(httptest.NewRequest(http.MethodGet, "/api/logs", nil), "mallory")
	otherRec := httptest.NewRecorder()
	ch.List()(otherRec, otherReq)
	var otherResp ListResponse
	if err := json.Unmarshal(otherRec.Body.Bytes(), &otherResp); err != nil {
		t.Fatal(err)
	}
	if otherResp.Total != 0 {
		t.Fatalf("non-owner sees %d rows, want 0", otherResp.Total)
	}
}

func TestOwnerScope_UpdateRejectsOtherUsersRow(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice original")

	body := strings.NewReader(`{"notes":"bob's edit"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/logs/log-a1", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "log-a1")
	req = withTestUser(req, "bob")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user UPDATE status = %d (want 404). body=%s", rec.Code, rec.Body.String())
	}

	// Confirm the row is untouched.
	var notes string
	if err := db.QueryRow("SELECT notes FROM logs WHERE id = ?", "log-a1").Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if notes != "alice original" {
		t.Errorf("row mutated by cross-user PUT: %q", notes)
	}
}

// TestOwnerScope_AnonymousCursorListIsRejected covers the cursor
// pagination branch (?cursor=...). RequireOwner fires at the top of
// List() before the cursor branch, so this should 401 like the
// standard list.
func TestOwnerScope_AnonymousCursorListIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice secret")

	req := httptest.NewRequest(http.MethodGet, "/api/logs?cursor=", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous cursor List status = %d (want 401). body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "alice secret") {
		t.Errorf("anonymous cursor List leaked row data: %s", rec.Body.String())
	}
}

// TestOwnerScope_AnonymousStreamListIsRejected covers the streaming
// list branch (?stream=true).
func TestOwnerScope_AnonymousStreamListIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice secret")

	req := httptest.NewRequest(http.MethodGet, "/api/logs?stream=true", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous stream List status = %d (want 401). body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "alice secret") {
		t.Errorf("anonymous stream List leaked row data: %s", rec.Body.String())
	}
}

func TestOwnerScope_AnonymousListIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice's row")
	seedRow(t, db, "log-b1", "bob", "bob's row")

	// No withTestUser call: anonymous request. Previously returned every
	// row (the WHERE clause was silently dropped). Must now 401.
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET status = %d (want 401). body=%s",
			rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "alice") || strings.Contains(rec.Body.String(), "bob") {
		t.Errorf("anonymous response leaks row data: %s", rec.Body.String())
	}
}

func TestOwnerScope_AnonymousGetByIdIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice secret")

	req := httptest.NewRequest(http.MethodGet, "/api/logs/log-a1", nil)
	req.SetPathValue("id", "log-a1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET-by-id status = %d (want 401). body=%s",
			rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "alice secret") {
		t.Errorf("anonymous response leaks row data: %s", rec.Body.String())
	}
}

func TestOwnerScope_AnonymousCreateIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupOwnerScopedHandler(t)

	body := strings.NewReader(`{"notes":"anonymous attempt"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/logs", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous POST status = %d (want 401). body=%s",
			rec.Code, rec.Body.String())
	}
}

func TestOwnerScope_AnonymousUpdateIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice original")

	req := httptest.NewRequest(http.MethodPut, "/api/logs/log-a1",
		strings.NewReader(`{"notes":"hijack"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "log-a1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous PUT status = %d (want 401). body=%s",
			rec.Code, rec.Body.String())
	}
	var notes string
	if err := db.QueryRow("SELECT notes FROM logs WHERE id = ?", "log-a1").Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if notes != "alice original" {
		t.Errorf("anonymous PUT mutated row: %q", notes)
	}
}

func TestOwnerScope_AnonymousDeleteIsRejected(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice's row")

	req := httptest.NewRequest(http.MethodDelete, "/api/logs/log-a1", nil)
	req.SetPathValue("id", "log-a1")
	rec := httptest.NewRecorder()
	ch.Delete()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous DELETE status = %d (want 401). body=%s",
			rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM logs WHERE id = ?", "log-a1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Error("anonymous DELETE removed row")
	}
}

func TestOwnerScope_NoExtractorRegisteredIsRejected(t *testing.T) {
	// Clear the extractor — simulating an app that set OwnerField but
	// never wired auth. The handler must fail closed, not return every
	// row. Properly restore on cleanup so subsequent tests (or
	// shuffled-order runs) don't inherit a nil extractor.
	prev := owner.GetExtractor()
	owner.SetExtractor(nil)
	t.Cleanup(func() { owner.SetExtractor(prev) })

	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice")

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-extractor GET status = %d (want 401, never 200 with rows). body=%s",
			rec.Code, rec.Body.String())
	}
}

func TestOwnerScope_DeleteRejectsOtherUsersRow(t *testing.T) {
	installOwnerExtractor(t)
	ch, db := setupOwnerScopedHandler(t)
	seedRow(t, db, "log-a1", "alice", "alice's row")

	req := httptest.NewRequest(http.MethodDelete, "/api/logs/log-a1", nil)
	req.SetPathValue("id", "log-a1")
	req = withTestUser(req, "bob")
	rec := httptest.NewRecorder()
	ch.Delete()(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user DELETE status = %d (want 404). body=%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM logs WHERE id = ?", "log-a1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("row deleted by cross-user DELETE")
	}
}
