package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeBP writes a blueprint and loads it, returning the error for inspection.
func loadBPWithEntity(t *testing.T, entityYAML string) (Blueprint, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	bp := "app:\n  name: testapp\nentities:\n" + entityYAML
	if err := os.WriteFile(path, []byte(bp), 0o644); err != nil {
		t.Fatal(err)
	}
	return loadBlueprint(path)
}

// TestRenamesDecodeFromYAML: a rename declared in blueprint YAML reaches
// EntityConfig.Renames, so the schema diff can emit RENAME COLUMN instead of a
// data-losing drop+add. Renames were Go-declaration-only, which left the
// blueprint workflow — the only input the standalone migration generator reads
// — unable to express the difference.
func TestRenamesDecodeFromYAML(t *testing.T) {
	bp, err := loadBPWithEntity(t, `  - name: posts
    table: posts
    renames:
      headline: title
    fields:
      - name: title
        type: string
`)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(bp.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(bp.Entities))
	}
	cfg, err := bp.Entities[0].Config()
	if err != nil {
		t.Fatalf("Config: %v", err)
	}
	if got := cfg.Renames["headline"]; got != "title" {
		t.Errorf("Renames[headline] = %q, want \"title\"", got)
	}
}

// TestRenamesRejectUnsafeIdentifier: both sides of a rename become column names
// in emitted ALTER TABLE DDL, so they must be safe identifiers — a blueprint is
// agent-transcribed text, not developer-authored SQL.
func TestRenamesRejectUnsafeIdentifier(t *testing.T) {
	for _, bad := range []string{
		`      "old\"; DROP TABLE posts; --": title`,
		`      headline: "title\"; DROP TABLE posts; --"`,
	} {
		_, err := loadBPWithEntity(t, `  - name: posts
    table: posts
    renames:
`+bad+`
    fields:
      - name: title
        type: string
`)
		if err == nil {
			t.Errorf("expected rejection for rename %s", strings.TrimSpace(bad))
			continue
		}
		// Must be rejected as an unsafe column name, not merely as an
		// unrecognised key — otherwise this passes before the feature exists.
		if !strings.Contains(err.Error(), "not a safe column name") {
			t.Errorf("rename %s rejected for the wrong reason: %v", strings.TrimSpace(bad), err)
		}
	}
}

// TestRenamesTargetMustBeDeclared: a rename to a column the entity does not
// declare would never fire, so it is a typo worth catching at validate time.
func TestRenamesTargetMustBeDeclared(t *testing.T) {
	_, err := loadBPWithEntity(t, `  - name: posts
    table: posts
    renames:
      headline: nope
    fields:
      - name: title
        type: string
`)
	if err == nil {
		t.Fatal("expected a rename to an undeclared field to be rejected")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should name the undeclared target: %v", err)
	}
}
