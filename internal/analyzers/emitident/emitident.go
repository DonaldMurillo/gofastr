// Package emitident catches names formatted into emitted code without an
// identifier gate.
//
// Bug class: a generator or template renders a caller-supplied name into
// a slot where the emitted text is CODE — a func/type/variable
// declaration, a SQL DDL identifier, a CSS string slot, a URL path
// segment, a JS/TS declaration or object key. Unless the value was
// validated as an identifier or quoted for that grammar, a name carrying
// quotes, operators, or keywords changes what the emitted code DOES.
// Found by the 2026-09-01 adversarial probes:
// TestHookHandlerMustDeriveIdentifier (blueprint hook handlers accepted
// `x"); PWN() //` straight into `func %s(...)`), TestCLIFieldPayloadBecomesStatements
// (CLI flag names flowed through toCamelCase into `fld%s :=` declarations
// — toCamelCase changes case, it does not validate), TestSDKSpecRefusesHostileDeclarations
// (entity tables landed raw in `"/%s"` path literals of the generated
// client), TestAlterTableQuotesHostileIdentifiers (kiln interpolated
// table/column names into `ALTER TABLE %s ADD COLUMN %s`), TestFontFaceCSSRejectsDeclarationBreakers
// (font families into `font-family: '%s'`), TestWidgetBehaviorURLMatchesRuntimeGate
// (widget ids into behavior URLs). Fixes: 29219c04, f06f4412, e936f791.
// The 2026-09-04 red-probe round added the JS/TS side:
// TestSDKJSIdentSlotRefusesTables (cmd/gofastr/generate_sdkjs.go wrote
// entity-table-derived camelCase names into `export const %sFields =`
// and `readonly %s:` of the generated client.d.ts with only toCamelCase
// — a transform, not a gate — in front; the .js got the quoted
// this[%q] spelling, the fix posture, and only the .d.ts was left raw).
//
// A slot's argument is gated when it is, resolves to, or is built only
// from: a call whose name says identifier/quote/slug/sanitize/safe/escape
// (query.QuoteIdent, SlugifyWidgetID, quotedSlotSanitized, url.PathEscape,
// sqlType...), a strconv/strings result, a constant, a local that an
// isXIdent/token.IsIdentifier check in the same function guards —
// guarding means the emit site is inside the check's success arm or
// after a failure arm that diverges (return/panic/os.Exit, or a
// continue confined to emits inside the same loop body); a
// warn-and-continue check gates nothing — or a derivation rooted at
// a struct field the SAME PACKAGE identifier-checks somewhere
// (validate-side gates count: the check may live in the validator
// while the emitter only renders), or at a field whose own name states
// the value's TREATMENT (quotedTable, sanitizedSlot). A struct field
// whose name is a noun for what the value IS (HandlerType, FieldType,
// Ident, SafeWord) is NOT a gate: the schema's word for a value says
// nothing about how it was treated.
// toCamelCase and kin are deliberately NOT gates: they transform, they
// do not validate — that is precisely the miss the CLI probe drove,
// and why the fix wrapped the derivation in token.IsIdentifier rather
// than trusting the casing.
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
//   - format-INITIAL func/type/var keywords without code evidence:
//     lowercase messages start with the same nouns ("type %s is not a
//     struct", "func %s(ctx) was replaced"). A format-initial keyword
//     is code only with positive evidence after the verb: for func, a
//     parenthesis immediately after the identifier run plus a '{' or
//     newline later in the format; for type, one of struct/interface/
//     map/func/chan/'='/'['/'*' next; for var, '='/':=' or one of
//     those keywords. After a real code boundary (newline, tab, brace,
//     paren, semicolon, colon, equals) the keyword alone is evidence.
//   - SQL statement shapes outside the anchored phrase list (alter/add/
//     create/drop table, create unique index/index, drop index, create
//     view/trigger, insert (or replace) into, delete from, pragma
//     table_info), with the verb allowed to complete an identifier the
//     phrase began (`create index idx_%s`): a shape not in the list is
//     silent until its phrase is added.
//   - verbs after '?' or '#' inside a '/'-leading quoted string: route
//     slots cover the PATH portion only. A query parameter is a
//     different grammar with a different escaper (url.QueryEscape),
//     not an identifier slot.
//   - JS/TS spellings without their code evidence, deliberately: every
//     declaration keyword (export const/const/let/function/class/
//     interface/type, readonly) demands '=' / ':' / '('+brace / '{' or
//     an extends clause after the name — "let %s go", "type %s is not
//     a struct", and "interface %s for plugins" are prose. A bare
//     object key must be the FIRST token of its line AND the emitted
//     line must end in ';' or ',' — `  %s: %d items` is a two-column
//     listing, not a member — and must not be quoted: %q and the
//     this[%q] bracket spelling ARE the fix posture for JS grammars,
//     where a quoted key cannot leave its object literal.
package emitident

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "emitident",
	Doc:  "forbids fmt.Sprintf/Fprintf/Appendf formats that substitute an ungated name into an identifier slot of emitted code (Go declarations, SQL DDL, CSS string slots, route paths, JS/TS declarations and object keys); validate with an ident check or quote for the grammar",
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

