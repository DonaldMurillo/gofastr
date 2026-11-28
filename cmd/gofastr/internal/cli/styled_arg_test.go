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

// The status helpers and the scrubbing prints (Printf, Print, Println,
// Fprintf, Fprintln) scrub every argument, so an argument styled with
// Bold/Green/Red/Yellow/Dim loses its escapes and, on a terminal, prints
// "[1m…[0m" as text. Styling belongs in the format or the helper. This
// walks the CLI's source for a styled argument handed to one.
func TestNoStyledArgumentReachesAStatusHelper(t *testing.T) {
	_, self, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(self), "..", "..") // cmd/gofastr
	// helpers maps each scrubbing helper to the index of its first value
	// argument, past the writer and the format.
	helpers := map[string]int{"Success": 1, "Fail": 1, "UsageFail": 1, "Info": 1, "Warn": 1, "InfoString": 1,
		"Printf": 1, "Print": 0, "Println": 0, "Fprintf": 2, "Fprintln": 1}
	styles := map[string]bool{"Bold": true, "Green": true, "Red": true, "Yellow": true, "Dim": true}
	fset := token.NewFileSet()
	out, err := parser.ParseFile(fset, filepath.Join(filepath.Dir(self), "output.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range out.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || !fn.Name.IsExported() || fn.Recv != nil || len(fn.Type.Params.List) == 0 {
			continue
		}
		params := fn.Type.Params.List
		if _, variadic := params[len(params)-1].Type.(*ast.Ellipsis); !variadic {
			continue
		}
		first := 0
		for _, p := range params[:len(params)-1] {
			first += len(p.Names)
		}
		if got, ok := helpers[fn.Name.Name]; !ok || got != first {
			t.Errorf("output.go's %s takes its values from argument %d; list it in helpers with that index (have %d, listed %v)", fn.Name.Name, first, got, ok)
		}
	}
	cliName := func(e ast.Expr, local bool) string {
		switch f := e.(type) {
		case *ast.SelectorExpr:
			if x, ok := f.X.(*ast.Ident); ok && x.Name == "cli" {
				return f.Sel.Name
			}
		case *ast.Ident:
			if local {
				return f.Name
			}
		}
		return ""
	}
	files := 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
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
			if !ok {
				return true
			}
			first, ok := helpers[cliName(call.Fun, local)]
			if !ok || len(call.Args) <= first {
				return true
			}
			for _, a := range call.Args[first:] {
				ast.Inspect(a, func(m ast.Node) bool {
					if c, ok := m.(*ast.CallExpr); ok && styles[cliName(c.Fun, local)] {
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
