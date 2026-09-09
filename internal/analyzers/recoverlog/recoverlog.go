// Package recoverlog catches a recover() value reaching a log sink
// without a scrub.
//
// The bug class is the controlbytes one from the panic side: a handler
// that panicked on request-derived data hands those bytes — CRLF, ESC,
// NUL, the C1 controls, the bidi set — to the panic value, and the
// recover-and-log spelling prints them raw into the operator tail. A
// raw CRLF forges a log line in every line-oriented consumer; a raw
// ESC or 8-bit CSI paints attacker bytes across the terminal. The
// 2026-09-07 red-probe round pinned ten sites, all the same shape:
//
//   - battery/auth audit.go emitSecurity, core.go
//     deliverRegisterDuplicateNotice (fmt.Sprint of the value),
//   - battery/semantic watcher.go safeMetadata,
//   - core/a2a exec.go invoke (the server's own logger),
//   - core/fanout subscriber_queue.go deliver (slog.Any),
//   - core/handler handler.go HandlerAdapter (truncateLog only — a
//     truncate is a length cap, not a scrub),
//   - core/mcp server.go runCallGate and checkServerGate,
//   - core/middleware metrics.go runCollectorSafely (truncate only),
//   - core/stream websocket.go Close's close-hook guard.
//
// controlbytes is the request-derived twin and deliberately stays
// quiet on the in-function recover-and-log; this rule is the additive
// arm for the recover() source and must not double-report with it.
// Where controlbytes does speak — a recover() value landing in a
// same-package struct field that a reporter seam later logs
// (battery/log's ErrorReport → SlogErrorReporter) — this rule stays
// quiet: the struct handoff is the carrying-struct seam, not a log
// sink, and the in-package recovery middleware scrubs before the
// literal anyway.
//
// Shape: the result of recover() — directly, through a local assigned
// from it, through fmt.Sprint/Sprintln/Sprintf rendering it (%v/%s; a
// %q format pre-escapes and is quiet), or through any
// string/error/any-returning wrapper the value is piped into
// (truncate, errors.New, fmt.Errorf) — reaches a LOG SINK in the same
// function, or is passed to a same-package helper whose body sinks
// that parameter, with no scrub in between. Log sinks are the ones
// controlbytes lists: package-level slog.* and *slog.Logger methods
// (message and values, slog.Any/slog.String), slog.Log, the std log
// package (package-level or on a *log.Logger), and fmt.Print* /
// fmt.Fprint* to os.Stdout/os.Stderr.
//
// A value counts as scrubbed when it passes through a call whose name
// says scrub/sanitiz/strip/Recovered (core/textsafe.Recovered is the
// fix spelling; core/middleware's scrubControlBytes is the older one)
// before the sink. A truncate-named helper never clears: it caps the
// length and passes every control byte.
//
// Silent postures, deliberately:
//   - the fix spellings — scrubControlBytes/StripUnsafe/Recovered in
//     front of the sink, the RecoveryFn and Timeout late-panic shape
//     truncate(scrubControlBytes(fmt.Sprint(v)), …) and battery/log's
//     recoveryMiddleware ErrorReport literal;
//   - a recover() value that reaches no log sink — returned as an
//     error (a2a's errHandlerPanicked), written into a response, or
//     stored: only the log sink is this rule's bug;
//   - taint does not cross package boundaries: a foreign helper
//     logging what it was handed is that package's business; the one
//     same-package helper hop is the whole interprocedural
//     concession;
//   - calls whose result cannot carry the bytes onward (bool, int,
//     a struct, a buffer): only string/error/any results propagate;
//   - a %q format: the verb pre-escapes control bytes, which is
//     exactly the hiding the scrub would do;
//   - _test.go files.
package recoverlog

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "recoverlog",
	Doc:  "report recover() values reaching log sinks without a control-byte scrub",
	Run:  run,
}

// scrubName matches the callee names the repo treats as a scrub for a
// recovered value. core/textsafe.Recovered is the fix spelling;
// scrubControlBytes and StripUnsafe are the in-repo older spellings.
var scrubName = regexp.MustCompile(`(?i)scrub|sanitiz|strip|recovered`)

