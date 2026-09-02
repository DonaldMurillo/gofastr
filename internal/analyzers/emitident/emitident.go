// Package emitident catches names formatted into emitted code without an
// identifier gate.
//
// Bug class: a generator or template renders a caller-supplied name into
// a slot where the emitted text is CODE — a func/type/variable
// declaration, a SQL DDL identifier, a CSS string slot, a URL path
// segment. Unless the value was validated as an identifier or quoted for
// that grammar, a name carrying quotes, operators, or keywords changes
// what the emitted code DOES. Found by the 2026-09-01 adversarial probes:
// TestHookHandlerMustDeriveIdentifier (blueprint hook handlers accepted
// `x"); PWN() //` straight into `func %s(...)`), TestCLIFieldPayloadBecomesStatements
// (CLI flag names flowed through toCamelCase into `fld%s :=` declarations
// — toCamelCase changes case, it does not validate), TestSDKSpecRefusesHostileDeclarations
// (entity tables landed raw in `"/%s"` path literals of the generated
// client), TestAlterTableQuotesHostileIdentifiers (kiln interpolated
// table/column names into `ALTER TABLE %s ADD COLUMN %s`), TestFontFaceCSSRejectsDeclarationBreakers
// (font families into `font-family: '%s'`), TestWidgetBehaviorURLMatchesRuntimeGate
// (widget ids into behavior URLs). Fixes: 29219c04, f06f4412, e936f791.
//
// A slot's argument is gated when it is, resolves to, or is built only
// from: a call whose name says identifier/quote/slug/sanitize/safe/escape
// (query.QuoteIdent, SlugifyWidgetID, quotedSlotSanitized, url.PathEscape,
// sqlType...), a strconv/strings result, a constant, a local that an
// isXIdent/token.IsIdentifier check in the same function guards, or a
// derivation rooted at a struct field the SAME PACKAGE identifier-checks
// somewhere (validate-side gates count: the check may live in the
// validator while the emitter only renders). toCamelCase and kin are
// deliberately NOT gates: they transform, they do not validate — that is
// precisely the miss the CLI probe drove, and why the fix wrapped the
// derivation in token.IsIdentifier rather than trusting the casing.
//
// The rule is deliberately silent on:
//   - format slots that are not identifier positions: plain %s/%q in
//     string, argument, or message slots, `List%s(` spellings where the
//     verb only completes an identifier the call already spells, and the
//     bare `%s(` callee shape generally — on this repo it matched only
//     human-readable renderings (diagnostics, TUI bullets, doc text
//     listing "tool(args)"), never emitted code, so the declaration
//     spellings (func/type/var/%s :=) carry the Go side alone.
//   - strconv/strings results, constants, and %v/%d-style verbs.
//   - concatenation into SQL (`"SELECT ..." + x`): GOFASTR1401 owns that
//     shape; this rule only sees the Sprintf family, so the two cannot
//     double-report.
package emitident

import (
	"go/ast"
	"go/constant"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastremitident",
	Doc:  "forbids fmt.Sprintf/Fprintf/Appendf formats that substitute an ungated name into an identifier slot of emitted code (Go declarations, SQL DDL, CSS string slots, route paths); validate with an ident check or quote for the grammar",
	Run:  run,
}

// gateNameRe matches call names that produce identifier-safe output or
// check it. "camel" is pointedly absent: toCamelCase was the exact
// non-gate the probes sailed through.
var gateNameRe = regexp.MustCompile(`(?i)ident|quote|slug|sanit|safe|escape|type`)

// checkNameRe matches the subset that treats a value as an identifier —
// checking it (isGoIdentifier, token.IsIdentifier, validateScaffoldName)
// or quoting it (query.QuoteIdent, query.SafeIdent). Used for the
// function-local guard tracking and the package field memo.
var checkNameRe = regexp.MustCompile(`(?i)ident|quote|safe|validat`)

// isCheckCall reports whether a call is treated as an identifier gate:
// named for checking/quoting identifiers, and not one of the stdlib
// display-quoting families (strconv.Quote and friends quote for humans;
// they are not gates).
func isCheckCall(pass *analysis.Pass, fun ast.Expr) bool {
	if !checkNameRe.MatchString(calleeLastName(pass, fun)) {
		return false
	}
	q := qualifiedCallee(pass, fun)
	return !strings.HasPrefix(q, "strconv.") && !strings.HasPrefix(q, "strings.") && !strings.HasPrefix(q, "fmt.")
}

