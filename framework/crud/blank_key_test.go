package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// refuseBlankKeyParts refused "" only in a column of a key spanning
// several, because such a row "could never be named again". A one-column
// caller-chosen key is no different: the record routes answer 400 for an
// empty {id}, so POST stored a row under "" that GET, PUT and DELETE could
// not reach (2026-11-01 bug bash). Every write path now refuses it.
func TestCreateRefusesBlankSingleColumnKey(t *testing.T) {
	db := setupDB(t, `CREATE TABLE codes (code TEXT PRIMARY KEY, label TEXT)`)
	ent := entity.Define("codes", entity.EntityConfig{
		Table:      "codes",
		PrimaryKey: []string{"code"},
		Fields: []schema.Field{
			{Name: "code", Type: schema.String},
			{Name: "label", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	post := func(path, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h(rec, r)
		return rec
	}

	if rec := post("/codes", `{"code":"","label":"orphan"}`, ch.Create()); rec.Code != http.StatusBadRequest {
		t.Errorf("POST with code \"\" = %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec := post("/codes/_batch", `{"items":[{"code":"","label":"orphan"}]}`, ch.BatchCreate()); rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Errorf("batch create with code \"\" = %d %s, want a refusal", rec.Code, rec.Body.String())
	}
	var ve *ValidationError
	if _, err := ch.CreateOne(context.Background(), map[string]any{"code": "", "label": "orphan"}); !errors.As(err, &ve) {
		t.Errorf("CreateOne with code \"\" = %v, want a ValidationError", err)
	}
	if _, err := ch.UpsertOne(context.Background(), map[string]any{"code": "", "label": "orphan"}); !errors.As(err, &ve) {
		t.Errorf("UpsertOne with code \"\" = %v, want a ValidationError", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codes WHERE code = ''`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows stored under the empty key = %d (%v), want 0", n, err)
	}

	// A named key still writes.
	if rec := post("/codes", `{"code":"a","label":"ok"}`, ch.Create()); rec.Code != http.StatusCreated {
		t.Errorf("POST with code \"a\" = %d %s, want 201", rec.Code, rec.Body.String())
	}
}
