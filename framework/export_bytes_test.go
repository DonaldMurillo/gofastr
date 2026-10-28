package framework

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// A raw battery table, created in each dialect's own spelling and
// registered for export.
func rawTableApp(name, sqliteDDL, pgDDL string, cols ...string) func(t *testing.T, db *sql.DB) *App {
	return func(t *testing.T, db *sql.DB) *App {
		t.Helper()
		ddl := sqliteDDL
		if migrate.DetectDialect(db) == migrate.DialectPostgres {
			ddl = pgDDL
		}
		cascadeExec(t, db, ddl)
		datexport.Register(datexport.DataExporter{
			Name: name, Source: "testbattery", Table: name, PrimaryKey: "id", Columns: cols,
		})
		t.Cleanup(func() { datexport.Unregister(name) })
		return NewApp(WithDB(db))
	}
}

// A BLOB or bytea restores byte for byte and as bytes: the export wrote
// string(x), which JSON turned into U+FFFD for invalid UTF-8, and the
// restore wrote TEXT where the source held a BLOB.
func TestImportKeepsBinaryBytes(t *testing.T) {
	datexport.Reset(t)
	want := map[string][]byte{"bad": {0xFF, 0x80, 0x00, 'h'}, "utf8": []byte("hi")}
	build := rawTableApp("blobs",
		`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`,
		`CREATE TABLE blobs (id TEXT PRIMARY KEY, data BYTEA)`, "id", "data")
	forEachRestore(t, build, func(t *testing.T, db *sql.DB) {
		for id, b := range want {
			if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES ($1, $2)`, id, b); err != nil {
				t.Fatal(err)
			}
		}
	}, func(t *testing.T, dst *sql.DB, d migrate.Dialect) {
		for id, b := range want {
			var got []byte
			if err := dst.QueryRow(`SELECT data FROM blobs WHERE id = $1`, id).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, b) {
				t.Errorf("%s after restore = %x, want %x", id, got, b)
			}
			if d != migrate.DialectSQLite {
				continue
			}
			var typ string
			if err := dst.QueryRow(`SELECT typeof(data) FROM blobs WHERE id = $1`, id).Scan(&typ); err != nil {
				t.Fatal(err)
			}
			if typ != "blob" {
				t.Errorf("%s restored as %s, want blob", id, typ)
			}
		}
	})
}

// The Postgres driver returns numeric and json as []byte text: those stay
// readable strings in the archive, only bytea is tagged.
func TestExportKeepsPgTextAsText(t *testing.T) {
	datexport.Reset(t)
	db := testdb.Open(t, migrate.DialectPostgres)
	app := rawTableApp("amounts", "", `CREATE TABLE amounts (id TEXT PRIMARY KEY, n NUMERIC, j JSONB)`, "id", "n", "j")(t, db)
	cascadeExec(t, db, `INSERT INTO amounts (id, n, j) VALUES ('a', 1.50, '{"k":1}')`)
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	rows, err := readNDJSON(filepath.Join(dir, "amounts.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["n"] != "1.50" || rows[0]["j"] != `{"k": 1}` {
		t.Errorf("archived row = %#v, want n and j as text", rows)
	}
}

// An archive written before bytes were tagged holds them as plain
// strings, and still restores.
func TestImportReadsUntaggedArchive(t *testing.T) {
	datexport.Reset(t)
	db := openSQLiteMem(t)
	app := rawTableApp("blobs", `CREATE TABLE blobs (id TEXT PRIMARY KEY, data BLOB)`, "", "id", "data")(t, db)
	dir := t.TempDir()
	row := []byte(`{"data":"hi","id":"a"}` + "\n")
	if err := os.WriteFile(filepath.Join(dir, "blobs.ndjson"), row, 0o600); err != nil {
		t.Fatal(err)
	}
	man, _ := json.Marshal(exportManifest{Format: "gofastr-data-v1", Entities: []manifestEntry{{
		Name: "blobs", Source: "testbattery", Table: "blobs", PrimaryKey: "id",
		RowCount: 1, SHA256: sha256Hex(row), Columns: []string{"id", "data"},
	}}})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), man, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatalf("ImportData of a v1 archive: %v", err)
	}
	wantIDs(t, db, "blobs", "a")
}

// An object in a row is a tagged byte string or nothing: a value of any
// other shape is refused, not bound as a map.
func TestImportRefusesUnknownObject(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.ndjson")
	for _, line := range []string{`{"data":{"other":"x"}}`, `{"data":{"$bytes":"%%%"}}`, `{"data":{"$bytes":1}}`} {
		if err := os.WriteFile(p, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readNDJSON(p); err == nil {
			t.Errorf("readNDJSON(%s) accepted the value", line)
		}
	}
}

// A raw battery table keyed by a SERIAL: the restore names every id, so
// the sequence never moved and the next insert reused id 1.
func TestImportAdvancesRawSerial(t *testing.T) {
	datexport.Reset(t)
	build := rawTableApp("tallies",
		`CREATE TABLE tallies (id INTEGER PRIMARY KEY, label TEXT)`,
		`CREATE TABLE tallies (id SERIAL PRIMARY KEY, label TEXT)`, "id", "label")
	forEachRestore(t, build, func(t *testing.T, db *sql.DB) {
		cascadeExec(t, db, `INSERT INTO tallies (label) VALUES ('a')`, `INSERT INTO tallies (label) VALUES ('b')`,
			`INSERT INTO tallies (label) VALUES ('c')`)
	}, func(t *testing.T, dst *sql.DB, _ migrate.Dialect) {
		var id int64
		if err := dst.QueryRow(`INSERT INTO tallies (label) VALUES ('next') RETURNING id`).Scan(&id); err != nil {
			t.Fatalf("insert after restore: %v", err)
		}
		if id != 4 {
			t.Errorf("next id after restore = %d, want 4", id)
		}
	})
}