// treatedFieldRe matches field names that state the VALUE'S TREATMENT
// (quoted, sanitized, escaped): the store constructed the value through
// a quoting/validation helper and named the field for what was done to
// it — the battery stores' quotedTable shape. This is deliberately
// narrower than gateNameRe: noun families like HandlerType/FieldType/
// Ident describe what a value IS, not how it was treated, and gate
// nothing.
var treatedFieldRe = regexp.MustCompile(`(?i)quoted|sanit|escap`)

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

// checkFile reports ungated names formatted into identifier slots. The
// function-local guard is computed per emit site: a check-named call
// gates its arguments only where it dominates the emit — the emit
// inside the check's success arm, or after a failure arm that diverges
// (return/panic/os.Exit, or a continue guarding emits inside the same
// loop body only). A check that only warns and falls
// through gates nothing: the hostile name still reaches the format.
func checkFile(pass *analysis.Pass, f *ast.File, helperDecls map[*types.Func]*ast.FuncDecl, memo map[string]bool, paramGate map[*types.Var]bool) {
	bound := boundExprs(pass, f)

	type guard struct {
		obj        types.Object
		st         *ast.IfStmt
		bodyIsFail bool // check negated (or in the if's INIT): the Body is the failure arm
	}
	var processBody func(body *ast.BlockStmt)
	processBody = func(body *ast.BlockStmt) {
		var ifs []*ast.IfStmt
		var loops []ast.Node
		var emits []*ast.CallExpr
		ast.Inspect(body, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.FuncLit:
				processBody(e.Body) // a closure may run later: separate scope
				return false
			case *ast.IfStmt:
				ifs = append(ifs, e)
			case *ast.ForStmt, *ast.RangeStmt:
				loops = append(loops, e)
			case *ast.CallExpr:
				if _, ok := fmtCalls[qualifiedCallee(pass, e.Fun)]; ok {
					emits = append(emits, e)
				}
			}
			return true
		})
		var guards []guard
		addGuards := func(call *ast.CallExpr, st *ast.IfStmt, bodyIsFail bool) {
			for _, a := range call.Args {
				if id, ok := a.(*ast.Ident); ok {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						guards = append(guards, guard{obj: obj, st: st, bodyIsFail: bodyIsFail})
					}
				}
			}
		}
		for _, st := range ifs {
			if st.Cond != nil {
				collectCondChecks(pass, st.Cond, 0, func(call *ast.CallExpr, neg int) {
					addGuards(call, st, neg%2 == 1)
				})
			}
			if st.Init != nil {
				// A check in the if's INIT:
				// `if _, err := query.SafeIdent(table); err != nil`
				// — the Body tests the check's failure.
				ast.Inspect(st.Init, func(c ast.Node) bool {
					if call, ok := c.(*ast.CallExpr); ok && isCheckCall(pass, call.Fun) {
						addGuards(call, st, true)
					}
					return true
				})
			}
		}
		for _, emit := range emits {
			guarded := map[types.Object]bool{}
			for _, g := range guards {
				if g.bodyIsFail {
					// The else arm of a negated check is its success arm.
					if g.st.Else != nil && emit.Pos() >= g.st.Else.Pos() && emit.Pos() <= g.st.Else.End() {
						guarded[g.obj] = true
						continue
					}
					if emit.Pos() > g.st.End() {
						if bodyDiverges(pass, g.st.Body) {
							guarded[g.obj] = true
						} else if bodyContinues(g.st.Body) && sameLoopBody(loops, g.st, emit) {
							// A continue only skips the current
							// iteration: it guards emits inside the
							// same loop body, never an emit after the
							// loop — the value that hit the continue
							// is exactly the unchecked one a post-loop
							// emit can format.
							guarded[g.obj] = true
						}
					}
					continue
				}
				if emit.Pos() >= g.st.Body.Pos() && emit.Pos() <= g.st.Body.End() {
					guarded[g.obj] = true
				}
			}
			checkEmit(pass, emit, bound, helperDecls, guarded, memo, paramGate)
		}
	}
	for _, decl := range f.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Body != nil {
			processBody(fd.Body)
		}
	}
}

