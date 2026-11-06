package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBindDocumentsDestinationError(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "bind.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	doc := ""
	for _, declaration := range source.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "Bind" {
			doc = function.Doc.Text()
		}
	}
	if !strings.Contains(doc, "non-nil pointer") || strings.Contains(doc, "panics") {
		t.Errorf("Bind must document a destination error, not a panic: %s", doc)
	}
	for _, destination := range []any{nil, 1, (*struct{})(nil)} {
		if err := Bind(httptest.NewRequest("GET", "/", nil), destination); err == nil {
			t.Errorf("invalid destination %T returned no error", destination)
		}
	}
}
