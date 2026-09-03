// Package controlbytes catches request-derived strings reaching a log,
// span-attribute, or header sink without a control-byte scrub.
//
// The bug class is terminal/log/header injection: r.URL.Path and
// r.Header values arrive PERCENT-DECODED, so %0d%0a, %1b and %00 in a
// request are real CRLF/ESC/NUL by the time middleware handles them. A
// raw CRLF forges an entry in any line-oriented log consumer; a raw ESC
// paints attacker bytes into every operator tail; a NUL in a header
// value reaches recorders and header-copying proxies verbatim
// (net/http only collapses CR/LF at write time). The 419-probe audit
// found this shape four times, each fixed the same way — scrub at the
// sink — and this rule fires on the shape, not the site:
//
//   - battery/log accessMiddleware entries (probe
//     TestAccessEntryScrubbedOfControlBytes, fixed 4b7a25d2),
//   - core/middleware Idempotency's Finish-failure log (probe
//     TestIdempotencyFinishLogKeyScrubbed, fixed b79942f7),
//   - core/middleware Tracing's span attributes (probe
//     TestTracing_SpanAttrsScrubControlBytes, fixed b79942f7),
//   - framework/uihost's Link-header alternate path (probe
//     TestLinkAlternatePathControlBytes, fixed a24928c1).
//
// Log sinks are: slog.String/slog.Any values; the key-value logger
// calls (Debug/Info/Warn/Error and their *Context forms, plus Log) on
// a *slog.Logger receiver AND package-level slog.* (the default logger
// writes to stderr); the log MESSAGE as well as the values — a CRLF in
// a message forges log lines exactly like one in a value; fmt print
// calls — Fprint* to os.Stdout/os.Stderr and Print/Printf/Println,
// which write to os.Stdout unconditionally; and the std log package's
// Print/Printf/Println, package-level or on a *log.Logger receiver.
//
// A value counts as scrubbed when it passes through a callee whose name
// says so (scrub/sanitize/escape/quote/redact — r.URL.EscapedPath
// and url.QueryEscape qualify) or through a same-package helper that
// inspects the value byte by byte (the byte-filter loop the uihost fix
// shipped inside markdownAlternate; a pass-through helper like
// TrimRight or truncate never looks at individual bytes and does not
// clear taint). A name whose only scrub evidence is the substring
// "clean" does NOT clear on the name: path.Clean and its kin are
// separator normalizers, not scrubbers, so a clean-named callee —
// foreign or local — must show the byte-level body evidence instead.
// Qualified calls into the stdlib path and path/filepath packages
// (Clean, Base, Dir, Join) never clear taint at all.
//
// A tested value is clean for that variable when a validator-named
// call (validRequestID(id)) vetted it anywhere earlier in the
// function, or when a map membership (allowed[origin]) appears in the
// condition of an if statement or switch case that lexically encloses
// the sink — the sink then runs only for members of the configured
// set, and a control byte cannot be in that set. The negated-denial
// spelling counts as the same vetting — membership tested with `!`
// (directly or through comma-ok) in an if whose denial arm diverges
// (returns or panics), so code after the if runs only for members
// (battery/auth's BFF origin guard). A positive dedup/seen lookup
// gates nothing anywhere else in the function: that sink runs exactly
// for values the map has never vetted. Residual: a DEDUP map spelled
// with the negated-diverging form (`if _, dup := seen[p]; !dup {
// return }`) is lexically indistinguishable from an allowlist and is
// granted the same credit — the names differ, the shape does not.
//
// Postures it deliberately stays silent on, because they are not this
// bug: JSON and HTML encoders escape structurally (encoding/json,
// html/template), so encoder arguments are left alone; the response
// BODY is not a sink — it is the response; span NAMES (tracer.Start,
// span.SetName) and log keys are left alone, only messages and VALUES
// are checked; fmt.Sprint* without a writer, and Fprint* to any writer
// other than os.Stdout/os.Stderr (an http.ResponseWriter or a
// bytes.Buffer has its own framing); Header.Set/Add on a map whose
// provenance is an OUTBOUND *http.Request — an
// http.NewRequest/NewRequestWithContext result, or any request-typed
// value that is not the inbound handler parameter — because the client
// transport validates header bytes at write time and rejects control
// bytes (the response writer's header map, and the inbound
// parameter's, still fire); taint does not cross function boundaries —
// a request-derived argument to a helper is the helper's business, and
// the byte-indexing form above is the whole interprocedural
// concession; and structured values like a whole *http.Request or an
// ErrorReport struct are not sources, only the string-bearing request
// selectors are (Method/Host/RemoteAddr/RequestURI/URL.Path/
// URL.RawQuery, the Header.Get/FormValue/PathValue/PostFormValue/
// Referer/UserAgent/BasicAuth accessors, url.Values.Get, and the
// Value field of a cookie bound from r.Cookie).
package controlbytes

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "controlbytes",
	Doc:  "report request-derived strings reaching log/span/header sinks without a control-byte scrub",
	Run:  run,
}

