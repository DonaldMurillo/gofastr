package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// layoutProps are the layout kinds' props validateAppSpecLayoutBlock
// checks; every other prop renderAppSpecCatalogBlock reads through
// AppSpecProp is text, which validation must refuse a number for.
var layoutProps = map[string]bool{"gap": true, "min": true, "align": true, "justify": true}

// Every prop the generator reads as text off a catalog block is in
// appspec's text table: a key missing from it lets `heading: 2026` load
// and the heading vanish from the page.
func TestCatalogTextPropsCoverGenerator(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "appspec.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var render *ast.FuncDecl
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == "renderAppSpecCatalogBlock" {
			render = fn
		}
	}
	if render == nil {
		t.Fatal("renderAppSpecCatalogBlock not found in appspec.go")
	}
	checked := 0
	ast.Inspect(render.Body, func(n ast.Node) bool {
		clause, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		var kinds []string
		for _, e := range clause.List {
			if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				k, _ := strconv.Unquote(lit.Value)
				if appspec.AppSpecCatalogKind(k) {
					kinds = append(kinds, k)
				}
			}
		}
		for _, stmt := range clause.Body {
			ast.Inspect(stmt, func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "AppSpecProp" {
					return true
				}
				lit, ok := call.Args[1].(*ast.BasicLit)
				if !ok {
					return true
				}
				key, _ := strconv.Unquote(lit.Value)
				if layoutProps[key] {
					return true
				}
				for _, kind := range kinds {
					checked++
					if !slices.Contains(appspec.CatalogTextProps(kind), key) {
						t.Errorf("the generator reads %s prop %q as text, and appspec's catalogTextProps does not list it", kind, key)
					}
				}
				return true
			})
		}
		return true
	})
	if checked < 20 {
		t.Fatalf("checked %d kind/prop pairs; the scan no longer reads renderAppSpecCatalogBlock", checked)
	}
}
