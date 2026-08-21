package framework

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// TestExportData_MultiVersionOneFilePerTable (F3): two versions of one entity
// share one physical table, so the archive must emit exactly ONE source
// (<name>.ndjson + one manifest entry) for that table, not one per version.
// Without the union, collectSources iterated AllSorted() and emitted each
// version under the same <name>.ndjson path; the second write clobbered the
// first while the manifest gained two entries, and import either
// checksum-mismatched or double-inserted the same primary keys.
func TestExportData_MultiVersionOneFilePerTable(t *testing.T) {
	db := openSQLiteMem(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2", routegroup.WithMCPNamespace("v2"))
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "summary", Type: schema.Text}, // v2-only column
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if _, err := db.Exec("INSERT INTO posts (id, title, summary) VALUES ('p1', 'hello', 'world')"); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir, WithExportTime(fixedExportTime())); err != nil {
		t.Fatalf("ExportData: %v", err)
	}

	// The manifest must carry exactly ONE posts entry.
	manifest := readManifest(t, dir)
	postsEntries := 0
	for _, e := range manifest.Entities {
		if e.Name == "posts" {
			postsEntries++
		}
	}
	if postsEntries != 1 {
		t.Errorf("expected exactly 1 'posts' manifest entry (one per physical table), got %d", postsEntries)
	}

	// The unioned column set must include the v2-only column.
	entry := manifest.Entities[0]
	hasSummary := false
	for _, c := range entry.Columns {
		if c == "summary" {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Errorf("exported columns missing v2-only 'summary': %v", entry.Columns)
	}

	// Exactly one .ndjson file for the table.
	ndjson := filepath.Join(dir, "posts.ndjson")
	if _, err := os.Stat(ndjson); err != nil {
		t.Errorf("posts.ndjson missing: %v", err)
	}

	// Round-trip: wipe + import restores the row.
	if _, err := db.Exec("DELETE FROM posts"); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if err := app.ImportData(context.Background(), dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}
	var title string
	if err := db.QueryRow("SELECT title FROM posts WHERE id = 'p1'").Scan(&title); err != nil {
		t.Fatalf("row missing after round-trip import: %v", err)
	}
	if title != "hello" {
		t.Errorf("round-trip title = %q, want %q", title, "hello")
	}
}

// compile-time: keep the sql import alive for the Exec calls above.
var _ = sql.ErrNoRows
