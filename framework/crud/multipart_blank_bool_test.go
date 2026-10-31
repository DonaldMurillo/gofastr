package crud

import (
	"bytes"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// blankBoolHandler is flags with an optional Bool, a Bool defaulting to
// true and a required one. f1 holds NULL in opt, f2 holds true.
func blankBoolHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE flags (id TEXT PRIMARY KEY, name TEXT, opt BOOLEAN, dflt BOOLEAN, must BOOLEAN)`)
	ent := entity.Define("flags", entity.EntityConfig{
		Table: "flags",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "name", Type: schema.String},
			{Name: "opt", Type: schema.Bool},
			{Name: "dflt", Type: schema.Bool, Default: true},
			{Name: "must", Type: schema.Bool, Required: true},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	seedRows(t, db, "flags", []map[string]any{
		{"id": "f1", "name": "a", "opt": nil, "dflt": true, "must": true},
		{"id": "f2", "name": "b", "opt": true, "dflt": true, "must": true},
	})
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

// blankBoolRequest sends fields as a multipart form or as JSON, with ""
// for each blank control.
func blankBoolRequest(t *testing.T, method, path string, multi bool, fields [][2]string) *http.Request {
	t.Helper()
	if !multi {
		parts := make([]string, len(fields))
		for i, f := range fields {
			parts[i] = `"` + f[0] + `":"` + f[1] + `"`
		}
		r := httptest.NewRequest(method, path, strings.NewReader("{"+strings.Join(parts, ",")+"}"))
		r.Header.Set("Content-Type", "application/json")
		return r
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

// A blank Bool control on a multipart form means what it means on JSON:
// not provided. The multipart parse coerced "" to false first, so saving
// any record through a form with an upload field wrote false into every
// unset optional Bool, and a required Bool left blank passed as false
// (2026-10-27 D19). The entity form draws a stored NULL as a select whose
// selected empty choice submits "".
func TestMultipartBlankBoolIsNotFalse(t *testing.T) {
	for _, multi := range []bool{true, false} {
		enc := map[bool]string{true: "multipart", false: "json"}[multi]
		t.Run(enc+" update keeps null", func(t *testing.T) {
			ch, db := blankBoolHandler(t)
			r := blankBoolRequest(t, http.MethodPatch, "/flags/f1", multi, [][2]string{{"name", "x"}, {"opt", ""}})
			r.SetPathValue("id", "f1")
			rec := httptest.NewRecorder()
			ch.Update()(rec, r)
			if rec.Code != http.StatusOK {
				t.Fatalf("PATCH = %d %s", rec.Code, rec.Body.String())
			}
			assertOpt(t, db, "f1", sql.NullBool{})
		})
		t.Run(enc+" update clears", func(t *testing.T) {
			ch, db := blankBoolHandler(t)
			r := blankBoolRequest(t, http.MethodPatch, "/flags/f2", multi, [][2]string{{"opt", ""}})
			r.SetPathValue("id", "f2")
			rec := httptest.NewRecorder()
			ch.Update()(rec, r)
			if rec.Code != http.StatusOK {
				t.Fatalf("PATCH = %d %s", rec.Code, rec.Body.String())
			}
			assertOpt(t, db, "f2", sql.NullBool{})
		})
		t.Run(enc+" create applies defaults", func(t *testing.T) {
			ch, db := blankBoolHandler(t)
			r := blankBoolRequest(t, http.MethodPost, "/flags", multi, [][2]string{{"id", "f3"}, {"opt", ""}, {"dflt", ""}, {"must", "true"}})
			rec := httptest.NewRecorder()
			ch.Create()(rec, r)
			if rec.Code != http.StatusCreated {
				t.Fatalf("POST = %d %s", rec.Code, rec.Body.String())
			}
			assertOpt(t, db, "f3", sql.NullBool{})
			var dflt sql.NullBool
			if err := db.QueryRow(`SELECT dflt FROM flags WHERE id = 'f3'`).Scan(&dflt); err != nil {
				t.Fatal(err)
			}
			if dflt != (sql.NullBool{Bool: true, Valid: true}) {
				t.Errorf("dflt = %+v after a blank control, want its default true", dflt)
			}
		})
		t.Run(enc+" create refuses blank required", func(t *testing.T) {
			ch, db := blankBoolHandler(t)
			r := blankBoolRequest(t, http.MethodPost, "/flags", multi, [][2]string{{"id", "f4"}, {"must", ""}})
			rec := httptest.NewRecorder()
			ch.Create()(rec, r)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST with a blank required Bool = %d %s, want 400", rec.Code, rec.Body.String())
			}
			var n int
			if err := db.QueryRow(`SELECT count(*) FROM flags WHERE id = 'f4'`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Error("the refused create stored a row")
			}
		})
	}
}

func assertOpt(t *testing.T, db *sql.DB, id string, want sql.NullBool) {
	t.Helper()
	var got sql.NullBool
	if err := db.QueryRow(`SELECT opt FROM flags WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("opt of %s = %+v, want %+v", id, got, want)
	}
}
