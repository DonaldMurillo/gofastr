package mediatype

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestStandaloneFold(t *testing.T) {
	src := StandaloneFold()
	file, err := parser.ParseFile(token.NewFileSet(), "", "package main\n"+src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Decls) != 1 {
		t.Fatalf("got %d declarations, want exactly one: %s", len(file.Decls), src)
	}
	fn, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok || fn.Recv != nil || fn.Name.Name != "foldMediaType" {
		t.Fatalf("want one function named foldMediaType: %s", src)
	}
}
