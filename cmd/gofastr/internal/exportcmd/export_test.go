package exportcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportOptions(t *testing.T) {
	opts, err := parseExportOptions([]string{"--pkg", "./cmd/web", "--base=/docs", "out"})
	if err != nil || opts != (exportOptions{pkg: "./cmd/web", base: "/docs", dir: "out"}) {
		t.Fatalf("opts = %+v, err %v", opts, err)
	}
	for _, args := range [][]string{
		{},                         // no directory
		{"a", "b"},                 // two directories
		{"--base", "docs", "out"},  // relative base
		{"--base=/docs/", "out"},   // trailing slash
		{"--pkg", "--base", "out"}, // flag as a value
		{"--force", "out"},
	} {
		if _, err := parseExportOptions(args); err == nil {
			t.Errorf("%q parsed", args)
		}
	}
}

// TestExportRunsTheApp runs `gofastr export` against a fixture app and
// reads the site it wrote: both pages under the base path and the
// search battery's index beside them.
func TestExportRunsTheApp(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles and runs an app")
	}
	dir := t.TempDir()
	RunExport([]string{"--pkg", "./testdata/exportapp", "--base", "/sub", dir})
	about, err := os.ReadFile(filepath.Join(dir, "about", "index.html"))
	if err != nil {
		t.Fatalf("about page missing: %v", err)
	}
	if !strings.Contains(string(about), "Wombats") || !strings.Contains(string(about), `"/sub/__gofastr/`) {
		t.Errorf("about page = %.300s", about)
	}
	if _, err := os.Stat(filepath.Join(dir, "__gofastr", "search", "manifest.json")); err != nil {
		t.Errorf("search index missing: %v", err)
	}
}
