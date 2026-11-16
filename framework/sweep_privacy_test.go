package framework

import (
	"context"
	"database/sql"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Two entities spelling one table Notes and notes are one table to erasure,
// as they are to export: each owned row is counted once.
func TestSweepPrivacyEraseFoldsTableSpellings(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		identityExec(t, db,
			`CREATE TABLE notes (id TEXT PRIMARY KEY, owner_id TEXT, body TEXT, extra TEXT)`,
			`INSERT INTO notes (id, owner_id, body, extra) VALUES ('n1', 'u1', 'b', 'e'), ('n2', 'u2', 'b', 'e')`)
		app := NewApp(WithDB(db))
		for _, e := range []struct{ name, table, field string }{{"notes", "notes", "body"}, {"note_extras", "Notes", "extra"}} {
			app.Registry.Register(entity.Define(e.name, entity.EntityConfig{
				Table:  e.table,
				Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
				Fields: []schema.Field{{Name: "owner_id", Type: schema.String}, {Name: e.field, Type: schema.String}},
			}.WithTimestamps(false)))
		}
		rep, err := app.EraseUserData(context.Background(), "u1")
		if err != nil {
			t.Fatal(err)
		}
		if n := rep.TotalErased(); n != 1 {
			t.Errorf("TotalErased = %d for one owned row, want 1: %+v", n, rep.Entities)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM notes`); n != 1 {
			t.Errorf("notes has %d rows, want u2's 1", n)
		}
	})
}

// Export's sources and erasure's relation graph group the registry into the
// same physical tables, pivots included, folded as the unquoted DDL folds.
func TestSweepPrivacyGraphAndSourcesAgreeOnTables(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := newPivotApp(t, db)
	app.Registry.Register(entity.Define("post_notes", entity.EntityConfig{
		Table:  "Posts",
		Fields: []schema.Field{{Name: "note", Type: schema.String}},
	}.WithTimestamps(false)))
	g, err := app.buildDataGraph()
	if err != nil {
		t.Fatal(err)
	}
	srcs, err := app.collectSources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var fromSources []string
	for _, s := range srcs {
		if s.source == "entity" || s.source == "pivot" {
			fromSources = append(fromSources, asciicase.Lower(s.table))
		}
	}
	slices.Sort(fromSources)
	if !slices.Equal(fromSources, g.sorted) {
		t.Fatalf("export sources name tables %v, the erase graph %v", fromSources, g.sorted)
	}
}