const maxDepth = 6

// fmtCalls maps the Sprintf family to the position of their format
// literal among the call's arguments.
var fmtCalls = map[string]int{
	"fmt.Sprintf": 0,
	"fmt.Fprintf": 1,
	"fmt.Appendf": 1,
}

func run(pass *analysis.Pass) (any, error) {
	helperDecls := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok {
				if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					helperDecls[fn] = fd
				}
			}
		}
	}

	// Package field memo: struct fields that some ident/quote/safe-named
	// call in this package checks or quotes. The gate may live in the
	// validator while the emitter only renders (validateBlueprint vs
	// renderBlueprintStubs), so the memo is package-scoped on the field
	// name. It only ever silences, never fires.
	memo := map[string]bool{}
	for _, f := range pass.Files {
		bound := boundExprs(pass, f)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isCheckCall(pass, call.Fun) {
				return true
			}
			for _, a := range call.Args {
				for field := range fieldRoots(pass, a, bound, helperDecls) {
					memo[field] = true
				}
			}
			return true
		})
	}

	// Param gate: a local helper's parameter whose EVERY call-site
	// argument is gated is itself gated — the emitter renders a param the
	// callers already validated or quoted (qtable/qcol conventions).
	// Computed to a fixpoint, because gating flows through call chains
	// (qtable → up → createMigrationsTable → ensureTrackingColumns).
	// It only ever silences.
	paramGate := map[*types.Var]bool{}
	type siteCall struct {
		fn    *types.Func
		args  []ast.Expr
		bound map[types.Object]ast.Expr
	}
	var calls []siteCall
	for _, f := range pass.Files {
		bound := boundExprs(pass, f)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := calleeFunc(pass, call.Fun)
			if !ok {
				return true
			}
			if _, isLocal := helperDecls[fn]; !isLocal {
				return true
			}
			calls = append(calls, siteCall{fn: fn, args: call.Args, bound: bound})
			return true
		})
	}
	for round := 0; round < 8; round++ {
		changed := false
		for _, c := range calls {
			sig, ok := c.fn.Type().(*types.Signature)
			if !ok {
				continue
			}
			params := sig.Params()
			for i := range c.args {
				if i >= params.Len() || paramGate[params.At(i)] {
					continue
				}
				g, fields := resolveArg(pass, c.args[i], c.bound, helperDecls, nil, memo, paramGate, 0)
				if g || allInMemo(fields, memo) {
					// Tentatively gated; confirm no other call site
					// passes an ungated value for this param.
					allGated := true
					for _, other := range calls {
						if other.fn != c.fn || len(other.args) <= i {
							continue
						}
						og, of := resolveArg(pass, other.args[i], other.bound, helperDecls, nil, memo, paramGate, 0)
						if !(og || allInMemo(of, memo)) {
							allGated = false
							break
						}
					}
					if allGated {
						paramGate[params.At(i)] = true
						changed = true
					}
				}
			}
		}
		if !changed {
			break
		}
	}

	for _, f := range pass.Files {
		checkFile(pass, f, helperDecls, memo, paramGate)
	}
	return nil, nil
}

