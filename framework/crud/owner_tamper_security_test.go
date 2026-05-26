package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestUpdate_OwnerIDTamperRejected verifies that including the OwnerField
// in an update body cannot reassign the row to another user. The owner
// scope pins the WHERE to the caller, but if the framework also allowed
// the owner field in the SET clause, an authenticated user could hand
// their own row to anyone by patching `user_id`.
func TestUpdate_OwnerIDTamperRejected(t *testing.T) {
	t.Parallel()
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n1", "user_id": "alice", "title": "alice note"},
	})

	body := `{"user_id":"bob","title":"updated"}`
	req := makeRequest(t, RequestOpts{
		Method: http.MethodPut,
		Path:   "/notes/n1",
		Body:   body,
		UserID: "alice",
	})
	req.SetPathValue("id", "n1")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)

	if rr.Code != http.StatusOK && rr.Code != http.StatusNoContent {
		t.Fatalf("update failed: %d %s", rr.Code, rr.Body.String())
	}
	var stored string
	if err := db.QueryRow("SELECT user_id FROM notes WHERE id = ?", "n1").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "alice" {
		t.Errorf("SECURITY: [crud-owner-tamper] owner_id changed from %q to %q via PUT body. Attack: transfer-by-tamper.", "alice", stored)
	}
}

// TestUpdate_InternalErrorNotLeaked verifies that an internal DB error
// surfaces as a generic 500 message rather than echoing driver-specific
// text ("UNIQUE constraint failed", "no such table", "pq: ...").
func TestUpdate_InternalErrorNotLeaked(t *testing.T) {
	t.Parallel()
	cfg := makeEntityConfig("things", "things", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	// Intentionally point at a non-existent table to force a 500 from
	// the database layer.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, _ = db.Exec("CREATE TABLE things_real (id TEXT PRIMARY KEY)") // not what entity points at
	ent := entity.Define(cfg.Table, cfg)
	ent.SetDB(db)
	installSecurityOwnerExtractor(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	body := `{"title":"x"}`
	req := makeRequest(t, RequestOpts{
		Method: http.MethodPut,
		Path:   "/things/n1",
		Body:   body,
		UserID: "alice",
	})
	req.SetPathValue("id", "n1")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rr.Code, rr.Body.String())
	}
	body = rr.Body.String()
	for _, leak := range []string{"no such table", "things", "sqlite", "syntax"} {
		if strings.Contains(body, leak) {
			t.Errorf("SECURITY: [crud-error-leak] response body contains %q: %s", leak, body)
		}
	}
}

// silence unused import warnings when none of the helpers are touched.
var _ = context.Background
