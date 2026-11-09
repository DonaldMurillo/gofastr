package framework

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	sqlite3 "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ImportData checked each archive file's checksum on one read and parsed
// the rows from a second read inside the transaction, so a file swapped
// between the two was restored unchecked (#669). The rows it writes now
// come from the bytes it hashed. The spy driver swaps the file while the
// import opens its transaction, after the old checksum read and before
// the old write read.
var (
	beginSpyMu   sync.Mutex
	beginSpyHook func()
)

type beginSpyDriver struct{ inner driver.Driver }

func (d beginSpyDriver) Open(dsn string) (driver.Conn, error) {
	c, err := d.inner.Open(dsn)
	if err != nil {
		return nil, err
	}
	return beginSpyConn{inner: c}, nil
}

type beginSpyConn struct{ inner driver.Conn }

func (c beginSpyConn) Prepare(q string) (driver.Stmt, error) { return c.inner.Prepare(q) }
func (c beginSpyConn) Close() error                          { return c.inner.Close() }
func (c beginSpyConn) Begin() (driver.Tx, error) {
	beginSpyMu.Lock()
	hook := beginSpyHook
	beginSpyMu.Unlock()
	if hook != nil {
		hook()
	}
	return c.inner.Begin()
}

func init() {
	sql.Register("import_begin_spy", beginSpyDriver{inner: &sqlite3.SQLiteDriver{}})
}

func TestImportRestoresTheBytesItChecked(t *testing.T) {
	db, err := sql.Open("import_begin_spy", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE gadgets (id TEXT PRIMARY KEY, kind TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO gadgets (id, kind) VALUES ('g1', 'real')`); err != nil {
		t.Fatal(err)
	}
	datexport.Register(datexport.DataExporter{
		Name: "gadgets", Source: "test", Table: "gadgets",
		PrimaryKey: "id", Columns: []string{"id", "kind"},
	})
	defer datexport.Unregister("gadgets")

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM gadgets`); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "gadgets.ndjson")
	beginSpyMu.Lock()
	beginSpyHook = func() {
		if err := os.WriteFile(path, []byte(`{"id":"g1","kind":"swapped"}`+"\n"), 0o600); err != nil {
			t.Error(err)
		}
	}
	beginSpyMu.Unlock()
	t.Cleanup(func() {
		beginSpyMu.Lock()
		beginSpyHook = nil
		beginSpyMu.Unlock()
	})

	importErr := app.ImportData(context.Background(), dir)
	beginSpyMu.Lock()
	beginSpyHook = nil
	beginSpyMu.Unlock()
	var kinds []string
	rows, err := db.Query(`SELECT kind FROM gadgets`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, kind)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if importErr == nil || !strings.Contains(importErr.Error(), "checksum mismatch") || len(kinds) != 0 {
		t.Fatalf("import of a swapped file = %v, stored kinds %v; want a checksum mismatch and no rows", importErr, kinds)
	}
}

// readNDJSON decodes the archive file at path, for tests that read one.
func readNDJSON(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeNDJSON(data)
}
