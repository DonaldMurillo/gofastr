package framework

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

// TestExportDataRestrictiveModes: ExportData is a raw dump of every
// physical column (password hashes, token hashes, sessions on a real
// app), so the export dir and every file in it are owner-only (0700 dir,
// 0600 files), matching the repo's discipline for every other
// secret-bearing artifact.
func TestExportDataRestrictiveModes(t *testing.T) {
	app, db := newExportTestApp(t)
	seedExportRows(t, db)

	dir := filepath.Join(t.TempDir(), "export")
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}

	filepermtest.AssertOwnerOnly(t, dir, 0o700)

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(ents) == 0 {
		t.Fatalf("export produced no files; surface moved, revisit this pin")
	}
	for _, ent := range ents {
		filepermtest.AssertOwnerOnly(t, filepath.Join(dir, ent.Name()), 0o600)
	}
}

func TestExportRestrictsExistingDirectory(t *testing.T) {
	app, _ := newExportTestApp(t)
	dir := filepath.Join(t.TempDir(), "export")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, dir, 0o700)
}