// scrubName matches the callee names the repo treats as a control-byte
// scrub. Deliberately generous: any escape/quote re-encodes by
// construction, and recognizing scrubbing by body shape across package
// boundaries is beyond a one-pass analyzer.
var scrubName = regexp.MustCompile(`(?i)scrub|sanitiz|clean|escape|quote|redact`)

// strongScrubName matches the unambiguous re-encoders; cleanOnlyName
// matches names whose ONLY scrub evidence is the substring "clean".
// path.Clean and friends normalize separators and dots and pass every
// control byte through, so a clean-named callee must show byte-level
// body evidence instead of clearing on the name.
var (
	strongScrubName = regexp.MustCompile(`(?i)scrub|sanitiz|escape|quote|redact`)
	cleanOnlyName   = regexp.MustCompile(`(?i)clean`)
)

// scrubTrusted: the name clears taint on the name alone.
func scrubTrusted(name string) bool {
	return scrubName.MatchString(name) && (strongScrubName.MatchString(name) || !cleanOnlyName.MatchString(name))
}

// isStdlibNormalizer: a qualified call into the stdlib path or
// path/filepath packages. Clean/Base/Dir/Join there reorder separators
// and dots; they never remove a control byte, so they clear nothing.
func isStdlibNormalizer(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	p := pkgPathOf(pass, sel)
	return p == "path" || p == "path/filepath"
}

func run(pass *analysis.Pass) (any, error) {
	// Package-local function decls, for the byte-indexing scrub check.
	decls := map[types.Object]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if obj := pass.TypesInfo.Defs[fn.Name]; obj != nil {
				decls[obj] = fn
			}
		}
	}

	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			// Tests are not production sinks; a control byte in a
			// test's log line fails nothing that ships.
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					checkBody(pass, decls, d.Body, nil)
				}
			case *ast.GenDecl:
				// Top-level func literals (var h = func() {...}).
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, val := range vs.Values {
						if lit, ok := val.(*ast.FuncLit); ok {
							checkBody(pass, decls, lit.Body, nil)
						}
					}
				}
			}
		}
	}
	return nil, nil
}

// checkBody analyzes one function body. A nested function literal is
// analyzed exactly once, here, with the enclosing taint as its base: a
// closure captures the enclosing function's locals, so a snapshot
// taken before a `defer func(){...}()` must still read as tainted
// inside the literal (that defer is exactly the access-log shape).
// Descending from the outside as well would report every sink twice,
// so the walk below stops at literals.
func checkBody(pass *analysis.Pass, decls map[types.Object]*ast.FuncDecl, body *ast.BlockStmt, parent *taint) {
	t := newTaint(pass, decls, body, parent)
	guards := testedValues(pass, body)
	for _, stmt := range body.List {
		walkStmt(pass, decls, t, guards, stmt)
	}
}

func walkStmt(pass *analysis.Pass, decls map[types.Object]*ast.FuncDecl, t *taint, guards map[types.Object][]guard, stmt ast.Stmt) {
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			checkBody(pass, decls, n.Body, t)
			return false
		case *ast.CallExpr:
			for _, s := range sinks {
				if !s.matches(t, n) {
					continue
				}
				for _, arg := range s.args(t, n) {
					if id, ok := unparen(arg).(*ast.Ident); ok {
						if testedAt(guards[pass.TypesInfo.ObjectOf(id)], n.Pos()) {
							// The value already passed a validator or
							// an enclosing allowlist on its way here;
							// the guard is the sanitizer (requestid's
							// validRequestID, CORS's originSet[origin]).
							continue
						}
					}
					if t.source(arg) {
						pass.Reportf(n.Pos(),
							"controlbytes: request-derived value reaches %s unscrubbed; C0/DEL bytes forge log lines and header values (scrub or escape at the sink)",
							s.name)
						break
					}
				}
			}
		}
		return true
	})
}

