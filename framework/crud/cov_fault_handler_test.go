package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// covFaultNotes builds a simple notes handler over a fault-injectable DB.
func covFaultNotes(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := covSetupFaultDB(t, `CREATE TABLE notes (id TEXT PRIMARY KEY, title TEXT)`)
	seedRows(t, db, "notes", []map[string]any{{"id": "n1", "title": "a"}, {"id": "n2", "title": "b"}})
	ent := entity.Define("notes", entity.EntityConfig{
		Name: "notes", Table: "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// --- crud.go List ---

func TestList_DataQueryErr(t *testing.T) {
	ch, _ := covFaultNotes(t)
	// COUNT(*) runs first and must succeed; only the data SELECT fails.
	covFault.set(func(c *covFaults) { c.queryErrOn = "title" })
	req := withTestUser(httptest.NewRequest("GET", "/notes", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("list data query err = %d, want 500", rec.Code)
	}
}

// --- crud_stream.go data query error ---

func TestStream_DataQueryErr(t *testing.T) {
	ch, _ := covFaultNotes(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "title" })
	req := withTestUser(httptest.NewRequest("GET", "/notes?stream=true", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("stream data query err = %d, want 500", rec.Code)
	}
}

// --- crud_ops.go doDelete soft-delete Exec error ---

func covFaultSoftNotes(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := covSetupFaultDB(t, `CREATE TABLE snotes (id TEXT PRIMARY KEY, title TEXT, deleted_at TEXT)`)
	seedRows(t, db, "snotes", []map[string]any{{"id": "n1", "title": "a"}})
	ent := entity.Define("snotes", entity.EntityConfig{
		Name: "snotes", Table: "snotes", SoftDelete: true,
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

func TestSoftDelete_ExecErr(t *testing.T) {
	ch, _ := covFaultSoftNotes(t)
	covFault.set(func(c *covFaults) { c.execErrOn = "UPDATE" })
	err := ch.doDelete(context.Background(), httptest.NewRequest("DELETE", "/", nil), "n1")
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("soft delete exec err = %v, want injected", err)
	}
}

// --- typed_query.go DeleteAll soft-delete Exec error ---

func TestTypedDeleteAll_SoftExecErr(t *testing.T) {
	ch, _ := covFaultSoftNotes(t)
	covFault.set(func(c *covFaults) { c.execErrOn = "UPDATE" })
	type snote struct {
		ID string `json:"id"`
	}
	_, err := NewTypedQuery[snote](ch).DeleteAll(context.Background())
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("typed DeleteAll soft exec err = %v, want injected", err)
	}
}

// --- crud_upsert.go upsertPreflight query error ---

func TestUpsertPreflight_QueryErr(t *testing.T) {
	ch, _ := covFaultSoftNotes(t)
	covFault.set(func(c *covFaults) { c.queryErrOn = "deleted_at" })
	err := ch.upsertPreflight(context.Background(), map[string]any{"id": "n1", "title": "x"})
	if !errors.Is(err, errCovInjected) {
		t.Fatalf("upsert preflight query err = %v, want injected", err)
	}
}
