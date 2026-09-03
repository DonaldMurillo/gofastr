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

// locBadgeDrift reports how far a badge is from the measured count as a
// percentage of the measurement, and whether that is within the 20%
// tolerance. A zero measurement is never ok: the badge names a directory
// with no non-test Go.
func locBadgeDrift(badge, got int) (pct float64, ok bool) {
	if got == 0 {
		return 100, false
	}
	pct = float64(badge-got) / float64(got) * 100
	if pct < 0 {
		pct = -pct
	}
	return pct, pct <= 20
}

func TestLoCBadgeDriftBoundary(t *testing.T) {
	cases := []struct {
		badge, got int
		ok         bool
	}{
		{100, 0, false},  // nothing measured
		{120, 100, true}, // exactly 20% over
		{80, 100, true},  // exactly 20% under
		{121, 100, false},
		{79, 100, false},
		{100, 100, true},
	}
	for _, c := range cases {
		if _, ok := locBadgeDrift(c.badge, c.got); ok != c.ok {
			t.Errorf("locBadgeDrift(%d, %d) ok = %v, want %v", c.badge, c.got, ok, c.ok)
		}
	}
}

func TestExampleLoCBadgesMatchTree(t *testing.T) {
	page := body(t, "/examples")
	for slug, badge := range exampleLoC {
		got := measureExampleLoC(t, slug)
		if got == 0 {
			t.Errorf("%s: no non-test Go under examples/%s", slug, slug)
			continue
		}
		if pct, ok := locBadgeDrift(badge, got); !ok {
			t.Errorf("%s: badge says ~%d LoC, tree has %d (%.0f%% off) — update exampleLoC", slug, badge, got, pct)
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