var validatorName = regexp.MustCompile(`(?i)valid|allow|permit|accept|match|safe`)

// guard is one recorded vetting of a variable: it fired at pos, and it
// covers sinks up to end (end == 0 means the rest of the function).
type guard struct {
	pos, end token.Pos
}

// testedAt: some guard on obj fired before pos and still covers it.
func testedAt(gs []guard, pos token.Pos) bool {
	for _, g := range gs {
		if g.pos < pos && (g.end == 0 || pos < g.end) {
			return true
		}
	}
	return false
}

// testedValues records where each variable was TESTED before use: an
// argument of a validator-named call (validRequestID(id),
// isSafePartialRedirect(u)) vets the variable for the rest of the
// function; a membership lookup (allowed[origin]) vets it only when
// the lookup keys a value the code gates on — the index appears in the
// condition of an if statement or switch case that lexically encloses
// the sink, so the sink runs only for members of the configured set.
// A value the code has already vetted cannot smuggle control bytes
// past that vetting; a dedup/seen lookup anywhere else proves nothing
// about the bytes and does not vet.
func testedValues(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object][]guard {
	out := map[types.Object][]guard{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			var name string
			switch fun := unparen(n.Fun).(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			if !validatorName.MatchString(name) {
				return true
			}
			for _, a := range n.Args {
				if id, ok := unparen(a).(*ast.Ident); ok {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						out[obj] = append(out[obj], guard{pos: n.Pos()})
					}
				}
			}
		case *ast.IfStmt:
			// Only the THEN arm runs for members; the else arm runs
			// for NON-members and is not vetted.
			markMembership(out, pass, n.Cond, n.Body.End())
			markNegatedDenial(out, pass, n)
		case *ast.SwitchStmt:
			// A tag lookup gates each non-default case body; the
			// default clause runs for non-members. Credit is taken
			// per CaseClause below.
		case *ast.CaseClause:
			// A case expression gates only that case's own body.
			for _, e := range n.List {
				markMembership(out, pass, e, n.End())
			}
		}
		return true
	})
	return out
}

// markMembership records map-membership lookups inside cond as guards
// covering sinks lexically inside [lookup, end).
func markMembership(out map[types.Object][]guard, pass *analysis.Pass, cond ast.Expr, end token.Pos) {
	if cond == nil {
		return
	}
	ast.Inspect(cond, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		id, ok := idx.Index.(*ast.Ident)
		if !ok {
			return true
		}
		if t := pass.TypesInfo.TypeOf(idx.X); t != nil {
			if _, ok := t.Underlying().(*types.Map); ok {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					out[obj] = append(out[obj], guard{pos: idx.Pos(), end: end})
				}
			}
		}
		return true
	})
}

// markNegatedDenial covers the allowlist shape the repo's BFF guard
// uses: the membership is tested NEGATED and the denial arm diverges
// (`if _, ok := allowed[origin]; !ok { http.Error(...); return }`,
// `if !allowed[origin] { return }`). Everything after the if runs only
// for MEMBERS of the configured set — the vetting the enclosing-if
// form gets — so the lookup guards the rest of the function. The
// positive spellings (`if seen[p] { return }`) stay unguarded: there
// the sink runs only for values the map has never vetted.
func markNegatedDenial(out map[types.Object][]guard, pass *analysis.Pass, ifst *ast.IfStmt) {
	if ifst.Body == nil || !diverges(ifst.Body) {
		return
	}
	var idx *ast.IndexExpr
	cond := unparen(ifst.Cond)
	negated := false
	if u, ok := cond.(*ast.UnaryExpr); ok && u.Op == token.NOT {
		negated = true
		cond = unparen(u.X)
	}
	// Comma-ok: `v, ok := m[k]` in Init, `!ok` in Cond.
	if as, ok := ifst.Init.(*ast.AssignStmt); ok && len(as.Lhs) == 2 && len(as.Rhs) == 1 {
		if i, ok := unparen(as.Rhs[0]).(*ast.IndexExpr); ok {
			if okID, ok := as.Lhs[1].(*ast.Ident); ok && negated {
				if id, ok := unparen(cond).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == pass.TypesInfo.ObjectOf(okID) {
					idx = i
				}
			}
		}
	}
	// Direct: `!allowed[origin]` in Cond.
	if idx == nil && negated {
		if i, ok := cond.(*ast.IndexExpr); ok {
			idx = i
		}
	}
	if idx == nil {
		return
	}
	id, ok := idx.Index.(*ast.Ident)
	if !ok {
		return
	}
	t := pass.TypesInfo.TypeOf(idx.X)
	if t == nil {
		return
	}
	if _, isMap := t.Underlying().(*types.Map); isMap {
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			out[obj] = append(out[obj], guard{pos: idx.Pos()})
		}
	}
}

