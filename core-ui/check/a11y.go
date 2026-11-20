package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

const htmlImportPath = "github.com/DonaldMurillo/gofastr/core-ui/html"

// LintA11yFile runs ONLY the accessibility element rules over one Go file: every call to a
// core-ui/html constructor with a config literal (html.Image(html.ImageConfig{…}), any import
// alias) is checked for its required a11y fields, Alt on images, Label on buttons and landmarks,
// For on labels, Legend on fieldsets, ….
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
		if !isQualifiedConfigLiteral(call, alias, sel.Sel.Name) {
			return true
		}
		checkElementConfig(call, filename, fset, result)
		return true
	})
	return result
}

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
