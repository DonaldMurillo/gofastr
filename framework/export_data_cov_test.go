package framework

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/migrate"

	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

func timeMustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// corruptExportFile overwrites <dir>/<name>.ndjson with data AND rewrites the
// manifest's sha256 for that entity to match, so the corrupt file survives the
// checksum gate and fails later at parse (exercising the write-phase readNDJSON
// error path).
func corruptExportFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".ndjson"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m exportManifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	sum := sha256Hex(data)
	for i := range m.Entities {
		if m.Entities[i].Name == name {
			m.Entities[i].SHA256 = sum
		}
	}
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExportImportRegisteredExporter round-trips a non-entity table via a
// registered DataExporter — covering the exporter branch of collectSources
// plus the raw read/write of a battery-style table.
func TestExportImportRegisteredExporter(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)

	// A raw (non-entity) table, like a battery owns.
	if _, err := db.Exec(`CREATE TABLE widgets (id TEXT PRIMARY KEY, label TEXT)`); err != nil {
		t.Fatalf("create widgets: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id, label) VALUES ('w1','Alpha'),('w2','Beta')`); err != nil {
		t.Fatalf("seed widgets: %v", err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "widgets", Source: "testbattery", Table: "widgets",
		PrimaryKey: "id", Columns: []string{"id", "label"},
	})

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	// Import restores every source in the archive — wipe all of them.
	for _, tbl := range []string{"documents", "tags", "widgets"} {
		if _, err := db.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("wipe %s: %v", tbl, err)
		}
	}
	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}
	rows := readAllRows(t, db, "widgets", []string{"id", "label"})
	if len(rows) != 2 {
		t.Fatalf("exporter table not restored: %d rows", len(rows))
	}
}

// TestExportNilRegistry covers registryView's nil branch: an App with no
// Registry exports only registered exporters (here: none → just a manifest).
func TestExportNilRegistry(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	defer db.Close()
	app := &App{DB: db} // no Registry
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData nil-registry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		t.Errorf("manifest not written: %v", err)
	}
}

// TestExportAbsentExporterTableSkipped covers the "registered table absent →
// skip" branch: an exporter names a table that doesn't exist in this DB.
func TestExportAbsentExporterTableSkipped(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	datexport.Register(datexport.DataExporter{
		Name: "ghost", Source: "testbattery", Table: "ghost_table",
		PrimaryKey: "id", Columns: []string{"id"},
	})
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ghost.ndjson")); !os.IsNotExist(err) {
		t.Error("absent exporter table should be skipped, not written")
	}
}

// TestExportMkdirError covers the os.MkdirAll error branch: the target dir
// path lives under an existing FILE, so it can't be created.
func TestExportMkdirError(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.ExportData(context.Background(), filepath.Join(f, "sub")); err == nil {
		t.Error("ExportData under a file path should error on mkdir")
	}
}

// TestImportManifestErrors covers the manifest read/parse/format branches.
func TestImportManifestErrors(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()

	// Missing manifest.
	if err := app.ImportData(context.Background(), t.TempDir()); err == nil {
		t.Error("missing manifest should error")
	}
	// Malformed manifest JSON.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Error("malformed manifest should error")
	}
	// Unknown source name in a well-formed manifest.
	dir2 := t.TempDir()
	m := map[string]any{"format": "gofastr-data-v1", "entities": []map[string]any{
		{"name": "nosuch", "table": "nosuch", "primary_key": "id", "columns": []string{"id"}, "sha256": "0", "row_count": 0},
	}}
	mb, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir2, "manifest.json"), mb, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportData(context.Background(), dir2); err == nil {
		t.Error("unknown source should error")
	}
}

// TestImportTableMismatch covers the "archive table != live table" branch:
// the entity exists but the manifest claims a different physical table.
func TestImportTableMismatch(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	dir := t.TempDir()
	m := map[string]any{"format": "gofastr-data-v1", "entities": []map[string]any{
		{"name": "tags", "table": "WRONG", "primary_key": "id", "columns": []string{"id"}, "sha256": "0", "row_count": 0},
	}}
	mb, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644)
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Error("archive table mismatch should error")
	}
}