func run(pass *analysis.Pass) (any, error) {
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

	a := &analyzer{pass: pass, decls: decls, helperMemo: map[*ast.FuncDecl][]bool{}}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			// A test's panic-assertion log line fails nothing that
			// ships.
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				a.checkFunc(d)
			case *ast.GenDecl:
				// Top-level func literals (var h = func() {...}).
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, val := range vs.Values {
						if lit, ok := val.(*ast.FuncLit); ok && lit.Body != nil {
							a.checkBody(lit.Body)
						}
					}
				}
			}
		}
	}
	return nil, nil
}

type analyzer struct {
	pass  *analysis.Pass
	decls map[types.Object]*ast.FuncDecl
	// helperMemo caches, per function declaration, whether each
	// parameter position reaches a log sink raw in that function's
	// own body.
	helperMemo map[*ast.FuncDecl][]bool
}

// checkFunc runs the recover-taint analysis over one function
// declaration.
func (a *analyzer) checkFunc(fn *ast.FuncDecl) {
	a.checkBody(fn.Body)
}

func (a *analyzer) checkBody(body *ast.BlockStmt) {
	t := a.newTaint(body, nil)
	a.walk(t, body)
}

// walk reports sinks in body. Nested function literals are visited in
// place, not separately: the dominant shape is `defer func(){ if r :=
// recover(); ... }()`, and the deferred literal's bindings must resolve
// in the enclosing function's map.
func (a *analyzer) walk(t *taint, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// The one-hop helper arm: a tainted argument handed to a
		// same-package function whose own body sinks that parameter
		// raw.
		if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
			if obj, ok := a.pass.TypesInfo.ObjectOf(id).(*types.Func); ok {
				if decl, ok := a.decls[obj]; ok {
					flags := a.helperParamFlags(decl)
					for i, arg := range call.Args {
						if i < len(flags) && flags[i] && t.tainted(arg) {
							a.pass.Reportf(call.Pos(),
								"recoverlog: recover() value passed to %s reaches its log sink unscrubbed; the panicked-on bytes forge log lines (scrub at the sink: textsafe.Recovered)",
								id.Name)
							break
						}
					}
				}
			}
		}
		for _, s := range sinks {
			if !s.matches(a, call) {
				continue
			}
			for _, arg := range s.args(call) {
				if t.tainted(arg) {
					a.pass.Reportf(call.Pos(),
						"recoverlog: recover() value reaches %s unscrubbed; the panicked-on bytes forge log lines in the operator tail (scrub at the sink: textsafe.Recovered)",
						s.name)
					break
				}
			}
		}
		return true
	})
}

// helperParamFlags reports, per parameter position of fn, whether that
// parameter reaches a log sink in fn's own body with no scrub in
// between (the one-hop helper arm's credit table).
func (a *analyzer) helperParamFlags(fn *ast.FuncDecl) []bool {
	if flags, ok := a.helperMemo[fn]; ok {
		return flags
	}
	var names []*ast.Ident
	for _, field := range fn.Type.Params.List {
		names = append(names, field.Names...)
	}
	flags := make([]bool, len(names))
	for i, name := range names {
		obj, ok := a.pass.TypesInfo.Defs[name].(*types.Var)
		if !ok {
			continue
		}
		// Seed exactly this parameter as a taint root; a recover()
		// inside the helper itself is the helper's own in-function
		// finding, checked by checkFunc.
		t := a.newTaint(fn.Body, map[*types.Var]bool{obj: true})
		flags[i] = a.bodySinksRaw(t, fn.Body)
	}
	a.helperMemo[fn] = flags
	return flags
}

// bodySinksRaw reports whether any sink in body receives a tainted
// value under taint state t.
func (a *analyzer) bodySinksRaw(t *taint, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, s := range sinks {
			if !s.matches(a, call) {
				continue
			}
			for _, arg := range s.args(call) {
				if t.tainted(arg) {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

// ---- taint ---------------------------------------------------------------

// taint is the per-function taint state: every local's bindings (the
// deferred-recover closure shape included) plus the helper-mode seed
// parameters. recover() is always a root.
type taint struct {
	a    *analyzer
	bind map[types.Object][]ast.Expr
	// seeds: helper-mode parameter roots (nil in the main pass).
	seeds map[*types.Var]bool
}

func (a *analyzer) newTaint(body *ast.BlockStmt, seeds map[*types.Var]bool) *taint {
	t := &taint{a: a, bind: map[types.Object][]ast.Expr{}, seeds: seeds}
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(assign.Lhs) == len(assign.Rhs) {
			for i, lhs := range assign.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				obj, ok := a.pass.TypesInfo.ObjectOf(id).(*types.Var)
				if !ok {
					continue
				}
				t.bind[obj] = append(t.bind[obj], assign.Rhs[i])
			}
			return true
		}
		// v, ok := m[k] / x, err := call(): bind every value slot to
		// the single right-hand expression.
		if len(assign.Lhs) > 1 && len(assign.Rhs) == 1 {
			for _, lhs := range assign.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				obj, ok := a.pass.TypesInfo.ObjectOf(id).(*types.Var)
				if !ok {
					continue
				}
				t.bind[obj] = append(t.bind[obj], assign.Rhs[0])
			}
		}
		return true
	})
	return t
}

