package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type formHookRow struct {
	Title string   `json:"title"`
	Count *int64   `json:"count"`
	Done  bool     `json:"done"`
	Ratio *float64 `json:"ratio"`
}

// The runtime posts a form as JSON with every control a string. A typed
// Before hook decoded that body into its struct before the blank and
// number cleanup ran, so a blank optional int, a filled "2" and a
// checkbox's "true" each failed with a 400 that named the Go type.
func TestTypedHookTakesFormShapedBody(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE forms (
		id TEXT PRIMARY KEY, title TEXT, count INTEGER, done INTEGER, ratio REAL,
		created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	app := NewApp(WithDB(db))
	app.Entity("forms", entity.EntityConfig{Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "count", Type: schema.Int},
		{Name: "done", Type: schema.Bool},
		{Name: "ratio", Type: schema.Float},
	}})
	var seen []formHookRow
	OnBeforeCreate[formHookRow](app, "forms", func(_ context.Context, v *formHookRow) error {
		seen = append(seen, *v)
		return nil
	})
	OnBeforeUpdate[formHookRow](app, "forms", func(_ context.Context, v *formHookRow) error {
		seen = append(seen, *v)
		return nil
	})

	send := func(method, path, body string, want int) map[string]any {
		t.Helper()
		req := widgetTestUser(httptest.NewRequest(method, path, strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s %s = %d, want %d: %s", method, path, body, rec.Code, want, rec.Body.String())
		}
		var out struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
		return out.Data
	}

	row := send(http.MethodPost, "/forms", `{"title":"a","count":"","done":"true","ratio":""}`, http.StatusCreated)
	if row["count"] != nil || row["done"] != true {
		t.Errorf("create with blanks: count = %#v, done = %#v; want null and true", row["count"], row["done"])
	}
	row = send(http.MethodPost, "/forms", `{"title":"b","count":"2","done":"false","ratio":"1.5"}`, http.StatusCreated)
	if row["count"] != float64(2) || row["done"] != false || row["ratio"] != 1.5 {
		t.Errorf("create with values: %v", row)
	}
	if len(seen) != 2 || seen[1].Count == nil || *seen[1].Count != 2 || seen[1].Done || seen[1].Ratio == nil || *seen[1].Ratio != 1.5 {
		t.Errorf("hook saw %+v, want count 2, done false, ratio 1.5 on the second create", seen)
	}

	id, _ := row["id"].(string)
	row = send(http.MethodPatch, "/forms/"+id, `{"count":"","done":"true","ratio":""}`, http.StatusOK)
	if row["count"] != nil || row["done"] != true || row["ratio"] != nil {
		t.Errorf("blank update: %v, want count and ratio null, done true", row)
	}
	row = send(http.MethodPatch, "/forms/"+id, `{"count":"7"}`, http.StatusOK)
	if row["count"] != float64(7) {
		t.Errorf("update count = %#v, want 7", row["count"])
	}

	// A value no pass can read still fails the hook's decode. The 400
	// says so without naming the hook's Go type.
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/forms"}, {http.MethodPatch, "/forms/" + id},
	} {
		req := widgetTestUser(httptest.NewRequest(c.method, c.path, strings.NewReader(`{"title":"c","count":"abc"}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s count abc = %d, want 400: %s", c.method, rec.Code, rec.Body.String())
		}
		if b := rec.Body.String(); strings.Contains(b, "formHookRow") || strings.Contains(b, "int64") || strings.Contains(b, "Go struct") {
			t.Errorf("%s 400 body names the hook's Go type: %s", c.method, b)
		}
	}
}

// UpsertOne runs the BeforeCreate hooks too, so it takes the same
// cleanup first. On the update arm a blank clears its column, unless a
// hook gave the field a value: then the hook's value is written.
func TestTypedHookUpsertTakesFormShapedBody(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE forms (
		id TEXT PRIMARY KEY, title TEXT, count INTEGER, done INTEGER, ratio REAL,
		created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forms (id, title, count, done, ratio) VALUES ('f1', 'a', 5, 0, 2.5)`); err != nil {
		t.Fatal(err)
	}
	app := NewApp(WithDB(db))
	app.Entity("forms", entity.EntityConfig{Fields: []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "title", Type: schema.String, Required: true},
		{Name: "count", Type: schema.Int},
		{Name: "done", Type: schema.Bool},
		{Name: "ratio", Type: schema.Float},
	}}.WithTimestamps(false))
	OnBeforeCreate[formHookRow](app, "forms", func(_ context.Context, v *formHookRow) error {
		if v.Ratio == nil {
			r := 9.0
			v.Ratio = &r
		}
		return nil
	})
	ch, err := app.CrudHandler("forms")
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithServerWrites(context.Background())
	if _, err := ch.UpsertOne(ctx, map[string]any{
		"id": "f1", "title": "b", "count": "", "done": "true", "ratio": "",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	var count sql.NullInt64
	var done bool
	var ratio sql.NullFloat64
	if err := db.QueryRow(`SELECT count, done, ratio FROM forms WHERE id = 'f1'`).Scan(&count, &done, &ratio); err != nil {
		t.Fatal(err)
	}
	if count.Valid || !done || ratio.Float64 != 9 {
		t.Errorf("row = count %v, done %v, ratio %v; want count null, done true, ratio 9 (the hook's)", count, done, ratio)
	}
}
