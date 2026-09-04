package framework

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat export dir: %v", err)
	}
	if di.Mode().Perm()&0o077 != 0 {
		t.Errorf("SECURITY: export dir is mode %o — a raw dump of every physical column (password_hash, token hashes, "+
			"sessions on a real app) sits group/world-listable; the export dir must be 0700.",
			di.Mode().Perm())
	}

	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(ents) == 0 {
		t.Fatalf("export produced no files; surface moved, revisit this pin")
	}
	for _, ent := range ents {
		fi, err := os.Stat(filepath.Join(dir, ent.Name()))
		if err != nil {
			t.Fatalf("stat %s: %v", ent.Name(), err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("SECURITY: exported file %s is mode %o — every local co-user reads the raw credential-bearing dump; "+
				"ndjson/manifest files must be written 0600.",
				ent.Name(), fi.Mode().Perm())
		}
	}
}
