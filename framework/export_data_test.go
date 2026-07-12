package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// openSQLiteMem opens an in-memory SQLite DB pinned to a single connection so
// every query shares the same :memory: database.
func openSQLiteMem(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skipf("sqlite3 driver not available: %v", err)
	}
	db.SetMaxOpenConns(1)
	return db
}

// newExportTestApp builds an App with two entities exercising owner scoping,
// soft delete, a hidden column, multi-tenancy, and a plain entity — then
// AutoMigrates. The caller seeds rows.
func newExportTestApp(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	db := openSQLiteMem(t)
	app := NewApp(WithDB(db))

	// documents: owner-scoped + soft-delete + hidden column + multi-tenant.
	app.Registry.Register(entity.Define("documents", entity.EntityConfig{
		Table:       "documents",
		OwnerField:  "owner_id",
		SoftDelete:  true,
		MultiTenant: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "body", Type: schema.Text},
			{Name: "owner_id", Type: schema.String},
			{Name: "views", Type: schema.Int},
			{Name: "internal_note", Type: schema.String, Hidden: true},
		},
	}))
	// tags: plain entity with timestamps.
	app.Registry.Register(entity.Define("tags", entity.EntityConfig{
		Table: "tags",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
		},
	}))

	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app, db
}

// seedExportRows inserts rows with explicit ids/timestamps/owner/tenant,
// including one soft-deleted row and one hidden-column value.
func seedExportRows(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `INSERT INTO documents
		(id, title, body, owner_id, views, internal_note, created_at, updated_at, tenant_id, deleted_at)
		VALUES
		('d1','Doc One','alpha','u1',5,'secret-1','2024-01-01T00:00:00Z','2024-01-02T00:00:00Z','t1',NULL),
		('d2','Doc Two','beta','u2',9,'secret-2','2024-01-03T00:00:00Z','2024-01-04T00:00:00Z','t2','2024-01-05T00:00:00Z')`)
	if err != nil {
		t.Fatalf("seed documents: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO tags (id, name, created_at, updated_at)
		VALUES ('g1','go','2024-02-01T00:00:00Z','2024-02-02T00:00:00Z')`)
	if err != nil {
		t.Fatalf("seed tags: %v", err)
	}
}

func readAllRows(t *testing.T, db *sql.DB, table string, cols []string) []map[string]any {
	t.Helper()
	rows, err := rawReadAll(context.Background(), db, table, cols, "id", 1000)
	if err != nil {
		t.Fatalf("rawReadAll %s: %v", table, err)
	}
	return rows
}

func entityColumns(t *testing.T, app *App, name string) []string {
	t.Helper()
	ent, err := app.Registry.Get(name)
	if err != nil {
		t.Fatalf("get entity %s: %v", name, err)
	}
	cols := make([]string, 0, len(ent.GetFields()))
	for _, f := range ent.GetFields() {
		cols = append(cols, f.Name)
	}
	return cols
}

// fixedExportTime is a deterministic clock for reproducible manifests.
func fixedExportTime() time.Time {
	return time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
}

// TestExportImportDataRoundTrip: export → wipe → import restores every row,
// column, id, timestamp, owner, tenant, hidden value, and the soft-deleted row.
func TestExportImportDataRoundTrip(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)

	docCols := entityColumns(t, app, "documents")
	tagCols := entityColumns(t, app, "tags")
	beforeDocs := readAllRows(t, db, "documents", docCols)
	beforeTags := readAllRows(t, db, "tags", tagCols)
	if len(beforeDocs) != 2 || len(beforeTags) != 1 {
		t.Fatalf("seed counts: docs=%d tags=%d", len(beforeDocs), len(beforeTags))
	}

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}

	// Wipe all rows (schema stays).
	if _, err := db.Exec("DELETE FROM documents"); err != nil {
		t.Fatalf("wipe documents: %v", err)
	}
	if _, err := db.Exec("DELETE FROM tags"); err != nil {
		t.Fatalf("wipe tags: %v", err)
	}

	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}

	afterDocs := readAllRows(t, db, "documents", docCols)
	afterTags := readAllRows(t, db, "tags", tagCols)
	if !reflect.DeepEqual(beforeDocs, afterDocs) {
		t.Errorf("documents not restored faithfully:\nbefore=%v\nafter =%v", beforeDocs, afterDocs)
	}
	if !reflect.DeepEqual(beforeTags, afterTags) {
		t.Errorf("tags not restored faithfully:\nbefore=%v\nafter =%v", beforeTags, afterTags)
	}

	// Pin the load-bearing fields explicitly (not just set-equality).
	byID := map[string]map[string]any{}
	for _, r := range afterDocs {
		byID[r["id"].(string)] = r
	}
	d2 := byID["d2"]
	if d2["deleted_at"] == nil {
		t.Error("soft-deleted row d2 lost its deleted_at on import")
	}
	if d2["owner_id"] != "u2" || d2["tenant_id"] != "t2" {
		t.Errorf("d2 owner/tenant not preserved: %+v", d2)
	}
	if d2["internal_note"] != "secret-2" {
		t.Errorf("d2 hidden column not preserved: %v", d2["internal_note"])
	}
	if d2["created_at"] != "2024-01-03T00:00:00Z" {
		t.Errorf("d2 created_at not preserved: %v", d2["created_at"])
	}
	d1 := byID["d1"]
	if d1["deleted_at"] != nil {
		t.Errorf("d1 should have NULL deleted_at, got %v", d1["deleted_at"])
	}
	if d1["views"] != int64(5) {
		t.Errorf("d1 views numeric fidelity: got %v", d1["views"])
	}
}

// TestExportManifestChecksumsAndSchema: the manifest records correct row
// counts, matching per-file sha256, the column list, and a schema fingerprint.
func TestExportManifestChecksumsAndSchema(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	man := readManifest(t, dir)
	if man.Format != exportFormatVersion {
		t.Errorf("format = %q, want %q", man.Format, exportFormatVersion)
	}
	if man.CreatedAt != fixedExportTime().Format(time.RFC3339Nano) {
		t.Errorf("created_at not the supplied time: %q", man.CreatedAt)
	}
	if len(man.Entities) != 2 {
		t.Fatalf("expected 2 manifest entities, got %d", len(man.Entities))
	}
	if man.Schema.Tables == nil {
		t.Fatal("manifest schema fingerprint missing")
	}
	for _, table := range []string{"documents", "tags"} {
		if _, ok := man.Schema.Tables[table]; !ok {
			t.Errorf("schema fingerprint missing table %q", table)
		}
	}
	for _, e := range man.Entities {
		data, err := os.ReadFile(filepath.Join(dir, e.Name+".ndjson"))
		if err != nil {
			t.Fatalf("read ndjson %s: %v", e.Name, err)
		}
		if got := sha256Hex(data); got != e.SHA256 {
			t.Errorf("entity %s sha256 mismatch: manifest %s, file %s", e.Name, e.SHA256, got)
		}
		wantRows := 0
		if len(data) > 0 {
			wantRows = strings.Count(strings.TrimRight(string(data), "\n"), "\n") + 1
		}
		if e.RowCount != wantRows {
			t.Errorf("entity %s row_count = %d, want %d", e.Name, e.RowCount, wantRows)
		}
		if len(e.Columns) == 0 {
			t.Errorf("entity %s has no column list", e.Name)
		}
	}
}

// TestImportRejectsMissingManifest: no manifest.json → error, nothing written.
func TestImportRejectsMissingManifest(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	dir := t.TempDir() // empty dir, no manifest
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Fatal("expected error importing dir without manifest")
	}
}

// TestImportRejectsUnknownSource: a manifest entry for a source not in the
// live registry is rejected before any write.
func TestImportRejectsUnknownSource(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	man := readManifest(t, dir)
	man.Entities = append(man.Entities, manifestEntry{
		Name: "evil", Source: "entity", Table: "evil", PrimaryKey: "id",
		Columns: []string{"id"}, SHA256: "00", RowCount: 0,
	})
	writeManifest(t, dir, man)
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Fatal("expected error for unknown source")
	}
}

// TestImportRejectsChecksumMismatch: a tampered ndjson fails the checksum
// check before any write.
func TestImportRejectsChecksumMismatch(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	p := filepath.Join(dir, "documents.ndjson")
	if err := os.WriteFile(p, append(mustReadFile(t, p), []byte("\n{\"id\":\"x\"}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Fatal("expected checksum-mismatch error")
	}
}

// TestImportRejectsIncompatibleColumns: an archive column absent from the live
// schema is rejected before any write.
func TestImportRejectsIncompatibleColumns(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	man := readManifest(t, dir)
	for i := range man.Entities {
		if man.Entities[i].Name == "documents" {
			man.Entities[i].Columns = append(man.Entities[i].Columns, "bogus_column")
		}
	}
	writeManifest(t, dir, man)
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Fatal("expected incompatible-column error")
	}
}

// TestImportRejectsSQLMetacharacters: an archive carrying SQL metacharacters in
// a column/table name is rejected by the live-whitelist check and never reaches
// a query. (Design decision D2.)
func TestImportRejectsSQLMetacharacters(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	// Case 1: a column name carrying an injection payload.
	man := readManifest(t, dir)
	for i := range man.Entities {
		if man.Entities[i].Name == "documents" {
			man.Entities[i].Columns = append(man.Entities[i].Columns, "name; DROP TABLE documents; --")
		}
	}
	writeManifest(t, dir, man)
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Fatal("expected rejection of SQL-metacharacter column name")
	}
	// The table must still exist and be intact (the payload never ran).
	if _, err := db.Exec("SELECT id FROM documents LIMIT 1"); err != nil {
		t.Errorf("documents table damaged by rejected import: %v", err)
	}

	// Case 2: a hostile table name in the manifest entry. It won't match the
	// live table, so it is rejected at the provenance check — never interpolated.
	man2 := readManifest(t, dir)
	man2.Entities = append(man2.Entities, manifestEntry{
		Name: "hostile", Source: "entity", Table: "t; DROP TABLE tags; --",
		PrimaryKey: "id", Columns: []string{"id"}, SHA256: "00", RowCount: 0,
	})
	writeManifest(t, dir, man2)
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Fatal("expected rejection of hostile table name")
	}
	if _, err := db.Exec("SELECT id FROM tags LIMIT 1"); err != nil {
		t.Errorf("tags table damaged by rejected import: %v", err)
	}
}

// TestExportIncludesRegisteredExporter: a table registered via the datexport
// registry (a battery-style raw table) is included in the archive.
func TestExportIncludesRegisteredExporter(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE widgets (id TEXT PRIMARY KEY, kind TEXT NOT NULL)`); err != nil {
		t.Fatalf("create widgets: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id, kind) VALUES ('w1','cog')`); err != nil {
		t.Fatalf("seed widgets: %v", err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "widgets", Source: "test", Table: "widgets",
		PrimaryKey: "id", Columns: []string{"id", "kind"},
	})
	defer datexport.Unregister("widgets")

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	man := readManifest(t, dir)
	found := false
	for _, e := range man.Entities {
		if e.Name == "widgets" {
			found = true
			if e.RowCount != 1 {
				t.Errorf("widgets row_count = %d, want 1", e.RowCount)
			}
		}
	}
	if !found {
		t.Error("registered exporter 'widgets' not included in export")
	}

	// Round-trip the exporter table too: wipe, import, restore.
	if _, err := db.Exec("DELETE FROM widgets"); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}
	var id, kind string
	if err := db.QueryRow("SELECT id, kind FROM widgets WHERE id = 'w1'").Scan(&id, &kind); err != nil {
		t.Fatalf("widgets not restored: %v", err)
	}
	if id != "w1" || kind != "cog" {
		t.Errorf("widgets restored wrong: id=%q kind=%q", id, kind)
	}
}

