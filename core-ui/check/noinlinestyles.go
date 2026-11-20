// check-csp:ignore-file

package check

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

var (
	inlineStyleAttrRe = regexp.MustCompile(`(?si)(?:^|[\s/])style\s*=\s*("[^"]*"|'[^']*'|[^\s>]*:[^\s>]*|["']?$)`)
)

// LintNoInlineStyles scans every .go file in dir (non-recursive) for inline style attributes in
// HTML, and for "style" keys passed as attrs to render.Tag / html.Attrs literals.
func LintNoInlineStyles(dir string) (*Result, error) {
	res, err := lintGoDir(dir, lintInlineStyles)
	if err != nil {
		return nil, err
	}
	return res[0], nil
}

func lintInlineStyles(fset *token.FileSet, file *ast.File, filename string, raw []byte, result *Result) {
	allowed := allowedLines(goComments(fset, file, raw), "GOFASTR1804")
	for index, line := range strings.Split(string(raw), "\n") {
		if allowed[index+1] && strings.TrimSpace(line) == "" {
			allowed[index+2] = true
		}
	}
	sub := &Result{}
	scanInlineStyles(fset, file, filename, sub)
	for _, v := range sub.Violations {
		if !allowed[v.Line] {
			result.Violations = append(result.Violations, v)
		}
	}
}

// ScanInlineStylesIn runs the inline-style detector over one parsed file, for a contracts pass.
func ScanInlineStylesIn(fset *token.FileSet, file *ast.File, filename string, raw []byte) *Result {
	if fset == nil || file == nil || hasCSPIgnoreDirective(raw) {
		return nil
	}
	result := &Result{}
	scanInlineStyles(fset, file, filename, result)
	return result
}

// LintNoInlineStylesRecursive walks dir and every subdirectory.
func LintNoInlineStylesRecursive(root string) (*Result, error) {
	return lintRecursive(root, LintNoInlineStyles)
}

func scanInlineStyles(fset *token.FileSet, file *ast.File, filename string, result *Result) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(node.Value)
			if err != nil {
				val = stripStringLiteral(node.Value)
			}
			checkInlineStyleInString(val, fset.Position(node.Pos()).Line, filename, result)
		case *ast.CompositeLit:
			checkStyleKeyInComposite(node, fset, filename, result)
		case *ast.AssignStmt:
			checkStyleIndexAssignment(node, fset, filename, result)
		}
		return true
	})
}

func checkStyleIndexAssignment(node *ast.AssignStmt, fset *token.FileSet, filename string, result *Result) {
	for i, lhs := range node.Lhs {
		idx, ok := lhs.(*ast.IndexExpr)
		if !ok {
			continue
		}
		key, ok := idx.Index.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			continue
		}
		if !strings.EqualFold(stripStringLiteral(key.Value), "style") {
			continue
		}
		if i < len(node.Rhs) && !valueLooksLikeString(node.Rhs[i]) {
			continue
		}
		result.add(filename, fset.Position(lhs.Pos()).Line,
			"\"style\" index assignment onto an attrs map forbidden: framework "+
				"strict-CSP strips inline style attributes. Use a class name backed by a "+
				"registered stylesheet (style.NewStyleSheet / registry.RegisterStyle / "+
				"theme.go) instead.")
	}
}

func checkInlineStyleInString(s string, line int, filename string, result *Result) {
	if !inlineStyleAttrRe.MatchString(s) {
		return
	}
	result.add(filename, line,
		"inline style=\"…\" attribute forbidden: framework strict-CSP "+
			"(default-src 'self', no 'unsafe-inline' for styles) strips the "+
			"attribute in the browser, collapsing the element's layout. "+
			"Move the rule into a registered stylesheet (style.NewStyleSheet "+
			"/ framework theme.go) and reference via a class name.")
}

func checkStyleKeyInComposite(node *ast.CompositeLit, fset *token.FileSet, filename string, result *Result) {
	for _, el := range node.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			continue
		}
		if !strings.EqualFold(stripStringLiteral(key.Value), "style") {
			continue
		}
		if !valueLooksLikeString(kv.Value) {
			continue
		}
		result.add(filename, fset.Position(key.Pos()).Line,
			"\"style\" key in HTML attrs forbidden: framework strict-CSP "+
				"strips inline style attributes. Use a class name backed by a "+
				"registered stylesheet (style.NewStyleSheet / registry.RegisterStyle / "+
				"theme.go) instead.")
	}
}

func valueLooksLikeString(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.Ident:
		switch v.Name {
		case "true", "false", "nil", "iota":
			return false
		}
		return true
	case *ast.ParenExpr:
		return valueLooksLikeString(v.X)
	case *ast.BinaryExpr:
		return valueLooksLikeString(v.X) || valueLooksLikeString(v.Y)
	case *ast.CallExpr:
		return true // fmt.Sprintf / string concat / formatter, assume stringy
	case *ast.SelectorExpr, *ast.IndexExpr:
		return true
	}
	return false
}
