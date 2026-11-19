package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "security",
		Doc:  "Injection, CSRF, cookie attributes, committed secrets, reflected proxy headers, raw body and frame decodes, cross-site predicate copies, URL-attribute escaping, Vary writes, queue SQL safety, dialect twin drift, erase/export coverage, inline DDL constraints, status-update stamps, and primary keys that accept NULL.",
		Rules: []string{
			contracts.RuleSQLStringConcat,
			contracts.RuleFormWithoutCSRF,
			contracts.RuleHTMLConcat,
			contracts.RuleInsecureCookie,
			contracts.RuleHardcodedSecret,
			contracts.RuleForwardedProtoEnum,
			contracts.RuleRawJSONBodyDecode,
			contracts.RuleAbsoluteAttempts,
			contracts.RuleUnfencedClaim,
			contracts.RuleFoldedKey,
			contracts.RuleFetchMetadata,
			contracts.RuleURLAttrEscape,
			contracts.RuleVarySet,
			contracts.RuleDialectDrift,
			contracts.RuleEraseCover,
			contracts.RuleDDLConstraint,
			contracts.RuleSettleStamp,
			contracts.RuleNullableKey,
		},
		Run: runSecurity,
	})
	contracts.Register(&contracts.Analyzer{
		Name:  "data",
		Doc:   "Persistence-layer correctness: discarded write results.",
		Rules: []string{contracts.RuleIgnoredExec},
		Run:   runData,
	})
}

func runSecurity(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		file, _ := p.AST(f.Rel)
		src := newLintSource(f.Rel, string(body), p.FileSet(), file)
		out = append(out, sharedSecurityRules(src)...)
		if file != nil {
			lines := src.lines
			out = append(out, ruleInsecureCookie(p, f.Rel, file, lines)...)
			out = append(out, ruleHardcodedSecret(p, f.Rel, file, lines)...)
			out = append(out, ruleForwardedProto(p, f.Rel, file)...)
			out = append(out, ruleRawJSONBody(p, f.Rel, file)...)
			out = append(out, ruleAbsoluteAttempts(p, f.Rel, file)...)
			out = append(out, ruleUnfencedClaim(p, f.Rel, file)...)
			out = append(out, ruleFoldedKey(p, f.Rel, file)...)
			out = append(out, ruleFetchMetadata(p, f.Rel, file)...)
			out = append(out, ruleURLAttrEscape(p, f.Rel, file)...)
			out = append(out, ruleVarySet(p, f.Rel, file)...)
			out = append(out, ruleDialectDrift(p, f.Rel, file)...)
			out = append(out, ruleEraseCover(p, f.Rel, file)...)
			out = append(out, ruleDDLConstraint(p, f.Rel, file)...)
			out = append(out, ruleSettleStamp(p, f.Rel, file)...)
			out = append(out, ruleNullableKey(p, f.Rel, file)...)
		}
	}
	return out, nil
}

func sharedSecurityRules(src lintSource) []contracts.Diagnostic {
	sql := ruleSQLConcat(src)
	html := ruleHTMLConcat(src)
	out := append(ruleFormCSRF(src), sql...)
	out = append(out, html...)
	if src.file != nil {
		out = append(out, ruleSQLConcatAST(src, sql)...)
		out = append(out, ruleHTMLBuiltAST(src, html)...)
	}
	return out
}

// GOFASTR1401: SQL built by concatenation.

var (
	sqlVerb            = `(?:SELECT\s|INSERT\s+(?:(?:OR\s+\w+|IGNORE)\s+)?INTO\s|UPDATE\s|DELETE\s+FROM\s)`
	reSQLConcatLiteral = regexp.MustCompile(`(?i)"[^"]*\b` + sqlVerb + `[^"]*"\s*\+\s*\w+`)
	reSQLSprintf       = regexp.MustCompile(`(?i)fmt\.S?(?:print|printf)\(\s*"[^"]*\b(?:` + sqlVerb + `|WHERE\s|HAVING\s)[^"]*%[sv]`)
	reSQLBuilderConcat = regexp.MustCompile(`\.(?:Where|Having|OrderBy|GroupBy)\(\s*"[^"]*"\s*\+\s*\w+`)
	// A value inside SQL quotes; an escaped %%s is a literal, not a directive.
	reSQLQuotedInterp = regexp.MustCompile(`'"\s*\+\s*\w+|'%[sv]|(?:^|[^%])%[sv]'`)
)

// taintMarkers advertise request-derived data; an unquoted interpolation without one is a dynamic identifier and stays quiet.
var taintMarkers = []string{
	"userinput", "user_input", "request.", "req.", "form.",
	"params", "queryparam", "query_param", "filtervalue",
	"filter_value", "rawvalue", "raw_value", "r.url.query",
	"pathvalue", "formvalue",
}

func ruleSQLConcat(src lintSource) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for i, line := range strings.Split(stripComments(src.body), "\n") {
		if !reSQLConcatLiteral.MatchString(line) &&
			!reSQLSprintf.MatchString(line) &&
			!reSQLBuilderConcat.MatchString(line) {
			continue
		}
		if hasLegacyAnnotation(src.lines, i+1, "safe-sql:") {
			continue
		}
		lower := strings.ToLower(line)
		suspicious := reSQLQuotedInterp.MatchString(line)
		for _, marker := range taintMarkers {
			if strings.Contains(lower, marker) {
				suspicious = true
				break
			}
		}
		if !suspicious {
			continue
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleSQLStringConcat, File: src.rel, Line: i + 1,
			Message: "request-derived value concatenated into a SQL statement",
			Snippet: strings.TrimSpace(src.lines[min(i, len(src.lines)-1)]),
		})
	}
	return out
}

var sqlCallNames = map[string]bool{
	"Query": true, "QueryRow": true, "QueryContext": true, "QueryRowContext": true,
	"Exec": true, "ExecContext": true,
	"Where": true, "Having": true, "OrderBy": true, "GroupBy": true,
}

var reSQLVerbText = regexp.MustCompile(`(?i)\b` + sqlVerb)

