package clitest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// AuditInjectedIdent reports whether src contains the marker as a Go
// identifier (the injection oracle), and whether it parses at all.
func AuditInjectedIdent(name, src string) (injected bool, parseErr error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.AllErrors)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "PWNX") {
			found = true
			return false
		}
		return true
	})
	return found, nil
}
