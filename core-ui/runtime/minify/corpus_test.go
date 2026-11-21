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
//   - selected modules keep representative anchors (sanity against truncation)
//
// We deliberately do NOT compare raw punct counts against the input,
// comments and string literals contain `[`, `(`, `{` characters that
// legitimately disappear during minification. Idempotency is the
// stronger invariant: the minifier must reach a fixed point in one
// pass.
//
//gofastr:test-reads core-ui/runtime/src/
func TestMinifyRuntimeCorpus(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}

	anchors := map[string]string{
		"envelope.js": "runtime.load_failed_status",
		"rpc.js":      "data-cui-rpc-body",
		"sse.js":      "gofastr:sse-status",
	}
	var files []string
	srcDir := filepath.Join(root, "core-ui/runtime/src")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("read src dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		files = append(files, e.Name())
	}
	if len(files) == 0 {
		t.Fatalf("no .js modules under %s: the corpus would check nothing", srcDir)
	}

	for _, name := range files {
		anchor := anchors[name]
		delete(anchors, name)
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(srcDir, name))
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
			if anchor != "" && !strings.Contains(out, anchor) {
				t.Errorf("%s: anchor %q missing from minified output", name, anchor)
			}
		})
	}
	for name := range anchors {
		t.Errorf("no corpus module %s for anchor check", name)
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
	for range 8 {
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
