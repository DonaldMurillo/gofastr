package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A Bool field takes a number only when it is exactly 0 or 1. The float64
// a literal rounds to is not enough: 1.0000000000000001 is exactly 1.0,
// and 1e-400 underflows to 0.
func TestBoolRefusesRoundedNumbers(t *testing.T) {
	w := newWireRig(t)
	for _, v := range []string{`1.0000000000000001`, `0.99999999999999999`, `1e-400`, `-1e-400`} {
		if code, body := w.post(`{"enabled":` + v + `}`); code != http.StatusBadRequest {
			t.Errorf("enabled %s: status %d, want 400: %s", v, code, body)
		}
	}
	for _, v := range []string{`1e0`, `10e-1`, `0e5`, `0.0`} {
		if code, body := w.post(`{"enabled":` + v + `}`); code != http.StatusCreated {
			t.Errorf("enabled %s: status %d, want 201: %s", v, code, body)
		}
	}
}

// A Bool field's text default is stored as the boolean, the way a body's
// "true" is: SQLite kept the default as text, which ?enabled=true (bound
// as 1) never matched.
func TestBoolTextDefaultIsBoolean(t *testing.T) {
	installSecurityOwnerExtractor(t)
	db := setupDB(t, `CREATE TABLE flags (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT, enabled BOOLEAN)`)
	ent := entity.Define("flags", makeEntityConfig("flags", "flags", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String},
		{Name: "name", Type: schema.String},
		{Name: "enabled", Type: schema.Bool, Default: "true"},
	}))
	ent.SetDB(db)
	r := router.New()
	RegisterCrudRoutes(r, NewCrudHandler(ent, db).WithJSONCase(CaseSnake), "/flags")
	serve := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body)).
			WithContext(handler.SetUser(context.Background(), &testUser{id: "alice"}))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	if rec := serve(http.MethodPost, "/flags", `{"name":"defaulted"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var stored string
	if err := db.QueryRow(`SELECT typeof(enabled) FROM flags`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "integer" {
		t.Errorf("default stored as %s, want integer", stored)
	}
	if rec := serve(http.MethodGet, "/flags?enabled=true", ""); !strings.Contains(rec.Body.String(), `"name":"defaulted"`) {
		t.Errorf("?enabled=true misses the defaulted row: %d %s", rec.Code, rec.Body.String())
	}
}

// A batch write takes the JSON-field number a single write takes: the
// envelope decode read every item number as a float64, so 1e400 failed
// the whole batch as invalid JSON.
func TestBatchKeepsHugeJSONNumbers(t *testing.T) {
	w := newWireRig(t)
	if code, body := w.post(`{"meta":{"a":[1e400]}}`); code != http.StatusCreated {
		t.Fatalf("single: status %d: %s", code, body)
	}
	code, body := w.postTo("/wire/_batch", `{"items":[{"meta":{"a":[1e400]}},{"meta":{"n":9007199254740993}}]}`)
	if code >= 300 || !strings.Contains(body, `1e400`) || !strings.Contains(body, `9007199254740993`) {
		t.Fatalf("batch: status %d: %s", code, body)
	}
	// A batch update stores a JSON field's numbers as a single write
	// does: below 2^53 an exponent spelling is the integer it names.
	var created struct {
		Results []struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || len(created.Results) == 0 {
		t.Fatalf("batch create body: %v: %s", err, body)
	}
	req := httptest.NewRequest(http.MethodPatch, "/wire/_batch",
		strings.NewReader(`{"items":[{"id":"`+created.Results[0].Data.ID+`","meta":{"n":5e0}}]}`)).
		WithContext(handler.SetUser(context.Background(), &testUser{id: "alice"}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	w.r.ServeHTTP(rec, req)
	if rec.Code >= 300 {
		t.Fatalf("batch update: status %d: %s", rec.Code, rec.Body.String())
	}
	// The stored text, not the read: a read normalizes the numbers again.
	var stored string
	if err := w.ch.DB.QueryRowContext(context.Background(), `SELECT meta FROM wire WHERE id = ?`, created.Results[0].Data.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != `{"n":5}` {
		t.Fatalf(`batch update stored %s, want {"n":5}`, stored)
	}
}
