package auditcmd

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/docs/content/audit-deps.md
func TestDependencyAuditDocumentsSharedExclusions(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "audit.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	documents := map[string]string{}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == "auditDeps" && function.Doc != nil {
			documents["auditDeps comment"] = function.Doc.Text()
		}
	}
	if documents["auditDeps comment"] == "" {
		t.Fatal("auditDeps has no documentation comment")
	}
	path := filepath.Join("..", "..", "..", "..", "framework", "docs", "content", "audit-deps.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	documents["audit-deps reference"] = string(content)
	for name, document := range documents {
		t.Run(name, func(t *testing.T) {
			for _, exclusion := range []string{"vendor", "node_modules", "dist", "bin", "build", "tmp", "hidden", "underscore"} {
				if !strings.Contains(document, exclusion) {
					t.Errorf("shared source-discovery exclusion %q is undocumented", exclusion)
				}
			}
		})
	}
}
