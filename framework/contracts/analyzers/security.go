package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "security",
		Doc:  "Injection, CSRF, cookie attributes, committed secrets, reflected proxy headers, and raw body decodes.",
		Rules: []string{
			contracts.RuleSQLStringConcat,
			contracts.RuleFormWithoutCSRF,
			contracts.RuleHTMLConcat,
			contracts.RuleInsecureCookie,
			contracts.RuleHardcodedSecret,
			contracts.RuleForwardedProtoEnum,
			contracts.RuleRawJSONBodyDecode,
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
		lines := strings.Split(string(body), "\n")
		out = append(out, ruleSQLConcat(p, f.Rel, string(body), lines)...)
		out = append(out, ruleHTMLConcat(p, f.Rel, lines)...)
		out = append(out, ruleFormCSRF(p, f.Rel, string(body), lines)...)
		if file, parsed := p.AST(f.Rel); parsed {
			out = append(out, ruleInsecureCookie(p, f.Rel, file, lines)...)
			out = append(out, ruleHardcodedSecret(p, f.Rel, file, lines)...)
			out = append(out, ruleForwardedProto(p, f.Rel, file)...)
			out = append(out, ruleRawJSONBody(p, f.Rel, file)...)
		}
	}
	return out, nil
}

// ----------------------------------------------------------------------
// GOFASTR1401: SQL built by concatenation.
// ----------------------------------------------------------------------

var (
	// The INSERT anchor accepts SQLite's `INSERT OR IGNORE/REPLACE INTO`
	// and MySQL's `INSERT IGNORE INTO`, all real statement forms.
	sqlVerb            = `(?:SELECT\s|INSERT\s+(?:(?:OR\s+\w+|IGNORE)\s+)?INTO\s|UPDATE\s|DELETE\s+FROM\s)`
	reSQLConcatLiteral = regexp.MustCompile(`(?i)"[^"]*\b` + sqlVerb + `[^"]*"\s*\+\s*\w+`)
	reSQLSprintf       = regexp.MustCompile(`(?i)fmt\.S?(?:print|printf)\(\s*"[^"]*\b(?:` + sqlVerb + `|WHERE\s|HAVING\s)[^"]*%[sv]`)
	reSQLBuilderConcat = regexp.MustCompile(`\.(?:Where|Having|OrderBy|GroupBy)\(\s*"[^"]*"\s*\+\s*\w+`)
	// Quote-adjacent interpolation: the variable or directive sits inside
	// SQL single-quotes. That is a VALUE position. A dynamic identifier
	// is never quoted, so it is suspicious whatever the variable is named.
	reSQLQuotedInterp = regexp.MustCompile(`'"\s*\+\s*\w+|'%[sv]|%[sv]'`)
)

// taintMarkers are substrings that advertise request-derived data. An
// unquoted interpolation is only reported when one appears, because
// dynamic table and column identifiers are legitimate and cannot use
// placeholders. Flagging every one of those would train people to
// ignore the rule.
var taintMarkers = []string{
	"userinput", "user_input", "request.", "req.", "form.",
	"params", "queryparam", "query_param", "filtervalue",
	"filter_value", "rawvalue", "raw_value", "r.url.query",
	"pathvalue", "formvalue",
}

func ruleSQLConcat(p *contracts.Pass, rel, body string, lines []string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for i, line := range strings.Split(stripComments(body), "\n") {
		if !reSQLConcatLiteral.MatchString(line) &&
			!reSQLSprintf.MatchString(line) &&
			!reSQLBuilderConcat.MatchString(line) {
			continue
		}
		if hasLegacyAnnotation(lines, i+1, "safe-sql:") {
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
			RuleID: contracts.RuleSQLStringConcat, File: rel, Line: i + 1,
			Message: "request-derived value concatenated into a SQL statement",
			Snippet: strings.TrimSpace(lines[min(i, len(lines)-1)]),
		})
	}
	return out
}

// ----------------------------------------------------------------------
// GOFASTR1403: render.HTML on a concatenation.
// ----------------------------------------------------------------------

