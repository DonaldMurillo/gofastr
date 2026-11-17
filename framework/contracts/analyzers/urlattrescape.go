package analyzers

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1412: render.Escape/html.EscapeString feeding a URL attribute.

func ruleURLAttrEscape(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	aliases := importAliases(file)
	var out []contracts.Diagnostic
	for _, fn := range functionsIn(file) {
		cleaned := cleanedIdents(fn.body)
		rooted := rootedIdents(fn.body)
		guarded := guardedExprs(fn.body)
		var credited func(e ast.Expr) bool
		credited = func(e ast.Expr) bool {
			if isCleanedValue(e, cleaned) {
				return true
			}
			if id, ok := e.(*ast.Ident); ok && rooted[id.Name] {
				return true
			}
			if guarded[exprText(e)] {
				return true
			}
			if src, ok := literalReplace(e); ok {
				return credited(src)
			}
			return false
		}
		escaped := escapedIdents(fn.body, aliases)
		// slotValue resolves a slot operand to the escape call behind it, directly or through a local.
		slotValue := func(e ast.Expr) (*ast.CallExpr, bool) {
			if esc, ok := escapeCallExpr(e, aliases); ok {
				return esc, true
			}
			if id, ok := e.(*ast.Ident); ok {
				esc, ok := escaped[id.Name]
				return esc, ok
			}
			return nil, false
		}
		report := func(e ast.Expr) {
			if esc, ok := slotValue(e); ok && !credited(esc.Args[0]) {
				out = append(out, urlAttrDiag(p, rel, e.Pos()))
			}
		}
		ast.Inspect(fn.body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BinaryExpr:
				operands := flattenConcat(v)
				for i := range len(operands) - 1 {
					lit, ok := operandLiteral(operands[i])
					if !ok || !isURLSlotSuffix(lit) {
						continue
					}
					next := operands[i+1]
					if _, ok := slotValue(next); !ok && i+2 < len(operands) {
						if glue, ok := operandLiteral(next); ok && isGlue(glue) {
							next = operands[i+2]
						}
					}
					report(next)
				}
			case *ast.CallExpr:
				name, ok := callFunName(v)
				if !ok || (name != "Sprintf" && name != "Fprintf") {
					return true
				}
				formatIdx := 0
				if name == "Fprintf" {
					formatIdx = 1
				}
				if len(v.Args) <= formatIdx+1 {
					return true
				}
				format, ok := stringLit(v.Args[formatIdx])
				if !ok {
					return true
				}
				for _, slot := range urlAttrSlots {
					at := strings.Index(format, slot)
					if at < 0 {
						continue
					}
					verb := format[at+len(slot):]
					if !strings.HasPrefix(verb, "%") {
						continue // the slot's value does not start with the verb
					}
					idx := formatIdx + 1 + countVerbs(format[:at+len(slot)])
					if idx >= len(v.Args) {
						continue
					}
					report(v.Args[idx])
				}
			}
			return true
		})
	}
	return out
}

// urlAttrSlots carry the leading space so data-*/aria-* cannot match.
var urlAttrSlots = []string{
	` href="`, ` src="`, ` action="`, ` formaction="`, ` poster="`, ` data="`,
	` href='`, ` src='`, ` action='`, ` formaction='`, ` poster='`, ` data='`,
}

func urlAttrDiag(p *contracts.Pass, rel string, pos token.Pos) contracts.Diagnostic {
	return diag(p, contracts.RuleURLAttrEscape, rel, pos,
		"render.Escape/html.EscapeString is HTML escaping and scheme-blind: javascript:, vbscript:, data:, and protocol-relative URLs survive it verbatim and this slot makes them live — run the value through core/urlsafe.Clean/CleanAnchor (the scheme allow-list menu.go's Href branch and html.setURLAttr already apply) and escape the cleaned result")
}

func flattenConcat(e ast.Expr) []ast.Expr {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op.String() == "+" {
		return append(flattenConcat(b.X), b.Y)
	}
	return []ast.Expr{e}
}

func operandLiteral(e ast.Expr) (string, bool) {
	if v, ok := e.(*ast.BasicLit); ok && v.Kind == token.STRING {
		return stringLit(v)
	}
	return "", false
}

func isURLSlotSuffix(lit string) bool {
	for _, slot := range urlAttrSlots {
		if strings.HasSuffix(lit, slot) {
			return true
		}
	}
	return false
}

func isGlue(lit string) bool {
	return lit != "" && len(lit) <= 2 && !strings.ContainsFunc(lit, func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
	})
}

