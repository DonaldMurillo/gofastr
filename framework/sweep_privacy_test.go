package framework

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
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

// An archive whose manifest names another table for a live source is
// refused, though its file and checksum are intact.
func TestSweepPrivacyImportRefusesRenamedTable(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dir)
	found := false
	for i := range m.Entities {
		if m.Entities[i].Name == "tags" {
			m.Entities[i].Table = "documents"
			found = true
		}
	}
	if !found {
		t.Fatal("archive has no tags source")
	}
	writeManifest(t, dir, m)
	if _, err := db.Exec(`DELETE FROM tags`); err != nil {
		t.Fatal(err)
	}
	err := app.ImportData(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), `archive table "documents"`) {
		t.Fatalf("ImportData = %v, want the table mismatch refused", err)
	}
}

// The cascade does not reach an Unmanaged entity: its rows that name an
// erased row stay.
func TestSweepPrivacyEraseSkipsUnmanagedChild(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	app.Registry.Register(entity.Define("post_views", entity.EntityConfig{
		Table:     "post_views",
		Unmanaged: true,
		Fields:    []schema.Field{{Name: "post_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("post", "posts", "post_id")},
	}.WithTimestamps(false)))
	identityExec(t, db,
		`CREATE TABLE post_views (id TEXT PRIMARY KEY, post_id TEXT)`,
		`INSERT INTO posts (id, owner_id) VALUES ('p1', 'u1')`,
		`INSERT INTO post_views (id, post_id) VALUES ('v1', 'p1')`)
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if n := identityCount(t, db, `SELECT COUNT(*) FROM posts`); n != 0 {
		t.Errorf("posts has %d rows, want the owned one erased", n)
	}
	if n := identityCount(t, db, `SELECT COUNT(*) FROM post_views`); n != 1 {
		t.Errorf("post_views has %d rows, want the Unmanaged row kept", n)
	}
}