// ruleSQLConcatAST covers what the line scan cannot see: raw-string literals and concatenations split across lines.
func ruleSQLConcatAST(src lintSource, already []contracts.Diagnostic) []contracts.Diagnostic {
	seen := map[int]bool{}
	for _, d := range already {
		seen[d.Line] = true
	}
	var out []contracts.Diagnostic
	ast.Inspect(src.file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !sqlCallNames[sel.Sel.Name] {
			return true
		}
		for _, arg := range call.Args {
			text, isConcat := sqlConcatText(arg)
			if !isConcat || !reSQLVerbText.MatchString(text) {
				continue
			}
			start := src.fset.Position(arg.Pos())
			end := src.fset.Position(arg.End())
			reported := false
			for ln := start.Line; ln <= end.Line && ln <= len(src.lines); ln++ {
				if seen[ln] || hasLegacyAnnotation(src.lines, ln, "safe-sql:") {
					reported = true
					break
				}
			}
			if reported {
				continue
			}
			suspicious := reSQLQuotedInterp.MatchString(text)
			expression := strings.ToLower(src.body[start.Offset:end.Offset])
			for _, marker := range taintMarkers {
				if strings.Contains(expression, marker) {
					suspicious = true
					break
				}
			}
			if !suspicious {
				continue
			}
			out = append(out, contracts.Diagnostic{
				RuleID:  contracts.RuleSQLStringConcat,
				File:    src.rel,
				Line:    start.Line,
				Message: "request-derived value concatenated into a SQL statement",
				Snippet: strings.TrimSpace(src.lines[min(start.Line-1, len(src.lines)-1)]),
			})
		}
		return true
	})
	return out
}

// sqlConcatText renders e with each non-literal operand as %s; ok is false when e is no string concatenation.
func sqlConcatText(e ast.Expr) (string, bool) {
	var sb strings.Builder
	lit, concat := false, false
	var walk func(ast.Expr)
	walk = func(e ast.Expr) {
		switch v := unparen(e).(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					sb.WriteString(s)
					lit = true
					return
				}
			}
			sb.WriteString("%s")
		case *ast.BinaryExpr:
			if v.Op == token.ADD {
				concat = true
				walk(v.X)
				walk(v.Y)
				return
			}
			sb.WriteString("%s")
		default:
			sb.WriteString("%s")
		}
	}
	walk(e)
	return sb.String(), lit && concat
}

// GOFASTR1403: render.HTML on a concatenation.

var reRenderHTMLConcat = regexp.MustCompile(`render\.HTML\([^)]*\+[^)]*\)`)

func ruleHTMLConcat(src lintSource) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for i, line := range src.lines {
		if !reRenderHTMLConcat.MatchString(line) {
			continue
		}
		if hasLegacyAnnotation(src.lines, i+1, "safe-html:") {
			continue
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleHTMLConcat, File: src.rel, Line: i + 1,
			Message: "render.HTML receives a concatenated string: the interpolated half is not escaped",
			Snippet: strings.TrimSpace(line),
		})
	}
	return out
}

// ruleHTMLBuiltAST covers Sprintf/Sprint/Join builds, split or call-led concatenations and locals assigned such a build.
func ruleHTMLBuiltAST(src lintSource, already []contracts.Diagnostic) []contracts.Diagnostic {
	seen := map[int]bool{}
	for _, d := range already {
		seen[d.Line] = true
	}
	aliases := importAliases(src.file)
	var out []contracts.Diagnostic
	for _, decl := range src.file.Decls {
		var locals map[string][]ast.Expr
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			locals = localAssignments(fn.Body)
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := qualifiedCall(n, aliases, "core/render", "HTML")
			if !ok || len(call.Args) != 1 {
				return true
			}
			how := htmlBuild(call.Args[0], aliases)
			if id, isIdent := unparen(call.Args[0]).(*ast.Ident); how == "" && isIdent {
				for _, rhs := range locals[id.Name] {
					if b := htmlBuild(rhs, aliases); b != "" {
						how = b + " (assigned to " + id.Name + ")"
						break
					}
				}
			}
			if how == "" {
				return true
			}
			line := src.fset.Position(call.Pos()).Line
			if seen[line] || hasLegacyAnnotation(src.lines, line, "safe-html:") {
				return true
			}
			seen[line] = true
			d := src.diagnostic(contracts.RuleHTMLConcat, call.Pos(),
				"render.HTML receives a string built by "+how+": the interpolated value is not escaped")
			out = append(out, d)
			return true
		})
	}
	return out
}

func localAssignments(body *ast.BlockStmt) map[string][]ast.Expr {
	locals := map[string][]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if len(v.Lhs) == len(v.Rhs) {
				for i, lhs := range v.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						locals[id.Name] = append(locals[id.Name], v.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			if len(v.Names) == len(v.Values) {
				for i, id := range v.Names {
					locals[id.Name] = append(locals[id.Name], v.Values[i])
				}
			}
		}
		return true
	})
	return locals
}

// isOSStdin reports whether v reads os's Stdin under any import
// spelling: the package name, an alias, or a suffixed path.
func isOSStdin(v *ast.SelectorExpr, aliases map[string]string) bool {
	if v.Sel == nil || v.Sel.Name != "Stdin" {
		return false
	}
	id, ok := v.X.(*ast.Ident)
	if !ok {
		return false
	}
	p := aliases[id.Name]
	return p == "os" || strings.HasSuffix(p, "/os") || id.Name == "os"
}

func unparen(e ast.Expr) ast.Expr {
	for {
		paren, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = paren.X
	}
}

// htmlBuild names how e builds a string around a non-literal value, or "".
func htmlBuild(e ast.Expr, aliases map[string]string) string {
	e = unparen(e)
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		if !constantString(bin) {
			return "concatenation"
		}
		return ""
	}
	for _, fn := range []string{"Sprintf", "Sprint", "Sprintln"} {
		call, ok := qualifiedCall(e, aliases, "fmt", fn)
		if !ok {
			continue
		}
		for _, a := range call.Args {
			if _, lit := a.(*ast.BasicLit); !lit {
				return "fmt." + fn
			}
		}
	}
	if call, ok := qualifiedCall(e, aliases, "strings", "Join"); ok && len(call.Args) == 2 {
		if elems, isLit := unparen(call.Args[0]).(*ast.CompositeLit); isLit {
			for _, el := range elems.Elts {
				if !constantString(el) {
					return "strings.Join"
				}
			}
			if constantString(call.Args[1]) {
				return ""
			}
		}
		return "strings.Join"
	}
	return ""
}

func constantString(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return constantString(v.X)
	case *ast.BinaryExpr:
		return v.Op == token.ADD && constantString(v.X) && constantString(v.Y)
	}
	return false
}

// GOFASTR1402: POST form without a CSRF input.

var (
	reFormPOST   = regexp.MustCompile(`(?i)<form\b[^>]*method=["']?POST["']?`)
	reCSRFCall   = regexp.MustCompile(`\bCSRFInputFromCtx\s*\(`)
	reCSRFField  = regexp.MustCompile(`(?i)name=["']_csrf["']`)
	reCSRFExempt = regexp.MustCompile(`(?i)csrf-exempt:\s*\S`)
)

