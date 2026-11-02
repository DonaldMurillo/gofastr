package cli

//gofastr:test-reads cmd/gofastr/**/*.go

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The status helpers scrub every argument, so an argument styled with
// Bold/Green/Red/Yellow/Dim loses its escapes and, on a terminal, prints
// "[1m…[0m" as text. Styling belongs in the format or the helper. This
// walks the CLI's source for a styled argument handed to one.
func TestNoStyledArgumentReachesAStatusHelper(t *testing.T) {
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(self), "..", "..") // cmd/gofastr
	helpers := map[string]bool{"Success": true, "Fail": true, "Info": true, "Warn": true, "InfoString": true}
	styles := map[string]bool{"Bold": true, "Green": true, "Red": true, "Yellow": true, "Dim": true}
	isCLI := func(e ast.Expr, names map[string]bool, local bool) bool {
		switch f := e.(type) {
		case *ast.SelectorExpr:
			x, ok := f.X.(*ast.Ident)
			return ok && x.Name == "cli" && names[f.Sel.Name]
		case *ast.Ident:
			return local && names[f.Name]
		}
		return false
	}
	fset := token.NewFileSet()
	files := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return nil // a template fragment, not this repo's code
		}
		files++
		local := f.Name.Name == "cli"
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isCLI(call.Fun, helpers, local) || len(call.Args) < 2 {
				return true
			}
			for _, a := range call.Args[1:] {
				ast.Inspect(a, func(m ast.Node) bool {
					if c, ok := m.(*ast.CallExpr); ok && isCLI(c.Fun, styles, local) {
						t.Errorf("%s: a styled argument goes to a status helper, which scrubs its escapes; put the styling in the format", fset.Position(c.Pos()))
					}
					return true
				})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 {
		t.Fatalf("setup broken: walked only %d Go files under %s", files, root)
	}
}