// escapedIdents maps names assigned only from an escape call to that call.
func escapedIdents(body ast.Node, aliases map[string]string) map[string]*ast.CallExpr {
	out := map[string]*ast.CallExpr{}
	dropped := map[string]bool{}
	bind := func(lhs []ast.Expr, rhs []ast.Expr) {
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			var esc *ast.CallExpr
			if len(lhs) == len(rhs) {
				esc, _ = escapeCallExpr(rhs[i], aliases)
			}
			if esc == nil || dropped[id.Name] || out[id.Name] != nil {
				delete(out, id.Name)
				dropped[id.Name] = true
				continue
			}
			out[id.Name] = esc
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			bind(v.Lhs, v.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(v.Names))
			for i, name := range v.Names {
				lhs[i] = name
			}
			bind(lhs, v.Values)
		}
		return true
	})
	return out
}

func escapeCallExpr(e ast.Expr, aliases map[string]string) (*ast.CallExpr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return nil, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	path := aliases[pkg.Name]
	switch sel.Sel.Name {
	case "Escape":
		return call, path == "core/render" || strings.HasSuffix(path, "/core/render")
	case "EscapeString":
		return call, path == "html" || strings.HasSuffix(path, "/html")
	}
	return nil, false
}

func cleanedIdents(body ast.Node) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		a, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, rhs := range a.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok || !cleanCall(call) {
				continue
			}
			if i < len(a.Lhs) {
				if id, ok := a.Lhs[i].(*ast.Ident); ok {
					out[id.Name] = true
				}
			}
		}
		return true
	})
	return out
}

func cleanCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	switch sel.Sel.Name {
	case "Clean", "CleanAnchor", "CleanResource":
	default:
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && strings.Contains(pkg.Name, "urlsafe")
}

func isCleanedValue(e ast.Expr, cleaned map[string]bool) bool {
	if call, ok := e.(*ast.CallExpr); ok && cleanCall(call) {
		return true
	}
	if id, ok := e.(*ast.Ident); ok {
		return cleaned[id.Name]
	}
	return false
}

// guardedExprs returns the arguments of an allow-list call (urlsafe.OK, isSafe*URL, IsSafeRelativePath) in an if-condition.
func guardedExprs(body ast.Node) map[string]bool {
	out := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		v, ok := n.(*ast.IfStmt)
		if !ok || v.Cond == nil {
			return true
		}
		ast.Inspect(v.Cond, func(c ast.Node) bool {
			call, ok := c.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			name := guardName(call)
			lower := strings.ToLower(name)
			if strings.Contains(lower, "urlsafe") || strings.Contains(lower, "saferelative") ||
				(strings.Contains(lower, "safe") && strings.Contains(lower, "url")) {
				for _, a := range call.Args {
					out[exprText(a)] = true
				}
			}
			return true
		})
		return true
	})
	return out
}

func literalReplace(e ast.Expr) (ast.Expr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) < 3 {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || (sel.Sel.Name != "ReplaceAll" && sel.Sel.Name != "Replace") {
		return nil, false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "strings" {
		return nil, false
	}
	if _, ok := operandLiteral(call.Args[1]); !ok {
		return nil, false
	}
	if _, ok := operandLiteral(call.Args[2]); !ok {
		return nil, false
	}
	return call.Args[0], true
}

func guardName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if fn.Sel == nil {
			return ""
		}
		if pkg, ok := fn.X.(*ast.Ident); ok {
			return pkg.Name + "." + fn.Sel.Name
		}
		return fn.Sel.Name
	}
	return ""
}

// rootedIdents returns names assigned a "/"-rooted literal and only appended to after.
func rootedIdents(body ast.Node) map[string]bool {
	rooted := map[string]bool{}
	disqualified := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		a, ok := n.(*ast.AssignStmt)
		if !ok || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
			return true
		}
		id, ok := a.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if lit, ok := operandLiteral(a.Rhs[0]); ok && strings.HasPrefix(lit, "/") {
			if !disqualified[id.Name] {
				rooted[id.Name] = true
			}
			return true
		}
		if a.Tok.String() == "+=" {
			return true // append-only: the root literal still pins the scheme
		}
		delete(rooted, id.Name)
		disqualified[id.Name] = true // a plain reassignment disqualifies
		return true
	})
	return rooted
}

func countVerbs(s string) int {
	n := 0
	for i := 0; i < len(s); i++ { // classic loop: the body advances i over %%
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++ // %% is a literal percent
			continue
		}
		n++
	}
	return n
}
