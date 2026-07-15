package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// htmlImportPath is the typed-HTML package whose element constructors
// carry required accessibility fields (see elementRequiredFields).
const htmlImportPath = "github.com/DonaldMurillo/gofastr/core-ui/html"

// LintA11yFile runs ONLY the accessibility element rules over one Go
// file: every call to a core-ui/html constructor with a config literal
// (html.Image(html.ImageConfig{…}), any import alias) is checked for
// its required a11y fields — Alt on images, Label on buttons and
// landmarks, For on labels, Legend on fieldsets, ….
//
// Unlike [LintFile] it applies to ANY .go file, not just .ui.go — so it
// skips the .ui.go sandbox rules (imports, goroutines, channels) and,
// to avoid false positives on app code, only checks calls it can
// attribute to the core-ui/html import (same-name local functions and
// foreign "html" packages are ignored). Files that don't import
// core-ui/html return an empty result immediately.
func LintA11yFile(filename string) (*Result, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	return lintA11yFile(fset, file, filename), nil
}

func lintA11yFile(fset *token.FileSet, file *ast.File, filename string) *Result {
	result := &Result{}
	alias := htmlImportAlias(file)
	if alias == "" {
		return result
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != alias {
			return true
		}
		if _, tracked := elementRequiredFields[sel.Sel.Name]; !tracked {
			return true
		}
		// Shadow-proofing: a local variable named like the alias can make
		// `html.Button(opts{})` match by identifier alone. Require the
		// first argument to be a composite literal whose TYPE is
		// `<alias>.<Elem>Config` — a shadowing variable can never qualify
		// a type, so only real core-ui/html calls survive this gate.
		if !isQualifiedConfigLiteral(call, alias, sel.Sel.Name) {
			return true
		}
		checkElementConfig(call, filename, fset, result)
		return true
	})
	return result
}

// isQualifiedConfigLiteral reports whether call's first argument is a
// composite literal of type `<alias>.<funcName>Config`.
func isQualifiedConfigLiteral(call *ast.CallExpr, alias, funcName string) bool {
	if len(call.Args) == 0 {
		return false
	}
	lit, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		return false
	}
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == alias && sel.Sel.Name == funcName+"Config"
}

// htmlImportAlias returns the local alias under which file imports
// core-ui/html, or "" when the file doesn't import it (or dot-imports
// it — dot imports can't be attributed reliably, so they're skipped).
func htmlImportAlias(file *ast.File) string {
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		path := strings.Trim(imp.Path.Value, `"`)
		if path != htmlImportPath {
			continue
		}
		if imp.Name != nil {
			if imp.Name.Name == "." || imp.Name.Name == "_" {
				return ""
			}
			return imp.Name.Name
		}
		return filepath.Base(path)
	}
	return ""
}