func ruleFormCSRF(src lintSource) []contracts.Diagnostic {
	stripped := stripComments(src.body)
	protections := len(reCSRFCall.FindAllString(stripped, -1)) +
		len(reCSRFField.FindAllString(stripped, -1)) +
		len(reCSRFExempt.FindAllString(src.body, -1))

	var formLines []int
	for i, line := range src.lines {
		if reFormPOST.MatchString(line) {
			formLines = append(formLines, i)
		}
	}
	if len(formLines) <= protections {
		return nil
	}
	var out []contracts.Diagnostic
	for _, idx := range formLines[:len(formLines)-protections] {
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleFormWithoutCSRF, File: src.rel, Line: idx + 1,
			Message: fmt.Sprintf("this file renders %d POST form(s) but wires CSRF %d time(s)",
				len(formLines), protections),
			Snippet:  strings.TrimSpace(src.lines[idx]),
			Evidence: map[string]string{"forms": fmt.Sprint(len(formLines)), "protections": fmt.Sprint(protections)},
		})
	}
	return out
}

// GOFASTR1404: http.Cookie without security attributes.

func ruleInsecureCookie(p *contracts.Pass, rel string, file *ast.File, lines []string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	aliases := importAliases(file)
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Cookie" {
			return true
		}
		pkg, isIdent := sel.X.(*ast.Ident)
		if !isIdent || (aliases[pkg.Name] != "net/http" && (pkg.Name != "http" || aliases["http"] != "")) {
			return true
		}
		set := map[string]bool{}
		emptyValue, negativeMaxAge := false, false
		for _, elt := range lit.Elts {
			kv, isKV := elt.(*ast.KeyValueExpr)
			if !isKV {
				continue
			}
			key, isIdent := kv.Key.(*ast.Ident)
			if !isIdent {
				continue
			}
			switch key.Name {
			case "Value":
				if s, litOK := stringLit(kv.Value); litOK && s == "" {
					emptyValue = true
				}
			case "MaxAge":
				if unary, isUnary := kv.Value.(*ast.UnaryExpr); isUnary && unary.Op == token.SUB {
					negativeMaxAge = true
				}
			}
			if val, isBool := kv.Value.(*ast.Ident); isBool && val.Name == "false" {
				continue
			}
			set[key.Name] = true
		}
		if !set["Value"] || (emptyValue && negativeMaxAge) {
			return true
		}
		var missing []string
		for _, want := range []string{"HttpOnly", "Secure", "SameSite"} {
			if !set[want] {
				missing = append(missing, want)
			}
		}
		if len(missing) == 0 {
			return true
		}
		pos := p.Position(lit.Pos())
		if hasLegacyAnnotation(lines, pos.Line, "insecure-cookie:") {
			return true
		}
		d := diag(p, contracts.RuleInsecureCookie, rel, lit.Pos(),
			fmt.Sprintf("cookie is missing %s", strings.Join(missing, ", ")))
		d.Evidence = map[string]string{"missing": strings.Join(missing, ",")}
		d.Fix = cookieFix(p, rel, lit, missing, httpAlias(sel))
		out = append(out, d)
		return true
	})
	return out
}

func httpAlias(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name
	}
	return "http"
}

// cookieFix inserts the missing attributes before the closing brace; Apply gofmts the result.
func cookieFix(p *contracts.Pass, rel string, lit *ast.CompositeLit, missing []string, alias string) *contracts.SuggestedFix {
	if len(lit.Elts) == 0 || !lit.Rbrace.IsValid() {
		return nil
	}
	values := map[string]string{
		"HttpOnly": "true",
		"Secure":   "true",
		"SameSite": alias + ".SameSiteLaxMode",
	}
	var b strings.Builder
	last := lit.Elts[len(lit.Elts)-1]
	if !hasTrailingComma(p, rel, last.End(), lit.Rbrace) {
		b.WriteString(",")
	}
	for _, field := range missing {
		b.WriteString("\n" + field + ": " + values[field] + ",")
	}
	b.WriteString("\n")

	offset := p.Position(lit.Rbrace).Offset
	if offset <= 0 {
		return nil
	}
	src, ok := p.Source(rel)
	anchorStart := p.Position(last.End()).Offset
	if !ok || anchorStart <= 0 || anchorStart > offset || offset+1 > len(src) {
		return nil
	}
	tail := string(src[anchorStart : offset+1])
	return &contracts.SuggestedFix{
		Description: fmt.Sprintf("add %s to the cookie", strings.Join(missing, ", ")),
		Edits: []contracts.TextEdit{{
			File: rel, Start: anchorStart, End: offset + 1, Old: tail,
			New: strings.TrimSuffix(tail, "}") + b.String() + "}",
		}},
	}
}

func hasTrailingComma(p *contracts.Pass, rel string, from, to token.Pos) bool {
	body, ok := p.Source(rel)
	if !ok {
		return false
	}
	start, end := p.Position(from).Offset, p.Position(to).Offset
	if start < 0 || end > len(body) || start >= end {
		return false
	}
	return strings.Contains(string(body[start:end]), ",")
}

// GOFASTR1405: secret assigned from a literal.

var reSecretName = regexp.MustCompile(`(?i)(secret|password|passwd|api_?key|apikey|token|private_?key|credential|client_?secret)`)

var secretPrefixes = []string{"sk_live_", "sk-live-", "sk-ant-", "ghp_", "gho_", "AKIA", "xoxb-", "xoxp-", "AIza"}