func checkFile(pass *analysis.Pass, f *ast.File, helperDecls map[*types.Func]*ast.FuncDecl, memo map[string]bool, paramGate map[*types.Var]bool) {
	bound := boundExprs(pass, f)

	// Function-local ident guards: objects checked by an isXIdent-style
	// call in some if-statement of the same function.
	guarded := map[types.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if st, ok := n.(*ast.IfStmt); ok {
			// A check in the if's INIT counts too:
			// `if _, err := query.SafeIdent(table); err != nil`.
			for _, root := range []ast.Node{st.Init, st.Cond} {
				if root == nil {
					continue
				}
				ast.Inspect(root, func(c ast.Node) bool {
					call, ok := c.(*ast.CallExpr)
					if !ok || !isCheckCall(pass, call.Fun) {
						return true
					}
					for _, a := range call.Args {
						if id, ok := a.(*ast.Ident); ok {
							if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
								guarded[obj] = true
							}
						}
					}
					return true
				})
			}
		}
		return true
	})

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		formatIndex, ok := fmtCalls[qualifiedCallee(pass, call.Fun)]
		if !ok || formatIndex >= len(call.Args) {
			return true
		}
		format, ok := stringLiteralValue(pass, call.Args[formatIndex])
		if !ok {
			return true
		}
		for _, s := range scanSlots(format) {
			argIndex := formatIndex + 1 + s.verbIndex
			if argIndex >= len(call.Args) {
				break
			}
			if s.quoted {
				continue // %q is the quoting mechanism in this slot's grammar
			}
			gated, fields := resolveArg(pass, call.Args[argIndex], bound, helperDecls, guarded, memo, paramGate, 0)
			if gated || allInMemo(fields, memo) {
				continue
			}
			pass.Reportf(call.Pos(),
				"fmt format substitutes an ungated value into identifier slot %q of emitted code: a name carrying quotes, operators, or keywords changes what the emitted code does; validate it (isGoIdentifier/token.IsIdentifier) or quote it for that grammar",
				s.text)
			break // one report per call
		}
		return true
	})
}

// ---- format scanning ----------------------------------------------------

// slot is one scanned verb occurrence: its index among ALL verbs of the
// format (the positional-argument slot) and the slot text for the
// diagnostic. quoted marks a %q verb in a grammar where quoting IS the
// gate (SQL DDL and CSS string slots take double/single-quoted
// identifiers; Go declaration slots do not — a quoted string in
// identifier position is still not an identifier).
type slot struct {
	verbIndex int
	text      string
	quoted    bool
}

const identChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"

// scanSlots returns every %s/%q verb of the format that sits in an
// identifier slot of the emitted code.
func scanSlots(format string) []slot {
	var out []slot
	verbs := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		length := verbLen(format, i)
		if length == 0 {
			continue
		}
		verb := format[i+length-1]
		if verb == 's' || verb == 'q' {
			if text, ok := identSlotText(format, i, verb == 'q'); ok {
				out = append(out, slot{verbIndex: verbs, text: text, quoted: verb == 'q'})
			}
		}
		verbs++
		i += length - 1
	}
	return out
}

// verbLen is the length of the verb sequence starting at '%' (0 for %%).
func verbLen(format string, i int) int {
	j, n := i+1, len(format)
	for j < n && strings.IndexByte("+-# 0123456789.", format[j]) >= 0 {
		j++
	}
	if j < n && format[j] != '%' {
		return j - i + 1
	}
	return 0
}

