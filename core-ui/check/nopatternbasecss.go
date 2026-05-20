package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Pattern-CSS contract enforcement.
//
// Pattern packages under core-ui/patterns/* must wire their stylesheet
// through registry.RegisterStyle so the runtime's data-fui-comp scanner
// auto-loads it. The legacy contract — exporting a BaseCSS() func that
// the host app concatenates by hand — leaks the wiring requirement onto
// every consumer and shipped components without their stylesheet (see
// the 2026-05-19 nestedlist incident).
//
// This linter flags any pattern package that still exports a top-level
// `func BaseCSS() string`. The rule applies only to core-ui/patterns/*
// packages so the framework's own theme bundle (framework/ui.BaseCSS)
// is unaffected.

// LintNoPatternBaseCSS walks core-ui/patterns/* (or any subdir of the
// given root that lives under "patterns") and reports any package that
// exports a top-level `func BaseCSS`. Use it from a test in any package
// to enforce the rule on every `go test ./...`.
func LintNoPatternBaseCSS(root string) (*Result, error) {
	result := &Result{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if path != root && (strings.HasPrefix(base, ".") ||
			base == "vendor" || base == "node_modules" || base == "testdata") {
			return filepath.SkipDir
		}
		// Only enforce the rule inside core-ui/patterns/. Other parts of
		// the repo legitimately export BaseCSS (framework/ui, examples).
		if !isPatternPackage(path) {
			return nil
		}
		return scanPatternDir(path, result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// isPatternPackage reports whether the directory is a leaf
// core-ui/patterns/<name>/ package — i.e. one level under
// .../core-ui/patterns/.
func isPatternPackage(dir string) bool {
	parent := filepath.Base(filepath.Dir(dir))
	grand := filepath.Base(filepath.Dir(filepath.Dir(dir)))
	return parent == "patterns" && grand == "core-ui"
}

func scanPatternDir(dir string, result *Result) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		filename := filepath.Join(dir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filename, nil, parser.AllErrors)
		if err != nil {
			return fmt.Errorf("parse %s: %w", filename, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Recv != nil { // method, not function
				continue
			}
			if fn.Name == nil || fn.Name.Name != "BaseCSS" {
				continue
			}
			result.add(filename, fset.Position(fn.Pos()).Line,
				"core-ui/patterns/* packages must register CSS via "+
					"registry.RegisterStyle (`var Style = registry.RegisterStyle("+
					"\"<name>\", styleFn)`) and wrap their rendered output with "+
					"Style.WrapHTML(). The legacy BaseCSS() export forces every "+
					"host app to concatenate the stylesheet by hand and breaks "+
					"when a consumer forgets. See core-ui/patterns/nestedlist for "+
					"the canonical shape.")
		}
	}
	return nil
}