var reIdentifierish = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[._-][A-Za-z0-9]+)*$`)

// looksLikeCredential is the entropy gate: long, and three character classes with a digit.
func looksLikeCredential(v string) bool {
	if len(v) < 16 {
		return false
	}
	if strings.ContainsAny(v, " \t/\\%<>{}()") {
		return false
	}
	if reIdentifierish.MatchString(v) {
		return false
	}
	var upper, lower, digit, symbol int
	for _, r := range v {
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
			lower++
		case r >= '0' && r <= '9':
			digit++
		default:
			symbol++
		}
	}
	classes := 0
	for _, n := range []int{upper, lower, digit, symbol} {
		if n > 0 {
			classes++
		}
	}
	return classes >= 3 && digit > 0
}

func ruleHardcodedSecret(p *contracts.Pass, rel string, file *ast.File, lines []string) []contracts.Diagnostic {
	var out []contracts.Diagnostic

	report := func(name string, value ast.Expr) {
		lit, ok := stringLit(value)
		if !ok {
			return
		}
		byShape := false
		for _, prefix := range secretPrefixes {
			if strings.HasPrefix(lit, prefix) {
				byShape = true
				break
			}
		}
		if !byShape {
			if !reSecretName.MatchString(name) || !looksLikeCredential(lit) {
				return
			}
		}
		pos := p.Position(value.Pos())
		if hasLegacyAnnotation(lines, pos.Line, "not-a-secret:") {
			return
		}
		d := diag(p, contracts.RuleHardcodedSecret, rel, value.Pos(),
			fmt.Sprintf("%s is assigned a literal value", name))
		d.Evidence = map[string]string{"name": name, "length": fmt.Sprint(len(lit))}
		d.Snippet, d.RedactSnippet = "", true
		out = append(out, d)
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				if i >= len(v.Rhs) {
					break
				}
				if id, ok := lhs.(*ast.Ident); ok {
					report(id.Name, v.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, name := range v.Names {
				if i < len(v.Values) {
					report(name.Name, v.Values[i])
				}
			}
		case *ast.KeyValueExpr:
			if id, ok := v.Key.(*ast.Ident); ok {
				report(id.Name, v.Value)
			}
		}
		return true
	})
	return out
}

// GOFASTR1406: X-Forwarded-Proto used as a scheme without an enum check.

func ruleForwardedProto(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, fn := range functionsIn(file) {
		var gets []*ast.CallExpr
		ast.Inspect(fn.body, func(n ast.Node) bool {
			if call, ok := forwardedProtoGet(n); ok {
				gets = append(gets, call)
			}
			return true
		})
		if len(gets) == 0 {
			continue
		}
		for _, get := range gets {
			holders := assignedIdents(fn.body, get)
			if protoEnumChecked(fn.body, get, holders) {
				continue
			}
			if !reflectedScheme(fn.body, get, holders) {
				continue
			}
			d := diag(p, contracts.RuleForwardedProtoEnum, rel, get.Pos(),
				"X-Forwarded-Proto is request-controlled and is spliced into output as a scheme with no http/https enum check in this function: a forged value (https://evil.example/p, or https,http from a proxy chain) is reflected verbatim")
			d.Evidence = map[string]string{"header": "X-Forwarded-Proto"}
			out = append(out, d)
		}
	}
	return out
}

func forwardedProtoGet(n ast.Node) (*ast.CallExpr, bool) {
	recv, method, call, ok := selectorCall(n)
	if !ok || method != "Get" || len(call.Args) != 1 {
		return nil, false
	}
	switch r := recv.(type) {
	case *ast.SelectorExpr:
		if r.Sel == nil || r.Sel.Name != "Header" {
			return nil, false
		}
	case *ast.Ident:
	default:
		return nil, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		return nil, false
	}
	v, ok := stringLit(lit)
	if !ok || !strings.EqualFold(v, "x-forwarded-proto") {
		return nil, false
	}
	return call, true
}

func protoEnumChecked(body ast.Node, call *ast.CallExpr, holders map[string]bool) bool {
	touches := func(e ast.Expr) bool { return exprTouches(e, call, holders) }
	checked := false
	ast.Inspect(body, func(n ast.Node) bool {
		if checked {
			return false
		}
		switch v := n.(type) {
		case *ast.BinaryExpr:
			if v.Op != token.EQL && v.Op != token.NEQ {
				return true
			}
			if lit, ok := v.X.(*ast.BasicLit); ok && isSchemeLit(lit) && touches(v.Y) {
				checked = true
			}
			if lit, ok := v.Y.(*ast.BasicLit); ok && isSchemeLit(lit) && touches(v.X) {
				checked = true
			}
		case *ast.CallExpr:
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel != nil &&
				strings.EqualFold(sel.Sel.Name, "EqualFold") {
				for i, a := range v.Args {
					lit, ok := a.(*ast.BasicLit)
					if !ok || !isSchemeLit(lit) {
						continue
					}
					for j, b := range v.Args {
						if j != i && touches(b) {
							checked = true
						}
					}
				}
			}
		case *ast.SwitchStmt:
			if !touches(v.Tag) {
				return true
			}
			for _, cl := range v.Body.List {
				cc, ok := cl.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range cc.List {
					if lit, ok := e.(*ast.BasicLit); ok && isSchemeLit(lit) {
						checked = true
					}
				}
			}
		}
		return !checked
	})
	return checked
}

func isSchemeLit(lit *ast.BasicLit) bool {
	if lit.Kind != token.STRING {
		return false
	}
	v, ok := stringLit(lit)
	return ok && (v == "http" || v == "https")
}

var reSchemeVar = regexp.MustCompile(`(?i)scheme|proto`)

func assignedIdents(body ast.Node, call *ast.CallExpr) map[string]bool {
	holders := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		a, ok := n.(*ast.AssignStmt)
		if !ok || len(a.Lhs) != len(a.Rhs) {
			return true
		}
		for i, rhs := range a.Rhs {
			if rhs == call {
				if id, ok := a.Lhs[i].(*ast.Ident); ok {
					holders[id.Name] = true
				}
			}
		}
		return true
	})
	return holders
}

// reflectedScheme reports whether the header value reaches output as a scheme.
func reflectedScheme(body ast.Node, call *ast.CallExpr, holders map[string]bool) bool {
	touches := func(e ast.Expr) bool { return exprTouches(e, call, holders) }
	reflected := false
	ast.Inspect(body, func(n ast.Node) bool {
		if reflected {
			return false
		}
		switch v := n.(type) {
		case *ast.BinaryExpr:
			if v.Op == token.ADD && (touches(v.X) || touches(v.Y)) {
				reflected = true
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || !reSchemeVar.MatchString(id.Name) {
					continue
				}
				rhs := v.Rhs
				if len(rhs) == i {
					rhs = nil
				} else if len(rhs) == len(v.Lhs) {
					rhs = rhs[i : i+1]
				}
				for _, r := range rhs {
					if touches(r) {
						reflected = true
					}
				}
			}
		case *ast.CallExpr:
			name := exprText(v.Fun)
			if strings.Contains(strings.ToLower(name), "url") {
				for _, a := range v.Args {
					if touches(a) {
						reflected = true
					}
				}
			}
			if fun, ok := callFunName(v); ok && (fun == "Sprintf" || fun == "Fprintf") && len(v.Args) > 1 {
				if lit, ok := v.Args[0].(*ast.BasicLit); ok {
					if f, ok := stringLit(lit); ok && strings.Contains(f, "://") {
						for _, a := range v.Args[1:] {
							if touches(a) {
								reflected = true
							}
						}
					}
				}
			}
		case *ast.CompositeLit:
			for _, e := range v.Elts {
				kv, ok := e.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Scheme" && touches(kv.Value) {
					reflected = true
				}
			}
		}
		return !reflected
	})
	return reflected
}

func callFunName(call *ast.CallExpr) (string, bool) {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if fun.Sel != nil {
			return fun.Sel.Name, true
		}
	case *ast.Ident:
		return fun.Name, true
	}
	return "", false
}

func exprTouches(e ast.Expr, call *ast.CallExpr, holders map[string]bool) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return holders[v.Name]
	case *ast.CallExpr:
		if v == call {
			return true
		}
		for _, a := range v.Args {
			if exprTouches(a, call, holders) {
				return true
			}
		}
	case *ast.SelectorExpr:
		return exprTouches(v.X, call, holders)
	case *ast.BinaryExpr:
		return exprTouches(v.X, call, holders) || exprTouches(v.Y, call, holders)
	case *ast.ParenExpr:
		return exprTouches(v.X, call, holders)
	case *ast.UnaryExpr:
		return exprTouches(v.X, call, holders)
	case *ast.StarExpr:
		return exprTouches(v.X, call, holders)
	case *ast.IndexExpr:
		return exprTouches(v.X, call, holders)
	}
	return false
}

// GOFASTR1407: raw JSON decoding of a request body or client frame outside the binder.

func ruleRawJSONBody(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	if rel == "core/handler" || strings.HasPrefix(rel, "core/handler/") {
		return nil
	}
	pkg := rawJSONPackageTaint(p, rel)
	aliases := importAliases(file)
	var out []contracts.Diagnostic
	for _, fn := range functionsIn(file) {
		reqs := httpRequestParamNames(fn.node, aliases)
		var seeds map[string]taintKind
		if decl, ok := fn.node.(*ast.FuncDecl); ok {
			seeds = pkg.paramTaint[decl]
		}
		tainted := taintFromBody(fn.body, reqs, pkg.helpers, seeds, pkg.fields, aliases, pkg)
		ast.Inspect(fn.body, func(n ast.Node) bool {
			if call, ok := qualifiedCall(n, aliases, "encoding/json", "NewDecoder"); ok && len(call.Args) == 1 {
				if k := exprFromRequestBody(call.Args[0], tainted, reqs, pkg.fields, aliases); k != 0 && !isKeyWalk(fn.body, call, aliases) {
					out = append(out, rawJSONDiag(p, rel, call, k))
				}
			}
			if call, ok := qualifiedCall(n, aliases, "encoding/json", "Unmarshal"); ok && len(call.Args) > 0 {
				if k := exprFromRequestBody(call.Args[0], tainted, reqs, pkg.fields, aliases); k != 0 {
					out = append(out, rawJSONDiag(p, rel, call, k))
				}
			}
			for _, name := range []string{"Unmarshal", "Decode"} {
				if call, ok := qualifiedCall(n, aliases, jsonexactPath, name); ok && len(call.Args) > 0 {
					if k := exprFromRequestBody(call.Args[0], tainted, reqs, pkg.fields, aliases); k != 0 {
						out = append(out, rawJSONDiag(p, rel, call, k))
					}
				}
			}
			return true
		})
	}
	return out
}

const jsonexactPath = "github.com/DonaldMurillo/gofastr/internal/jsonexact"

func rawJSONDiag(p *contracts.Pass, rel string, call *ast.CallExpr, k taintKind) contracts.Diagnostic {
	const fix = "decode via handler.Bind or a strict top-level key walk (battery/auth decodeJSONLimitedStrict is the model)"
	source, msg := "request body",
		"request body decoded with encoding/json outside core/handler: stdlib keeps the last duplicate key and matches key case-insensitively, so a smuggled body can resolve by parser accident — "+fix
	switch {
	case k == taintFrame:
		source = "websocket frame"
		msg = "websocket frame decoded with encoding/json outside core/handler: stdlib keeps the last duplicate key and matches key case-insensitively, so a frame a first-read intermediary parsed one way dispatches another — " + fix
	case k&taintBody != 0 && k&taintFrame != 0:
		source = "request body or websocket frame"
		msg = "client-controlled JSON decoded with encoding/json outside core/handler: stdlib keeps the last duplicate key and matches key case-insensitively, so a smuggled body or frame can resolve by parser accident — " + fix
	}
	d := diag(p, contracts.RuleRawJSONBodyDecode, rel, call.Pos(), msg)
	d.Evidence = map[string]string{"source": source}
	return d
}

type funcScope struct {
	node ast.Node
	body *ast.BlockStmt
}

func functionsIn(file *ast.File) []*funcScope {
	var out []*funcScope
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			if v.Body != nil {
				out = append(out, &funcScope{node: v, body: v.Body})
			}
		case *ast.FuncLit:
			out = append(out, &funcScope{node: v, body: v.Body})
		}
		return true
	})
	return out
}

func httpRequestParamNames(fn ast.Node, aliases map[string]string) map[string]bool {
	httpAlias := map[string]bool{}
	for name, path := range aliases {
		if path == "net/http" {
			httpAlias[name] = true
		}
	}
	if len(httpAlias) == 0 {
		return nil
	}
	var ft *ast.FuncType
	switch v := fn.(type) {
	case *ast.FuncDecl:
		ft = v.Type
	case *ast.FuncLit:
		ft = v.Type
	default:
		return nil
	}
	if ft == nil || ft.Params == nil {
		return nil
	}
	names := map[string]bool{}
	for _, param := range ft.Params.List {
		star, ok := param.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		sel, ok := star.X.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Request" {
			continue
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && httpAlias[pkg.Name] {
			for _, name := range param.Names {
				names[name.Name] = true
			}
		}
	}
	return names
}

type taintKind uint8

const (
	taintBody taintKind = 1 << iota
	taintFrame
)

// rawJSONPkg is the package-wide taint context for one directory, computed once per pass.
type rawJSONPkg struct {
	files        map[string]*ast.File
	aliases      map[string]map[string]string
	helpers      map[string]bool
	paramTaint   map[*ast.FuncDecl]map[string]taintKind
	fields       map[string]taintKind
	structFields map[string]map[string]bool
	methodRecv   map[string][]string
	// sourceFields hold a client-controlled source directly; only they feed methodResultTaint.
	sourceFields map[string]bool
}

func rawJSONPackageTaint(p *contracts.Pass, rel string) *rawJSONPkg {
	dir := path.Dir(rel)
	v := p.Memo("rawjsonbody.package:"+dir, func() any {
		pkg := &rawJSONPkg{
			files:      map[string]*ast.File{},
			aliases:    map[string]map[string]string{},
			paramTaint: map[*ast.FuncDecl]map[string]taintKind{},
			fields:     map[string]taintKind{},
		}
		for _, f := range p.AppFiles() {
			if path.Dir(f.Rel) != dir {
				continue
			}
			af, ok := p.AST(f.Rel)
			if !ok {
				continue
			}
			pkg.files[f.Rel] = af
			pkg.aliases[f.Rel] = importAliases(af)
		}
		pkg.indexTypes()
		seedSourceFields(pkg)
		seedScannerFields(pkg)
		pkg.helpers = packageBodyReaders(pkg)
		targets := packageCallTargets(pkg)
		for changed := true; changed; {
			changed = false
			for fileRel, af := range pkg.files {
				aliases := pkg.aliases[fileRel]
				for _, fn := range functionsIn(af) {
					reqs := httpRequestParamNames(fn.node, aliases)
					var seeds map[string]taintKind
					if decl, ok := fn.node.(*ast.FuncDecl); ok {
						seeds = pkg.paramTaint[decl]
					}
					tainted := taintFromBody(fn.body, reqs, pkg.helpers, seeds, pkg.fields, aliases, pkg)
					ast.Inspect(fn.body, func(n ast.Node) bool {
						call, ok := n.(*ast.CallExpr)
						if !ok {
							return true
						}
						var name string
						isMethod := false
						switch fun := call.Fun.(type) {
						case *ast.Ident:
							name = fun.Name
						case *ast.SelectorExpr:
							if fun.Sel != nil {
								name, isMethod = fun.Sel.Name, true
							}
						}
						if name == "" {
							return true
						}
						for _, decl := range targets[name] {
							if (decl.Recv != nil) != isMethod || decl.Type == nil || decl.Type.Params == nil {
								continue
							}
							for i, arg := range call.Args {
								if i >= len(decl.Type.Params.List) {
									break
								}
								k := exprFromRequestBody(arg, tainted, reqs, pkg.fields, aliases)
								if k == 0 {
									continue
								}
								if pkg.paramTaint[decl] == nil {
									pkg.paramTaint[decl] = map[string]taintKind{}
								}
								for _, param := range decl.Type.Params.List[i].Names {
									if pkg.paramTaint[decl][param.Name]&k != k {
										pkg.paramTaint[decl][param.Name] |= k
										changed = true
									}
								}
							}
						}
						return true
					})
					ast.Inspect(fn.body, func(n ast.Node) bool {
						a, ok := n.(*ast.AssignStmt)
						if !ok || len(a.Lhs) != len(a.Rhs) {
							return true
						}
						for i, lhs := range a.Lhs {
							sel, ok := lhs.(*ast.SelectorExpr)
							if !ok || sel.Sel == nil {
								continue
							}
							if id, ok := sel.X.(*ast.Ident); ok && reqs[id.Name] && sel.Sel.Name == "Body" {
								continue
							}
							k := exprFromRequestBody(a.Rhs[i], tainted, reqs, pkg.fields, aliases)
							if k == 0 {
								continue
							}
							if pkg.fields[sel.Sel.Name]&k != k {
								pkg.fields[sel.Sel.Name] |= k
								changed = true
							}
						}
						return true
					})
					ast.Inspect(fn.body, func(n ast.Node) bool {
						lit, ok := n.(*ast.CompositeLit)
						if !ok {
							return true
						}
						for _, elt := range lit.Elts {
							kv, ok := elt.(*ast.KeyValueExpr)
							if !ok {
								continue
							}
							key, ok := kv.Key.(*ast.Ident)
							if !ok {
								continue // a map literal keys by value
							}
							k := exprFromRequestBody(kv.Value, tainted, reqs, pkg.fields, aliases)
							if k == 0 {
								continue
							}
							if pkg.fields[key.Name]&k != k {
								pkg.fields[key.Name] |= k
								changed = true
							}
						}
						return true
					})
				}
			}
		}
		return pkg
	})
	pkg, _ := v.(*rawJSONPkg)
	return pkg
}

func packageCallTargets(pkg *rawJSONPkg) map[string][]*ast.FuncDecl {
	targets := map[string][]*ast.FuncDecl{}
	for _, af := range pkg.files {
		for _, fn := range functionsIn(af) {
			decl, ok := fn.node.(*ast.FuncDecl)
			if !ok || decl.Name == nil {
				continue
			}
			targets[decl.Name.Name] = append(targets[decl.Name.Name], decl)
		}
	}
	return targets
}

// taintFromBody returns the locals holding client-controlled bytes, to a fixpoint.
func taintFromBody(body ast.Node, reqs map[string]bool, helpers map[string]bool, seeds map[string]taintKind, fields map[string]taintKind, aliases map[string]string, pkg *rawJSONPkg) map[string]taintKind {
	var assigns []*ast.AssignStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			assigns = append(assigns, a)
		}
		return true
	})
	tainted := make(map[string]taintKind, len(seeds))
	for name, k := range seeds {
		tainted[name] = k
	}
	for changed := true; changed; {
		changed = false
		for _, a := range assigns {
			var k taintKind
			for _, rhs := range a.Rhs {
				k |= exprFromRequestBody(rhs, tainted, reqs, fields, aliases)
				if call, ok := rhs.(*ast.CallExpr); ok {
					if len(helpers) > 0 {
						k |= helperCallTainted(call, tainted, reqs, fields, helpers, aliases)
					}
					k |= frameReadKind(call, aliases)
					if pkg != nil {
						k |= pkg.methodResultTaint(call)
					}
				}
			}
			if k == 0 {
				continue
			}
			for _, lhs := range a.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && tainted[id.Name]&k != k {
					tainted[id.Name] |= k
					changed = true
				}
			}
		}
	}
	return tainted
}

func (pkg *rawJSONPkg) indexTypes() {
	pkg.structFields = map[string]map[string]bool{}
	pkg.methodRecv = map[string][]string{}
	pkg.sourceFields = map[string]bool{}
	for _, af := range pkg.files {
		for _, decl := range af.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok || ts.Name == nil {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}
				fields := map[string]bool{}
				for _, fld := range st.Fields.List {
					for _, name := range fld.Names {
						fields[name.Name] = true
					}
				}
				pkg.structFields[ts.Name.Name] = fields
			}
		}
		for _, fn := range functionsIn(af) {
			d, ok := fn.node.(*ast.FuncDecl)
			if !ok || d.Recv == nil || d.Name == nil || len(d.Recv.List) == 0 {
				continue
			}
			if base := recvBaseName(d); base != "" {
				pkg.methodRecv[d.Name.Name] = append(pkg.methodRecv[d.Name.Name], base)
			}
		}
	}
}

func (pkg *rawJSONPkg) methodResultTaint(call *ast.CallExpr) taintKind {
	if len(pkg.sourceFields) == 0 || pkg.structFields == nil {
		return 0
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return 0
	}
	for _, recv := range pkg.methodRecv[sel.Sel.Name] {
		for field := range pkg.structFields[recv] {
			if pkg.sourceFields[field] {
				return taintFrame
			}
		}
	}
	return 0
}

func seedSourceFields(pkg *rawJSONPkg) {
	var direct func(e ast.Expr, tainted map[string]bool, aliases map[string]string) bool
	direct = func(e ast.Expr, tainted map[string]bool, aliases map[string]string) bool {
		switch v := e.(type) {
		case *ast.Ident:
			return tainted[v.Name]
		case *ast.CallExpr:
			if frameReadKind(v, aliases) != 0 {
				return true
			}
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel != nil && readerWrapper[sel.Sel.Name] {
				if direct(sel.X, tainted, aliases) {
					return true
				}
				for _, a := range v.Args {
					if direct(a, tainted, aliases) {
						return true
					}
				}
			}
		case *ast.SelectorExpr:
			if isOSStdin(v, aliases) {
				return true
			}
		case *ast.ParenExpr:
			return direct(v.X, tainted, aliases)
		case *ast.UnaryExpr:
			return direct(v.X, tainted, aliases)
		}
		return false
	}
	mark := func(fieldName string) {
		if fieldName != "" {
			pkg.sourceFields[fieldName] = true
		}
	}
	for rel, af := range pkg.files {
		aliases := pkg.aliases[rel]
		for _, fn := range functionsIn(af) {
			tainted := map[string]bool{}
			for changed := true; changed; {
				changed = false
				ast.Inspect(fn.body, func(n ast.Node) bool {
					a, ok := n.(*ast.AssignStmt)
					if !ok {
						return true
					}
					for i, rhs := range a.Rhs {
						if !direct(rhs, tainted, aliases) {
							continue
						}
						if i < len(a.Lhs) {
							if id, ok := a.Lhs[i].(*ast.Ident); ok && !tainted[id.Name] {
								tainted[id.Name] = true
								changed = true
							}
						}
					}
					return true
				})
			}
			ast.Inspect(fn.body, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.AssignStmt:
					for i, lhs := range v.Lhs {
						if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel != nil && i < len(v.Rhs) {
							if direct(v.Rhs[i], tainted, aliases) {
								mark(sel.Sel.Name)
							}
						}
					}
				case *ast.CompositeLit:
					for _, elt := range v.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						if direct(kv.Value, tainted, aliases) {
							mark(key.Name)
						}
					}
				}
				return true
			})
		}
	}
}

func seedScannerFields(pkg *rawJSONPkg) {
	for rel, af := range pkg.files {
		aliases := pkg.aliases[rel]
		for _, fn := range functionsIn(af) {
			fd, ok := fn.node.(*ast.FuncDecl)
			if !ok || fd.Type == nil || fd.Type.Params == nil {
				continue
			}
			transport := map[string]bool{}
			for _, param := range fd.Type.Params.List {
				sel, ok := param.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil {
					continue
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				switch aliases[pkgIdent.Name] {
				case "io", "net":
					switch sel.Sel.Name {
					case "Reader", "ReadCloser", "Conn":
						for _, name := range param.Names {
							transport[name.Name] = true
						}
					}
				}
			}
			if len(transport) == 0 {
				continue
			}
			scanners := map[string]bool{}
			ast.Inspect(fn.body, func(n ast.Node) bool {
				a, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, rhs := range a.Rhs {
					call, ok := rhs.(*ast.CallExpr)
					if !ok {
						continue
					}
					if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel == nil || sel.Sel.Name != "NewScanner" || len(call.Args) == 0 {
						continue
					}
					if id, ok := call.Args[0].(*ast.Ident); ok && transport[id.Name] {
						if i < len(a.Lhs) {
							if lhs, ok := a.Lhs[i].(*ast.Ident); ok {
								scanners[lhs.Name] = true
							}
						}
					}
				}
				return true
			})
			if len(scanners) == 0 {
				continue
			}
			mark := func(fieldName string) {
				if fieldName != "" {
					pkg.sourceFields[fieldName] = true
					pkg.fields[fieldName] |= taintFrame
				}
			}
			ast.Inspect(fn.body, func(n ast.Node) bool {
				switch v := n.(type) {
				case *ast.AssignStmt:
					for i, lhs := range v.Lhs {
						if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel != nil && i < len(v.Rhs) {
							if id, ok := v.Rhs[i].(*ast.Ident); ok && scanners[id.Name] {
								mark(sel.Sel.Name)
							}
						}
					}
				case *ast.CompositeLit:
					for _, elt := range v.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						if id, ok := kv.Value.(*ast.Ident); ok && scanners[id.Name] {
							mark(key.Name)
						}
					}
				}
				return true
			})
		}
	}
}

func packageBodyReaders(pkg *rawJSONPkg) map[string]bool {
	out := map[string]bool{}
	for rel, af := range pkg.files {
		aliases := pkg.aliases[rel]
		for _, fn := range functionsIn(af) {
			decl, ok := fn.node.(*ast.FuncDecl)
			if !ok || decl.Name == nil || decl.Recv != nil {
				continue // methods are not bare-name callable
			}
			reqs := httpRequestParamNames(fn.node, aliases)
			if len(reqs) == 0 {
				continue
			}
			tainted := taintFromBody(fn.body, reqs, nil, nil, nil, aliases, pkg)
			derived := false
			ast.Inspect(fn.body, func(n ast.Node) bool {
				ret, ok := n.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				for _, r := range ret.Results {
					if exprFromRequestBody(r, tainted, reqs, nil, aliases) != 0 {
						derived = true
						return false
					}
				}
				return true
			})
			if derived {
				out[decl.Name.Name] = true
			}
		}
	}
	return out
}

func helperCallTainted(call *ast.CallExpr, tainted map[string]taintKind, reqs map[string]bool, fields map[string]taintKind, helpers map[string]bool, aliases map[string]string) taintKind {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || !helpers[id.Name] {
		return 0
	}
	for _, a := range call.Args {
		if k := exprFromRequestBody(a, tainted, reqs, fields, aliases); k != 0 {
			return k
		}
		touches := false
		ast.Inspect(a, func(n ast.Node) bool {
			if arg, ok := n.(*ast.Ident); ok && reqs[arg.Name] {
				touches = true
			}
			return !touches
		})
		if touches {
			return taintBody
		}
	}
	return 0
}

// readerWrapper names the calls whose result is the same client bytes as their argument or receiver.
var readerWrapper = map[string]bool{
	"ReadAll":        true,
	"MaxBytesReader": true,
	"LimitReader":    true,
	"NopCloser":      true,
	"TeeReader":      true,
	"NewReader":      true,
	"NewScanner":     true,
	"append":         true,
	"TrimSpace":      true,
	"Bytes":          true,
}

var wsFrameReads = map[string]bool{"readFrame": true}

func importsStream(aliases map[string]string) bool {
	for _, p := range aliases {
		if p == "core/stream" || strings.HasSuffix(p, "/core/stream") {
			return true
		}
	}
	return false
}

func frameReadKind(call *ast.CallExpr, aliases map[string]string) taintKind {
	var name string
	selector := false
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		name = fn.Name
	case *ast.SelectorExpr:
		if fn.Sel != nil {
			name, selector = fn.Sel.Name, true
		}
	}
	if name == "" {
		return 0
	}
	if wsFrameReads[name] {
		return taintFrame
	}
	if selector && len(call.Args) == 0 {
		switch name {
		case "Read":
			if importsStream(aliases) {
				return taintFrame
			}
		case "StdoutPipe":
			return taintFrame
		}
	}
	return 0
}

// isKeyWalk reports whether this decoder only walks top-level keys (Token/More, Decode into json.RawMessage).
func isKeyWalk(body ast.Node, call *ast.CallExpr, aliases map[string]string) bool {
	decoders := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			for i, rhs := range v.Rhs {
				if rhs == call && i < len(v.Lhs) {
					if id, ok := v.Lhs[i].(*ast.Ident); ok {
						decoders[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for i, val := range v.Values {
				if val == call && i < len(v.Names) {
					decoders[v.Names[i].Name] = true
				}
			}
		}
		return true
	})
	if len(decoders) == 0 {
		return false
	}
	skips := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Values) > 0 || len(spec.Names) == 0 {
			return true
		}
		sel, ok := spec.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "RawMessage" {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && aliases[id.Name] == "encoding/json" {
			skips[spec.Names[0].Name] = true
		}
		return true
	})
	walked, clean := false, true
	ast.Inspect(body, func(n ast.Node) bool {
		use, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := use.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || !decoders[id.Name] {
			return true
		}
		switch sel.Sel.Name {
		case "Token", "More":
			walked = true
		case "Decode":
			if len(use.Args) != 1 {
				clean = false
				return true
			}
			arg := use.Args[0]
			if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.AND {
				arg = u.X
			}
			if skip, ok := arg.(*ast.Ident); ok && skips[skip.Name] {
				walked = true
				return true
			}
			clean = false
		default:
			clean = false
		}
		return true
	})
	return clean && walked
}

func exprFromRequestBody(e ast.Expr, tainted map[string]taintKind, reqs map[string]bool, fields map[string]taintKind, aliases map[string]string) taintKind {
	switch v := e.(type) {
	case *ast.Ident:
		return tainted[v.Name]
	case *ast.CallExpr:
		var k taintKind
		switch fn := v.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel != nil && readerWrapper[fn.Sel.Name] {
				k = exprFromRequestBody(fn.X, tainted, reqs, fields, aliases)
				for _, a := range v.Args {
					k |= exprFromRequestBody(a, tainted, reqs, fields, aliases)
				}
			}
		case *ast.Ident:
			if readerWrapper[fn.Name] {
				for _, a := range v.Args {
					k |= exprFromRequestBody(a, tainted, reqs, fields, aliases)
				}
			}
		}
		return k
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok && reqs[id.Name] && v.Sel != nil && v.Sel.Name == "Body" {
			return taintBody
		}
		if isOSStdin(v, aliases) {
			return taintFrame
		}
		if v.Sel != nil {
			if k := fields[v.Sel.Name]; k != 0 {
				return k
			}
		}
		return exprFromRequestBody(v.X, tainted, reqs, fields, aliases)
	case *ast.BinaryExpr:
		return exprFromRequestBody(v.X, tainted, reqs, fields, aliases) | exprFromRequestBody(v.Y, tainted, reqs, fields, aliases)
	case *ast.ParenExpr:
		return exprFromRequestBody(v.X, tainted, reqs, fields, aliases)
	case *ast.UnaryExpr:
		return exprFromRequestBody(v.X, tainted, reqs, fields, aliases)
	case *ast.StarExpr:
		return exprFromRequestBody(v.X, tainted, reqs, fields, aliases)
	case *ast.IndexExpr:
		return exprFromRequestBody(v.X, tainted, reqs, fields, aliases)
	case *ast.CompositeLit:
		var k taintKind
		for _, elt := range v.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				k |= exprFromRequestBody(kv.Value, tainted, reqs, fields, aliases)
				continue
			}
			k |= exprFromRequestBody(elt, tainted, reqs, fields, aliases)
		}
		return k
	}
	return 0
}

// GOFASTR1601: discarded Exec result.

var reIgnoredExec = regexp.MustCompile(`(?:^|[\s;{])_,\s*_\s*=\s*\S+\.Exec(?:Context)?\b`)

func runData(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		file, _ := p.AST(f.Rel)
		out = append(out, ruleIgnoredExec(newLintSource(f.Rel, string(body), p.FileSet(), file))...)
	}
	return out, nil
}

func ruleIgnoredExec(src lintSource) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	flagged := map[int]bool{}
	report := func(lineNo int) {
		if flagged[lineNo] || hasLegacyAnnotation(src.lines, lineNo, "best-effort", "ignore the error", "errors here") {
			return
		}
		flagged[lineNo] = true
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleIgnoredExec, File: src.rel, Line: lineNo,
			Message: "both the result and the error of this write are discarded",
			Snippet: strings.TrimSpace(src.lines[lineNo-1]),
		})
	}
	for i, line := range src.lines {
		if reIgnoredExec.MatchString(line) {
			report(i + 1)
		}
	}
	if src.file != nil {
		for _, pos := range bareExecStatements(src.file) {
			if line := src.fset.Position(pos).Line; line >= 1 && line <= len(src.lines) {
				report(line)
			}
		}
	}
	return out
}

func bareExecStatements(file *ast.File) []token.Pos {
	var out []token.Pos
	isExec := func(e ast.Expr) bool {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		return ok && (sel.Sel.Name == "Exec" || sel.Sel.Name == "ExecContext")
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.ExprStmt:
			if isExec(s.X) {
				out = append(out, s.Pos())
			}
		case *ast.DeferStmt:
			if isExec(s.Call) {
				out = append(out, s.Pos())
			}
		case *ast.GoStmt:
			if isExec(s.Call) {
				out = append(out, s.Pos())
			}
		}
		return true
	})
	return out
}
