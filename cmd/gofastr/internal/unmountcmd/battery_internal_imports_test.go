package unmountcmd

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

// Batteries that already import a gofastr internal/ package, so unmount
// refuses them. Every other battery stays unmountable: a shared helper
// it needs is copied into it and pinned to the internal package by a
// parity test, never imported (framework/docs/content/unmount.md).
var batteriesImportingInternal = map[string]bool{
	"admin": true, "cache": true, "desktop": true, "email": true,
	"log": true, "rtc": true, "search": true, "semantic": true,
	"service": true, "sitesearch": true, "storage": true, "telemetry": true,
}

//gofastr:test-reads battery/
func TestUnmountableBatteriesImportNoInternal(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "battery")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	internal := project.GofastrModule + "/internal/"
	for _, e := range entries {
		if !e.IsDir() || batteriesImportingInternal[e.Name()] {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, e.Name()), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range f.Imports {
				if p, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(p, internal) {
					t.Errorf("%s imports %s: battery/%s would stop being unmountable; copy the helper and pin it with a parity test", path, p, e.Name())
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
