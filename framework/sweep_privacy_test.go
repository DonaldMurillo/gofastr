package framework

import (
	"context"
	"database/sql"
	"errors"
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

// A foreign key whose columns do not line up with the key it references
// fails the erasure instead of being skipped, which would leave its rows.
func TestSweepPrivacyEraseRefusesMisshapenForeignKey(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("posts", entity.EntityConfig{
		Table:  "posts",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
	}.WithTimestamps(false)))
	app.Registry.Register(entity.Define("comments", entity.EntityConfig{
		Table:     "comments",
		Fields:    []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "post_rev", Type: schema.String}},
		Relations: []entity.Relation{{Name: "post", Type: entity.RelManyToOne, Entity: "posts", ForeignKey: []string{"post_id", "post_rev"}}},
	}.WithTimestamps(false)))
	identityExec(t, db,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, owner_id TEXT)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, post_rev TEXT)`,
		`INSERT INTO posts (id, owner_id) VALUES ('p1', 'u1')`)
	_, err := app.EraseUserData(context.Background(), "u1")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("EraseUserData = %v, want the misshapen foreign key refused", err)
	}
	if n := identityCount(t, db, `SELECT COUNT(*) FROM posts`); n != 1 {
		t.Errorf("posts has %d rows after a refused erasure, want 1", n)
	}
}

// A failing store eraser does not stop the ones after it, and each failure
// is named.
func TestSweepPrivacyEveryStoreEraserRuns(t *testing.T) {
	app, _ := storeEraseApp(t)
	ran := false
	for _, name := range []string{"a_down", "b_down"} {
		_, _ = datexport.RegisterStoreEraser(datexport.StoreEraser{Name: name, Source: "test",
			Erase: func(context.Context, string, bool) (int, error) { return 0, errors.New("unreachable") }})
	}
	_, _ = datexport.RegisterStoreEraser(datexport.StoreEraser{Name: "c_up", Source: "test",
		Erase: func(context.Context, string, bool) (int, error) { ran = true; return 1, nil }})
	rep, err := app.EraseUserData(context.Background(), "u1")
	if err == nil || !strings.Contains(err.Error(), "a_down") || !strings.Contains(err.Error(), "b_down") {
		t.Fatalf("EraseUserData = %v, want both failing stores named", err)
	}
	if !ran {
		t.Fatal("the store eraser after the failing ones did not run")
	}
	if !slices.ContainsFunc(rep.Stores, func(r EraseTableResult) bool { return r.Name == "c_up" }) {
		t.Errorf("report stores = %+v, want c_up", rep.Stores)
	}
}

// An audit table without actor_id fails the erasure, which would otherwise
// anonymize nothing.
func TestSweepPrivacyEraseRefusesAuditTableWithoutActor(t *testing.T) {
	app, _ := storeEraseApp(t)
	identityExec(t, app.DB, `CREATE TABLE audit_log (id TEXT PRIMARY KEY, entity TEXT, diff TEXT)`)
	_, err := app.EraseUserData(context.Background(), "u1")
	if err == nil || !strings.Contains(err.Error(), `no column "actor_id"`) {
		t.Fatalf("EraseUserData = %v, want the audit table refused", err)
	}
	if n := identityCount(t, app.DB, `SELECT COUNT(*) FROM notes WHERE owner_id = 'u1'`); n != 1 {
		t.Errorf("u1 has %d notes after a refused erasure, want 1", n)
	}
}

// An audit row that names an erased row by the digest WithAuditLog stores
// for a sensitive key is scrubbed with the row.
func TestSweepPrivacyEraseScrubsDigestedAuditRecord(t *testing.T) {
	app, _ := storeEraseApp(t)
	if err := EnsureAuditTable(app.DB, ""); err != nil {
		t.Fatal(err)
	}
	identityExec(t, app.DB, `INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff)
		VALUES ('adm1', 'notes', 'update', '`+auditKeyDigest("n1")+`', 'admin', '2024-01-01T00:00:00Z', '{"new":{"body":"secret"}}')`)
	if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	var diff string
	if err := app.DB.QueryRow(`SELECT diff FROM audit_log WHERE id = 'adm1'`).Scan(&diff); err != nil {
		t.Fatal(err)
	}
	if diff != erasedAuditDiff {
		t.Errorf("digested audit diff = %q, want the erasure marker", diff)
	}
}

// Archive values decode exactly: an integer past int64 keeps its digits,
// an array is refused, and an object must be exactly {"$bytes": ...}.
func TestSweepPrivacyArchiveValuesDecodeExactly(t *testing.T) {
	rows, err := decodeNDJSON([]byte(`{"n":18446744073709551617}` + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["n"] != "18446744073709551617" {
		t.Errorf("big integer decoded as %#v, want its decimal text", rows[0]["n"])
	}
	for _, bad := range []string{`{"a":[1]}`, `{"a":{"$bytes":"AA==","x":1}}`} {
		if _, err := decodeNDJSON([]byte(bad + "\n")); err == nil {
			t.Errorf("decodeNDJSON(%s) = nil error, want it refused", bad)
		}
	}
}

// Import orders sources by the live schema, so an archive listing children
// before parents still restores.
func TestSweepPrivacyImportReordersArchiveListing(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := newPivotApp(t, db)
	identityExec(t, db,
		`INSERT INTO posts (id, owner_id, title) VALUES ('p1', 'u1', 't')`,
		`INSERT INTO tags (id, name) VALUES ('g1', 'go')`,
		`INSERT INTO post_tags (post_id, tag_id) VALUES ('p1', 'g1')`)
	dir := t.TempDir()
	ctx := context.Background()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dir)
	slices.Reverse(m.Entities)
	if i, j := slices.IndexFunc(m.Entities, func(e manifestEntry) bool { return e.Name == "post_tags" }),
		slices.IndexFunc(m.Entities, func(e manifestEntry) bool { return e.Name == "posts" }); i < 0 || j < i {
		t.Fatalf("reversed listing does not put post_tags before posts: %+v", m.Entities)
	}
	writeManifest(t, dir, m)
	identityExec(t, db, `DELETE FROM post_tags`, `DELETE FROM posts`, `DELETE FROM tags`)
	if err := app.ImportData(ctx, dir); err != nil {
		t.Fatalf("ImportData of a children-first listing: %v", err)
	}
	if n := identityCount(t, db, `SELECT COUNT(*) FROM post_tags`); n != 1 {
		t.Errorf("post_tags has %d rows, want 1", n)
	}
}