var reRenderHTMLConcat = regexp.MustCompile(`render\.HTML\([^)]*\+[^)]*\)`)

func ruleHTMLConcat(p *contracts.Pass, rel string, lines []string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for i, line := range lines {
		if !reRenderHTMLConcat.MatchString(line) {
			continue
		}
		if hasLegacyAnnotation(lines, i+1, "safe-html:") {
			continue
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleHTMLConcat, File: rel, Line: i + 1,
			Message: "render.HTML receives a concatenated string: the interpolated half is not escaped",
			Snippet: strings.TrimSpace(line),
		})
	}
	return out
}

// ----------------------------------------------------------------------
// GOFASTR1402: POST form without a CSRF input.
// ----------------------------------------------------------------------

var (
	reFormPOST   = regexp.MustCompile(`(?i)<form\b[^>]*method=["']?POST["']?`)
	reCSRFCall   = regexp.MustCompile(`\bCSRFInputFromCtx\s*\(`)
	reCSRFField  = regexp.MustCompile(`(?i)name=["']_csrf["']`)
	reCSRFExempt = regexp.MustCompile(`(?i)csrf-exempt:\s*\S`)
)

// ruleFormCSRF counts POST forms against CSRF wirings in the same file
// rather than doing a file-level "does it mention CSRF anywhere" grep,
// so one correct form does not appear to protect four others beside it.
// Which specific form is unprotected needs real template parsing; naming
// the file and the count is the honest limit of a text pass.
func ruleFormCSRF(p *contracts.Pass, rel, body string, lines []string) []contracts.Diagnostic {
	stripped := stripComments(body)
	protections := len(reCSRFCall.FindAllString(stripped, -1)) +
		len(reCSRFField.FindAllString(stripped, -1)) +
		// The exempt annotation lives in a comment by design, so it is
		// counted on the raw body.
		len(reCSRFExempt.FindAllString(body, -1))

	var formLines []int
	for i, line := range lines {
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
			RuleID: contracts.RuleFormWithoutCSRF, File: rel, Line: idx + 1,
			Message: fmt.Sprintf("this file renders %d POST form(s) but wires CSRF %d time(s)",
				len(formLines), protections),
			Snippet:  strings.TrimSpace(lines[idx]),
			Evidence: map[string]string{"forms": fmt.Sprint(len(formLines)), "protections": fmt.Sprint(protections)},
		})
	}
	return out
}

// ----------------------------------------------------------------------
// GOFASTR1404: http.Cookie without security attributes.
// ----------------------------------------------------------------------

func ruleInsecureCookie(p *contracts.Pass, rel string, file *ast.File, lines []string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Cookie" {
			return true
		}
		if pkg, isIdent := sel.X.(*ast.Ident); !isIdent || pkg.Name != "http" {
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
			// A field present but explicitly false is not protection.
			if val, isBool := kv.Value.(*ast.Ident); isBool && val.Name == "false" {
				continue
			}
			set[key.Name] = true
		}
		// A cookie carrying nothing has nothing to protect. The two shapes
		// are an unset cookie and the standard deletion, an empty value
		// with a negative MaxAge. Reporting either is pure noise.
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

// httpAlias recovers the import alias the file uses for net/http, so the
// generated `SameSite:` value names the same package the literal does.
func httpAlias(sel *ast.SelectorExpr) string {
	if id, ok := sel.X.(*ast.Ident); ok {
		return id.Name
	}
	return "http"
}

// cookieFix inserts the missing security attributes just before the
// literal's closing brace.
//
// It does not try to match the surrounding indentation: [contracts.Report.Apply]
// runs the result through gofmt, so the edit only has to be syntactically
// correct. That is what makes inserting into a multi-line composite
// literal safe rather than a formatting guess.
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
	// A literal whose last element has no trailing comma needs one before
	// the additions; gofmt cannot repair a missing separator.
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
	// The edit consumes the literal's tail, from the end of its last
	// element through the closing brace, and re-emits it around the
	// insertions. Same output as inserting before the brace, but it gives
	// Apply's staleness check a span worth verifying: a lone "}" is the
	// most common byte in Go source, so after a concurrent edit shifted
	// the offsets, a one-byte Old matched a coincidental brace and the
	// fields landed inside whatever now lived there. The exact whitespace
	// run between the last element and the brace is discriminating in a
	// way one byte never is.
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

