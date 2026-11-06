package check

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRecursiveStylesSkipBuildOutput(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"dist", "bin", "build", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "bad.go"), []byte("package app\nvar page = `<div style=\"display:none\"></div>`\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := LintNoInlineStylesRecursive(root)
	if err != nil || len(result.Violations) != 0 {
		t.Fatalf("build outputs were scanned: result=%+v err=%v", result, err)
	}
	result, err = LintNoInlineStylesRecursive(filepath.Join(root, "dist"))
	if err != nil || len(result.Violations) != 1 {
		t.Fatalf("explicit output-named root skipped: result=%+v err=%v", result, err)
	}
}

func TestBehaviorDiscoverySkipsBuildOutput(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"dist", "bin", "build", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "bad.go"), []byte("package bad\nfunc { RegisterBehavior\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := RegisteredBehaviorSources(root)
	if err != nil || len(paths) != 0 {
		t.Fatalf("generated behavior parsed: paths=%v err=%v", paths, err)
	}
}