// TestImportTxBeginError covers the BeginTx failure branch: a closed DB.
func TestImportTxBeginError(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	db.Close() // now BeginTx will fail during the write phase
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Error("ImportData on a closed DB should error at BeginTx")
	}
}

// TestImportExecErrorRollsBack covers the rawWriteAll exec-error + rollback
// branch: importing on top of existing rows conflicts on the primary key.
func TestImportExecErrorRollsBack(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	// Do NOT wipe — importing over existing rows conflicts on PK.
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Error("ImportData over existing rows should error on PK conflict")
	}
}

// Direct unit tests for the package-level IO helpers — cheaper than driving
// every branch through ExportData/ImportData.

func TestNormalizeScan(t *testing.T) {
	if got := normalizeScan([]byte("hi")); got != "hi" {
		t.Errorf("[]byte → %v, want string", got)
	}
	tm := timeMustParse(t, "2024-01-02T03:04:05Z")
	if got := normalizeScan(tm); got != "2024-01-02T03:04:05Z" {
		t.Errorf("time.Time → %v, want RFC3339 string", got)
	}
	if got := normalizeScan(int64(7)); got != int64(7) {
		t.Errorf("int64 passthrough → %v", got)
	}
	if got := normalizeScan(nil); got != nil {
		t.Errorf("nil passthrough → %v", got)
	}
}

func TestIndexOf(t *testing.T) {
	if indexOf([]string{"a", "b", "c"}, "b") != 1 {
		t.Error("indexOf found case")
	}
	if indexOf([]string{"a"}, "z") != -1 {
		t.Error("indexOf not-found case")
	}
}

func TestWriteNDJSONError(t *testing.T) {
	// A path whose parent is a FILE can't be written.
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeNDJSON(filepath.Join(f, "nope.ndjson"), []map[string]any{{"id": "1"}}); err == nil {
		t.Error("writeNDJSON to an unwritable path should error")
	}
}

func TestReadNDJSONErrors(t *testing.T) {
	// Missing file.
	if _, err := readNDJSON(filepath.Join(t.TempDir(), "missing.ndjson")); err == nil {
		t.Error("readNDJSON on a missing file should error")
	}
	// Malformed JSON line.
	p := filepath.Join(t.TempDir(), "bad.ndjson")
	if err := os.WriteFile(p, []byte("{not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readNDJSON(p); err == nil {
		t.Error("readNDJSON on malformed JSON should error")
	}
}

// TestImportCorruptNDJSONInWritePhase covers readNDJSON's error path inside
// ImportData: a valid-checksum file whose bytes are non-JSON (checksum is over
// the raw bytes, so it passes the checksum gate and fails at parse).
func TestImportCorruptNDJSONInWritePhase(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	// Overwrite one ndjson with non-JSON bytes AND fix its manifest checksum so
	// it survives the checksum gate and fails at readNDJSON in the write phase.
	corruptExportFile(t, dir, "tags", []byte("garbage not json\n"))
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Error("corrupt ndjson should fail at parse in the write phase")
	}
	for _, tbl := range []string{"documents", "tags"} {
		if _, err := db.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatal(err)
		}
	}
}

// TestExportImportReachableBranches sweeps several small reachable branches:
// negative page size, format mismatch, exporter table/name collision with an
// entity, an exporter whose columns omit the PK, and rawWriteAll's empty-cols
// guard.
func TestExportImportReachableBranches(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)

	// Exporter colliding with the "tags" entity table → collectSources skip.
	datexport.Register(datexport.DataExporter{
		Name: "tags", Source: "dupe", Table: "tags", PrimaryKey: "id", Columns: []string{"id"},
	})
	// Exporter for a real raw table whose Columns OMIT the pk → rawReadAll
	// appends pk for paging only (!hasPK branch).
	if _, err := db.Exec(`CREATE TABLE notes (id TEXT PRIMARY KEY, txt TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO notes (id, txt) VALUES ('n1','hi')`); err != nil {
		t.Fatal(err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "notes", Source: "b", Table: "notes", PrimaryKey: "id", Columns: []string{"txt"},
	})

	dir := t.TempDir()
	// Negative page size → the <=0 fallback to the default.
	if err := app.ExportData(context.Background(), dir,
		WithExportTime(fixedExportTime()), WithExportPageSize(-5)); err != nil {
		t.Fatalf("ExportData: %v", err)
	}

	// Format mismatch on import.
	bad := t.TempDir()
	os.WriteFile(filepath.Join(bad, "manifest.json"),
		[]byte(`{"format":"nope","entities":[]}`), 0o644)
	if err := app.ImportData(context.Background(), bad); err == nil {
		t.Error("format mismatch should error")
	}

	// rawWriteAll empty-columns guard.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := rawWriteAll(context.Background(), tx, "notes", nil, nil); err == nil {
		t.Error("rawWriteAll with no columns should error")
	}
}

