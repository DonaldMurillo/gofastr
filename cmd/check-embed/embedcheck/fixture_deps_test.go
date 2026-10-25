package embedcheck

// The fixtures under ../testdata are a module that replaces this repo with
// the working tree, and loadAll type-checks them from source, so these
// packages and everything they import are inputs to every analyzer test.
// The blank imports make that an import edge cmd/affected can see:
// without them a change to core/render, or to anything it imports, never
// selected this package.
import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/core-ui/app"
	_ "github.com/DonaldMurillo/gofastr/core-ui/component"
	_ "github.com/DonaldMurillo/gofastr/core-ui/island"
	_ "github.com/DonaldMurillo/gofastr/core/render"
	_ "github.com/DonaldMurillo/gofastr/framework/embed"
)

// TestFixtureImportsAreTestImports fails when a fixture imports a repo
// package this file does not, the edge the comment above exists for.
func TestFixtureImportsAreTestImports(t *testing.T) {
	const repo = "github.com/DonaldMurillo/gofastr/"
	fset := token.NewFileSet()
	self, err := parser.ParseFile(fset, "fixture_deps_test.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var have []string
	for _, imp := range self.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(p, repo) {
			have = append(have, p)
		}
	}
	err = filepath.WalkDir(fixtureDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil // a fixture that does not parse is some test's subject
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); strings.HasPrefix(p, repo) && !slices.Contains(have, p) {
				t.Errorf("%s imports %s; add it to fixture_deps_test.go's blank imports", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
