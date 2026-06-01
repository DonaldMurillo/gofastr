package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMigrateDiff_PrintsChanges exercises the migrate diff CLI end-to-end:
// install the binary, point it at a SQLite DB whose schema diverges from
// the entities/*.json, and assert the printed output contains the ALTER
// statements. Skips if gofastr isn't on PATH.
func TestMigrateDiff_PrintsChanges(t *testing.T) {
	bin, err := exec.LookPath("gofastr")
	if err != nil {
		t.Skip("gofastr binary not on PATH (run `go install ./cmd/gofastr`)")
	}

	dir := t.TempDir()
	// Live SQLite DB with a 1-column posts table.
	dbPath := filepath.Join(dir, "live.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed schema: %v", err)
	}
	db.Close()

	// Entities directory declaring an extra field the live DB lacks.
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	declJSON := `{
		"name": "posts",
		"table": "posts",
		"fields": [
			{"name": "title", "type": "string", "required": true},
			{"name": "views", "type": "int"}
		]
	}`
	if err := os.WriteFile(filepath.Join(entDir, "posts.json"), []byte(declJSON), 0o644); err != nil {
		t.Fatalf("write declaration: %v", err)
	}

	// Invoke `gofastr migrate diff` against the live DB. Run with cwd=dir
	// so relative entity paths and the sqlite file resolve correctly.
	cmd := exec.Command(bin, "migrate", "diff",
		"--db-url=file:"+dbPath,
		"--entities=entities",
	)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gofastr migrate diff: %v\n%s", err, string(out))
	}
	s := string(out)
	// Should print one ADD COLUMN change for the missing `views` field.
	if !strings.Contains(s, "posts: add column views") {
		t.Fatalf("expected ADD COLUMN summary, got:\n%s", s)
	}
	if !strings.Contains(s, `ALTER TABLE "posts" ADD COLUMN "views"`) {
		t.Fatalf("expected SQL fragment, got:\n%s", s)
	}
}

// TestMigrateDiff_ApplyExecutesChanges runs `migrate diff --apply` and then
// verifies the column actually landed in the live DB.
func TestMigrateDiff_ApplyExecutesChanges(t *testing.T) {
	bin, err := exec.LookPath("gofastr")
	if err != nil {
		t.Skip("gofastr binary not on PATH")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	db.Close()

	entDir := filepath.Join(dir, "entities")
	_ = os.MkdirAll(entDir, 0o755)
	_ = os.WriteFile(filepath.Join(entDir, "posts.json"), []byte(`{
		"name":"posts","table":"posts",
		"fields":[{"name":"title","type":"string","required":true},{"name":"views","type":"int"}]
	}`), 0o644)

	cmd := exec.Command(bin, "migrate", "diff",
		"--db-url=file:"+dbPath,
		"--entities=entities",
		"--apply",
	)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply: %v\n%s", err, string(out))
	}

	// Now `views` must be insertable.
	db2, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if _, err := db2.Exec("INSERT INTO posts(id, title, views) VALUES (?, ?, ?)", "p1", "hi", 7); err != nil {
		t.Fatalf("post-apply insert: %v", err)
	}
}

// TestMigrateDiff_RefusesDestructiveByDefault runs `migrate diff --apply`
// against a live DB with an extra column the entity no longer declares. The
// default must refuse the DROP and leave the column intact; --allow-destructive
// must then drop it.
func TestMigrateDiff_RefusesDestructiveByDefault(t *testing.T) {
	bin, err := exec.LookPath("gofastr")
	if err != nil {
		t.Skip("gofastr binary not on PATH")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Live table has a legacy column the entity won't declare.
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, legacy TEXT)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	db.Close()

	entDir := filepath.Join(dir, "entities")
	_ = os.MkdirAll(entDir, 0o755)
	_ = os.WriteFile(filepath.Join(entDir, "posts.json"), []byte(`{
		"name":"posts","table":"posts",
		"fields":[{"name":"title","type":"string","required":true}]
	}`), 0o644)

	// Default --apply: must refuse and exit non-zero.
	cmd := exec.Command(bin, "migrate", "diff", "--db-url=file:"+dbPath, "--entities=entities", "--apply")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit refusing the destructive change, got:\n%s", out)
	}
	if !strings.Contains(string(out), "destructive") {
		t.Fatalf("expected a destructive-refusal message, got:\n%s", out)
	}
	// Column must still be there.
	db2, _ := sql.Open("sqlite3", dbPath)
	var n int
	_ = db2.QueryRow("SELECT COUNT(*) FROM pragma_table_info('posts') WHERE name='legacy'").Scan(&n)
	db2.Close()
	if n != 1 {
		t.Fatalf("legacy column should survive the refused apply, found %d", n)
	}

	// --allow-destructive: now it drops.
	cmd2 := exec.Command(bin, "migrate", "diff", "--db-url=file:"+dbPath, "--entities=entities", "--apply", "--allow-destructive")
	cmd2.Dir = dir
	if out, err := cmd2.CombinedOutput(); err != nil {
		t.Fatalf("allow-destructive apply failed: %v\n%s", err, out)
	}
	db3, _ := sql.Open("sqlite3", dbPath)
	defer db3.Close()
	_ = db3.QueryRow("SELECT COUNT(*) FROM pragma_table_info('posts') WHERE name='legacy'").Scan(&n)
	if n != 0 {
		t.Fatal("legacy column should be dropped after --allow-destructive")
	}
}