// tainted reports whether e carries a recover()-derived value.
func (t *taint) tainted(e ast.Expr) bool {
	return t.orig(e, map[types.Object]bool{}, 0)
}

func (t *taint) orig(e ast.Expr, seen map[types.Object]bool, depth int) bool {
	if depth > 24 {
		return false
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj := t.a.pass.TypesInfo.ObjectOf(e)
		if obj == nil || seen[obj] {
			return false
		}
		if v, ok := obj.(*types.Var); ok && t.seeds[v] {
			return true
		}
		seen[obj] = true
		for _, b := range t.bind[obj] {
			if t.orig(b, seen, depth+1) {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		if isRecoverCall(t.a.pass, e) {
			return true
		}
		if scrubNamed(e.Fun) {
			return false
		}
		// fmt.Sprintf's %q verb pre-escapes control bytes: the format
		// itself is the scrub.
		if t.a.qualifiedFunc(e.Fun) == "fmt.Sprintf" && len(e.Args) > 0 {
			if lit, ok := e.Args[0].(*ast.BasicLit); ok && strings.Contains(lit.Value, "%q") {
				return false
			}
		}
		// A wrapper the value is piped through (truncate, errors.New,
		// fmt.Errorf, fmt.Sprint, a same-package renderer) carries
		// the bytes onward when its result can hold them.
		if t.a.callCarries(e) {
			for _, arg := range e.Args {
				if t.orig(arg, seen, depth+1) {
					return true
				}
			}
		}
		return false
	case *ast.SelectorExpr:
		return t.orig(e.X, seen, depth+1)
	case *ast.BinaryExpr:
		return t.orig(e.X, seen, depth+1) || t.orig(e.Y, seen, depth+1)
	case *ast.TypeAssertExpr:
		return t.orig(e.X, seen, depth+1)
	case *ast.CompositeLit:
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
	case *ast.UnaryExpr:
		return t.orig(e.X, seen, depth+1)
	default:
		return false
	}
}

// callCarries reports whether the call's result type can carry the
// recovered bytes onward: string, error, any interface, or a pointer
// to one of those. Boolean and numeric results, structs, and buffers
// are not carriers (a value read back out of a buffer is a fresh
// derivation this rule does not follow).
func (a *analyzer) callCarries(call *ast.CallExpr) bool {
	tv, ok := a.pass.TypesInfo.Types[call]
	if !ok {
		return false
	}
	return valueCarrying(tv.Type)
}

func valueCarrying(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Kind() == types.String
	case *types.Interface:
		// error and any included.
		return true
	case *types.Pointer:
		switch e := u.Elem().Underlying().(type) {
		case *types.Basic:
			return e.Kind() == types.String
		case *types.Interface:
			return true
		}
		return false
	default:
		return false
	}
}

// scrubNamed reports whether the callee's name says scrub.
func scrubNamed(fun ast.Expr) bool {
	var name string
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name
	default:
		return false
	}
	return scrubName.MatchString(name)
}

// isRecoverCall: the builtin recover().
func isRecoverCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "recover" {
		return false
	}
	_, isBuiltin := pass.TypesInfo.Uses[id].(*types.Builtin)
	return isBuiltin
}

// ---- sinks ---------------------------------------------------------------

type sink struct {
	name    string
	matches func(a *analyzer, call *ast.CallExpr) bool
	args    func(call *ast.CallExpr) []ast.Expr
}