// TestExportSkipsAbsentRegisteredTable: a registered exporter whose table is
// absent from the DB is skipped (not exported), not fatal.
func TestExportSkipsAbsentRegisteredTable(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	datexport.Register(datexport.DataExporter{
		Name: "ghost", Source: "test", Table: "ghost_table",
		PrimaryKey: "id", Columns: []string{"id"},
	})
	defer datexport.Unregister("ghost")

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData with absent table: %v", err)
	}
	man := readManifest(t, dir)
	for _, e := range man.Entities {
		if e.Name == "ghost" {
			t.Error("absent registered table 'ghost' should not appear in export")
		}
	}
}

// TestRawWriteAllRejectsUnsafeIdent: the SQL-safety choke point refuses an
// unsafe table or column name even if a caller bypassed staged validation.
// Identifiers are validated with query.MustIdent at this boundary, so an
// unsafe name is a fail-fast PANIC (it can never arrive from a validated
// registry/archive) — never a silently-interpolated injection.
func TestRawWriteAllRejectsUnsafeIdent(t *testing.T) {
	db := openSQLiteMem(t)
	defer db.Close()
	rows := []map[string]any{{"id": "1"}}

	panics := func(table string, cols []string) (didPanic bool) {
		defer func() { didPanic = recover() != nil }()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		_ = rawWriteAll(context.Background(), tx, table, cols, rows)
		return
	}
	if !panics("t; DROP TABLE x", []string{"id"}) {
		t.Error("expected unsafe table name to be rejected (panic)")
	}
	if !panics("ok", []string{"id; DROP TABLE x"}) {
		t.Error("expected unsafe column name to be rejected (panic)")
	}
}

