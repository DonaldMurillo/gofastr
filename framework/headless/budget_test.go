package headless

// Per-module gzip size budgets for the behaviour modules the
// pattern port registered (headless-tree, headless-sortablelist,
// headless-multiselect). The retired core-ui/runtime modules carried
// theirs in TestRuntimeModule_Tree and siblings; those checks died
// with the modules, and the runtime's own budget walk holds every
// registered behaviour to a blanket 3 KB goal — this file adds the
// per-module rows the port's brief asked for, measured the way the
// walk measures: the production minifier, then gzip at the level
// browsers actually receive.
//
// The same rule the runtime's budget_test.go states applies: never
// raise a row to silence a regression — split or shrink the module
// instead. sortablelist sits close to the shared 3 KB walk line
// already, so its row is the tripwire that turns a slow drift into a
// named failure before the walk catches it generically.

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

// behaviorGzipSize minifies the module source the way serving does
// and returns its gzip size at the default level.
func behaviorGzipSize(t *testing.T, file string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatalf("gzip writer: %v", err)
	}
	if _, err := w.Write([]byte(minify.Minify(string(raw)))); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Len()
}

// TestBehaviorModuleSizeBudgets holds each pattern-port module to its
// row: a named ceiling over the measured size, tight enough that a
// careless addition fails here first. Measured 2026-09-24, the day
// the conflict toast returned to sortablelist: tree 1347 (row 1536,
// 189 bytes of room), sortablelist 3027 (row 3072, the walk's own
// line — no extra room to give), multiselect 865 (row 1024, 159
// bytes of room).
func TestBehaviorModuleSizeBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		file   string
		budget int
	}{
		{TreeBehaviorName, "tree.js", 1536},
		{SortableListBehaviorName, "sortablelist.js", 3072},
		{MultiSelectBehaviorName, "multiselect.js", 1024},
	} {
		got := behaviorGzipSize(t, tc.file)
		t.Logf("%s (%s) gzip = %d bytes (budget %d)", tc.name, tc.file, got, tc.budget)
		if got > tc.budget {
			t.Errorf("%s gzip = %d bytes — exceeds %d byte budget: split or shrink the module", tc.name, got, tc.budget)
		}
	}
}