// collectCondChecks finds check-named calls in an if condition, tracking
// how many negations wrap each (the parity decides which arm is the
// check's success arm).
func collectCondChecks(pass *analysis.Pass, x ast.Expr, neg int, add func(call *ast.CallExpr, neg int)) {
	switch e := x.(type) {
	case *ast.ParenExpr:
		collectCondChecks(pass, e.X, neg, add)
	case *ast.UnaryExpr:
		n := neg
		if e.Op == token.NOT {
			n++
		}
		collectCondChecks(pass, e.X, n, add)
	case *ast.BinaryExpr:
		collectCondChecks(pass, e.X, neg, add)
		collectCondChecks(pass, e.Y, neg, add)
	}
	if call, ok := x.(*ast.CallExpr); ok && isCheckCall(pass, call.Fun) {
		add(call, neg)
	}
}

// bodyDiverges reports whether the block's control flow unconditionally
// leaves the FUNCTION: a top-level return, panic, or os.Exit. Statements
// nested in inner conditionals do not count — their other paths fall
// through. A continue does not count either: it only skips to the next
// iteration (see bodyContinues).
func bodyDiverges(pass *analysis.Pass, body *ast.BlockStmt) bool {
	for _, st := range body.List {
		switch s := st.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.ExprStmt:
			if call, ok := s.X.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
					return true
				}
				if qualifiedCallee(pass, call.Fun) == "os.Exit" {
					return true
				}
			}
		}
	}
	return false
}

// bodyContinues reports whether the block's top level is a continue.
func bodyContinues(body *ast.BlockStmt) bool {
	for _, st := range body.List {
		if s, ok := st.(*ast.BranchStmt); ok && s.Tok == token.CONTINUE {
			return true
		}
	}
	return false
}

// sameLoopBody reports whether st and emit sit inside the same loop's
// BODY: the innermost loop enclosing st, with emit lexically inside
// that loop's body. A continue divergence guards only there.
func sameLoopBody(loops []ast.Node, st *ast.IfStmt, emit *ast.CallExpr) bool {
	var innermost ast.Node
	for _, lp := range loops {
		if st.Pos() >= lp.Pos() && st.End() <= lp.End() {
			if innermost == nil || lp.End()-lp.Pos() < innermost.End()-innermost.Pos() {
				innermost = lp
			}
		}
	}
	if innermost == nil {
		return false
	}
	var body *ast.BlockStmt
	switch l := innermost.(type) {
	case *ast.ForStmt:
		body = l.Body
	case *ast.RangeStmt:
		body = l.Body
	}
	return body != nil && emit.Pos() >= body.Pos() && emit.End() <= body.End()
}

