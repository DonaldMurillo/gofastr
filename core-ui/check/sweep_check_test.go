package check

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecursiveScriptsSkipDist(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"dist", "app"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
		src := "package app\nvar page = `<script>alert(1)</script>`\n"
		if err := os.WriteFile(filepath.Join(root, dir, "page.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := LintNoInlineScriptsRecursive(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Violations) != 1 || filepath.Base(filepath.Dir(result.Violations[0].File)) != "app" {
		t.Fatalf("want one finding, in app/, got %+v", result.Violations)
	}
}
