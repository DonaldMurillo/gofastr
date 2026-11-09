package crud

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A caller-chosen string key that is neither Required nor generated,
// sent as null or left out, was stored under NULL on SQLite: AutoMigrate
// declares a one-column key `id TEXT PRIMARY KEY`, and SQLite lets such
// a column hold NULL. Postgres refuses it at the write with a 400 naming
// the field (#669). Every insert path now refuses it, the same way
// on both.
func TestCreateRefusesNullKey(t *testing.T) {
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
	for _, body := range []string{`{"label":"orphan"}`, `{"code":null,"label":"orphan"}`} {
		rec := post("/codes", body, ch.Create())
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":[`) || !strings.Contains(rec.Body.String(), "required") {
			t.Errorf("POST %s = %d %s, want 400 naming code as required", body, rec.Code, rec.Body.String())
		}
		if rec := post("/codes/_batch", `{"items":[`+body+`]}`, ch.BatchCreate()); rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
			t.Errorf("batch create %s = %d %s, want a refusal", body, rec.Code, rec.Body.String())
		}
	}
	for name, write := range map[string]func(map[string]any) (map[string]any, error){
		"CreateOne": func(b map[string]any) (map[string]any, error) { return ch.CreateOne(context.Background(), b) },
		"UpsertOne": func(b map[string]any) (map[string]any, error) { return ch.UpsertOne(context.Background(), b) },
	} {
		for _, body := range []map[string]any{{"label": "orphan"}, {"code": nil, "label": "orphan"}} {
			var ve *ValidationError
			_, err := write(body)
			if !errors.As(err, &ve) || len(ve.Fields()["code"]) != 1 || ve.Fields()["code"][0].Reason != schema.ReasonRequired {
				t.Errorf("%s(%v) = %v, want code required", name, body, err)
			}
		}
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM codes WHERE code IS NULL`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows stored under a NULL key = %d (%v), want 0", n, err)
	}
	if rec := post("/codes", `{"code":"a","label":"ok"}`, ch.Create()); rec.Code != http.StatusCreated {
		t.Errorf("POST with code \"a\" = %d %s, want 201", rec.Code, rec.Body.String())
	}
}

// The key is judged by the row the database wrote, not by the insert's
// values: a key the database fills (a column DEFAULT, a declared Default
// the insert leaves to the DDL, SQLite's rowid) is written, and a NULL
// part of a composite key, which SQLite stores even when it is an
// integer, is refused.
func TestNullKeyReadsTheWrittenRow(t *testing.T) {
	cases := []struct {
		name   string
		ddl    string
		pk     []string
		fields []schema.Field
		body   map[string]any
		refuse string
	}{
		{"column default", `CREATE TABLE codes (code TEXT PRIMARY KEY DEFAULT ('gen'), label TEXT)`, []string{"code"},
			[]schema.Field{{Name: "code", Type: schema.String}, {Name: "label", Type: schema.String}}, map[string]any{"label": "x"}, ""},
		{"read-only key with a default", `CREATE TABLE codes (code TEXT PRIMARY KEY DEFAULT ('dflt'), label TEXT)`, []string{"code"},
			[]schema.Field{{Name: "code", Type: schema.String, ReadOnly: true, Default: "dflt"}, {Name: "label", Type: schema.String}}, map[string]any{"label": "x"}, ""},
		{"integer key", `CREATE TABLE codes (code INTEGER PRIMARY KEY, label TEXT)`, []string{"code"},
			[]schema.Field{{Name: "code", Type: schema.Int}, {Name: "label", Type: schema.String}}, map[string]any{"label": "x"}, ""},
		{"composite integer part", `CREATE TABLE codes (code TEXT, n INTEGER, label TEXT, PRIMARY KEY (code, n))`, []string{"code", "n"},
			[]schema.Field{{Name: "code", Type: schema.String}, {Name: "n", Type: schema.Int}, {Name: "label", Type: schema.String}}, map[string]any{"code": "a", "label": "x"}, "n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db := setupDB(t, c.ddl)
			ent := entity.Define("codes", entity.EntityConfig{Table: "codes", PrimaryKey: c.pk, Fields: c.fields}.WithTimestamps(false))
			ent.SetDB(db)
			ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
			for name, write := range map[string]func(map[string]any) (map[string]any, error){
				"CreateOne": func(b map[string]any) (map[string]any, error) { return ch.CreateOne(context.Background(), b) },
				"UpsertOne": func(b map[string]any) (map[string]any, error) { return ch.UpsertOne(context.Background(), b) },
			} {
				if _, err := db.Exec(`DELETE FROM codes`); err != nil {
					t.Fatal(err)
				}
				_, err := write(maps.Clone(c.body))
				var ve *ValidationError
				switch {
				case c.refuse == "" && err != nil:
					t.Errorf("%s = %v, want the database to fill the key", name, err)
				case c.refuse != "" && (!errors.As(err, &ve) || len(ve.Fields()[c.refuse]) != 1):
					t.Errorf("%s = %v, want %s required", name, err, c.refuse)
				}
				var nulls int
				if err := db.QueryRow(`SELECT COUNT(*) FROM codes WHERE ` + strings.Join(c.pk, " IS NULL OR ") + ` IS NULL`).Scan(&nulls); err != nil || nulls != 0 {
					t.Errorf("%s stored %d rows under a NULL key (%v)", name, nulls, err)
				}
			}
		})
	}
}
