//go:build linux && (amd64 || arm64)

package linux

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

//gofastr:test-reads activation_linux.go

// This is a compiler/source contract, not a native stack-growth stress test.
// KeepAlive prevents collection, but does not itself move stack allocations to
// the heap. Preserve the explicit escape boundaries used by the pure-Go FFI.
func TestActivationPointerEscapeContract(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "activation_linux.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			functions[function.Name.Name] = function
		}
	}
	for function, directive := range map[string]string{
		"activationCall":       "//go:uintptrescapes",
		"activationStringCall": "//go:uintptrescapes",
		"activationCString":    "//go:noinline",
	} {
		declaration := functions[function]
		if declaration == nil || declaration.Doc == nil {
			t.Fatalf("%s is missing its explicit pointer-lifetime contract", function)
		}
		found := false
		for _, comment := range declaration.Doc.List {
			found = found || comment.Text == directive
		}
		if !found {
			t.Errorf("%s must retain %s (or replace this contract with equivalent heap-lifetime evidence)", function, directive)
		}
	}
	tuple := functions["activationTuple"]
	if tuple == nil {
		t.Fatal("activationTuple is missing")
	}
	foundDirect := false
	ast.Inspect(tuple.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		function, ok := call.Fun.(*ast.Ident)
		if !ok || function.Name != "activationCall" {
			return true
		}
		name, ok := call.Args[0].(*ast.BasicLit)
		if !ok || name.Value != `"g_variant_new_tuple"` {
			return true
		}
		if literal, ok := call.Args[1].(*ast.BasicLit); ok && literal.Value == "0" {
			return true // Empty tuples have no Go backing array.
		}
		conversion, ok := call.Args[1].(*ast.CallExpr)
		if !ok || len(conversion.Args) != 1 {
			t.Error("tuple backing pointer must be converted directly in annotated activationCall, not stored as uintptr")
			return true
		}
		kind, ok := conversion.Fun.(*ast.Ident)
		pointer, pointerOK := conversion.Args[0].(*ast.CallExpr)
		if !ok || kind.Name != "uintptr" || !pointerOK || len(pointer.Args) != 1 {
			t.Error("tuple backing pointer must use uintptr(unsafe.Pointer(...)) directly in annotated activationCall")
			return true
		}
		selector, ok := pointer.Fun.(*ast.SelectorExpr)
		if ok {
			qualifier, qualifierOK := selector.X.(*ast.Ident)
			foundDirect = qualifierOK && qualifier.Name == "unsafe" && selector.Sel.Name == "Pointer"
		}
		return true
	})
	if !foundDirect {
		t.Error("no direct tuple backing-array conversion reaches annotated activationCall")
	}
}
