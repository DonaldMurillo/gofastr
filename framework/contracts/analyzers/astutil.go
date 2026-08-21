// Package analyzers holds every detector behind `gofastr verify`.
//
// The rules themselves, IDs, severities, the Why and the Fix, live in
// [github.com/DonaldMurillo/gofastr/framework/contracts]. This package
// only finds the code. That split is deliberate: the catalog has to be
// readable and serveable (over MCP, in docs) without dragging in a Go
// parser, and an analyzer has to be replaceable without touching the
// documentation contract it satisfies.
//
// Analyzers here are AST-based rather than type-checked. The trade is
// explicit: a type-checked pass would be more precise and would need a
// full `go/packages` load of the module, which costs seconds and fails
// outright on a project that does not compile. Verify has to be useful
// mid-edit, so precision is bought back with narrow patterns and
// suppression rather than with types.
package analyzers

import (
	"go/ast"
	"go/token"
	"path"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// importAliases maps each of a file's import aliases to its path.
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

// rendersUI reports whether any app file in the module imports a UI
// package (core-ui or framework/ui). Rules whose premise is a browser,
// such as scroll position, focus, or history, use this to stay silent
// on headless modules, where the same route shapes are ordinary REST.
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

// importsAny reports whether the file imports any path with one of the
// given suffixes. Suffix matching keeps the check working for a vendored
// or forked module path.
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

// stringLit returns the value of a string-literal expression, resolving
// concatenations of literals ("/api" + "/v1"). Anything with a non-literal
// operand returns ok=false. Guessing at a runtime-computed path would
// produce findings nobody can act on.
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

// selectorCall decomposes `x.Method(...)` into the receiver expression and
// the method name.
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

// qualifiedCall matches `alias.Func(...)` where alias resolves to an
// import whose path ends in pkgSuffix (or equals it, for stdlib).
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

// exprText renders an expression back to source-ish text for evidence
// fields. Not valid Go for every node. It is a label, not a rewrite.
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

// isRequestHandler reports whether a function's signature makes it a
// request handler: the `(http.ResponseWriter, *http.Request)` shape, or
// a closure returning one. Used by the performance rules, which only care
// about work that happens per-request.
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

// diag is the shared constructor: it positions a finding against the pass
// and fills the snippet, so every analyzer produces the same shape.
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

// hasLegacyAnnotation reports whether one of the pre-contracts escape
// hatches sits on the line or in the four non-blank lines above it.
//
// These predate `//gofastr:allow` and are load-bearing across the
// repository and in shipped apps. Honouring them is not backwards
// compatibility for its own sake: `// best-effort:` and `// safe-sql:`
// say *why* in the same breath, which is exactly what the newer directive
// demands, so re-spelling thousands of them would buy nothing.
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
		// Stop at a block boundary: an annotation three functions up is
		// not an annotation for this line.
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "}" || strings.HasPrefix(trimmed, "func ") {
			break
		}
	}
	return false
}

// stripComments removes // line and /* */ block comments so a rule
// counting call sites is not fooled by prose. It does not honour string
// literals containing "//", which is acceptable because every consumer
// here is counting occurrences, not parsing.
func stripComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	inBlock := false
	for line := range strings.SplitSeq(src, "\n") {
		out := line
		if inBlock {
			if i := strings.Index(out, "*/"); i >= 0 {
				out, inBlock = out[i+2:], false
			} else {
				b.WriteByte('\n')
				continue
			}
		}
		for {
			bs := strings.Index(out, "/*")
			ls := strings.Index(out, "//")
			if ls >= 0 && (bs < 0 || ls < bs) {
				out = out[:ls]
				break
			}
			if bs < 0 {
				break
			}
			if e := strings.Index(out[bs+2:], "*/"); e >= 0 {
				out = out[:bs] + out[bs+2+e+2:]
				continue
			}
			out, inBlock = out[:bs], true
			break
		}
		b.WriteString(out)
		b.WriteByte('\n')
	}
	return b.String()
}
