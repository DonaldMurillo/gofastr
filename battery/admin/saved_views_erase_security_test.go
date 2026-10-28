package admin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Erasing a user removes their saved views and keeps everyone else's,
// and a data export carries the table.
func TestEraseRemovesSavedViews(t *testing.T) {
	x := newSavedViewsEnv(t)
	mustCreateView(t, ownerCtx("u1", ""), x.store, "Mine")
	mustCreateView(t, ownerCtx("u1", "t1"), x.store, "Tenant copy")
	mustCreateView(t, ownerCtx("u2", ""), x.store, "Theirs")

	count := func(owner string) int {
		t.Helper()
		var n int
		if err := x.db.QueryRow(`SELECT COUNT(*) FROM admin_saved_views WHERE owner = ?`, owner).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	ctx := context.Background()
	dir := t.TempDir()
	if err := x.app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "admin_saved_views.ndjson")); err != nil {
		t.Errorf("the export has no admin_saved_views file: %v", err)
	}
	if _, err := x.app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if n := count("u1"); n != 0 {
		t.Errorf("%d saved view(s) of the erased user survive", n)
	}
	if n := count("u2"); n != 1 {
		t.Errorf("another user's saved views: %d, want 1", n)
	}
}
