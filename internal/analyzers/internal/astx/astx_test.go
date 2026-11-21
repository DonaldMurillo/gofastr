package astx_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
)

// check parses and type-checks src, returning a pass-shaped handle on
// the result.
func check(t *testing.T, src string) (*analysis.Pass, *types.Scope, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "a.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Uses:  map[*ast.Ident]types.Object{},
		Defs:  map[*ast.Ident]types.Object{},
	}
	pkg, err := (&types.Config{}).Check("p", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatal(err)
	}
	return &analysis.Pass{Fset: fset, TypesInfo: info}, pkg.Scope(), f
}

func TestNamedTypeRequiresExplicitAliasUnwrap(t *testing.T) {
	_, scope, _ := check(t, `package p

type Logger struct{}

type L = Logger

var v *L
`)
	alias := scope.Lookup("L").Type()
	if astx.IsNamed(alias, "p", "Logger") || !astx.IsNamed(types.Unalias(alias), "p", "Logger") {
		t.Error("IsNamed must recognize Logger only after unwrapping the alias")
	}
	ptr := scope.Lookup("v").Type()
	if n := astx.NamedOf(ptr); n != nil {
		t.Errorf("NamedOf(*alias) = %v, want nil", n)
	}
}

func TestCalleeFuncRequiresExplicitUnparen(t *testing.T) {
	pass, _, f := check(t, `package p

func f() {}

func main() { (f)() }
`)
	var call *ast.CallExpr
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			call = c
		}
		return true
	})
	if call == nil {
		t.Fatal("no call in fixture")
	}
	if fn, ok := astx.CalleeFunc(pass, call.Fun); ok || fn != nil {
		t.Errorf("CalleeFunc((f)) = %v, %v; want nil, false", fn, ok)
	}
	fn, ok := astx.CalleeFunc(pass, ast.Unparen(call.Fun))
	if !ok || fn == nil || fn.Name() != "f" {
		t.Errorf("CalleeFunc(f) = %v, %v; want f, true", fn, ok)
	}
}
