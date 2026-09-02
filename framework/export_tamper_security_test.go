package framework

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Archive-side tamper shapes for ImportData. The archive directory is
// untrusted input (it arrives as a file drop, a download, or an upload),
// so every value the manifest carries is attacker-controlled. The
// property family: a manifest-borne source name is only ever resolved
// against the LIVE known-source set before it is used — it must never
// steer the ndjson read outside the archive dir, and a name that is not
// exactly a live source is refused before any row is written.
//
// The unknown-source rejection is pinned for a plain ghost name by
// TestImportRejectsUnknownSource (export_data_test.go); these tests pin
// the traversal-shaped spellings of the same attack, where the name
// would also be a filesystem escape if it ever reached the
// filepath.Join un-checked.

// TestImportRejectsTraversalSourceName: a manifest entry whose Name
// carries path-traversal syntax must be rejected as a non-live source,
// and no file may be read from (or written to) outside the archive
// directory while processing it.
func TestImportRejectsTraversalSourceName(t *testing.T) {
	app, db := newExportTestApp(t)
	seedExportRows(t, db)
	defer db.Close()

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("export: %v", err)
	}

	shapes := []string{
		"../evil",
		"..\\evil",
		"sub/../../evil",
		"tags/../../documents",
		"/absolute/evil",
	}
	for _, shape := range shapes {
		// Fresh archive per shape: re-export is cheap and keeps each
		// tamper independent.
		if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
			t.Fatalf("export: %v", err)
		}
		m := readManifest(t, dir)
		if len(m.Entities) == 0 {
			t.Fatal("export produced no manifest entries")
		}
		m.Entities[0].Name = shape
		writeManifest(t, dir, m)

		err := app.ImportData(context.Background(), dir)
		if err == nil {
			t.Fatalf("manifest name %q was accepted; a traversal-shaped source name must be refused", shape)
		}
		if !strings.Contains(err.Error(), "not a live entity") && !strings.Contains(err.Error(), "registered exporter") {
			t.Fatalf("name %q: want unknown-source rejection, got: %v", shape, err)
		}

		// Nothing outside the archive dir: the parent of the temp dir
		// must not have gained an "evil" artifact from this import.
		if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); statErr == nil {
			t.Fatalf("import with name %q touched %q outside the archive dir", shape, filepath.Join(filepath.Dir(dir), "evil"))
		}
	}
}

// TestImportRejectsNameConfusionBetweenSources: a manifest entry may
// not swap one live source's identity for another's — renaming the
// "documents" entry to "tags" while keeping documents' table/columns
// must fail the table-match check, not quietly import one table's rows
// under the other's name. This is the identity half of the same
// live-source contract (the table check is the enforcement; the test
// pins that it fires rather than being skipped for a live name).
func TestImportRejectsNameConfusionBetweenSources(t *testing.T) {
	app, db := newExportTestApp(t)
	seedExportRows(t, db)
	defer db.Close()

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("export: %v", err)
	}
	m := readManifest(t, dir)
	for i := range m.Entities {
		if m.Entities[i].Name == "documents" {
			m.Entities[i].Name = "tags" // live name, foreign table/columns/checksum
		}
	}
	writeManifest(t, dir, m)

	err := app.ImportData(context.Background(), dir)
	if err == nil {
		t.Fatal("renaming one source's manifest entry to another live name was accepted")
	}
	if !strings.Contains(err.Error(), "live table") && !strings.Contains(err.Error(), "checksum") && !strings.Contains(err.Error(), "not in the live schema") {
		t.Fatalf("want table/column/checksum mismatch rejection, got: %v", err)
	}
}
