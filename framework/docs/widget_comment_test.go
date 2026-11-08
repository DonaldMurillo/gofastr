package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The round-trip test rejects every script URL. Its explanation must
// describe the inline-client assertion a reader will find in its body.
//
//gofastr:test-reads core/mcp/appdoc_test.go
func TestWidgetRoundTripCommentMatchesWire(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "../../core/mcp/appdoc_test.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "TestWidgetDocument_RoundTrip" {
			continue
		}
		text := fn.Doc.Text()
		if !strings.Contains(text, "inline") || !strings.Contains(text, "no script URL") {
			t.Error("round-trip comment must describe the inline client with no script URL")
		}
		if strings.Contains(text, "script URL the server mounts") {
			t.Error("round-trip comment describes the removed linked-client contract")
		}
		return
	}
	t.Fatal("widget round-trip test not found")
}
