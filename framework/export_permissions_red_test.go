//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: secret-bearing files the framework writes get restrictive modes —
// the repo's own discipline (freeze world.json 0600, battery/log 0600, DEK
// 0600, uploads 0600). Export archives are the loudest instance of that
// family: they are raw, all-physical-column dumps of every table.
//
// Surfaces: App.ExportData (export_data.go) — MkdirAll(dir, 0o755) at :134,
// writeNDJSON os.WriteFile 0o644 at :573, manifest.json 0o644 at :190.
//
// Finding: ExportData is documented to read "all physical columns, all rows
// including soft-deleted" (export_data.go:112-116), which on a real app
// means auth battery tables: users.password_hash, 2FA secrets, api-token
// hashes, active sessions. All of it lands in 0644 files inside a 0755 dir,
// so every local co-user of the host the export ran on can read the full
// credential dump. The repo already pins 0600 for every other artifact in
// this family; the export path is the outlier.
//
// Fix direction: MkdirAll(dir, 0o700) and write the ndjson/manifest files
// 0600 (the import side only reads, so no importer change is needed).
//
// Severity: high. ExportData is a shipped product surface (host apps call it
// for data portability), the dumps are deliberately raw, and the export dir
// location is caller-chosen (often a shared scratch path or an attachments
// dir). Existing pins cover export name traversal (registry_red_test) and
// fidelity — neither pins modes.

package framework

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportDataRedRestrictiveModes(t *testing.T) {
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
			"sessions on a real app) sits group/world-listable. Fix: MkdirAll 0700 at export_data.go:134.",
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
			t.Errorf("SECURITY: exported file %s is mode %o — every local co-user reads the raw credential-bearing dump. "+
				"Fix: write ndjson/manifest 0600 (export_data.go:573 writeNDJSON, :190 manifest).",
				ent.Name(), fi.Mode().Perm())
		}
	}
}