// hasTrailingComma reports whether a comma already separates the last
// element from the closing brace.
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

// ----------------------------------------------------------------------
// GOFASTR1405: secret assigned from a literal.
// ----------------------------------------------------------------------

var reSecretName = regexp.MustCompile(`(?i)(secret|password|passwd|api_?key|apikey|token|private_?key|credential|client_?secret)`)

// secretPrefixes are vendor key formats. A literal starting with one is
// reported whatever the variable is called. The shape *is* the evidence.
var secretPrefixes = []string{"sk_live_", "sk-live-", "sk-ant-", "ghp_", "gho_", "AKIA", "xoxb-", "xoxp-", "AIza"}

// reIdentifierish matches a value that is a *name* rather than a secret:
// a column ("password_hash"), an enum ("TokenExpired"), an i18n key
// ("ui.auth.password"), an env var ("STRIPE_API_KEY"), a header. These
// are what a name-only heuristic drowns in. Every one of them lives in
// a variable called something like `PasswordHash`, and none of them is
// a credential.
var reIdentifierish = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[._-][A-Za-z0-9]+)*$`)

// looksLikeCredential is the entropy gate. A real key is long and mixes
// character classes; an identifier, a path, a sentence, or a format
// string does not. Being strict here is the whole point. A secret
// detector that cries wolf is one people switch off, and then it catches
// nothing at all.
func looksLikeCredential(v string) bool {
	if len(v) < 16 {
		return false
	}
	// Paths, URLs, sentences, and format strings are configuration.
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
	// Three character classes with digits present: the shape of a
	// generated key, and not the shape of anything typed by hand.
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
			// Name alone is nowhere near enough. A variable called
			// PasswordHash holds the string "password_hash" far more often
			// than it holds a password, so the value has to look like a
			// credential too: long, and drawn from a mixed alphabet no
			// human-written identifier uses.
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
		// The report must not print the secret back out into a terminal,
		// a CI log, or a SARIF artifact. That would spread it further
		// than the commit already did.
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

// ----------------------------------------------------------------------
// GOFASTR1406: X-Forwarded-Proto used as a scheme without an enum check.
// ----------------------------------------------------------------------

// Bug class: a reverse-proxy scheme header read with Header.Get and
// spliced into output as a URL scheme. framework/uihost/agentready.go
// resolveBaseURL shipped it (probe TestDiscoveryURLsIgnoreForwardedProto,
// fixed a24928c1): the raw value reached the agent card's service URL
// and the Link header, so one forged `X-Forwarded-Proto:
// https://evil.example/p` painted an attacker-named origin into cacheable
// discovery output. The fix — and framework/pluginhost/assets.go, which
// never had the bug — is an exact "http"/"https" enum.
//
// The read is Get("X-Forwarded-Proto") on any receiver ending in .Header
// AND on any bare identifier (h http.Header: the map handed to a helper
// is the same header). The argument literal is the whole scope gate; the
// reflection sink carries the precision, so a read that never reaches
// output stays quiet.
//
// Deliberately silent on:
//   - any function that compares THE VALUE — the get itself or one of
//     its holders — against "http"/"https", however spelled: == / !=,
//     strings.EqualFold, or a switch on the value. The enum is the
//     contract; its position is not. A scheme comparison on any OTHER
//     value (an outbound target.Scheme) is not a gate for this header
//     and does not silence the rule;
//   - Header.Set (writing the header outbound, as battery/relay does)
//     and reads that are never reflected (a log line, a boolean);
//   - values compared against any other literal ("", "HTTPS,http"):
//     only the two legal schemes silence the rule;
//   - _test.go and generated files (AppFiles already excludes both).
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

// forwardedProtoGet matches Get("X-Forwarded-Proto") on a receiver
// expression ending in .Header (r.Header) or on any bare identifier
// (h http.Header: the header map as a helper parameter).
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
		// h.Get("X-Forwarded-Proto"): the map passed as a parameter.
		// The reflection sink keeps this precise.
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