// checkEmit scans one Sprintf-family call's format for identifier slots
// and reports the first ungated one.
func checkEmit(pass *analysis.Pass, call *ast.CallExpr, bound map[types.Object]ast.Expr, helperDecls map[*types.Func]*ast.FuncDecl, guarded map[types.Object]bool, memo map[string]bool, paramGate map[*types.Var]bool) {
	formatIndex, ok := fmtCalls[qualifiedCallee(pass, call.Fun)]
	if !ok || formatIndex >= len(call.Args) {
		return
	}
	format, ok := stringLiteralValue(pass, call.Args[formatIndex])
	if !ok {
		return
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
			if kwStart == 0 {
				// A format-INITIAL keyword is not evidence by itself:
				// lowercase messages start with the same nouns
				// ("type %s is not a struct"). Demand positive code
				// evidence after the verb (declFollows).
				if declFollows(format, after, kw) {
					return kw + " " + format[b:after], true
				}
				continue
			}
			// After a code boundary (newline, tab, brace, paren,
			// semicolon, colon, equals) the keyword alone is evidence:
			// "component type %s from another package" is prose, but
			// nothing but code follows a boundary.
			if strings.IndexByte("\n\t{;(:=", lower[kwStart-1]) >= 0 {
				return kw + " " + format[b:after], true
			}
		}
	}

	// SQL identifier slots. %q here IS the quoting mechanism
	// (SQLite/Postgres double-quoted identifiers). This is an anchored
	// phrase list, declared as such: a statement shape outside it is
	// silent until its phrase is added. The verb may complete an
	// identifier the phrase began (`create index idx_%s`).
	if !quoted {
		for _, phrase := range []string{
			"alter table ", "add column if not exists ", "add column ",
			"create table if not exists ", "create table ",
			"drop table if exists ", "drop table ",
			"create unique index ", "create index ", "drop index ",
			"create view ", "create trigger ",
			"insert or replace into ", "insert into ", "delete from ",
			"pragma table_info(",
		} {
			if slotAfterPhrase(lower, i, phrase) {
				return phrase + "%s", true
			}
		}
	}

	// CSS string slots: any verb inside a single-quoted run opened as a
	// CSS string — after a property name and colon (`content: '%s'`,
	// `font-family: '%s'`) or inside a function call (`url('...')`).
	// @font-face is only where these slots first appeared. The property
	// prefix is what keeps an apostrophe in prose ("the app's fonts")
	// from opening a run; an odd quote count alone would let one.
	if !quoted && strings.Count(lower[:i], "'")%2 == 1 {
		q := strings.LastIndexByte(lower[:i], '\'')
		if q > 0 && cssStringOpen(lower[:q]) {
			return "'%s'", true
		}
	}

	// The widget behavior URL shape, then route path literals generally:
	// a verb that starts a path segment anywhere inside a double-quoted
	// string beginning with "/" — `"/%s"` and `"/api/v1/%s"` alike; a
	// deeper segment rewrites the route the same way the first does.
	if !quoted && strings.Contains(lower, "/__gofastr/widget/") {
		return "/__gofastr/widget/%s", true
	}
	if !quoted && b == i {
		if q := strings.LastIndexByte(format[:i], '"'); q >= 0 && format[q+1] == '/' {
			// The PATH portion only: a verb after '?' or '#' sits in
			// the query string or fragment, a different grammar with
			// a different escaper.
			if frag := format[q:i]; !strings.ContainsAny(frag, "?#") {
				return frag + "%s", true
			}
		}
	}

	// JS/TS declaration spellings: export const/const/let declarations,
	// function/class/interface/type declarations, readonly properties,
	// and unquoted object keys at line start. %q never lands here: a
	// quoted key or this["name"] spelling IS the fix posture.
	if !quoted {
		if text, ok := jsIdentSlotText(format, i); ok {
			return text, true
		}
	}
	return "", false
}

