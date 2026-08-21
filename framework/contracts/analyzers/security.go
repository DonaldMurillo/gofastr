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
		Doc:  "Injection, CSRF, cookie attributes, and committed secrets.",
		Rules: []string{
			contracts.RuleSQLStringConcat,
			contracts.RuleFormWithoutCSRF,
			contracts.RuleHTMLConcat,
			contracts.RuleInsecureCookie,
			contracts.RuleHardcodedSecret,
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