// TestDialectDetectionSafeOnSQLite: the export path calls DetectDialect; it
// must not panic on a fresh SQLite DB.
func TestDialectDetectionSafeOnSQLite(t *testing.T) {
	db := openSQLiteMem(t)
	defer db.Close()
	if d := migrate.DetectDialect(db); d != migrate.DialectSQLite {
		t.Errorf("DetectDialect = %v, want DialectSQLite", d)
	}
}

// --- helpers ---

func readManifest(t *testing.T, dir string) exportManifest {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m exportManifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	return m
}

func writeManifest(t *testing.T, dir string, m exportManifest) {
	t.Helper()
	sort.Slice(m.Entities, func(i, j int) bool { return m.Entities[i].Name < m.Entities[j].Name })
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestExportPagedRoundTrip forces keyset paging (page size 1) so the
// multi-page read loop + WHERE pk > $1 continuation are exercised, and
// asserts the round-trip stays faithful — no rows lost or duplicated at
// page boundaries.
func TestExportPagedRoundTrip(t *testing.T) {
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)

	docCols := entityColumns(t, app, "documents")
	before := readAllRows(t, db, "documents", docCols)

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir,
		WithExportTime(fixedExportTime()), WithExportPageSize(1)); err != nil {
		t.Fatalf("ExportData paged: %v", err)
	}
	if _, err := db.Exec("DELETE FROM documents"); err != nil {
		t.Fatalf("wipe documents: %v", err)
	}
	if _, err := db.Exec("DELETE FROM tags"); err != nil {
		t.Fatalf("wipe tags: %v", err)
	}
	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}
	after := readAllRows(t, db, "documents", docCols)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("paged export lost/duplicated rows:\nbefore=%v\nafter =%v", before, after)
	}
}

// TestExportDataNilGuards covers the early error guards.
func TestExportDataNilGuards(t *testing.T) {
	var nilApp *App
	if err := nilApp.ExportData(context.Background(), t.TempDir()); err == nil {
		t.Error("nil App ExportData should error")
	}
	if err := nilApp.ImportData(context.Background(), t.TempDir()); err == nil {
		t.Error("nil App ImportData should error")
	}
	app := &App{}
	if err := app.ExportData(context.Background(), t.TempDir()); err == nil {
		t.Error("ExportData with nil DB should error")
	}
	if err := app.ImportData(context.Background(), t.TempDir()); err == nil {
		t.Error("ImportData with nil DB should error")
	}
}
