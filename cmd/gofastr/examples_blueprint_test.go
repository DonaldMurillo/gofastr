package main

import (
	"path/filepath"
	"testing"
)

// TestExampleBlueprintsLoad validates every examples/<name>/gofastr.yml parses
// and validates cleanly. The blueprint-only examples (ecommerce, lms,
// portfolio, project-manager, real-estate, blog) ship no Go, so nothing else
// in the suite exercises them — a broken blueprint would otherwise rot
// unnoticed. This is their boot test.
func TestExampleBlueprintsLoad(t *testing.T) {
	matches, err := filepath.Glob("../../examples/*/gofastr.yml")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no example blueprints found (run from repo)")
	}
	for _, path := range matches {
		path := path
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			if _, err := loadBlueprint(path); err != nil {
				t.Errorf("%s failed to load: %v", path, err)
			}
		})
	}
}
