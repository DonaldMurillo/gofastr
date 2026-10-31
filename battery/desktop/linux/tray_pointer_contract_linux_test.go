//go:build linux && (amd64 || arm64)

package linux

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

//gofastr:test-reads tray_variant_linux.go

// Source-only companion to TestActivationPointerEscapeContract. No native
// stack-growth diagnostic is needed to preserve the explicit escape boundary.
func TestTrayPointerEscapeContract(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "tray_variant_linux.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			functions[function.Name.Name] = function
		}
	}
	call := functions["trayCall"]
	annotated := false
	if call != nil && call.Doc != nil {
		for _, comment := range call.Doc.List {
			annotated = annotated || comment.Text == "//go:uintptrescapes"
		}
	}
	if !annotated {
		t.Fatal("trayCall must retain its explicit //go:uintptrescapes contract")
	}
	for function, index := range map[string]int{"trayTuple": 1, "trayArray": 2, "trayBytes": 2} {
		declaration := functions[function]
		if declaration == nil {
			t.Fatalf("%s is missing", function)
		}
		found := false
		ast.Inspect(declaration.Body, func(node ast.Node) bool {
			native, ok := node.(*ast.CallExpr)
			if !ok || len(native.Args) <= index {
				return true
			}
			callee, ok := native.Fun.(*ast.Ident)
			if !ok || callee.Name != "trayCall" {
				return true
			}
			conversion, ok := native.Args[index].(*ast.CallExpr)
			if !ok || len(conversion.Args) != 1 {
				return true // Empty arrays have no Go backing storage.
			}
			kind, ok := conversion.Fun.(*ast.Ident)
			pointer, pointerOK := conversion.Args[0].(*ast.CallExpr)
			if !ok || kind.Name != "uintptr" || !pointerOK || len(pointer.Args) != 1 {
				return true
			}
			selector, ok := pointer.Fun.(*ast.SelectorExpr)
			if ok {
				qualifier, qualifierOK := selector.X.(*ast.Ident)
				found = found || (qualifierOK && qualifier.Name == "unsafe" && selector.Sel.Name == "Pointer")
			}
			return true
		})
		if !found {
			t.Errorf("%s must convert its Go backing pointer directly inside annotated trayCall", function)
		}
	}
}