// diverges: the block exits the function here (return or panic), so
// code AFTER the enclosing statement runs only when the condition was
// false.
func diverges(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			found = true
		case *ast.CallExpr:
			if id, ok := unparen(x.Fun).(*ast.Ident); ok && id.Name == "panic" {
				found = true
			}
		}
		return !found
	})
	return found
}

// ---- sinks -------------------------------------------------------------

type sink struct {
	name    string
	args    func(t *taint, call *ast.CallExpr) []ast.Expr
	matches func(t *taint, call *ast.CallExpr) bool
}

var sinks = []sink{
	{
		name: "slog.String/slog.Any",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch qualifiedFunc(t.pass, sel) {
			case "slog.String", "slog.Any":
				return true
			}
			return false
		},
		args: valueArg1,
	},
	{
		name: "attribute.String",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			return qualifiedFunc(t.pass, sel) == "attribute.String"
		},
		args: valueArg1,
	},
	{
		name: "logger.Debug/Info/Warn/Error key-value",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch sel.Sel.Name {
			case "Debug", "Info", "Warn", "Error",
				"DebugContext", "InfoContext", "WarnContext", "ErrorContext":
			default:
				return false
			}
			tv, ok := t.pass.TypesInfo.Types[sel.X]
			if !ok {
				return false
			}
			return isNamed(deref(tv.Type), "log/slog", "Logger")
		},
		// Warn(msg, k1, v1, k2, v2): the message and the values sit at
		// even offsets — msg at 0 for the plain form, 1 for *Context.
		args: func(t *taint, call *ast.CallExpr) []ast.Expr {
			sel, _ := unparen(call.Fun).(*ast.SelectorExpr)
			start := 0
			if sel != nil && strings.HasSuffix(sel.Sel.Name, "Context") {
				start = 1
			}
			var out []ast.Expr
			for i := start; i < len(call.Args); i += 2 {
				out = append(out, call.Args[i])
			}
			return out
		},
	},
	{
		// Package-level slog.* writes to the default logger (stderr).
		name: "slog.Debug/Info/Warn/Error key-value",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch sel.Sel.Name {
			case "Debug", "Info", "Warn", "Error",
				"DebugContext", "InfoContext", "WarnContext", "ErrorContext":
			default:
				return false
			}
			return pkgPathOf(t.pass, sel) == "log/slog"
		},
		// msg, k1, v1: message and values at even offsets from 0; the
		// *Context forms shift one for ctx.
		args: func(t *taint, call *ast.CallExpr) []ast.Expr {
			sel, _ := unparen(call.Fun).(*ast.SelectorExpr)
			start := 0
			if sel != nil && strings.HasSuffix(sel.Sel.Name, "Context") {
				start = 1
			}
			var out []ast.Expr
			for i := start; i < len(call.Args); i += 2 {
				out = append(out, call.Args[i])
			}
			return out
		},
	},
	{
		// Log(ctx, level, msg, k1, v1) on a receiver or package-level:
		// message and values at even offsets from 2.
		name: "slog.Log key-value",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Log" {
				return false
			}
			if pkgPathOf(t.pass, sel) == "log/slog" {
				return true
			}
			tv, ok := t.pass.TypesInfo.Types[sel.X]
			return ok && isNamed(deref(tv.Type), "log/slog", "Logger")
		},
		args: func(t *taint, call *ast.CallExpr) []ast.Expr {
			var out []ast.Expr
			for i := 2; i < len(call.Args); i += 2 {
				out = append(out, call.Args[i])
			}
			return out
		},
	},
	{
		name: "http.Header.Set/Add",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			if sel.Sel.Name != "Set" && sel.Sel.Name != "Add" {
				return false
			}
			s, ok := t.pass.TypesInfo.Selections[sel]
			if !ok {
				return false
			}
			if !isNamed(deref(s.Recv()), "net/http", "Header") {
				return false
			}
			// A header map provenance-typed to an OUTBOUND request is
			// not a sink: the client transport rejects control bytes
			// at write time. See outboundHeaderMap.
			return !t.outboundHeaderMap(sel.X)
		},
		args: valueArg1,
	},
	{
		name: "std log print",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch sel.Sel.Name {
			case "Print", "Printf", "Println":
			default:
				return false
			}
			// Package-level log.Print* (default logger → stderr), or
			// the same methods on a *log.Logger receiver.
			if pkgPathOf(t.pass, sel) == "log" {
				return true
			}
			tv, ok := t.pass.TypesInfo.Types[sel.X]
			return ok && isNamed(deref(tv.Type), "log", "Logger")
		},
		args: func(t *taint, call *ast.CallExpr) []ast.Expr { return call.Args },
	},
	{
		name: "stdout/stderr print",
		matches: func(t *taint, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch qualifiedFunc(t.pass, sel) {
			case "fmt.Print", "fmt.Printf", "fmt.Println":
				// No writer argument: these write to os.Stdout
				// outright, which is the terminal sink.
				return true
			case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln":
			default:
				return false
			}
			if len(call.Args) == 0 {
				return false
			}
			// os.Stdout is itself a selector on the os package; a
			// local variable or parameter that merely shares the name
			// is not the terminal sink this rule means.
			wsel, ok := unparen(call.Args[0]).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			if wsel.Sel.Name != "Stdout" && wsel.Sel.Name != "Stderr" {
				return false
			}
			return pkgPathOf(t.pass, wsel) == "os"
		},
		// The F forms carry the writer at offset 0; the bare Print
		// forms check every argument, format string included.
		args: func(t *taint, call *ast.CallExpr) []ast.Expr {
			if sel, ok := unparen(call.Fun).(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "F") {
				return call.Args[1:]
			}
			return call.Args
		},
	},
}

