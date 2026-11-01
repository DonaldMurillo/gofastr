package framework

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An archive whose manifest leaves the key column out wrote every row
// with a NULL id: SQLite admits NULL in a TEXT primary key, so the
// restored rows could never be read back by key (2026-10-27 D18).
func TestImportRefusesKeylessArchive(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dir)
	for i, e := range m.Entities {
		if e.Name == "tags" {
			m.Entities[i].Columns = slices.DeleteFunc(slices.Clone(e.Columns), func(c string) bool { return c == "id" })
		}
	}
	writeManifest(t, dir, m)
	identityExec(t, db, "DELETE FROM tags", "DELETE FROM documents")
	err := app.ImportData(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), `key column "id"`) {
		t.Fatalf("ImportData = %v, want a refusal naming the key column", err)
	}
	if n := identityCount(t, db, "SELECT COUNT(*) FROM tags"); n != 0 {
		t.Errorf("%d tags written before the refusal", n)
	}
}

// A row that carries its key column as null (or not at all) is refused
// the same way: the archive's columns name the key, its rows do not.
func TestImportRefusesNullKeyRow(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "tags.ndjson")
	data := strings.Replace(string(mustReadFile(t, path)), `"id":"g1"`, `"id":null`, 1)
	if !strings.Contains(data, `"id":null`) {
		t.Fatalf("the archive row has no id to null: %s", data)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	m := readManifest(t, dir)
	for i, e := range m.Entities {
		if e.Name == "tags" {
			m.Entities[i].SHA256 = sha256Hex([]byte(data))
		}
	}
	writeManifest(t, dir, m)
	identityExec(t, db, "DELETE FROM tags", "DELETE FROM documents")
	err := app.ImportData(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), `key column "id"`) {
		t.Fatalf("ImportData = %v, want a refusal naming the key column", err)
	}
	if n := identityCount(t, db, "SELECT COUNT(*) FROM documents"); n != 0 {
		t.Errorf("%d documents kept after the refusal: the import did not roll back", n)
	}
}

// On SQLite an entity on main.widgets was compared with sqlite_master's
// bare widgets, taken for absent, and left out of the archive with only
// a stderr note (2026-10-27 S12).
func TestExportSQLiteQualifiedTable(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db))
	if err := app.Registry.Register(entity.Define("widgets", entity.EntityConfig{
		Table:  "main.widgets",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	identityExec(t, db, "INSERT INTO widgets (id, title) VALUES ('w1', 'one')")
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if rows, n := manifestRows(t, dir, "widgets"); n != 1 || rows != 1 {
		t.Fatalf("archive holds %d widgets entries with %d rows, want 1 with 1", n, rows)
	}
	identityExec(t, db, "DELETE FROM widgets")
	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if n := identityCount(t, db, "SELECT COUNT(*) FROM widgets"); n != 1 {
		t.Errorf("restored %d rows, want 1", n)
	}
}

// An exporter that names the entity's table with its schema is the same
// table. The dedup compared the declared strings, so users and
// <schema>.users both went into the archive and the restore failed on
// the second copy's keys (2026-10-27 S13).
func TestExportDedupsQualifiedAlias(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		datexport.Reset(t)
		t.Cleanup(func() { datexport.Reset(t) })
		app := NewApp(WithDB(db))
		if err := app.Registry.Register(entity.Define("users", entity.EntityConfig{
			Table:  "users",
			Fields: []schema.Field{{Name: "email", Type: schema.String}},
		}.WithTimestamps(false))); err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatal(err)
		}
		identityExec(t, db, "ALTER TABLE users ADD COLUMN name TEXT",
			"INSERT INTO users (id, email, name) VALUES ('u1', 'ada@example.test', 'Ada')")
		schemaName := "main"
		if d == DialectPostgres {
			if err := db.QueryRow("SELECT current_schema()").Scan(&schemaName); err != nil {
				t.Fatal(err)
			}
		}
		datexport.Register(datexport.DataExporter{
			Name: "auth_users", Source: "auth", Table: schemaName + ".users", PrimaryKey: "id",
			Columns: []string{"id", "email", "name"},
		})
		dir := t.TempDir()
		if err := app.ExportData(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		if _, n := manifestRows(t, dir, "auth_users"); n != 0 {
			t.Errorf("users was exported twice, as users and auth_users")
		}
		var cols []string
		for _, e := range readManifest(t, dir).Entities {
			if e.Name == "users" {
				cols = e.Columns
			}
		}
		if !slices.Contains(cols, "name") {
			t.Errorf("users exports %v, without the exporter's name column", cols)
		}
		identityExec(t, db, "DELETE FROM users")
		if err := app.ImportData(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		if n := identityCount(t, db, "SELECT COUNT(*) FROM users WHERE name = 'Ada'"); n != 1 {
			t.Errorf("restored %d users named Ada, want 1", n)
		}
	})
}

// A schema the SQLite connection has not attached holds no table: the
// exporter is skipped as absent, as an absent bare table is, rather than
// failing the export on the missing sqlite_master.
func TestExportSkipsUnattachedSchema(t *testing.T) {
	datexport.Reset(t)
	t.Cleanup(func() { datexport.Reset(t) })
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db))
	datexport.Register(datexport.DataExporter{
		Name: "jobs", Source: "queue", Table: "elsewhere.jobs", PrimaryKey: "id",
		Columns: []string{"id"},
	})
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if _, n := manifestRows(t, dir, "jobs"); n != 0 {
		t.Errorf("an unattached schema's table went into the archive")
	}
}

// Two entities that spell one table differently are refused: grouping
// on the declared spelling would export the table twice.
func TestExportRefusesTwoSpellingsOfTable(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := NewApp(WithDB(db))
	for _, ent := range [][2]string{{"users", "users"}, {"accounts", "main.users"}} {
		if err := app.Registry.Register(entity.Define(ent[0], entity.EntityConfig{
			Table:  ent[1],
			Fields: []schema.Field{{Name: "email", Type: schema.String}},
		}.WithTimestamps(false))); err != nil {
			t.Fatal(err)
		}
	}
	identityExec(t, db, "CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT)")
	err := app.ExportData(context.Background(), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "name one table") {
		t.Fatalf("ExportData = %v, want a refusal of the two spellings", err)
	}
}
