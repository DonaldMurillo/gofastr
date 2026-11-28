package appgen

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestStylePatternsSkipTheTreesGoSkips(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{"web", "vendor/x", "testdata/x", "node_modules/x", ".hidden", "_draft", "web/sub"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	dirs, err := stylePatternDirsAt(base, []string{"./..."})
	if err != nil {
		t.Fatal(err)
	}
	var rel []string
	for _, d := range dirs {
		r, _ := filepath.Rel(base, d)
		rel = append(rel, filepath.ToSlash(r))
	}
	if want := []string{".", "web", "web/sub"}; !slices.Equal(rel, want) {
		t.Fatalf("dirs = %v, want %v", rel, want)
	}
	for _, pat := range []string{"../...", "../x", "/abs"} {
		if _, err := stylePatternDirsAt(base, []string{pat}); err == nil || !strings.Contains(err.Error(), "working directory") {
			t.Errorf("pattern %q: err = %v, want a refusal naming the working directory", pat, err)
		}
	}
}