var sinks = []sink{
	{
		name: "slog.String/slog.Any",
		matches: func(a *analyzer, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch a.qualifiedFunc(sel) {
			case "slog.String", "slog.Any":
				return true
			}
			return false
		},
		args: valueArg1,
	},
	{
		// slog.Debug/... on a *slog.Logger receiver — slog.Default()
		// and injected loggers alike — and package-level slog.*, which
		// writes to the default logger (stderr).
		name: "slog.Debug/Info/Warn/Error key-value",
		matches: func(a *analyzer, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch sel.Sel.Name {
			case "Debug", "Info", "Warn", "Error",
				"DebugContext", "InfoContext", "WarnContext", "ErrorContext":
			default:
				return false
			}
			if a.pkgPathOf(sel) == "log/slog" {
				return true
			}
			tv, ok := a.pass.TypesInfo.Types[sel.X]
			if !ok {
				return false
			}
			return astx.IsNamed(astx.Deref(tv.Type), "log/slog", "Logger")
		},
		// msg, k1, v1: message and values at even offsets from 0; the
		// *Context forms shift one for ctx.
		args: astx.EvenOffsetArgs,
	},
	{
		// Log(ctx, level, msg, k1, v1): message and values at even
		// offsets from 2.
		name: "slog.Log key-value",
		matches: func(a *analyzer, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Log" {
				return false
			}
			if a.pkgPathOf(sel) == "log/slog" {
				return true
			}
			tv, ok := a.pass.TypesInfo.Types[sel.X]
			return ok && astx.IsNamed(astx.Deref(tv.Type), "log/slog", "Logger")
		},
		args: func(call *ast.CallExpr) []ast.Expr {
			var out []ast.Expr
			for i := 2; i < len(call.Args); i += 2 {
				out = append(out, call.Args[i])
			}
			return out
		},
	},
	{
		name: "std log print",
		matches: func(a *analyzer, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
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
			if a.pkgPathOf(sel) == "log" {
				return true
			}
			tv, ok := a.pass.TypesInfo.Types[sel.X]
			return ok && astx.IsNamed(astx.Deref(tv.Type), "log", "Logger")
		},
		args: func(call *ast.CallExpr) []ast.Expr { return call.Args },
	},
	{
		name: "stdout/stderr print",
		matches: func(a *analyzer, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch a.qualifiedFunc(sel) {
			case "fmt.Print", "fmt.Printf", "fmt.Println":
				// No writer argument: these write to os.Stdout
				// outright.
				return true
			case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln":
			default:
				return false
			}
			if len(call.Args) == 0 {
				return false
			}
			wsel, ok := ast.Unparen(call.Args[0]).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			if wsel.Sel.Name != "Stdout" && wsel.Sel.Name != "Stderr" {
				return false
			}
			return a.pkgPathOf(wsel) == "os"
		},
		// The F forms carry the writer at offset 0; the bare Print
		// forms check every argument, format string included.
		args: func(call *ast.CallExpr) []ast.Expr {
			if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "F") {
				return call.Args[1:]
			}
			return call.Args
		},
	},
}

func valueArg1(call *ast.CallExpr) []ast.Expr {
	if len(call.Args) >= 2 {
		return call.Args[1:2]
	}
	return nil
}

// ---- small helpers -------------------------------------------------------

// qualifiedFunc renders a selector callee as "pkg.Func", resolving the
// import through the type checker; a bare identifier callee renders as
// its own name.
func (a *analyzer) qualifiedFunc(fun ast.Expr) string {
	sel, ok := ast.Unparen(fun).(*ast.SelectorExpr)
	if !ok {
		if id, ok := fun.(*ast.Ident); ok {
			return id.Name
		}
		return ""
	}
	if pkg := a.pkgNameOf(sel); pkg != "" {
		return pkg + "." + sel.Sel.Name
	}
	return sel.Sel.Name
}

// pkgNameOf renders the imported package NAME behind a selector's X
// (slog, not log/slog — the spelling a call site writes).
func (a *analyzer) pkgNameOf(sel *ast.SelectorExpr) string {
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkgName, ok := a.pass.TypesInfo.Uses[x].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkgName.Imported().Name()
}

// pkgPathOf renders the imported package path behind a selector's X,
// "" when X is not a package identifier.
func (a *analyzer) pkgPathOf(sel *ast.SelectorExpr) string {
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	obj := a.pass.TypesInfo.Uses[x]
	if obj == nil {
		return ""
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return ""
	}
	return pkgName.Imported().Path()
}