// protoEnumChecked reports whether the function body compares the header
// value — the get call itself or one of its holders — against the
// literal "http" or "https": == / !=, strings.EqualFold, or a switch
// whose tag is the value. A scheme comparison on any other value does
// not count: the gate must gate this value, not a neighbouring one.
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

// reSchemeVar is the reflection sink the probe actually exploited: a
// variable that ends up spliced where a scheme belongs.
var reSchemeVar = regexp.MustCompile(`(?i)scheme|proto`)

// assignedIdents collects the identifiers this call's result is assigned
// to anywhere in the function, including `if u := Get(...)` inits, so the
// one-hop `u := Get(...); scheme = u` shape is tracked too.
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

// reflectedScheme reports whether the header value reaches output: the
// call itself inside a concatenation, an assignment into a scheme-named
// variable (directly or one hop through a holder), an argument to a URL
// builder / the Scheme field of a composite literal, or an argument of
// fmt.Sprintf/fmt.Fprintf whose format literal splices a scheme
// position ("%s://%s") — the most common spelling of a built origin.
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
			// fmt.Sprintf("%s://%s", u, host): the concatenation
			// bug's most common spelling. Only scheme-bearing formats
			// count — a format that merely logs the value is not a
			// reflection. fmt.Fprintf(w, ...) included: the w first
			// argument is never the value.
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

// callFunName returns the callee name of a call expression: the selector
// method (fmt.Sprintf → Sprintf) or the bare identifier, honouring
// import aliases. Dot-imported calls read the same either way.
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

// exprTouches reports whether e contains the call node or one of the
// holder identifiers (in plain identifier position, not as a selector).
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

// ----------------------------------------------------------------------
// GOFASTR1407: raw JSON decoding of a request body outside the binder.
// ----------------------------------------------------------------------

// Bug class: a request body decoded with encoding/json's default
// semantics anywhere other than core/handler, which owns the strict
// binder. battery/auth decoded login and register bodies with
// json.NewDecoder (probe TestLoginJSONStrictTopLevelKeys, fixed 4b7a25d2):
// stdlib keeps the LAST duplicate key and folds key case, while form
// parsing keeps the FIRST, so {"email":A,"EMAIL":B} authenticated a
// different identity depending on Content-Type. The smuggled body
// resolves by parser accident, which is the whole attack.
//
// The rule tracks r.Body through local assignments (io.ReadAll(r.Body),
// http.MaxBytesReader(w, r.Body, n), one more hop for ReadAll of the
// wrapped reader) so the wrapped spellings are the same finding, plus
// ONE bounded level of same-file helpers: raw, err := readBody(r), where
// readBody is declared in this file and its return expressions are
// body-derived, taints raw exactly as io.ReadAll(r.Body) would. Taint
// reaches a call's RESULT only through those reader wrappers and the
// helper ferry: an arbitrary call that merely receives tainted arguments
// (the module proxy's peer.CallWithID(ctx, params)) returns its own
// output, not the body. Helpers in other files are not loaded by this
// pass, and helper-calling-helper chains are not followed — one level
// keeps the walk terminating.
//
// Deliberately silent on:
//   - core/handler/**, which owns handler.Bind and its duplicate-key
//     walk — the strict binder itself decodes raw bytes by design;
//   - decodes whose bytes never came from a request body: resp.Body of
//     an outbound call, queue and pub-sub payloads, files, JWT segments;
//   - functions with no *http.Request parameter, and _test.go /
//     generated files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1407) <why>, which is
//     how a transport that accepts a JSON-RPC envelope object as-is
//     records that decision.
func ruleRawJSONBody(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	if rel == "core/handler" || strings.HasPrefix(rel, "core/handler/") {
		return nil
	}
	var out []contracts.Diagnostic
	aliases := importAliases(file)
	helpers := sameFileBodyReaders(file, aliases)
	for _, fn := range functionsIn(file) {
		reqs := httpRequestParamNames(fn.node, aliases)
		if len(reqs) == 0 {
			continue
		}
		tainted := taintFromBody(fn.body, reqs, helpers)
		ast.Inspect(fn.body, func(n ast.Node) bool {
			if call, ok := qualifiedCall(n, aliases, "encoding/json", "NewDecoder"); ok && len(call.Args) == 1 &&
				exprFromRequestBody(call.Args[0], tainted, reqs) {
				out = append(out, rawJSONDiag(p, rel, call))
			}
			if call, ok := qualifiedCall(n, aliases, "encoding/json", "Unmarshal"); ok && len(call.Args) > 0 &&
				exprFromRequestBody(call.Args[0], tainted, reqs) {
				out = append(out, rawJSONDiag(p, rel, call))
			}
			return true
		})
	}
	return out
}

