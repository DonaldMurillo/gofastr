package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func identityExec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func identityCount(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func manifestRows(t *testing.T, dir, name string) (rows, entries int) {
	t.Helper()
	for _, e := range readManifest(t, dir).Entities {
		if e.Name == name {
			rows = e.RowCount
			entries++
		}
	}
	return rows, entries
}

// An entity on a schema-qualified table is exported and restored. The
// existence probe compared "app.widgets" with the catalog's bare
// "widgets" in the current schema, found nothing, and left the table out
// of the archive with only a stderr note.
func TestExportSchemaQualifiedTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		if d != DialectPostgres {
			t.Skip("a schema-qualified table names a Postgres schema")
		}
		s := newSchemaName(t)
		identityExec(t, db, "CREATE SCHEMA "+s)
		t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA " + s + " CASCADE") })
		identityExec(t, db,
			"CREATE TABLE "+s+".widgets (id TEXT PRIMARY KEY, title TEXT)",
			"INSERT INTO "+s+".widgets (id, title) VALUES ('w1', 'one')",
			// A quoted "Widgets" is another table; the entity's DDL made
			// the folded one.
			"CREATE TABLE "+s+`."Widgets" (id TEXT PRIMARY KEY, title TEXT)`,
			"INSERT INTO "+s+`."Widgets" (id, title) VALUES ('x1', 'x'), ('x2', 'x')`)
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("widgets", entity.EntityConfig{
			Table:  s + ".Widgets",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false)))
		dir := t.TempDir()
		if err := app.ExportData(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		if rows, n := manifestRows(t, dir, "widgets"); n != 1 || rows != 1 {
			t.Fatalf("archive holds %d widgets entries with %d rows, want 1 with 1", n, rows)
		}
		identityExec(t, db, "DELETE FROM "+s+".widgets")
		if err := app.ImportData(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		if n := identityCount(t, db, "SELECT COUNT(*) FROM "+s+".widgets"); n != 1 {
			t.Errorf("restored %d rows, want 1", n)
		}
		if n := identityCount(t, db, "SELECT COUNT(*) FROM "+s+`."Widgets"`); n != 2 {
			t.Errorf("the quoted \"Widgets\" has %d rows, want its 2 untouched", n)
		}
	})
}

