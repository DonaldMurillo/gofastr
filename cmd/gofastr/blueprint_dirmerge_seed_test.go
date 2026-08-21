package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Directory mode (--from=blueprints/) loads every blueprint file in a dir and
// folds them with mergeBlueprints. Seed was the only slice field that
// mergeBlueprints dropped, so a `seed:` block in any file but the last was
// silently discarded. Demos shipped empty with no error.
func TestDirMergePreservesSeedFromEveryFile(t *testing.T) {
	dir := t.TempDir()
	bpDir := filepath.Join(dir, "bp")
	if err := os.MkdirAll(bpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bpDir, "a.yml"), []byte(`app:
  name: Demo
  module: ex.com/demo
entities:
  - name: posts
    fields:
      - name: title
        type: string
seed:
  - entity: posts
    rows:
      - title: seeded post
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bpDir, "b.yml"), []byte(`entities:
  - name: tags
    fields:
      - name: label
        type: string
seed:
  - entity: tags
    rows:
      - label: seeded tag
`), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := loadBlueprintPath(bpDir, false)
	if err != nil {
		t.Fatalf("dir merge: %v", err)
	}
	if len(bp.Seed) != 2 {
		t.Fatalf("directory merge dropped seed: want 2 seed entities, got %d (%+v)", len(bp.Seed), bp.Seed)
	}
	entities := map[string]bool{}
	for _, s := range bp.Seed {
		entities[s.Entity] = true
	}
	if !entities["posts"] || !entities["tags"] {
		t.Fatalf("directory merge lost a seed entity: got %v", entities)
	}
}
