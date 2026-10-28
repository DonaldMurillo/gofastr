package framework

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

// A symlinked export dir is refused, and its target keeps its mode: the
// chmod used to follow the link (2026-10-27 S43).
func TestExportRefusesSymlinkDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privilege on Windows")
	}
	app, _ := newExportTestApp(t)
	base := t.TempDir()
	target := filepath.Join(base, "shared")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "export")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := app.ExportData(context.Background(), link); err == nil {
		t.Fatal("ExportData through a symlinked dir succeeded")
	}
	if fi, err := os.Stat(target); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("symlink target mode changed: %v %v", fi.Mode(), err)
	}
}
