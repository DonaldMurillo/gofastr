package main

import (
	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
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
		// The repoAnalyzers composite literal is the registration list;
		// main hands it to multichecker.Main.
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "repoAnalyzers" || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, arg := range lit.Elts {
			// allow.Guard(pkg.Analyzer) is the registered spelling since
			// the marker filter landed; the bare selector still counts.
			if wrap, ok := arg.(*ast.CallExpr); ok && len(wrap.Args) == 1 {
				if fn, ok := wrap.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "Guard" {
					arg = wrap.Args[0]
				}
			}
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
		if !declaresAnalyzer(t, filepath.Join(dir, name, name+".go")) {
			continue // a helper package (allow) that no registration could name
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

// declaresAnalyzer reports whether the package's main file declares the
// Analyzer variable a registration would reference.
func declaresAnalyzer(t *testing.T, path string) bool {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			for _, n := range spec.(*ast.ValueSpec).Names {
				if n.Name == "Analyzer" {
					return true
				}
			}
		}
	}
	return false
}

// TestAllowNamesMatchRegistration pins allow.Names to the registered set,
// so a marker can name every analyzer that runs and nothing that does not.
func TestAllowNamesMatchRegistration(t *testing.T) {
	got := map[string]bool{}
	for _, a := range repoAnalyzers {
		got[a.Name] = true
		if !allow.Known(a.Name) {
			t.Errorf("%s is registered but missing from allow.Names", a.Name)
		}
	}
	for _, n := range allow.Names {
		if !got[n] {
			t.Errorf("allow.Names lists %s, which is not registered", n)
		}
	}
}
