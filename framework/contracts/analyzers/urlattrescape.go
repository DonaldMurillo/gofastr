package analyzers

import (
	"go/ast"
	"go/token"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// ----------------------------------------------------------------------
// GOFASTR1412: render.Escape/html.EscapeString feeding a URL attribute.
// ----------------------------------------------------------------------

// Bug class: a Go string literal or concatenation that places
// render.Escape(…)/html.EscapeString(…) into a URL-typed attribute
// slot — href, src, action, formaction, poster, data. HTML escaping is
// scheme-blind: "javascript:alert(1)" escapes to itself and becomes a
// live href/src/action the moment a user clicks or a form submits.
// Produced by the 2026-09-06/07 adversarial round: battery/print
// renderShell linked a caller-named stylesheet href and an auto-print
// script src through render.Escape (probe
// TestPrintShellEscapedHrefIsNotASchemeGuard), core-ui's noscript
// infinitescroll form action (probe
// TestNoscriptFormActionJavascriptURL), framework/ui menu.go's
// MenuAction Path (probe TestMenuActionEscapeIsSchemeBlind), and
// battery/auth's magic-link confirm page action. The fix is the scheme
// allow-list, core/urlsafe.Clean/CleanAnchor — which the quiet
// siblings already apply before escaping.
//
// The rule reads two spellings:
//   - a concatenation whose previous operand is a string literal
//     ending in ` href="` (et al.), optionally with one punctuation
//     glue literal between, and whose next operand is the escape call;
//   - a fmt.Sprintf/Fprintf format whose ` href="` slot is filled by
//     the very next verb and whose argument at that index is the
//     escape call.
//
// The escaped value is CREDITED (stays quiet) when it has already
// been scheme-vetted in any of the repo's three spellings:
//   - urlsafe.Clean/CleanAnchor/CleanResource itself, or an identifier
//     an assignment in the function holds from one (menu.go's Href
//     branch: href := urlsafe.CleanAnchor(it.Href); render.Escape(href));
//   - a scheme guard in an if-condition vetted it — urlsafe.OK or a
//     local allow-list helper whose name says what it does
//     (uihost's isSafeHeadURL wraps urlsafe.OK and guards all five of
//     its head-tag sites);
//   - the value is rooted at a root-relative path literal and can only
//     be appended to — a value that STARTS "/" cannot carry a scheme
//     (embed.go's appCSS := "/__gofastr/app.css" … += "?t=" + key).
//
// Deliberately silent on:
//   - non-URL attribute slots (class, method, value, title): escaping
//     there is the correct defense;
//   - render.Tag/attrs-map construction, which routes through
//     core-ui/html setURLAttr by design;
//   - _test.go and generated files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1412) <why>.
func ruleURLAttrEscape(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	aliases := importAliases(file)
	var out []contracts.Diagnostic
	for _, fn := range functionsIn(file) {
		cleaned := cleanedIdents(fn.body)
		rooted := rootedIdents(fn.body)
		guarded := guardedExprs(fn.body)
		credited := func(e ast.Expr) bool {
			if isCleanedValue(e, cleaned) {
				return true
			}
			if id, ok := e.(*ast.Ident); ok && rooted[id.Name] {
				return true
			}
			return guarded[exprText(e)]
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
					// One punctuation glue literal may sit between the
					// slot and the escaped value.
					if !escapeCall(next, aliases) && i+2 < len(operands) {
						if glue, ok := operandLiteral(next); ok && isGlue(glue) {
							next = operands[i+2]
						}
					}
					if esc, ok := escapeCallExpr(next, aliases); ok && !credited(esc.Args[0]) {
						out = append(out, urlAttrDiag(p, rel, esc))
					}
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
					if esc, ok := escapeCallExpr(v.Args[idx], aliases); ok && !credited(esc.Args[0]) {
						out = append(out, urlAttrDiag(p, rel, esc))
					}
				}
			}
			return true
		})
	}
	return out
}

// urlAttrSlots are the URL-typed attribute openings, leading space
// included so data-*/aria-* cannot match.
var urlAttrSlots = []string{` href="`, ` src="`, ` action="`, ` formaction="`, ` poster="`, ` data="`}

func urlAttrDiag(p *contracts.Pass, rel string, esc *ast.CallExpr) contracts.Diagnostic {
	return diag(p, contracts.RuleURLAttrEscape, rel, esc.Pos(),
		"render.Escape/html.EscapeString is HTML escaping and scheme-blind: javascript:, vbscript:, data:, and protocol-relative URLs survive it verbatim and this slot makes them live — run the value through core/urlsafe.Clean/CleanAnchor (the scheme allow-list menu.go's Href branch and html.setURLAttr already apply) and escape the cleaned result")
}

// flattenConcat returns the left-assoc operands of a + chain.
func flattenConcat(e ast.Expr) []ast.Expr {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op.String() == "+" {
		return append(flattenConcat(b.X), b.Y)
	}
	return []ast.Expr{e}
}

// operandLiteral returns a pure string-literal operand's value.
func operandLiteral(e ast.Expr) (string, bool) {
	if v, ok := e.(*ast.BasicLit); ok && v.Kind == token.STRING {
		return stringLit(v)
	}
	return "", false
}

// isURLSlotSuffix reports whether a literal ends in one of the URL
// attribute openings, so the following operand opens the value.
func isURLSlotSuffix(lit string) bool {
	for _, slot := range urlAttrSlots {
		if strings.HasSuffix(lit, slot) {
			return true
		}
	}
	return false
}

// isGlue reports whether a literal is short punctuation glue (a quote
// or brace fragment), not content.
func isGlue(lit string) bool {
	return lit != "" && len(lit) <= 2 && !strings.ContainsFunc(lit, func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
	})
}

// escapeCall reports whether e is a render.Escape / html.EscapeString
// call in this file's imports.
func escapeCall(e ast.Expr, aliases map[string]string) bool {
	_, ok := escapeCallExpr(e, aliases)
	return ok
}

// escapeCallExpr is escapeCall returning the call node.
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

// cleanedIdents returns the identifiers this body assigns from a
// urlsafe.Clean* call: the quiet posture's holders.
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

// cleanCall reports whether a call is urlsafe.Clean / CleanAnchor /
// CleanResource. The qualifier must be (or resolve to) the urlsafe
// package: the credit is the fix posture, and it must name the
// allow-list, not some other package's Clean.
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

// isCleanedValue reports whether the escaped argument has been through
// the scheme allow-list: the Clean call itself, or an identifier an
// assignment in the body holds from one.
func isCleanedValue(e ast.Expr, cleaned map[string]bool) bool {
	if call, ok := e.(*ast.CallExpr); ok && cleanCall(call) {
		return true
	}
	if id, ok := e.(*ast.Ident); ok {
		return cleaned[id.Name]
	}
	return false
}

// guardedExprs returns the expressions a scheme guard has already
// vetted: any call in an if-condition whose name is (or resolves to)
// the allow-list — urlsafe.OK, isSafeHeadURL — credits its argument
// for the whole function. uihost's five head-tag sites spell the fix
// exactly this way, under a local helper name.
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
			if strings.Contains(lower, "urlsafe") || (strings.Contains(lower, "safe") && strings.Contains(lower, "url")) {
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

// guardName renders a guard call's callee as qualifier.Name.
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

// rootedIdents returns identifiers whose value is rooted at a
// root-relative path literal and can only be appended to: a value that
// STARTS "/" cannot carry a scheme, whatever is concatenated after it
// (embed.go's appCSS := "/__gofastr/app.css" … appCSS += "?t=" + key).
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

// countVerbs counts formatting verbs in a format prefix.
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