// jsModifiers are the words that may precede a JS/TS declaration keyword
// at the start of a line. readonly is pointedly NOT here: it is its own
// slot spelling (readonly %s:), not a modifier of another keyword.
var jsModifiers = map[string]bool{
	"export": true, "declare": true, "default": true, "async": true,
	"abstract": true, "public": true, "private": true, "protected": true,
	"static": true,
}

// jsIdentSlotText reports whether the unquoted verb at offset i sits in
// a JS/TS identifier slot of the emitted code. The grammars are
// line-oriented: a declaration or object key must start its line, so
// prose mid-sentence never matches, and every spelling demands positive
// code evidence after the name (=, :, (, or {) — the same defense the
// Go side's declFollows mounts against lowercase messages.
func jsIdentSlotText(format string, i int) (string, bool) {
	lower := strings.ToLower(format)
	// w is the first non-blank byte of the verb's line.
	w := strings.LastIndexByte(format[:i], '\n') + 1
	for w < i && (format[w] == ' ' || format[w] == '\t') {
		w++
	}

	// Declaration keywords, each optionally preceded by modifiers.
	mw := w
	var mods []string
	for {
		e := mw
		for e < len(format) && isIdentByte(format[e]) {
			e++
		}
		word := lower[mw:e]
		if !jsModifiers[word] {
			break
		}
		mods = append(mods, word)
		mw = e
		for mw < len(format) && (format[mw] == ' ' || format[mw] == '\t') {
			mw++
		}
	}
	if kw := jsKeywordAt(lower, mw); kw != "" {
		// b is the start of the identifier run containing the verb
		// (computed by the caller too; re-derived here for locality).
		b := i
		for b > 0 && isIdentByte(format[b-1]) {
			b--
		}
		if onlyBlank(format[mw+len(kw) : b]) {
			if r := i + verbLen(format, i); r < len(format) {
				r = skipIdent(format, r)
				if jsDeclEvidence(format, r, kw) {
					spelling := strings.Join(append(mods, kw), " ") + " %s"
					if kw == "readonly" {
						spelling += ":"
					}
					if kw == "function" {
						spelling += "("
					}
					return spelling, true
				}
			}
		}
		// A declaration spelling that does not carry its keyword's
		// evidence is prose ("let %s go", "type %s is not a struct");
		// fall through to the bare-key check, which has its own.
	}

	// Bare object key: the verb sits inside the line's FIRST token, a
	// run of identifier bytes and verbs closed by an optional '?' then
	// ':', with a member terminator (';' or ',') at the end of the
	// emitted line — that terminator is what separates `  %s: %q;` from
	// a two-column listing like `  %s: %d items`.
	k, sawVerb := w, false
	for k < len(format) {
		if k == i {
			sawVerb = true
			k += verbLen(format, i)
			continue
		}
		if isIdentByte(format[k]) {
			k++
			continue
		}
		if format[k] == '%' {
			if l := verbLen(format, k); l > 0 {
				k += l
				continue
			}
		}
		break
	}
	if !sawVerb {
		return "", false
	}
	if k < len(format) && format[k] == '?' {
		k++
	}
	if k >= len(format) || format[k] != ':' {
		return "", false
	}
	e := k
	for e < len(format) && format[e] != '\n' {
		e++
	}
	for e > k && (format[e-1] == ' ' || format[e-1] == '\t' || format[e-1] == '\r') {
		e--
	}
	if e > k && (format[e-1] == ';' || format[e-1] == ',') {
		return "%s:", true
	}
	return "", false
}

// jsKeywordAt reports which JS/TS declaration keyword starts at w.
func jsKeywordAt(lower string, w int) string {
	for _, kw := range []string{"function", "interface", "readonly", "const", "class", "let", "type"} {
		if strings.HasPrefix(lower[w:], kw) {
			if n := w + len(kw); n >= len(lower) || !isIdentByte(lower[n]) {
				return kw
			}
		}
	}
	return ""
}

