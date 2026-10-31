package framework

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	sqlite3 "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ExportData collected every page of a table into one slice and then the
// serialized file into one buffer before writing it, so the page size
// bounded nothing and a large table could exhaust memory during an admin
// export (2026-10-27 bug bash S11). Each page now reaches the file before
// the next is read. The spy driver sees each statement the export
// prepares; at every continuation page it counts the rows already on disk.

var (
	exportSpyMu   sync.Mutex
	exportSpyHook func(q string)
)

type exportSpyDriver struct{ inner driver.Driver }

func (d exportSpyDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return exportSpyConn{inner: c}, nil
}

type exportSpyConn struct{ inner driver.Conn }

func (c exportSpyConn) Prepare(q string) (driver.Stmt, error) {
	exportSpyMu.Lock()
	hook := exportSpyHook
	exportSpyMu.Unlock()
	if hook != nil {
		hook(q)
	}
	return c.inner.Prepare(q)
}
func (c exportSpyConn) Close() error              { return c.inner.Close() }
func (c exportSpyConn) Begin() (driver.Tx, error) { return c.inner.Begin() }

func init() {
	sql.Register("export_spy", exportSpyDriver{inner: &sqlite3.SQLiteDriver{}})
}

func TestExportStreamsEachPageToDisk(t *testing.T) {
	db, err := sql.Open("export_spy", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE widgets (id TEXT PRIMARY KEY, kind TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id, kind) VALUES ('w1','<a&b>'), ('w2','b'), ('w3','c')`); err != nil {
		t.Fatal(err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "widgets", Source: "test", Table: "widgets",
		PrimaryKey: "id", Columns: []string{"id", "kind"},
	})
	defer datexport.Unregister("widgets")

	dir := t.TempDir()
	path := filepath.Join(dir, "widgets.ndjson")
	var onDisk []int
	exportSpyMu.Lock()
	exportSpyHook = func(q string) {
		if !strings.Contains(q, "widgets") || !strings.Contains(q, " WHERE ") {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			onDisk = append(onDisk, -1)
			return
		}
		onDisk = append(onDisk, bytes.Count(data, []byte("\n")))
	}
	exportSpyMu.Unlock()
	t.Cleanup(func() {
		exportSpyMu.Lock()
		exportSpyHook = nil
		exportSpyMu.Unlock()
	})

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	if err := app.ExportData(context.Background(), dir, WithExportPageSize(1)); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	// Pages of one row: the continuation reads after w1, w2 and w3 must
	// each find the rows before them already written.
	if want := []int{1, 2, 3}; !slices.Equal(onDisk, want) {
		t.Fatalf("rows on disk at each continuation page = %v, want %v (-1: no file yet)", onDisk, want)
	}
	man := readManifest(t, dir)
	for _, e := range man.Entities {
		if e.Name != "widgets" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if e.RowCount != 3 || e.SHA256 != sha256Hex(data) {
			t.Fatalf("manifest row_count %d sha %s, want 3 and the file's %s", e.RowCount, e.SHA256, sha256Hex(data))
		}
		if !bytes.Contains(data, []byte(`"<a&b>"`)) {
			t.Fatalf("archive escaped markup characters, want them as written:\n%s", data)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("ndjson mode %o, want 600", st.Mode().Perm())
		}
		return
	}
	t.Fatal("widgets missing from the manifest")
}

// A failed write or page flush stops the read where it happened, and a
// flush the file refuses fails the close instead of returning a checksum
// for bytes that never landed.
func TestExportStreamStopsOnWriteFailure(t *testing.T) {
	db := openSQLiteMem(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE widgets (id TEXT PRIMARY KEY, kind TEXT); INSERT INTO widgets VALUES ('w1','a'), ('w2','b')`); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("disk full")
	ctx := context.Background()
	rows := 0
	err := rawReadEach(ctx, db, migrate.DialectSQLite, "widgets", []string{"id", "kind"}, []string{"id"}, 10,
		func(map[string]any) error { rows++; return boom }, func() error { return nil })
	if !errors.Is(err, boom) || rows != 1 {
		t.Fatalf("emit failure: err %v after %d rows, want disk full after 1", err, rows)
	}
	pages := 0
	err = rawReadEach(ctx, db, migrate.DialectSQLite, "widgets", []string{"id", "kind"}, []string{"id"}, 1,
		func(map[string]any) error { return nil }, func() error { pages++; return boom })
	if !errors.Is(err, boom) || pages != 1 {
		t.Fatalf("flush failure: err %v after %d pages, want disk full after 1", err, pages)
	}

	f, err := os.Create(filepath.Join(t.TempDir(), "x.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	buf := bufio.NewWriter(failingWriter{boom})
	w := &ndjsonFile{f: f, buf: buf, h: sha256.New(), enc: json.NewEncoder(buf)}
	if err := w.write(map[string]any{"id": "w1"}); err != nil {
		t.Fatal(err)
	}
	if sum, err := w.close(); !errors.Is(err, boom) {
		t.Fatalf("close with a refused flush = %q, %v; want disk full", sum, err)
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// A table the export cannot finish fails the export, and no manifest
// vouches for the partial file.
func TestExportReadFailureWritesNoManifest(t *testing.T) {
	db := openSQLiteMem(t)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE notes (id TEXT PRIMARY KEY, body TEXT); INSERT INTO notes VALUES (NULL, 'x'), ('a', 'y')`); err != nil {
		t.Fatal(err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "notes", Source: "test", Table: "notes",
		PrimaryKey: "id", Columns: []string{"id", "body"},
	})
	defer datexport.Unregister("notes")
	dir := t.TempDir()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	if err := app.ExportData(context.Background(), dir, WithExportPageSize(1)); err == nil {
		t.Fatal("ExportData of a NULL-key table succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest after a failed export: %v, want none", err)
	}
}

// rawReadAll collects rawReadEach's rows, for tests that compare tables.
func rawReadAll(ctx context.Context, db *sql.DB, dialect migrate.Dialect, table string, columns, keys []string, pageSize int) ([]map[string]any, error) {
	var out []map[string]any
	err := rawReadEach(ctx, db, dialect, table, columns, keys, pageSize, func(r map[string]any) error {
		out = append(out, r)
		return nil
	}, func() error { return nil })
	return out, err
}

// writeNDJSON writes rows the way an export does and returns the file's
// sha256, for tests that hand-build an archive entry.
func writeNDJSON(path string, rows []map[string]any) (string, error) {
	w, err := createNDJSON(path)
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if err := w.write(r); err != nil {
			w.f.Close()
			return "", err
		}
	}
	return w.close()
}