func rawJSONDiag(p *contracts.Pass, rel string, call *ast.CallExpr) contracts.Diagnostic {
	d := diag(p, contracts.RuleRawJSONBodyDecode, rel, call.Pos(),
		"request body decoded with encoding/json outside core/handler: stdlib keeps the last duplicate key and matches key case-insensitively, so a smuggled body can resolve by parser accident — decode via handler.Bind or a strict top-level key walk (battery/auth decodeJSONLimitedStrict is the model)")
	d.Evidence = map[string]string{"source": "request body"}
	return d
}

// funcScope is one function declaration or literal with a body.
type funcScope struct {
	node ast.Node
	body *ast.BlockStmt
}

// functionsIn returns every function and function literal in the file.
// Literals are included because handlers are closures more often than
// named methods.
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

// httpRequestParamNames returns the names of parameters typed
// *http.Request, honouring the file's import alias for net/http.
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

// taintFromBody computes which local identifiers hold bytes read from a
// request body, to a fixpoint: body := MaxBytesReader(w, r.Body, n) then
// raw, err := io.ReadAll(body) leaves both body and raw tainted. Plain
// assignments are tracked, plus one bounded level of same-file helpers
// (helpers, from sameFileBodyReaders): raw, err := readBody(r) taints
// raw when readBody's returns are body-derived. A body ferried through
// a helper in ANOTHER file stays invisible — this pass never loads
// another file's bodies — and stays unreported.
func taintFromBody(body ast.Node, reqs map[string]bool, helpers map[string]bool) map[string]bool {
	var assigns []*ast.AssignStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok {
			assigns = append(assigns, a)
		}
		return true
	})
	tainted := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for _, a := range assigns {
			touched := false
			for _, rhs := range a.Rhs {
				if exprFromRequestBody(rhs, tainted, reqs) {
					touched = true
					break
				}
				if call, ok := rhs.(*ast.CallExpr); ok && len(helpers) > 0 && helperCallTainted(call, tainted, reqs, helpers) {
					touched = true
					break
				}
			}
			if !touched {
				continue
			}
			for _, lhs := range a.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && !tainted[id.Name] {
					tainted[id.Name] = true
					changed = true
				}
			}
		}
	}
	return tainted
}