func valueArg1(t *taint, call *ast.CallExpr) []ast.Expr {
	if len(call.Args) >= 2 {
		return call.Args[1:2]
	}
	return nil
}

// ---- taint -------------------------------------------------------------

type taint struct {
	pass  *analysis.Pass
	decls map[types.Object]*ast.FuncDecl
	// bind maps each variable assigned in this function to the
	// expression(s) assigned to it. A variable rewritten mid-function
	// keeps every binding: which write precedes a given sink is not
	// recoverable without real dataflow, and tainting every write is
	// the conservative direction for a bug-finding rule.
	bind map[types.Object][]ast.Expr
}

// bindable: string-carrying variables are taint carriers. A
// *http.Request or *http.Cookie variable is bound only so provenance
// walks can see through the local (the outbound-header split, the
// cookie .Value source); the variable itself is never a source.
func bindable(t types.Type) bool {
	if carriesStrings(t) {
		return true
	}
	n, ok := deref(t).(*types.Named)
	if !ok {
		return false
	}
	return isNamed(n, "net/http", "Request") || isNamed(n, "net/http", "Cookie")
}

func newTaint(pass *analysis.Pass, decls map[types.Object]*ast.FuncDecl, body *ast.BlockStmt, parent *taint) *taint {
	t := &taint{pass: pass, decls: decls, bind: map[types.Object][]ast.Expr{}}
	if parent != nil {
		// A closure inherits the enclosing function's bindings for
		// the variables it captures; its own assignments are added on
		// top below.
		for obj, exprs := range parent.bind {
			t.bind[obj] = append([]ast.Expr(nil), exprs...)
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			bindAssign(t, pass, n)
			return true
		case *ast.RangeStmt:
			// for k, v := range x declares its variables through the
			// statement, not an assignment: key and value are exactly
			// as derived as the ranged expression, whichever spelling
			// splits the request-derived value.
			for _, e := range []ast.Expr{n.Key, n.Value} {
				id := rangeIdent(e)
				if id == nil {
					continue
				}
				obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
				if !ok || !carriesStrings(obj.Type()) {
					continue
				}
				t.bind[obj] = append(t.bind[obj], n.X)
			}
			return true
		case *ast.FuncLit:
			// FuncLits are visited by checkBody separately, and their
			// locals must not bind in the enclosing function's map.
			return false
		}
		return true
	})
	return t
}