// Direct fault tests for the IO helpers' error branches — crafted inputs hit
// the query/encode/dialect paths without brittle filesystem tricks.

func TestRawReadAllQueryError(t *testing.T) {
	db := openSQLiteMem(t)
	defer db.Close()
	if _, err := rawReadAll(context.Background(), db, "nonexistent_table",
		[]string{"id"}, "id", 10); err == nil {
		t.Error("rawReadAll on a missing table should error")
	}
}

func TestWriteNDJSONEncodeError(t *testing.T) {
	// A channel is not JSON-encodable → the encoder errors.
	rows := []map[string]any{{"bad": make(chan int)}}
	if _, err := writeNDJSON(filepath.Join(t.TempDir(), "x.ndjson"), rows); err == nil {
		t.Error("writeNDJSON of an unencodable value should error")
	}
}

func TestTableExistsBranches(t *testing.T) {
	db := openSQLiteMem(t)
	if _, err := db.Exec(`CREATE TABLE present (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	// SQLite present / absent.
	if ok, err := tableExists(context.Background(), db, "present", migrate.DialectSQLite); err != nil || !ok {
		t.Errorf("present table: ok=%v err=%v", ok, err)
	}
	if ok, _ := tableExists(context.Background(), db, "absent", migrate.DialectSQLite); ok {
		t.Error("absent table should report false")
	}
	// Postgres branch: running the information_schema query against SQLite
	// fails, but it EXECUTES the Postgres code path.
	if _, err := tableExists(context.Background(), db, "present", migrate.DialectPostgres); err == nil {
		t.Error("Postgres probe against SQLite should error (exercises the PG branch)")
	}
}

// TestExportEmptyPKExporter covers collectSources' empty-PrimaryKey default:
// an exporter with no PrimaryKey defaults to "id".
func TestExportEmptyPKExporter(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE gadgets (id TEXT PRIMARY KEY, label TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gadgets (id,label) VALUES ('x','y')`); err != nil {
		t.Fatal(err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "gadgets", Source: "b", Table: "gadgets", PrimaryKey: "", // → defaults to "id"
		Columns: []string{"id", "label"},
	})
	if err := app.ExportData(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("ExportData with empty-PK exporter: %v", err)
	}
}

// TestImportMissingNDJSONFile covers the staged ReadFile error: a manifest
// entry whose .ndjson file is absent.
func TestImportMissingNDJSONFile(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "tags.ndjson")); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Error("import with a missing ndjson file should error")
	}
}

// TestImportParseErrorInWritePhase covers readNDJSON's error inside the write
// phase: a valid-checksum file whose second line is not JSON (passes the
// checksum gate, fails at parse).
func TestImportParseErrorInWritePhase(t *testing.T) {
	datexport.Reset(t)
	app, db := newExportTestApp(t)
	defer db.Close()
	seedExportRows(t, db)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	// First line is valid JSON so json.Decoder.More() advances to the bad
	// second token, which fails Decode.
	corruptExportFile(t, dir, "tags", []byte("{\"id\":\"g1\"}\nnotjson\n"))
	if err := app.ImportData(context.Background(), dir); err == nil {
		t.Error("import of a file with an invalid JSON line should error at parse")
	}
	for _, tbl := range []string{"documents", "tags"} {
		db.Exec("DELETE FROM " + tbl)
	}
}