// A battery table created with its case kept ("Jobs", the way
// battery/queue quotes WithTable) is exported, restored and erased under
// that spelling. Folding it named jobs: a missing table, or here another
// table, which was exported in its place and had the user's row deleted.
func TestRawTableKeepsOwnerCase(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		if d != DialectPostgres {
			t.Skip("SQLite identifiers have no case")
		}
		identityExec(t, db,
			`CREATE TABLE "Jobs" (id TEXT PRIMARY KEY, user_id TEXT, seq SERIAL)`,
			`INSERT INTO "Jobs" (id, user_id) VALUES ('j1', 'u1'), ('j2', 'u2')`,
			`CREATE TABLE jobs (id TEXT PRIMARY KEY, user_id TEXT)`,
			`INSERT INTO jobs (id, user_id) VALUES ('d1', 'u1')`)
		datexport.Register(datexport.DataExporter{
			Name: "case_jobs", Source: "test", Table: "Jobs",
			PrimaryKey: "id", Columns: []string{"id", "user_id", "seq"},
		})
		t.Cleanup(func() { datexport.Unregister("case_jobs") })
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "case_jobs", Source: "test", Table: "Jobs",
			Column: "user_id", Mode: datexport.EraseDelete,
		})
		t.Cleanup(func() { datexport.UnregisterEraser("case_jobs") })
		app := NewApp(WithDB(db))
		ctx := context.Background()

		dir := t.TempDir()
		if err := app.ExportData(ctx, dir); err != nil {
			t.Fatal(err)
		}
		if rows, _ := manifestRows(t, dir, "case_jobs"); rows != 2 {
			t.Errorf("exported %d case_jobs rows, want the 2 in \"Jobs\"", rows)
		}
		identityExec(t, db, `DELETE FROM "Jobs"`)
		if err := app.ImportData(ctx, dir); err != nil {
			t.Fatal(err)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM "Jobs"`); n != 2 {
			t.Errorf("restored %d rows into \"Jobs\", want 2", n)
		}
		// The restore moved "Jobs".seq past the restored 2.
		identityExec(t, db, `INSERT INTO "Jobs" (id, user_id) VALUES ('j3', 'u3')`)
		if n := identityCount(t, db, `SELECT seq FROM "Jobs" WHERE id = 'j3'`); n != 3 {
			t.Errorf("next seq after restore = %d, want 3", n)
		}

		if _, err := app.EraseUserData(ctx, "u1"); err != nil {
			t.Fatal(err)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM "Jobs" WHERE user_id = 'u1'`); n != 0 {
			t.Errorf("\"Jobs\" keeps %d of the erased user's rows", n)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM jobs`); n != 1 {
			t.Errorf("the other table jobs has %d rows, want its 1 untouched", n)
		}
	})
}

// Tables spelled Widgets and widgets fold to one table, so they are one
// source. Two sources exported the same rows twice, and the restore
// inserted each key twice and failed.
func TestExportFoldsTableIdentity(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		identityExec(t, db,
			`CREATE TABLE widgets (id TEXT PRIMARY KEY, title TEXT, note TEXT)`,
			`INSERT INTO widgets (id, title, note) VALUES ('w1', 'one', 'n')`,
			`CREATE TABLE parts (id TEXT PRIMARY KEY, widget_id TEXT REFERENCES widgets(id))`,
			`INSERT INTO parts (id, widget_id) VALUES ('p1', 'w1')`)
		app := NewApp(WithDB(db))
		// parts sorts before widgets, so only its relation, from "Parts" to
		// "Widgets", puts it after them on restore.
		app.Registry.Register(entity.Define("parts", entity.EntityConfig{
			Table:     "Parts",
			Fields:    []schema.Field{{Name: "widget_id", Type: schema.String}},
			Relations: []entity.Relation{entity.BelongsTo("widget", "widget_notes", "widget_id")},
		}.WithTimestamps(false)))
		app.Registry.Register(entity.Define("widgets", entity.EntityConfig{
			Table:  "widgets",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false)))
		app.Registry.Register(entity.Define("widget_notes", entity.EntityConfig{
			Table:  "Widgets",
			Fields: []schema.Field{{Name: "note", Type: schema.String}},
		}.WithTimestamps(false)))
		datexport.Register(datexport.DataExporter{
			Name: "fold_widgets", Source: "test", Table: "WIDGETS",
			PrimaryKey: "id", Columns: []string{"id", "title"},
		})
		t.Cleanup(func() { datexport.Unregister("fold_widgets") })
		ctx := context.Background()

		dir := t.TempDir()
		if err := app.ExportData(ctx, dir); err != nil {
			t.Fatal(err)
		}
		if m := readManifest(t, dir); len(m.Entities) != 2 {
			t.Fatalf("archive has %d sources for widgets and parts, want 2: %+v", len(m.Entities), m.Entities)
		}
		identityExec(t, db, `DELETE FROM parts`, `DELETE FROM widgets`)
		if err := app.ImportData(ctx, dir); err != nil {
			t.Fatal(err)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM widgets WHERE note = 'n' AND title = 'one'`); n != 1 {
			t.Errorf("restored %d complete rows, want 1", n)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM parts`); n != 1 {
			t.Errorf("restored %d parts, want 1", n)
		}
	})
}

// Anonymizers on "Logs" and logs, two tables on Postgres, each scrub their
// own. Grouped by the lowered name they ran as one UPDATE on one table,
// and the folded "Logs" scrubbed logs.
func TestRawAnonymizerKeepsOwnerCase(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		if d != DialectPostgres {
			t.Skip("SQLite identifiers have no case")
		}
		identityExec(t, db,
			`CREATE TABLE "Logs" (id TEXT PRIMARY KEY, user_id TEXT, note TEXT)`,
			`INSERT INTO "Logs" (id, user_id, note) VALUES ('a', 'u1', 'secret')`,
			`CREATE TABLE logs (id TEXT PRIMARY KEY, user_id TEXT, note TEXT)`,
			`INSERT INTO logs (id, user_id, note) VALUES ('b', 'u1', 'secret')`)
		for _, e := range []datexport.DataEraser{
			{Name: "case_logs", Table: "Logs", Tombstone: "[erased]"},
			{Name: "fold_logs", Table: "logs", Tombstone: "[gone]"},
		} {
			e.Source, e.Column, e.Mode, e.ScrubColumns = "test", "user_id", datexport.EraseAnonymize, []string{"note"}
			datexport.RegisterEraser(e)
			t.Cleanup(func() { datexport.UnregisterEraser(e.Name) })
		}
		if _, err := NewApp(WithDB(db)).EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatal(err)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM "Logs" WHERE note = '[erased]'`); n != 1 {
			t.Errorf("\"Logs\" has %d rows scrubbed by its own anonymizer, want 1", n)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM logs WHERE note = '[gone]'`); n != 1 {
			t.Errorf("logs has %d rows scrubbed by its own anonymizer, want 1", n)
		}
	})
}

// A restore into a database without the archived table fails naming it.
func TestImportRefusesAbsentTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		identityExec(t, db,
			`CREATE TABLE gadgets (id TEXT PRIMARY KEY, title TEXT)`,
			`INSERT INTO gadgets (id, title) VALUES ('g1', 'one')`)
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("gadgets", entity.EntityConfig{
			Table:  "gadgets",
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false)))
		dir := t.TempDir()
		if err := app.ExportData(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		identityExec(t, db, `DROP TABLE gadgets`)
		err := app.ImportData(context.Background(), dir)
		if err == nil || !strings.Contains(err.Error(), `"gadgets"`) {
			t.Errorf("import = %v, want a refusal naming gadgets", err)
		}
	})
}

// The identity resolver reads a case-kept "People" table, not the folded
// people beside it: folded, it bound another user's email and erased that
// user's tokens.
func TestResolverKeepsOwnerCase(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		if d != DialectPostgres {
			t.Skip("SQLite identifiers have no case")
		}
		identityExec(t, db,
			`CREATE TABLE "People" (id TEXT PRIMARY KEY, email TEXT)`,
			`INSERT INTO "People" (id, email) VALUES ('u1', 'u1@x.test')`,
			`CREATE TABLE people (id TEXT PRIMARY KEY, email TEXT)`,
			`INSERT INTO people (id, email) VALUES ('u1', 'other@x.test')`,
			`CREATE TABLE tokens (token TEXT PRIMARY KEY, email TEXT)`,
			`INSERT INTO tokens (token, email) VALUES ('t1', 'u1@x.test'), ('t2', 'other@x.test')`)
		prev, had := datexport.ResolveIdentity(datexport.IdentityEmail)
		datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
			Table: "People", IDColumn: "id", ValueColumn: "email",
		})
		t.Cleanup(func() {
			datexport.UnregisterIdentityResolver(datexport.IdentityEmail)
			if had {
				datexport.RegisterIdentityResolver(datexport.IdentityEmail, prev)
			}
		})
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "case_tokens", Source: "test", Table: "tokens",
			Column: "email", Mode: datexport.EraseDelete, Identity: datexport.IdentityEmail,
		})
		t.Cleanup(func() { datexport.UnregisterEraser("case_tokens") })
		if _, err := NewApp(WithDB(db)).EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatal(err)
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM tokens WHERE token = 't1'`); n != 0 {
			t.Error("the user's token survived the erasure")
		}
		if n := identityCount(t, db, `SELECT COUNT(*) FROM tokens WHERE token = 't2'`); n != 1 {
			t.Error("another email's token was erased")
		}
	})
}
