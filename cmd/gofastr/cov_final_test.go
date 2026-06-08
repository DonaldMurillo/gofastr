package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// ── blueprint loaders ─────────────────────────────────────────────────

func TestDecodeBlueprintFileJSONAndBadExt(t *testing.T) {
	dir := t.TempDir()
	jsonBP := filepath.Join(dir, "bp.json")
	body := `{"app":{"name":"D","module":"ex.com/d"},"entities":[{"name":"users","fields":[{"name":"email","type":"string"}]}]}`
	if err := os.WriteFile(jsonBP, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeBlueprintFile(jsonBP); err != nil {
		t.Fatalf("json blueprint: %v", err)
	}
	// Bad JSON.
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeBlueprintFile(bad); err == nil {
		t.Fatal("malformed json should error")
	}
	// Non-blueprint extension.
	if _, err := decodeBlueprintFile(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("non-blueprint extension should error")
	}
}

func TestLoadBlueprintPathDirectoryMerge(t *testing.T) {
	dir := t.TempDir()
	bpDir := filepath.Join(dir, "bp")
	if err := os.MkdirAll(bpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bpDir, "a.gofastr.yml"), []byte("app:\n  name: A\n  module: ex.com/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bpDir, "b.gofastr.yml"), []byte("entities:\n  - name: users\n    fields:\n      - name: email\n        type: string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := loadBlueprintPath(bpDir, false)
	if err != nil {
		t.Fatalf("dir merge: %v", err)
	}
	if bp.App.Name != "A" || len(bp.Entities) != 1 {
		t.Fatalf("merged blueprint = %+v", bp)
	}
}

// ── migrate diff dry-run + apply + destructive ────────────────────────

func TestRunMigrateDiffApplyAndDestructive(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	dbPath := filepath.Join(dir, "live.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// Live DB has an extra column the declaration drops → destructive.
	if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, legacy TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	bp := filepath.Join(dir, "gofastr.yml")
	blueprint := `app:
  name: testapp
entities:
  - name: posts
    table: posts
    fields:
      - name: title
        type: string
      - name: views
        type: int
`
	if err := os.WriteFile(bp, []byte(blueprint), 0o644); err != nil {
		t.Fatal(err)
	}
	// Plain diff (prints destructive warning + would-apply hint).
	out := covT_capStdout(t, func() {
		runMigrateDiff([]string{"--db-url=file:" + dbPath, "--from=" + bp})
	})
	if !strings.Contains(out, "change") {
		t.Fatalf("diff output: %s", out)
	}
	// Apply with --allow-destructive succeeds.
	covT_capStdout(t, func() {
		runMigrateDiff([]string{"--db-url=file:" + dbPath, "--from=" + bp, "--apply", "--allow-destructive"})
	})
}

func TestRunMigrateGenerateNoDownNote(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	bp := filepath.Join(dir, "gofastr.yml")
	blueprint := `app:
  name: testapp
entities:
  - name: posts
    table: posts
    fields:
      - name: title
        type: string
`
	if err := os.WriteFile(bp, []byte(blueprint), 0o644); err != nil {
		t.Fatal(err)
	}
	// postgres driver path → DialectPostgres branch.
	covT_capStdout(t, func() { runMigrateGenerate([]string{"create", "--from=" + bp, "--driver=postgres"}) })
	matches, _ := filepath.Glob(filepath.Join(dir, "migrations", "0001_*.sql"))
	if len(matches) == 0 {
		t.Fatal("no migration generated")
	}
}

// ── runBuild with a discovered codegen config ─────────────────────────

func TestRunBuildWithCodegenConfig(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bcfg\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entDir, "posts.json"), []byte(`{"name":"posts","fields":[{"name":"t","type":"string"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A discovered config triggers the discovery.Found branch in runBuild.
	cfg := "version: 1\ncodegen:\n  output: .gen\n  generators:\n    - name: go/entities\n      source:\n        type: json_dir\n        path: entities\n"
	if err := os.WriteFile(filepath.Join(dir, "gofastr.codegen.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := covT_capStdout(t, func() {
		_ = covT_capExit(t, func() { runBuild(nil) })
	})
	if !strings.Contains(out, "Generating") {
		t.Fatalf("expected codegen-config step: %s", out)
	}
}
