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
// A value counts as scrubbed when it passes through a callee whose name
// says so (scrub/sanitize/clean/escape/quote/redact — r.URL.EscapedPath
// and url.QueryEscape qualify) or through a same-package helper that
// inspects the value byte by byte (the byte-filter loop the uihost fix
// shipped inside markdownAlternate; a pass-through helper like
// TrimRight or truncate never looks at individual bytes and does not
// clear taint).
//
// Postures it deliberately stays silent on, because they are not this
// bug: JSON and HTML encoders escape structurally (encoding/json,
// html/template), so encoder arguments are left alone; the response
// BODY is not a sink — it is the response; span NAMES (tracer.Start,
// span.SetName) and log keys are left alone, only VALUES are checked;
// fmt.Sprint* without a writer, and Fprint* to any writer other than
// os.Stdout/os.Stderr (an http.ResponseWriter or a bytes.Buffer has its
// own framing); taint does not cross function boundaries — a
// request-derived argument to a helper is the helper's business, and
// the byte-indexing form above is the whole interprocedural
// concession; and structured values like a whole *http.Request or an
// ErrorReport struct are not sources, only the string-bearing request
// selectors are.
package controlbytes

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"

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

func walkStmt(pass *analysis.Pass, decls map[types.Object]*ast.FuncDecl, t *taint, guards map[types.Object]token.Pos, stmt ast.Stmt) {
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			checkBody(pass, decls, n.Body, t)
			return false
		case *ast.CallExpr:
			for _, s := range sinks {
				if !s.matches(pass, n) {
					continue
				}
				for _, arg := range s.args(n) {
					if id, ok := unparen(arg).(*ast.Ident); ok {
						if at, tested := guards[pass.TypesInfo.ObjectOf(id)]; tested && at < n.Pos() {
							// The value already passed a validator or
							// allowlist on its way here; the guard is
							// the sanitizer (requestid's validRequestID,
							// CORS's originSet[origin]).
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

// testedValues records where each variable was TESTED before use: an
// argument of a validator-named call (validRequestID(id),
// isSafePartialRedirect(u)) or the key of a membership lookup
// (originSet[origin], allowed[origin]). A value the code has already
// vetted cannot smuggle control bytes past that vetting, so a sink
// after the test is clean for that variable.
func testedValues(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]token.Pos {
	out := map[types.Object]token.Pos{}
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
						recordMin(out, obj, n.Pos())
					}
				}
			}
		case *ast.IndexExpr:
			if id, ok := n.Index.(*ast.Ident); ok {
				if t := pass.TypesInfo.TypeOf(n.X); t != nil {
					if _, ok := t.Underlying().(*types.Map); ok {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							recordMin(out, obj, n.Pos())
						}
					}
				}
			}
		}
		return true
	})
	return out
}

func recordMin(out map[types.Object]token.Pos, obj types.Object, pos token.Pos) {
	if prev, ok := out[obj]; !ok || pos < prev {
		out[obj] = pos
	}
}

// ---- sinks -------------------------------------------------------------

type sink struct {
	name    string
	args    func(call *ast.CallExpr) []ast.Expr
	matches func(pass *analysis.Pass, call *ast.CallExpr) bool
}

var sinks = []sink{
	{
		name: "slog.String/slog.Any",
		matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch qualifiedFunc(pass, sel) {
			case "slog.String", "slog.Any":
				return true
			}
			return false
		},
		args: valueArg1,
	},
	{
		name: "attribute.String",
		matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			return qualifiedFunc(pass, sel) == "attribute.String"
		},
		args: valueArg1,
	},
	{
		name: "logger.Info/Warn/Error key-value",
		matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch sel.Sel.Name {
			case "Info", "Warn", "Error":
			default:
				return false
			}
			tv, ok := pass.TypesInfo.Types[sel.X]
			if !ok {
				return false
			}
			return isNamed(deref(tv.Type), "log/slog", "Logger")
		},
		// Info(msg, k1, v1, k2, v2, ...): values sit at odd offsets of
		// the variadic tail.
		args: func(call *ast.CallExpr) []ast.Expr {
			var out []ast.Expr
			for i := 2; i < len(call.Args); i += 2 {
				out = append(out, call.Args[i])
			}
			return out
		},
	},
	{
		name: "http.Header.Set/Add",
		matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			if sel.Sel.Name != "Set" && sel.Sel.Name != "Add" {
				return false
			}
			s, ok := pass.TypesInfo.Selections[sel]
			if !ok {
				return false
			}
			return isNamed(deref(s.Recv()), "net/http", "Header")
		},
		args: valueArg1,
	},
	{
		name: "stdout/stderr print",
		matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch qualifiedFunc(pass, sel) {
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
			pkgID, ok := wsel.X.(*ast.Ident)
			if !ok {
				return false
			}
			pkg, ok := pass.TypesInfo.Uses[pkgID].(*types.PkgName)
			return ok && pkg.Imported().Path() == "os"
		},
		args: func(call *ast.CallExpr) []ast.Expr { return call.Args[1:] },
	},
}

func valueArg1(call *ast.CallExpr) []ast.Expr {
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
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			// FuncLits fall through here: their bodies are visited by
			// checkBody separately, and their locals must not bind in
			// the enclosing function's map.
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			return true
		}
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
				if !ok || !carriesStrings(obj.Type()) {
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
			return true
		}
		// v, ok := m[k] / v, ok := x.Field — bind the value slot.
		if len(assign.Lhs) == 2 && len(assign.Rhs) == 1 {
			if id, ok := assign.Lhs[0].(*ast.Ident); ok {
				if obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok && carriesStrings(obj.Type()) {
					t.bind[obj] = append(t.bind[obj], assign.Rhs[0])
				}
			}
		}
		return true
	})
	return t
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
		return t.orig(e.X, seen, depth+1)
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
		} else if scrubName.MatchString(fun.Sel.Name) {
			return true
		}
	default:
		return false
	}
	if fn == nil || scrubName.MatchString(fn.Name()) {
		return fn != nil && scrubName.MatchString(fn.Name())
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
				name = fun.Sel.Name
			}
			if !scrubName.MatchString(name) {
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

// isRequestSelector: r.Method, r.Host, r.RemoteAddr, r.URL.Path,
// r.URL.RawQuery on an *http.Request (or an http.Request value).
func isRequestSelector(pass *analysis.Pass, e *ast.SelectorExpr) bool {
	switch e.Sel.Name {
	case "Method", "Host", "RemoteAddr":
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

// isRequestSourceCall: r.Header.Get, r.FormValue, r.PathValue, and .Get
// on a url.Values receiver (r.URL.Query().Get is covered by type: a
// Values is practically always the parsed query). A Get on a bare
// http.Header is a source only when that header is a request's
// (r.Header); w.Header().Get reads back what the SERVER wrote, which
// this rule does not treat as request-derived.
func isRequestSourceCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "FormValue", "PathValue":
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
