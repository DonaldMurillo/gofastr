// Package analyzers holds every detector behind `gofastr verify`; the
// rules they report live in the contracts catalog.
package analyzers

import (
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/framework/contracts/internal/codetext"
)

func importAliases(f *ast.File) map[string]string {
	out := make(map[string]string, len(f.Imports))
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		alias := path.Base(p)
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "_" || alias == "." {
			continue
		}
		out[alias] = p
	}
	return out
}

func rendersUI(p *contracts.Pass) bool {
	const key = "analyzers.rendersui"
	return p.Memo(key, func() any {
		for _, f := range p.AppFiles() {
			file, ok := p.AST(f.Rel)
			if !ok {
				continue
			}
			for _, imp := range file.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					continue
				}
				if strings.Contains(path, "/core-ui/") || strings.HasSuffix(path, "/core-ui") ||
					strings.Contains(path, "/framework/ui/") || strings.HasSuffix(path, "/framework/ui") ||
					strings.Contains(path, "/framework/uihost/") || strings.HasSuffix(path, "/framework/uihost") {
					return true
				}
			}
		}
		return false
	}).(bool)
}

func importsAny(f *ast.File, suffixes ...string) bool {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		for _, s := range suffixes {
			if p == s || strings.HasSuffix(p, "/"+s) {
				return true
			}
		}
	}
	return false
}

func stringLit(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, okL := stringLit(v.X)
		r, okR := stringLit(v.Y)
		if !okL || !okR {
			return "", false
		}
		return l + r, true
	case *ast.ParenExpr:
		return stringLit(v.X)
	}
	return "", false
}

func selectorCall(n ast.Node) (recv ast.Expr, method string, call *ast.CallExpr, ok bool) {
	c, isCall := n.(*ast.CallExpr)
	if !isCall {
		return nil, "", nil, false
	}
	sel, isSel := c.Fun.(*ast.SelectorExpr)
	if !isSel {
		return nil, "", nil, false
	}
	return sel.X, sel.Sel.Name, c, true
}

func qualifiedCall(n ast.Node, aliases map[string]string, pkgSuffix, funcName string) (*ast.CallExpr, bool) {
	recv, method, call, ok := selectorCall(n)
	if !ok || method != funcName {
		return nil, false
	}
	ident, isIdent := recv.(*ast.Ident)
	if !isIdent {
		return nil, false
	}
	p, known := aliases[ident.Name]
	if !known {
		return nil, false
	}
	if p == pkgSuffix || strings.HasSuffix(p, "/"+pkgSuffix) {
		return call, true
	}
	return nil, false
}

func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprText(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return exprText(v.Fun) + "(…)"
	case *ast.BasicLit:
		return v.Value
	case *ast.StarExpr:
		return "*" + exprText(v.X)
	case *ast.IndexExpr:
		return exprText(v.X) + "[…]"
	case *ast.ParenExpr:
		return "(" + exprText(v.X) + ")"
	default:
		return "?"
	}
}

func isRequestHandler(fn *ast.FuncType) bool {
	if fn == nil || fn.Params == nil {
		return false
	}
	sawWriter, sawRequest := false, false
	for _, p := range fn.Params.List {
		switch t := p.Type.(type) {
		case *ast.SelectorExpr:
			if t.Sel.Name == "ResponseWriter" {
				sawWriter = true
			}
		case *ast.StarExpr:
			if sel, ok := t.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Request" {
				sawRequest = true
			}
		}
	}
	return sawWriter && sawRequest
}

func diag(p *contracts.Pass, ruleID, file string, pos token.Pos, msg string) contracts.Diagnostic {
	position := p.Position(pos)
	return contracts.Diagnostic{
		RuleID:  ruleID,
		File:    file,
		Line:    position.Line,
		Column:  position.Column,
		Message: msg,
		Snippet: p.Line(file, position.Line),
	}
}

func hasLegacyAnnotation(lines []string, lineNo int, markers ...string) bool {
	if lineNo < 1 || lineNo > len(lines) {
		return false
	}
	check := func(s string) bool {
		lower := strings.ToLower(s)
		for _, m := range markers {
			if strings.Contains(lower, m) {
				return true
			}
		}
		return false
	}
	if check(lines[lineNo-1]) {
		return true
	}
	for i, seen := lineNo-2, 0; i >= 0 && seen < 4; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		seen++
		if check(lines[i]) {
			return true
		}
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "}" || strings.HasPrefix(trimmed, "func ") {
			break
		}
	}
	return false
}

func stripComments(src string) string { return codetext.Strip(src, false) }

func stripScriptComments(src string) string { return codetext.Strip(src, true) }