// identSlotText reports whether the verb at byte offset i sits in an
// identifier slot of the emitted code, returning slot text for the
// diagnostic. quoted says the verb is %q; in the grammars where a
// quoted identifier is the sanctioned spelling (SQL DDL, CSS string
// slots, route literals) a quoted verb is the gate itself and the slot
// reports not-scanned.
func identSlotText(format string, i int, quoted bool) (string, bool) {
	lower := strings.ToLower(format)
	after := i + verbLen(format, i)

	// b is the start of the emitted identifier token containing the verb.
	b := i
	for b > 0 && strings.IndexByte(identChars, format[b-1]) >= 0 {
		b--
	}

	// Suffix declaration: the emitted identifier containing the verb is
	// followed by ":=" — `fld%s :=`, `flt%sLike :=`.
	j := after
	for j < len(format) && strings.IndexByte(identChars, format[j]) >= 0 {
		j++
	}
	for j < len(format) && (format[j] == ' ' || format[j] == '\t') {
		j++
	}
	if strings.HasPrefix(format[j:], ":=") {
		return format[b:min(j+2, len(format))], true
	}

	// Keyword declaration: func/type/var before the identifier containing
	// the verb — `func %s(`, `type %s struct`, `var %s =`.
	pb := b
	for pb > 0 && (format[pb-1] == ' ' || format[pb-1] == '\t') {
		pb--
	}
	for _, kw := range []string{"func", "type", "var"} {
		kwStart := pb - len(kw)
		if kwStart >= 0 && strings.HasSuffix(lower[:pb], kw) && (kwStart == 0 || !isIdentByte(lower[kwStart-1])) {
			// The keyword must start a code context, not English prose:
			// at the format's start or after a code boundary
			// (newline, tab, brace, paren, semicolon, colon, equals).
			// "component type %s from another package" is prose.
			if kwStart == 0 || strings.IndexByte("\n\t{;(:=", lower[kwStart-1]) >= 0 {
				return kw + " " + format[b:after], true
			}
		}
	}

	// SQL DDL identifier slots. %q here IS the quoting mechanism
	// (SQLite/Postgres double-quoted identifiers).
	if !quoted {
		for _, phrase := range []string{
			"alter table ", "add column if not exists ", "add column ",
			"create table if not exists ", "create table ", "drop table ",
			"pragma table_info(",
		} {
			if slotAfterPhrase(lower, i, phrase) {
				return phrase + "%s", true
			}
		}
	}

	// CSS string slots inside an @font-face rule: any verb inside a
	// single-quoted run opened after a colon or paren —
	// `font-family: '%s'`, `url('%s/%s.woff2')`. An odd quote count alone
	// would let an apostrophe in prose ("the app's fonts") open a run.
	if !quoted && strings.Contains(lower, "@font-face") && strings.Count(lower[:i], "'")%2 == 1 { //gofastr:allow(GOFASTR1801) the analyzer names the CSS at-rule it inspects; no CSS is emitted here
		q := strings.LastIndexByte(lower[:i], '\'')
		if q > 0 && (lower[q-1] == '(' || (lower[q-1] == ' ' && q >= 2 && lower[q-2] == ':')) {
			return "'%s'", true
		}
	}

	// Route path literals: `"/%s` (a quoted path whose first segment is
	// the name) and the widget behavior URL shape.
	if !quoted && i >= 2 && strings.HasPrefix(format[i-2:], "\"/") && b == i {
		return `"/%s`, true
	}
	if !quoted && strings.Contains(lower, "/__gofastr/widget/") {
		return "/__gofastr/widget/%s", true
	}
	return "", false
}

// slotAfterPhrase reports whether the verb at offset i is the identifier
// slot directly following the given (lowercased) phrase, allowing only
// spaces between.
func slotAfterPhrase(lower string, i int, phrase string) bool {
	start := 0
	for {
		p := strings.Index(lower[start:], phrase)
		if p < 0 {
			return false
		}
		p += start
		end := p + len(phrase)
		if end > i {
			return false
		}
		if strings.TrimRight(lower[end:i], " \t") == "" {
			return true
		}
		start = p + 1
	}
}

func isIdentByte(c byte) bool { return strings.IndexByte(identChars, c) >= 0 }

// ---- argument resolution -------------------------------------------------