// jsDeclEvidence reports whether the text at r (right after the declared
// name) is the positive code evidence the keyword requires: '=' or ':'
// for const/let/readonly, a parameter list plus a brace or newline for
// function, and a '{' (directly or after one extends clause) for
// class/interface; type takes '=' after an optional generic clause.
func jsDeclEvidence(format string, r int, kw string) bool {
	s := r
	for s < len(format) && (format[s] == ' ' || format[s] == '\t') {
		s++
	}
	if s >= len(format) {
		return false
	}
	switch kw {
	case "const", "let", "readonly":
		return format[s] == '=' || format[s] == ':' || (kw == "readonly" && format[s] == '?')
	case "function":
		if format[s] != '(' {
			return false
		}
		return strings.IndexByte(format[s:], '{') >= 0 || strings.IndexByte(format[s:], '\n') >= 0
	case "class", "interface":
		if format[s] == '{' {
			return true
		}
		// One extends clause: `extends Base` (dots allowed), then '{'.
		if strings.HasPrefix(format[s:], "extends") && !isIdentByte(format[s+7]) {
			s += 7
			for s < len(format) && (format[s] == ' ' || format[s] == '\t') {
				s++
			}
			for s < len(format) && (isIdentByte(format[s]) || format[s] == '.') {
				s++
			}
			for s < len(format) && (format[s] == ' ' || format[s] == '\t') {
				s++
			}
			return s < len(format) && format[s] == '{'
		}
		return false
	case "type":
		// Optional generic clause `<T extends X = D>`: scan to the
		// matching '>' allowing only identifier-ish bytes inside.
		if format[s] == '<' {
			d := s + 1
			for d < len(format) && format[d] != '>' && (isIdentByte(format[d]) || strings.IndexByte(", \t", format[d]) >= 0) {
				d++
			}
			if d < len(format) && format[d] == '>' {
				s = d + 1
				for s < len(format) && (format[s] == ' ' || format[s] == '\t') {
					s++
				}
			}
		}
		return s < len(format) && format[s] == '='
	}
	return false
}

func skipIdent(s string, i int) int {
	for i < len(s) && isIdentByte(s[i]) {
		i++
	}
	return i
}

func onlyBlank(s string) bool {
	for i := range s {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// declFollows reports whether the text after a format-initial
// func/type/var keyword's identifier slot is positive evidence of a
// declaration rather than prose: for func, a parenthesis immediately
// after the identifier run plus a brace or newline later in the format
// ("func %s(ctx context.Context) error {", not "func %s(ctx) was
// replaced"); for type/var, the next word being struct/interface/map/
// func/chan (or '*'/'[') or an assignment operator.
func declFollows(format string, after int, kw string) bool {
	rest := format[after:]
	spaces := 0
	for spaces < len(rest) && (rest[spaces] == ' ' || rest[spaces] == '\t') {
		spaces++
	}
	rest = rest[spaces:]
	switch kw {
	case "func":
		// The parenthesis follows the identifier RUN, not the verb:
		// "func %sHandler(ctx context.Context) error {".
		for len(rest) > 0 && isIdentByte(rest[0]) {
			rest = rest[1:]
		}
		return strings.HasPrefix(rest, "(") &&
			(strings.IndexByte(rest, '{') >= 0 || strings.IndexByte(rest, '\n') >= 0)
	case "type", "var":
		for _, t := range []string{"struct", "interface", "map", "func", "chan"} {
			if strings.HasPrefix(rest, t) && (len(rest) == len(t) || !isIdentByte(rest[len(t)])) {
				return true
			}
		}
		return strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, "[") ||
			strings.HasPrefix(rest, "*") || (kw == "var" && strings.HasPrefix(rest, ":="))
	}
	return false
}

// cssStringOpen reports whether the text before a single quote opens a
// CSS string: a property name then a colon (`content: '`), or an open
// paren (`url('`).
func cssStringOpen(before string) bool {
	j := len(before)
	for j > 0 && (before[j-1] == ' ' || before[j-1] == '\t') {
		j--
	}
	if j == 0 {
		return false
	}
	switch before[j-1] {
	case '(':
		return true
	case ':':
		n := 0
		for j--; j > 0 && isPropByte(before[j-1]); j-- {
			n++
		}
		return n > 0
	}
	return false
}

func isPropByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-'
}

