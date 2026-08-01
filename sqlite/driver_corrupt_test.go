package sqlite

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenFileRejectsCorruptedSchema pins that a corrupted schema page is
// surfaced as an error on open, not silently read as an empty database.
// newDiskEngine used to discard LoadSchema's error with `_ = err`, so a
// damaged schema page produced an engine that looked fresh — every
// subsequent query saw "no such table" with no hint of corruption.
func TestOpenFileRejectsCorruptedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")

	db, err := OpenFile(path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	mustExec(t, db, "CREATE TABLE t (id INTEGER PRIMARY KEY, val TEXT)")
	if err := db.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The schema is persisted as JSON (`schemaData` → {"tables":[...]}).
	// Scramble the first table key without resizing the file so the page
	// length prefix stays valid but the payload no longer parses.
	if !bytes.Contains(data, []byte(`"tables"`)) {
		t.Fatalf("schema JSON marker not found; cannot stage corruption")
	}
	corrupted := bytes.Replace(data, []byte(`"tables"`), []byte("XXXXXXXX"), 1)
	if err := os.WriteFile(path, corrupted, 0o644); err != nil {
		t.Fatal(err)
	}

	db2, err := OpenFile(path)
	if err == nil {
		db2.Close()
		t.Fatal("OpenFile accepted a corrupted schema page (silently read as empty)")
	}
}
