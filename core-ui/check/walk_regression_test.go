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

// The combined walk the CLI uses must answer both detectors from one
// walk, so the two can never disagree about build-output directories:
// dist is skipped for scripts exactly as it is for styles, by the
// shared sourcewalk exclusion rather than one lane's private skip.
func TestCombinedInlineWalkSkipsBuildOutputForBothLanes(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dist"), 0o700); err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "dist")
	const src = "package app\nvar page = `<div style=\"display:none\"></div>`\nvar s = `<script>var x = 1;</script>`\n"
	if err := os.WriteFile(filepath.Join(dist, "bad.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	scripts, styles, err := LintNoInlineRecursive(root)
	if err != nil || len(scripts.Violations) != 0 || len(styles.Violations) != 0 {
		t.Fatalf("build outputs were scanned: scripts=%+v styles=%+v err=%v", scripts, styles, err)
	}
	scripts, styles, err = LintNoInlineRecursive(dist)
	if err != nil || len(scripts.Violations) != 1 || len(styles.Violations) != 1 {
		t.Fatalf("explicit output-named root skipped: scripts=%+v styles=%+v err=%v", scripts, styles, err)
	}
}