// resolveArg decides whether an argument is gated for an identifier slot
// and, when not, which struct fields its derivation is rooted at (the
// package memo may still gate those).
func resolveArg(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr, helperDecls map[*types.Func]*ast.FuncDecl, guarded map[types.Object]bool, memo map[string]bool, paramGate map[*types.Var]bool, depth int) (gated bool, fields map[string]bool) {
	fields = map[string]bool{}
	if depth > maxDepth {
		return false, fields
	}
	collect := func(sub map[string]bool) {
		for f := range sub {
			fields[f] = true
		}
	}
	switch e := x.(type) {
	case *ast.ParenExpr:
		return resolveArg(pass, e.X, bound, helperDecls, guarded, memo, paramGate, depth+1)
	case *ast.BasicLit:
		return true, fields // a literal cannot smuggle anything
	case *ast.Ident:
		if tv, ok := pass.TypesInfo.Types[e]; ok && tv.Value != nil {
			return true, fields // constant
		}
		obj := pass.TypesInfo.ObjectOf(e)
		if obj == nil {
			return false, fields
		}
		if guarded[obj] {
			return true, fields
		}
		if v, ok := obj.(*types.Var); ok && paramGate[v] {
			return true, fields
		}
		if src, ok := bound[obj]; ok {
			return resolveArg(pass, src, bound, helperDecls, guarded, memo, paramGate, depth+1)
		}
		return false, fields
	case *ast.SelectorExpr:
		_, sub := resolveArg(pass, e.X, bound, helperDecls, guarded, memo, paramGate, depth+1)
		collect(sub)
		fields[e.Sel.Name] = true
		if gateNameRe.MatchString(e.Sel.Name) {
			return true, fields // quotedTable, safeIdent: self-describing
		}
		if literalField(pass, e, bound, helperDecls, depth) {
			return true, fields // a field fixed to a literal: no input to smuggle
		}
		return false, fields
	case *ast.IndexExpr:
		return resolveArg(pass, e.X, bound, helperDecls, guarded, memo, paramGate, depth+1)
	case *ast.UnaryExpr:
		return resolveArg(pass, e.X, bound, helperDecls, guarded, memo, paramGate, depth+1)
	case *ast.CallExpr:
		q := qualifiedCallee(pass, e.Fun)
		last := calleeLastName(pass, e.Fun)
		if last != "" && gateNameRe.MatchString(last) {
			return true, fields
		}
		if q == "fmt.Sprintf" && len(e.Args) > 0 {
			// A constant format whose verbs are all numeric synthesizes a
			// safe name ("pp%d"); no string input can reach it.
			if f, ok := stringLiteralValue(pass, e.Args[0]); ok && numericOnlyVerbs(f) {
				return true, fields
			}
		}
		if strings.HasPrefix(q, "strconv.") || strings.HasPrefix(q, "strings.") {
			return true, fields // silent on strconv/strings results
		}
		if fn, ok := calleeFunc(pass, e.Fun); ok {
			if decl, ok := helperDecls[fn]; ok && decl.Body != nil {
				// A local helper: its arguments and the struct fields its
				// body touches are the derivation roots, and its returns
				// must themselves be gated.
				for _, a := range e.Args {
					_, sub := resolveArg(pass, a, bound, helperDecls, guarded, memo, paramGate, depth+1)
					collect(sub)
				}
				ast.Inspect(decl.Body, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok {
						// Only struct-field selectors: a package qualifier
						// (strings.ToUpper) is not a derivation root.
						if !isPkgIdent(pass, sel.X) {
							fields[sel.Sel.Name] = true
						}
					}
					return true
				})
				return helperReturnsGated(pass, decl, bound, helperDecls, guarded, memo, paramGate, depth), fields
			}
		}
		// Any other call: keep walking its arguments for field roots.
		for _, a := range e.Args {
			_, sub := resolveArg(pass, a, bound, helperDecls, guarded, memo, paramGate, depth+1)
			collect(sub)
		}
		return false, fields
	case *ast.BinaryExpr:
		allGated := true
		for _, op := range []ast.Expr{e.X, e.Y} {
			g, sub := resolveArg(pass, op, bound, helperDecls, guarded, memo, paramGate, depth+1)
			collect(sub)
			if !g && (len(sub) == 0 || !allInMemo(sub, memo)) {
				allGated = false
			}
		}
		return allGated, fields
	case *ast.CompositeLit:
		for _, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			_, sub := resolveArg(pass, elt, bound, helperDecls, guarded, memo, paramGate, depth+1)
			collect(sub)
		}
		return false, fields
	}
	return false, fields
}

// numericOnlyVerbs reports whether a constant format's verbs are all
// numeric (%d and friends): no string argument can shape the output.
func numericOnlyVerbs(f string) bool {
	saw := false
	for i := 0; i < len(f); i++ {
		if f[i] != '%' {
			continue
		}
		length := verbLen(f, i)
		if length == 0 {
			continue
		}
		switch f[i+length-1] {
		case 'd', 'b', 'o', 'x', 'X', 'c', 'U', 'e', 'E', 'f', 'F', 'g', 'G':
			saw = true
		default:
			return false
		}
		i += length - 1
	}
	return saw
}

// helperReturnsGated reports whether every return expression of a local
// helper is itself gated.
func helperReturnsGated(pass *analysis.Pass, decl *ast.FuncDecl, bound map[types.Object]ast.Expr, helperDecls map[*types.Func]*ast.FuncDecl, guarded map[types.Object]bool, memo map[string]bool, paramGate map[*types.Var]bool, depth int) bool {
	gated := true
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			for _, r := range ret.Results {
				g, sub := resolveArg(pass, r, bound, helperDecls, guarded, memo, paramGate, depth+1)
				if !g && (len(sub) == 0 || !allInMemo(sub, memo)) {
					gated = false
				}
			}
		}
		return true
	})
	return gated
}