// slotAfterPhrase reports whether the verb at offset i is the identifier
// slot directly following the given (lowercased) phrase: whitespace,
// then optionally an identifier prefix the verb completes
// (`create index idx_%s`). Prose words between phrase and verb (a word
// followed by more space) disqualify the slot.
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
		if gapOK(lower[end:i]) {
			return true
		}
		start = p + 1
	}
}

// gapOK reports whether the text between a phrase and its verb is only
// whitespace followed by identifier bytes glued to the verb.
func gapOK(s string) bool {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for ; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
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
		fields[fieldKey(pass, e)] = true
		if treatedFieldRe.MatchString(e.Sel.Name) {
			return true, fields // quotedTable, sanitizedSlot: the name states the VALUE'S TREATMENT
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
							fields[fieldKey(pass, sel)] = true
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
		allGated := len(e.Elts) > 0
		for _, elt := range e.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				elt = kv.Value
			}
			g, sub := resolveArg(pass, elt, bound, helperDecls, guarded, memo, paramGate, depth+1)
			collect(sub)
			if !g && (len(sub) == 0 || !allInMemo(sub, memo)) {
				allGated = false
			}
		}
		if allGated {
			return true, fields // a composite of constants carries no caller input
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
// helper is itself gated. The returns are resolved against the HELPER'S
// own local bindings, not the caller's: a helper whose source local is
// bound to a strings result (blueprintEndpointHandlerName's
// strings.TrimSpace(endpoint.Handler)) is gated even though the caller
// has never heard of that local.
func helperReturnsGated(pass *analysis.Pass, decl *ast.FuncDecl, bound map[types.Object]ast.Expr, helperDecls map[*types.Func]*ast.FuncDecl, guarded map[types.Object]bool, memo map[string]bool, paramGate map[*types.Var]bool, depth int) bool {
	hb := bodyBindings(pass, decl)
	gated := true
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if ret, ok := n.(*ast.ReturnStmt); ok {
			for _, r := range ret.Results {
				g, sub := resolveArg(pass, r, hb, helperDecls, guarded, memo, paramGate, depth+1)
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

// fieldKey names a derivation root for the package memo as
// TypeName.Field: a check on one struct's Table field says nothing
// about a DIFFERENT struct's Table field (the sdkjsident probe slipped
// through exactly that conflation: blueprint's query.SafeIdent on
// EntityDeclaration.Table silenced cliEntity.Table in the SDK emitter).
// Unnamed struct types fall back to the bare field name.
func fieldKey(pass *analysis.Pass, sel *ast.SelectorExpr) string {
	t := pass.TypesInfo.TypeOf(sel.X)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name() + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}

// fieldRoots collects the struct-field derivation roots (type-qualified
// by fieldKey) for the package memo.
func fieldRoots(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr, helperDecls map[*types.Func]*ast.FuncDecl) map[string]bool {
	_, fields := resolveArg(pass, x, bound, helperDecls, nil, map[string]bool{}, nil, 0)
	return fields
}

func boundExprs(pass *analysis.Pass, f *ast.File) map[types.Object]ast.Expr {
	return bodyBindings(pass, f)
}

// bodyBindings maps each local defined inside n to the expression it was
// last bound to: single and tuple assignments, := declarations, and
// range value variables.
func bodyBindings(pass *analysis.Pass, n ast.Node) map[types.Object]ast.Expr {
	m := map[types.Object]ast.Expr{}
	ast.Inspect(n, func(node ast.Node) bool {
		switch st := node.(type) {
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

// stringLiteralValue returns the constant string value of e.
func stringLiteralValue(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
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
