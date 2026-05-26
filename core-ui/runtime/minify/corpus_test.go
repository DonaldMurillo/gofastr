package minify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMinifyRuntimeCorpus runs every embedded runtime source through
// the minifier and asserts:
//
//   - output is non-empty
//   - output is strictly smaller than input
//   - the minifier is idempotent: Minify(Minify(x)) == Minify(x)
//   - well-known anchor strings survive (sanity that we didn't truncate)
//
// We deliberately do NOT compare raw punct counts against the input —
// comments and string literals contain `[`, `(`, `{` characters that
// legitimately disappear during minification. Idempotency is the
// stronger invariant: the minifier must reach a fixed point in one
// pass.
func TestMinifyRuntimeCorpus(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}

	type file struct {
		path    string
		anchors []string
	}
	files := []file{
		{
			path: filepath.Join(root, "core-ui/runtime/runtime.js"),
			anchors: []string{
				// Identifiers and string literals — comments are stripped.
				"data-fui-os",
				"__gofastr",
			},
		},
	}
	srcDir := filepath.Join(root, "core-ui/runtime/src")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read src dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		files = append(files, file{path: filepath.Join(srcDir, e.Name())})
	}

	for _, f := range files {
		name := filepath.Base(f.path)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			src := string(raw)
			out := Minify(src)
			if out == "" {
				t.Fatalf("%s: minifier returned empty", name)
			}
			if len(out) >= len(src) {
				t.Errorf("%s: did not shrink (in=%d out=%d)", name, len(src), len(out))
			}
			if out2 := Minify(out); out2 != out {
				t.Errorf("%s: not idempotent (len1=%d len2=%d)", name, len(out), len(out2))
			}
			for _, a := range f.anchors {
				if !strings.Contains(out, a) {
					t.Errorf("%s: anchor %q missing from minified output", name, a)
				}
			}
		})
	}
}

// repoRoot walks upward from the package dir to find the gofastr
// repo root (the dir holding go.mod). The minify package lives at
// core-ui/runtime/minify so the root is three levels up.
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}
