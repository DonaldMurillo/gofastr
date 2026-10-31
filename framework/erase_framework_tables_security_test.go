package framework

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// exportRows counts the rows in the archive file for name, failing the
// test when the export has no such file.
func exportRows(t *testing.T, dir, name string) int {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name+".ndjson"))
	if err != nil {
		t.Errorf("archive has no %s: %v", name, err)
		return 0
	}
	defer f.Close()
	n := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		n++
	}
	return n
}

// Erasing a user cuts them from their queued bulk runs: the creator is
// tombstoned, so the run's Principal no longer rebuilds the erased
// user's context, and another user's run is untouched (S26). A data
// export carries the jobs and their snapshot ids.
func TestEraseCutsBulkJobCreator(t *testing.T) {
	app, db := newEraseTestApp(t)
	ctx := context.Background()
	s, err := newSQLBulkStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	theirs := bulkStoreJob("j2", "k2", 1)
	theirs.Creator = "u2"
	if _, err := s.Create(ctx, bulkStoreJob("j1", "k1", 1), []string{"r1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, theirs, []string{"r2"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if n := exportRows(t, dir, "gofastr_bulk_jobs"); n != 2 {
		t.Errorf("export holds %d bulk job(s), want 2", n)
	}
	if n := exportRows(t, dir, "gofastr_bulk_items"); n != 2 {
		t.Errorf("export holds %d bulk item(s), want 2", n)
	}
	if _, err := app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	for id, want := range map[string]bool{"j1": false, "j2": true} {
		j, err := s.Job(ctx, id)
		if err != nil {
			t.Fatalf("Job %s: %v", id, err)
		}
		if got := j.Creator == "u1" || j.Creator == "u2"; got != want {
			t.Errorf("job %s creator = %q after erasing u1", id, j.Creator)
		}
	}
}

// A data export carries the audit trail under its configured name (S32).
func TestExportCarriesAuditLog(t *testing.T) {
	app, db := newEraseTestApp(t)
	ctx := context.Background()
	if err := EnsureAuditTable(db, "change_trail"); err != nil {
		t.Fatal(err)
	}
	if err := AppendAuditEvent(ctx, db, "change_trail", "notes", "create", "n1", "u1", nil); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if n := exportRows(t, dir, "audit_log-change_trail"); n != 1 {
		t.Errorf("export holds %d audit row(s), want 1", n)
	}
}
