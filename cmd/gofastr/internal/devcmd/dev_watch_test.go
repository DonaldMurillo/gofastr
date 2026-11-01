package devcmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeTree writes each path => body under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for f, body := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func watched(root string, w *Watch) []string {
	var got []string
	for p := range w.Scan() {
		rel, _ := filepath.Rel(root, p)
		got = append(got, filepath.ToSlash(rel))
	}
	slices.Sort(got)
	return got
}

// Every directory named gen or dist was skipped, source packages
// included: an internal/gen package, or app spec output generated into
// gen/ that main imports, never triggered a rebuild. Only root-level
// output is skipped, and never a package the build target imports.
func TestWatcherKeepsGenSourcePackages(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":              "module example.com/w\n\ngo 1.22\n",
		"main.go":             "package main\n\nimport (\n\t_ \"example.com/w/gen\"\n\t_ \"example.com/w/internal/gen\"\n)\n\nfunc main() {}\n",
		"gen/api.go":          "package gen\n",
		"internal/gen/m.go":   "package gen\n",
		"web/dist/app.js":     "x",
		"dist/bundle.js":      "x",
		"tmp/scratch.go":      "package scratch\n",
		"node_modules/x/i.js": "x",
		".gofastr/state.md":   "x",
	})
	want := []string{"gen/api.go", "internal/gen/m.go", "main.go", "web/dist/app.js"}
	if got := watched(root, NewWatch(context.Background(), root, ".", nil)); !slices.Equal(got, want) {
		t.Fatalf("watched %v, want %v", got, want)
	}
}

// `gofastr dev --pkg ./gen` builds a main package that lives in gen/:
// the watcher must walk the directory it builds.
func TestWatcherWalksPkgGen(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":      "module example.com/w\n\ngo 1.22\n",
		"gen/main.go": "package main\n\nfunc main() {}\n",
	})
	want := []string{"gen/main.go"}
	if got := watched(root, NewWatch(context.Background(), root, "./gen", nil)); !slices.Equal(got, want) {
		t.Fatalf("watched %v, want %v", got, want)
	}
	// With no go.mod go list fails; the target's directory still counts.
	if err := os.Remove(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	if got := watched(root, NewWatch(context.Background(), root, "./gen", nil)); !slices.Equal(got, want) {
		t.Fatalf("watched %v with go list failing, want %v", got, want)
	}
}

// A package the target imports stays watched while a save leaves the
// tree unbuildable: go list then reports nothing, and the last graph
// stands until the next refresh succeeds.
func TestWatcherKeepsGraphOnBrokenSave(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"go.mod":     "module example.com/w\n\ngo 1.22\n",
		"main.go":    "package main\n\nimport _ \"example.com/w/gen\"\n\nfunc main() {}\n",
		"gen/api.go": "package gen\n",
	})
	w := NewWatch(context.Background(), root, ".", nil)
	// A half-typed import line: go list -e reports main with no imports
	// and exits 0, a graph without gen/. The query runs without -e, so
	// it fails and the last graph stands.
	writeTree(t, root, map[string]string{"main.go": "package main\n\nimport _ \"example.com/w/gen\n\nfunc main() {}\n"})
	w.Refresh()
	if got := watched(root, w); !slices.Contains(got, "gen/api.go") {
		t.Fatalf("watched %v after a half-typed import; gen/api.go dropped", got)
	}
	writeTree(t, root, map[string]string{"go.mod": "not a go.mod"})
	w.Refresh()
	if got := watched(root, w); !slices.Contains(got, "gen/api.go") {
		t.Fatalf("watched %v after a broken save; gen/api.go dropped", got)
	}
}
