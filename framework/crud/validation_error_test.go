package crud_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// newValidationTestHandler builds a bare CrudHandler (no OwnerField, no
// multi-tenancy) over an in-memory SQLite table, so an external-package
// test can exercise CreateOne / Create() without wiring owner/tenant
// extractors.
func newValidationTestHandler(t *testing.T, fields []schema.Field, ddl string) (*crud.CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("exec DDL: %v", err)
	}
	ent := entity.Define("widgets", entity.EntityConfig{
		Table:  "widgets",
		Fields: fields,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := crud.NewCrudHandler(ent, db).WithJSONCase(crud.CaseSnake)
	return ch, db
}

// TestValidationErrorMatchesErrorsAs drives the in-process CreateOne with
// an invalid body and asserts the returned error unwraps to
// *crud.ValidationError via errors.As — the contract host code relies on
// to branch on validation failures.
func TestValidationErrorMatchesErrorsAs(t *testing.T) {
	ch, _ := newValidationTestHandler(t,
		[]schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "qty", Type: schema.Int, Required: true},
		},
		`CREATE TABLE widgets (id TEXT PRIMARY KEY, title TEXT NOT NULL, qty INTEGER NOT NULL)`,
	)

	// Missing both required fields → validation error.
	_, err := ch.CreateOne(context.Background(), map[string]any{})

	var ve *crud.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("errors.As(err, *crud.ValidationError) = false; err=%v", err)
	}
}

// TestValidationErrorFieldsPerField asserts Fields() exposes one entry per
// offending field (not a single flattened message), so callers can map
// errors back to form inputs.
func TestValidationErrorFieldsPerField(t *testing.T) {
	ch, _ := newValidationTestHandler(t,
		[]schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "qty", Type: schema.Int, Required: true},
		},
		`CREATE TABLE widgets (id TEXT PRIMARY KEY, title TEXT NOT NULL, qty INTEGER NOT NULL)`,
	)

	_, err := ch.CreateOne(context.Background(), map[string]any{})
	var ve *crud.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("errors.As: %v", err)
	}
	fields := ve.Fields()
	if _, ok := fields["title"]; !ok {
		t.Errorf("Fields() missing 'title' entry; got %v", fields)
	}
	if _, ok := fields["qty"]; !ok {
		t.Errorf("Fields() missing 'qty' entry; got %v", fields)
	}
}

// TestValidationWireShapeUnchanged locks down the exact 400 response body
// the HTTP Create path emits for a validation failure — three keys
// (error, success, fields) and the canonical "validation failed" string.
func TestValidationWireShapeUnchanged(t *testing.T) {
	ch, _ := newValidationTestHandler(t,
		[]schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
		`CREATE TABLE widgets (id TEXT PRIMARY KEY, title TEXT NOT NULL)`,
	)

	body := strings.NewReader(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/widgets", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body=%s", err, rr.Body.String())
	}
	if resp["error"] != "validation failed" {
		t.Errorf("error = %v, want %q", resp["error"], "validation failed")
	}
	if success, ok := resp["success"]; !ok || success != false {
		t.Errorf("success = %v (present=%v), want key 'success': false", success, ok)
	}
	fields, ok := resp["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields key missing or wrong type: %v", resp["fields"])
	}
	if _, ok := fields["title"]; !ok {
		t.Errorf("response fields missing 'title'; got %v", fields)
	}
}
