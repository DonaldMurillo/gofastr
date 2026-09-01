package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEveryAnalyzerIsWiredOrExplained closes the hole that let four
// finished analyzers (reqparamlimit, fmtformat, testgap, discardmutator)
// sit in internal/analyzers with fixture tests and no line in this file:
// they passed their own tests forever while `make analyze`, the
// pre-commit hook, and CI's vet step never ran them over the repo, and
// nothing said why. A package here is either registered in
// multichecker.Main or named in this file's prose with the measurement
// that justifies holding it back. Silence is the one thing it can't be.
func TestEveryAnalyzerIsWiredOrExplained(t *testing.T) {
	const dir = "../../internal/analyzers"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	imported := map[string]bool{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if rest, ok := strings.CutPrefix(path, "github.com/DonaldMurillo/gofastr/internal/analyzers/"); ok {
			imported[rest] = true
		}
	}

	// Package qualifiers reaching multichecker.Main's argument list. An
	// import that never lands here would not compile, but reading the
	// call is what makes the assertion about REGISTRATION rather than
	// about an import line.
	registered := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		fn, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || fn.Sel.Name != "Main" {
			return true
		}
		if pkg, ok := fn.X.(*ast.Ident); !ok || pkg.Name != "multichecker" {
			return true
		}
		for _, arg := range call.Args {
			sel, ok := arg.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); ok {
				registered[pkg.Name] = true
			}
		}
		return false
	})

	// Prose that names a held-back package. Comments only: a bare
	// mention anywhere in the file would let an unused import pass as
	// an explanation.
	var comments strings.Builder
	for _, group := range file.Comments {
		comments.WriteString(group.Text())
	}
	explained := comments.String()

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if _, err := os.Stat(filepath.Join(dir, name, name+".go")); err != nil {
			continue // not an analyzer package
		}
		switch {
		case registered[name]:
			if !imported[name] {
				t.Errorf("%s: registered under a qualifier that is not an internal/analyzers import", name)
			}
		case strings.Contains(explained, name):
			// Held back on purpose, with the reason in this file.
		default:
			t.Errorf("internal/analyzers/%s is neither registered in multichecker.Main "+
				"nor explained in a comment here: it never runs over the repo and "+
				"nothing records why", name)
		}
	}
}
