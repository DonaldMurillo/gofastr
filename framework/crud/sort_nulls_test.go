package crud

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// A sort pins the null position only on SQLite and only for a column
// that can hold NULL, so Postgres SQL and never-null index scans stay bare.
func TestSortPinsNullsOnlyOnSQLiteNullableColumns(t *testing.T) {
	ent := entity.Define("posts", entity.EntityConfig{Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "views", Type: schema.Int},
	}})
	pinned := func(db *sql.DB) map[string]bool {
		ch := &CrudHandler{Entity: ent, DB: db}
		out := map[string]bool{}
		sorts := []filter.ParsedSort{{Field: "views", Desc: true}, {Field: "title"}, {Field: "created_at"}}
		for _, s := range ch.sortTerms(context.Background(), sorts) {
			out[s.Field] = s.PinNulls
		}
		return out
	}
	open := func() *sql.DB {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	got := pinned(open())
	for field, want := range map[string]bool{"views": true, "title": false, "created_at": false, "id": false} {
		if pin, ok := got[field]; !ok || pin != want {
			t.Errorf("SQLite: %s pinned=%v (present=%v), want %v", field, pin, ok, want)
		}
	}
	pg := open()
	rememberDialect(pg, true)
	for field, pin := range pinned(pg) {
		if pin {
			t.Errorf("Postgres: %s is pinned, want a bare ORDER BY", field)
		}
	}
}