func bindAssign(t *taint, pass *analysis.Pass, assign *ast.AssignStmt) {
	if len(assign.Lhs) == len(assign.Rhs) {
		for i, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			// ObjectOf, not Defs: a plain `x = rhs` assignment
			// has no Defs entry — the lhs is a use of the object
			// declared elsewhere, and those rebindings are
			// exactly the append/join chains this rule follows.
			obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
			if !ok || !bindable(obj.Type()) {
				// Control bytes live in string data. A
				// non-string variable — most importantly err,
				// but also a fetch response struct that merely
				// received a tainted URL — is not a taint
				// carrier: the wrapper rule is for strings that
				// COME OUT of calls, not values that come back
				// from doing I/O with them.
				continue
			}
			t.bind[obj] = append(t.bind[obj], assign.Rhs[i])
		}
		return
	}
	// v, ok := m[k] / user, pass, _ := r.BasicAuth() / c, err :=
	// r.Cookie(...) — bind every value slot of a multi-result
	// assignment to the single call on the right.
	if len(assign.Lhs) > 1 && len(assign.Rhs) == 1 {
		for _, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
			if !ok || !bindable(obj.Type()) {
				continue
			}
			t.bind[obj] = append(t.bind[obj], assign.Rhs[0])
		}
	}
}

// rangeIdent unwraps a range key/value identifier, nil for _ and for
// non-identifier expressions.
func rangeIdent(e ast.Expr) *ast.Ident {
	if e == nil {
		return nil
	}
	id, ok := unparen(e).(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil
	}
	return id
}

// source reports whether e carries a request-derived value, following
// local bindings, wrapper calls, concatenations, and slice/map element
// reads within the function.
func (t *taint) source(e ast.Expr) bool {
	return t.orig(e, map[types.Object]bool{}, 0)
}

func (t *taint) orig(e ast.Expr, seen map[types.Object]bool, depth int) bool {
	if depth > 24 {
		return false
	}
	switch e := unparen(e).(type) {
	case *ast.Ident:
		obj := t.pass.TypesInfo.ObjectOf(e)
		if obj == nil || seen[obj] {
			return false
		}
		seen[obj] = true
		for _, b := range t.bind[obj] {
			if t.orig(b, seen, depth+1) {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		if isRequestSourceCall(t.pass, e) {
			return true
		}
		if t.callCleared(e, seen, depth) {
			return false
		}
		for _, arg := range e.Args {
			if t.orig(arg, seen, depth+1) {
				return true
			}
		}
		return false
	case *ast.BinaryExpr:
		return t.orig(e.X, seen, depth+1) || t.orig(e.Y, seen, depth+1)
	case *ast.SelectorExpr:
		if isRequestSelector(t.pass, e) {
			return true
		}
		if e.Sel.Name == "Value" && t.boundToCookie(e.X) {
			// c.Value where c was assigned from r.Cookie(...): the
			// cookie's Value is request bytes.
			return true
		}
		return t.orig(e.X, seen, depth+1)
	case *ast.CompositeLit:
		// A map or slice literal built from tainted values carries
		// them onward ({"xff": r.Header.Get(...)} wraps the same
		// bytes).
		for _, elt := range e.Elts {
			if t.orig(elt, seen, depth+1) {
				return true
			}
		}
		return false
	case *ast.KeyValueExpr:
		return t.orig(e.Value, seen, depth+1)
	case *ast.IndexExpr:
		return t.orig(e.X, seen, depth+1)
	default:
		return false
	}
}

// callCleared reports whether the call visibly cleanses its arguments:
// a scrub-named callee, or a same-package function whose tainted string
// parameters it byte-indexes (the markdownAlternate fix shape: a helper
// that walks the value as bytes is doing byte-level filtering, and a
// pass-through helper never indexes).
func (t *taint) callCleared(call *ast.CallExpr, seen map[types.Object]bool, depth int) bool {
	var fn types.Object
	switch fun := unparen(call.Fun).(type) {
	case *ast.Ident:
		fn = t.pass.TypesInfo.ObjectOf(fun)
	case *ast.SelectorExpr:
		if sel, ok := t.pass.TypesInfo.Selections[fun]; ok {
			fn = sel.Obj()
		} else if scrubTrusted(fun.Sel.Name) && !isStdlibNormalizer(t.pass, fun) {
			return true
		}
	default:
		return false
	}
	trusted := fn != nil && scrubTrusted(fn.Name())
	if fn == nil || trusted {
		return trusted
	}
	decl, ok := t.decls[fn]
	if !ok || decl.Body == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Variadic() && call.Ellipsis.IsValid() {
		return false
	}
	params := sig.Params()
	cleared, any := true, false
	for i, arg := range call.Args {
		if !t.orig(arg, seen, depth+1) {
			continue
		}
		any = true
		if i >= params.Len() {
			cleared = false
			break
		}
		p := params.At(i)
		if !isString(p.Type()) || !(indexesBytes(decl, t.pass, p) || returnsScrubOf(decl, t.pass, p)) {
			cleared = false
			break
		}
	}
	return any && cleared
}

// returnsScrubOf reports whether fn's every return statement hands the
// parameter straight to a scrub-named call — the one-line wrapper
// spelling (core/middleware's safeLogPath/safeLogMethod are exactly
// `return scrubControlBytes(p)`). The wrapper carries the scrub; only
// its name does not say so.
func returnsScrubOf(fn *ast.FuncDecl, pass *analysis.Pass, p *types.Var) bool {
	found, returns := 0, 0
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		returns++
		for _, res := range ret.Results {
			call, ok := unparen(res).(*ast.CallExpr)
			if !ok {
				continue
			}
			var name string
			switch fun := unparen(call.Fun).(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				if isStdlibNormalizer(pass, fun) {
					// path.Clean(p) is not a scrub; a wrapper around
					// it carries no scrub either.
					continue
				}
				name = fun.Sel.Name
			}
			if !scrubTrusted(name) {
				continue
			}
			for _, a := range call.Args {
				if id, ok := unparen(a).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == p {
					found++
					return true
				}
			}
		}
		return true
	})
	return returns > 0 && found == returns
}