// sameFileBodyReaders returns the same-file FUNCTIONS (bare-name calls
// only; a method's receiver expression cannot be matched by name) whose
// return expressions are request-body-derived given their own
// *http.Request parameters: readBody(r) { return io.ReadAll(r.Body) }.
// This is the one level of interprocedural tracking — a helper whose own
// return calls another helper is not followed further.
func sameFileBodyReaders(file *ast.File, aliases map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, fn := range functionsIn(file) {
		decl, ok := fn.node.(*ast.FuncDecl)
		if !ok || decl.Name == nil || decl.Recv != nil {
			continue // methods are not bare-name callable
		}
		reqs := httpRequestParamNames(fn.node, aliases)
		if len(reqs) == 0 {
			continue
		}
		tainted := taintFromBody(fn.body, reqs, nil)
		derived := false
		ast.Inspect(fn.body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok {
				return true
			}
			for _, r := range ret.Results {
				if exprFromRequestBody(r, tainted, reqs) {
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
	return out
}

// helperCallTainted reports whether call is a call to a known same-file
// body-reading helper whose arguments actually carry this request (the
// request itself, its body, or an already-tainted value): the ferry must
// carry bytes, not a coincidental name.
func helperCallTainted(call *ast.CallExpr, tainted map[string]bool, reqs map[string]bool, helpers map[string]bool) bool {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || !helpers[id.Name] {
		return false
	}
	for _, a := range call.Args {
		if exprFromRequestBody(a, tainted, reqs) {
			return true
		}
		touches := false
		ast.Inspect(a, func(n ast.Node) bool {
			if arg, ok := n.(*ast.Ident); ok && reqs[arg.Name] {
				touches = true
			}
			return !touches
		})
		if touches {
			return true
		}
	}
	return false
}

// readerWrapper names the calls whose RESULT is the request body's
// bytes: the reader constructors and readers of the wrapped spellings.
// NopCloser/TeeReader/NewReader return a reader over the SAME bytes —
// bufio.NewReader(r.Body), bytes.NewReader(raw), io.NopCloser(r.Body) —
// which is what distinguishes them from a call that returns its own
// output (peer.CallWithID).
var readerWrapper = map[string]bool{
	"ReadAll":        true,
	"MaxBytesReader": true,
	"LimitReader":    true,
	"NopCloser":      true,
	"TeeReader":      true,
	"NewReader":      true,
}

// exprFromRequestBody reports whether e is, contains, or was assigned
// from a request body: the <req>.Body selector itself, or a tainted
// identifier in plain identifier position.
func exprFromRequestBody(e ast.Expr, tainted map[string]bool, reqs map[string]bool) bool {
	switch v := e.(type) {
	case *ast.Ident:
		return tainted[v.Name]
	case *ast.CallExpr:
		// Only reader-wrapping calls propagate taint to their RESULT
		// (io.ReadAll, MaxBytesReader, LimitReader, and aliases): the
		// result of an arbitrary call that merely receives tainted
		// arguments is not the body (the module proxy's
		// peer.CallWithID(ctx, params) returns the child's response).
		switch fn := v.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel != nil && readerWrapper[fn.Sel.Name] {
				for _, a := range v.Args {
					if exprFromRequestBody(a, tainted, reqs) {
						return true
					}
				}
			}
		case *ast.Ident:
			if readerWrapper[fn.Name] {
				for _, a := range v.Args {
					if exprFromRequestBody(a, tainted, reqs) {
						return true
					}
				}
			}
		}
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok && reqs[id.Name] && v.Sel != nil && v.Sel.Name == "Body" {
			return true
		}
		return exprFromRequestBody(v.X, tainted, reqs)
	case *ast.BinaryExpr:
		return exprFromRequestBody(v.X, tainted, reqs) || exprFromRequestBody(v.Y, tainted, reqs)
	case *ast.ParenExpr:
		return exprFromRequestBody(v.X, tainted, reqs)
	case *ast.UnaryExpr:
		return exprFromRequestBody(v.X, tainted, reqs)
	case *ast.StarExpr:
		return exprFromRequestBody(v.X, tainted, reqs)
	case *ast.IndexExpr:
		return exprFromRequestBody(v.X, tainted, reqs)
	case *ast.CompositeLit:
		for _, elt := range v.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if exprFromRequestBody(kv.Value, tainted, reqs) {
					return true
				}
				continue
			}
			if exprFromRequestBody(elt, tainted, reqs) {
				return true
			}
		}
	}
	return false
}

// ----------------------------------------------------------------------
// GOFASTR1601: discarded Exec result.
// ----------------------------------------------------------------------

var reIgnoredExec = regexp.MustCompile(`(?:^|[\s;{])_,\s*_\s*=\s*\S+\.Exec(?:Context)?\b`)

func runData(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if !reIgnoredExec.MatchString(line) {
				continue
			}
			if hasLegacyAnnotation(lines, i+1, "best-effort", "ignore the error", "errors here") {
				continue
			}
			out = append(out, contracts.Diagnostic{
				RuleID: contracts.RuleIgnoredExec, File: f.Rel, Line: i + 1,
				Message: "both the result and the error of this write are discarded",
				Snippet: strings.TrimSpace(line),
			})
		}
	}
	return out, nil
}