// literalField reports whether x.Sel resolves to a field whose value is
// a basic literal: a range variable over a composite literal of constant
// entries (checksum/dirty DDL fragments) carries no caller input.
func literalField(pass *analysis.Pass, x *ast.SelectorExpr, bound map[types.Object]ast.Expr, helperDecls map[*types.Func]*ast.FuncDecl, depth int) bool {
	if depth > maxDepth {
		return false
	}
	base := x.X
	for i := 0; i < maxDepth; i++ {
		switch b := base.(type) {
		case *ast.ParenExpr:
			base = b.X
			continue
		case *ast.Ident:
			obj := pass.TypesInfo.ObjectOf(b)
			if obj == nil {
				return false
			}
			src, ok := bound[obj]
			if !ok {
				return false
			}
			base = src
			continue
		case *ast.CompositeLit:
			for _, elt := range b.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == x.Sel.Name {
					if _, isLit := kv.Value.(*ast.BasicLit); isLit {
						return true
					}
					return false
				}
			}
			return false
		default:
			return false
		}
	}
	return false
}

func allInMemo(fields map[string]bool, memo map[string]bool) bool {
	if len(fields) == 0 {
		return false
	}
	for f := range fields {
		if !memo[f] {
			return false
		}
	}
	return true
}

// fieldRoots collects the struct field names a derivation is rooted at,
// for the package memo.
func fieldRoots(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr, helperDecls map[*types.Func]*ast.FuncDecl) map[string]bool {
	_, fields := resolveArg(pass, x, bound, helperDecls, nil, map[string]bool{}, nil, 0)
	return fields
}

// ---- shared helpers ------------------------------------------------------

func stringLiteralValue(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

func boundExprs(pass *analysis.Pass, f *ast.File) map[types.Object]ast.Expr {
	m := map[types.Object]ast.Expr{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if st.Tok.String() != ":=" && st.Tok.String() != "=" {
				return true
			}
			switch {
			case len(st.Lhs) == len(st.Rhs): // tuple: bind each lhs to its rhs
				for idx, lhs := range st.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							m[obj] = st.Rhs[idx]
						}
					}
				}
			case len(st.Lhs) == 2 && len(st.Rhs) == 1: // v, err := call()
				if id, ok := st.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						m[obj] = st.Rhs[0]
					}
				}
			}
		case *ast.ValueSpec:
			if len(st.Names) != 1 || len(st.Values) != 1 || st.Names[0].Name == "_" {
				return true
			}
			if obj := pass.TypesInfo.ObjectOf(st.Names[0]); obj != nil {
				m[obj] = st.Values[0]
			}
		case *ast.RangeStmt:
			// Bind a range value variable to its source, so a selector on
			// it (a.col) can resolve to a literal's field.
			if id, ok := st.Value.(*ast.Ident); ok && id.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					m[obj] = st.X
				}
			}
		}
		return true
	})
	return m
}

// isPkgIdent reports whether x is an identifier that resolves to an
// imported package (used where Uses on the selector's X yields nothing
// for unevaluated positions).
func isPkgIdent(pass *analysis.Pass, x ast.Expr) bool {
	id, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = pass.TypesInfo.Uses[id].(*types.PkgName)
	return ok
}

func calleeLastName(pass *analysis.Pass, fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func calleeFunc(pass *analysis.Pass, fun ast.Expr) (*types.Func, bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		fn, ok := pass.TypesInfo.Uses[f].(*types.Func)
		return fn, ok
	case *ast.SelectorExpr:
		fn, ok := pass.TypesInfo.Uses[f.Sel].(*types.Func)
		return fn, ok
	}
	return nil, false
}

// qualifiedCallee renders a call target as "pkg.Func" through the type
// checker, so an aliased import still resolves to the real package.
func qualifiedCallee(pass *analysis.Pass, fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			if pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName); ok {
				return pkg.Imported().Name() + "." + f.Sel.Name
			}
		}
	case *ast.Ident:
		if fn, ok := pass.TypesInfo.Uses[f].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Name() + "." + fn.Name()
		}
	}
	return ""
}