// indexesBytes reports whether fn's body reads individual bytes of the
// parameter p: p[i] somewhere, or `for ... range p`.
func indexesBytes(fn *ast.FuncDecl, pass *analysis.Pass, p *types.Var) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.IndexExpr:
			if id, ok := n.X.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == p {
				found = true
			}
		case *ast.RangeStmt:
			if id, ok := n.X.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == p {
				found = true
			}
		}
		return !found
	})
	return found
}

// ---- source expressions ------------------------------------------------

// isRequestSelector: r.Method, r.Host, r.RemoteAddr, r.RequestURI,
// r.URL.Path, r.URL.RawQuery on an *http.Request (or an http.Request
// value).
func isRequestSelector(pass *analysis.Pass, e *ast.SelectorExpr) bool {
	switch e.Sel.Name {
	case "Method", "Host", "RemoteAddr", "RequestURI":
		return isRequestTyped(pass, e.X)
	case "Path", "RawQuery":
		inner, ok := e.X.(*ast.SelectorExpr)
		if !ok || inner.Sel.Name != "URL" {
			return false
		}
		tv, ok := pass.TypesInfo.Types[inner]
		if !ok || !isNamed(deref(tv.Type), "net/url", "URL") {
			return false
		}
		return isRequestTyped(pass, inner.X)
	}
	return false
}

// isRequestSourceCall: the request accessors — r.Header.Get,
// r.FormValue, r.PathValue, r.PostFormValue, r.Referer, r.UserAgent,
// r.BasicAuth (the wrapper accessors are r.Header.Get in disguise) —
// and .Get on a url.Values receiver (r.URL.Query().Get is covered by
// type: a Values is practically always the parsed query). A Get on a
// bare http.Header is a source only when that header is a request's
// (r.Header); w.Header().Get reads back what the SERVER wrote, which
// this rule does not treat as request-derived.
func isRequestSourceCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "FormValue", "PathValue", "PostFormValue", "Referer", "UserAgent", "BasicAuth":
		return isRequestTyped(pass, sel.X)
	case "Get":
		tv, ok := pass.TypesInfo.Types[sel.X]
		if !ok {
			return false
		}
		t := deref(tv.Type)
		if isNamed(t, "net/url", "Values") {
			return true
		}
		if !isNamed(t, "net/http", "Header") {
			return false
		}
		hdr, ok := unparen(sel.X).(*ast.SelectorExpr)
		return ok && hdr.Sel.Name == "Header" && isRequestTyped(pass, hdr.X)
	}
	return false
}

