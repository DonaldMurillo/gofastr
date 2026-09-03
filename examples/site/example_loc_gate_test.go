package main

// Gate for the "~N LoC" badges on /examples. Each badge is a constant in
// exampleLoC; this test measures the non-test Go lines under
// examples/<slug> and fails when a badge is more than 20% off, so the
// badges get re-measured instead of remembered. It also pins that every
// badge slug has a row and that the blueprint-only list stays linked.

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func measureExampleLoC(t *testing.T, slug string) int {
	t.Helper()
	dir := filepath.Join("..", slug)
	total := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			total++
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return total
}

func TestExampleLoCBadgesMatchTree(t *testing.T) {
	page := body(t, "/examples")
	for slug, badge := range exampleLoC {
		got := measureExampleLoC(t, slug)
		if got == 0 {
			t.Errorf("%s: no non-test Go under examples/%s", slug, slug)
			continue
		}
		diff := float64(badge-got) / float64(got)
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.20 {
			t.Errorf("%s: badge says ~%d LoC, tree has %d (%.0f%% off) — update exampleLoC", slug, badge, got, diff*100)
		}
		if !strings.Contains(page, `id="`+slug+`"`) {
			t.Errorf("%s: has a LoC badge but no row on /examples", slug)
		}
	}
	for _, slug := range []string{"lms", "portfolio", "project-manager", "real-estate"} {
		if !strings.Contains(page, "tree/main/examples/"+slug) {
			t.Errorf("/examples blueprint list is missing a source link for %q", slug)
		}
		if _, err := os.Stat(filepath.Join("..", slug, "gofastr.yml")); err != nil {
			t.Errorf("examples/%s/gofastr.yml: %v", slug, err)
		}
	}
}
