package analyzers

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// AuditLint runs the legacy audit rules against one parsed Go file.
func AuditLint(rel string, body []byte, fset *token.FileSet, file *ast.File) []contracts.Diagnostic {
	src := newLintSource(rel, string(body), fset, file)
	if strings.HasSuffix(rel, "_test.go") {
		return ruleDisabledTests(src, true)
	}
	return append(sharedSecurityRules(src), ruleIgnoredExec(src)...)
}

type lintSource struct {
	rel, body string
	lines     []string
	comments  map[int][]string
	fset      *token.FileSet
	file      *ast.File
}

func newLintSource(rel, body string, fset *token.FileSet, file *ast.File) lintSource {
	return lintSource{
		rel: rel, body: body, lines: strings.Split(body, "\n"),
		comments: legacyCommentLines(fset, file), fset: fset, file: file,
	}
}

func (s lintSource) diagnostic(rule string, pos token.Pos, message string) contracts.Diagnostic {
	position := s.fset.Position(pos)
	d := contracts.Diagnostic{
		RuleID: rule, File: s.rel, Line: position.Line, Column: position.Column, Message: message,
	}
	if position.Line >= 1 && position.Line <= len(s.lines) {
		d.Snippet = strings.TrimSpace(s.lines[position.Line-1])
	}
	return d
}