func isRequestTyped(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok {
		return false
	}
	return isNamed(deref(tv.Type), "net/http", "Request")
}

// isRequestCookieCall: r.Cookie(name) — the *http.Cookie it returns
// carries request bytes in its Value field.
func isRequestCookieCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Cookie" && isRequestTyped(pass, sel.X)
}

// boundToCookie: e is a variable whose binding came from an
// r.Cookie(...) call.
func (t *taint) boundToCookie(e ast.Expr) bool {
	id, ok := unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	obj, ok := t.pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok {
		return false
	}
	for _, b := range t.bind[obj] {
		if call, ok := unparen(b).(*ast.CallExpr); ok && isRequestCookieCall(t.pass, call) {
			return true
		}
	}
	return false
}

// outboundHeaderMap reports whether e provenance-traces to the header
// map of an OUTBOUND *http.Request — one built by
// http.NewRequest/NewRequestWithContext, or otherwise held in a value
// that is not the inbound handler parameter. The client transport
// validates header bytes at write time and rejects control bytes
// (net/http: invalid header field value for ...), so those maps are
// not forgery sinks; the response writer's map, and the inbound
// parameter's, are.
func (t *taint) outboundHeaderMap(e ast.Expr) bool {
	var hdr *ast.SelectorExpr
	if s, ok := unparen(e).(*ast.SelectorExpr); ok && s.Sel.Name == "Header" {
		hdr = s
	} else if id, ok := unparen(e).(*ast.Ident); ok {
		if obj, ok := t.pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
			for _, b := range t.bind[obj] {
				if s, ok := unparen(b).(*ast.SelectorExpr); ok && s.Sel.Name == "Header" {
					hdr = s
					break
				}
			}
		}
	}
	if hdr == nil {
		return false
	}
	return t.requestNotParam(hdr.X, map[types.Object]bool{})
}

// requestNotParam: e is a *http.Request-typed value that is not the
// inbound (parameter) request. A parameter-rooted request — directly
// or through locals bound from it — is inbound; anything else (a
// NewRequest result, a stored field, a clone) is outbound.
func (t *taint) requestNotParam(e ast.Expr, seen map[types.Object]bool) bool {
	if !requestTypedExpr(t.pass, e) {
		return false
	}
	id, ok := unparen(e).(*ast.Ident)
	if !ok {
		return true
	}
	obj, ok := t.pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || obj.Kind() == types.ParamVar {
		return false
	}
	if seen[obj] {
		return false
	}
	seen[obj] = true
	for _, b := range t.bind[obj] {
		if t.requestNotParam(b, seen) {
			return true
		}
	}
	return false
}

// requestTypedExpr: e is *http.Request-typed, tolerating a multi-result
// call whose first result is the request (out, err :=
// http.NewRequest(...)) — the call expression itself carries the tuple
// type, not the request.
func requestTypedExpr(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok {
		return false
	}
	t := tv.Type
	if tup, ok := t.(*types.Tuple); ok && tup.Len() > 0 {
		t = tup.At(0).Type()
	}
	return isNamed(deref(t), "net/http", "Request")
}

// ---- small helpers -----------------------------------------------------

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func deref(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// isNamed reports whether t is the named type pkgPath.name.
func isNamed(t types.Type, pkgPath, name string) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

// carriesStrings: string, []string, map[string]string — the shapes
// that can carry control bytes onward. Structs, interfaces, errors and
// byte slices are not tracked.
func carriesStrings(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Slice:
		return carriesStrings(u.Elem())
	case *types.Map:
		return carriesStrings(u.Elem())
	}
	return false
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	name := pass.Fset.Position(f.Pos()).Filename
	return len(name) >= 8 && name[len(name)-8:] == "_test.go"
}

// qualifiedFunc renders a selector as "pkg.Func", resolving the import
// through the type checker (same contract as mapwriter's).
func qualifiedFunc(pass *analysis.Pass, sel *ast.SelectorExpr) string {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkg.Imported().Name() + "." + sel.Sel.Name
}

// pkgPathOf renders the imported package path behind a selector's X,
// "" when X is not a package identifier.
func pkgPathOf(pass *analysis.Pass, sel *ast.SelectorExpr) string {
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkg.Imported().Path()
}
