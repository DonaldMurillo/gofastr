// Package logsink names the log-sink call shapes the controlbytes and
// recoverlog tables share: which calls carry a value toward the
// operator tail, and which of their arguments carry it. Each analyzer
// keeps its own taint, guard and scrub rules; this package only
// matches sinks. Where the two tables had drifted, this is the union:
// the terminal stream through a local bound to it (controlbytes'
// spelling) and WriteString/Write on os.Stdout/os.Stderr themselves
// (recoverlog's) both count.
package logsink

import (
	"go/ast"
	"go/types"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

// Shape is one sink call shape: its name for diagnostics, which calls
// match, and which arguments carry the logged value.
type Shape struct {
	Name    string
	Matches func(pass *analysis.Pass, call *ast.CallExpr) bool
	Args    func(call *ast.CallExpr) []ast.Expr
}

// SlogLevels names the shared slog level-call shape.
const SlogLevels = "slog.Debug/Info/Warn/Error key-value"

// Shapes are the log sinks both analyzers list: slog.String/slog.Any
// values, the key-value logger levels on a receiver or package-level,
// slog.Log, the std log package (Print*, and the Fatal* and Panic*
// families, which print the message before they exit or panic), fmt
// print to os.Stdout/os.Stderr, and the streams' own WriteString/Write.
var Shapes = []Shape{
	{
		Name: "slog.String/slog.Any",
		Matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch astx.PkgFuncName(pass, sel) {
			case "slog.String", "slog.Any":
				return true
			}
			return false
		},
		Args: ValueArg1,
	},
	{
		// slog.Debug/... on a *slog.Logger receiver — slog.Default()
		// and injected loggers alike — and package-level slog.*, which
		// writes to the default logger (stderr).
		Name: SlogLevels,
		Matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
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
			if astx.PkgPathOf(pass, sel) == "log/slog" {
				return true
			}
			tv, ok := pass.TypesInfo.Types[sel.X]
			if !ok {
				return false
			}
			return astx.IsNamed(astx.Deref(tv.Type), "log/slog", "Logger")
		},
		// Warn(msg, k1, v1, k2, v2): the message and the values sit at
		// even offsets — msg at 0 for the plain form, 1 for *Context.
		Args: astx.EvenOffsetArgs,
	},
	{
		// Log(ctx, level, msg, k1, v1) on a receiver or package-level:
		// message and values at even offsets from 2.
		Name: "slog.Log key-value",
		Matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Log" {
				return false
			}
			if astx.PkgPathOf(pass, sel) == "log/slog" {
				return true
			}
			tv, ok := pass.TypesInfo.Types[sel.X]
			return ok && astx.IsNamed(astx.Deref(tv.Type), "log/slog", "Logger")
		},
		Args: func(call *ast.CallExpr) []ast.Expr {
			var out []ast.Expr
			for i := 2; i < len(call.Args); i += 2 {
				out = append(out, call.Args[i])
			}
			return out
		},
	},
	{
		Name: "std log print",
		Matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			switch sel.Sel.Name {
			case "Print", "Printf", "Println",
				// Fatal* and Panic* print the same message through the
				// logger before they exit or panic.
				"Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln":
			default:
				return false
			}
			// Package-level log.Print* (default logger → stderr), or
			// the same methods on a *log.Logger receiver.
			if astx.PkgPathOf(pass, sel) == "log" {
				return true
			}
			tv, ok := pass.TypesInfo.Types[sel.X]
			return ok && astx.IsNamed(astx.Deref(tv.Type), "log", "Logger")
		},
		Args: func(call *ast.CallExpr) []ast.Expr { return call.Args },
	},
	{
		Name: "stdout/stderr print",
		Matches: func(pass *analysis.Pass, call *ast.CallExpr) bool {
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok {
				return false
			}
			// os.Stderr.WriteString(msg) / .Write([]byte(msg)): the
			// stream written directly, without fmt.
			if sel.Sel.Name == "WriteString" || sel.Sel.Name == "Write" {
				if wsel, ok := ast.Unparen(sel.X).(*ast.SelectorExpr); ok &&
					(wsel.Sel.Name == "Stdout" || wsel.Sel.Name == "Stderr") && astx.PkgPathOf(pass, wsel) == "os" {
					return true
				}
			}
			switch astx.PkgFuncName(pass, sel) {
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
			// local bound to it (every binding) is the same stream,
			// while a parameter that merely shares the name is not
			// the terminal sink this rule means.
			if wsel, ok := ast.Unparen(call.Args[0]).(*ast.SelectorExpr); ok {
				if wsel.Sel.Name != "Stdout" && wsel.Sel.Name != "Stderr" {
					return false
				}
				return astx.PkgPathOf(pass, wsel) == "os"
			}
			if id, ok := ast.Unparen(call.Args[0]).(*ast.Ident); ok {
				return stdStreamLocals(pass)[pass.TypesInfo.ObjectOf(id)]
			}
			return false
		},
		// The F forms carry the writer at offset 0; the bare Print
		// forms check every argument, format string included.
		Args: func(call *ast.CallExpr) []ast.Expr {
			if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "F") {
				return call.Args[1:]
			}
			return call.Args
		},
	},
}

// ValueArg1 returns the value argument of a two-argument key-value
// call (offset 1).
func ValueArg1(call *ast.CallExpr) []ast.Expr {
	if len(call.Args) >= 2 {
		return call.Args[1:2]
	}
	return nil
}

// stdLocalsMemo caches stdStreamLocals per pass.
var stdLocalsMemo = struct {
	sync.Mutex
	byPass map[*analysis.Pass]map[types.Object]bool
}{byPass: map[*analysis.Pass]map[types.Object]bool{}}

// stdStreamLocals maps each local EVERY binding of which is os.Stdout
// or os.Stderr: fmt.Fprintf(w, …) through such a local is the terminal
// sink. A local re-bound to some other writer is not, and a parameter
// that merely shares the name never was.
func stdStreamLocals(pass *analysis.Pass) map[types.Object]bool {
	stdLocalsMemo.Lock()
	defer stdLocalsMemo.Unlock()
	if m, ok := stdLocalsMemo.byPass[pass]; ok {
		return m
	}
	out := map[types.Object]bool{}
	nonStd := map[types.Object]bool{}
	isStdStream := func(e ast.Expr) bool {
		sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Stdout" && sel.Sel.Name != "Stderr") {
			return false
		}
		return astx.PkgPathOf(pass, sel) == "os"
	}
	bind := func(id *ast.Ident, rhs ast.Expr) {
		obj := pass.TypesInfo.ObjectOf(id)
		if obj == nil {
			return
		}
		if isStdStream(rhs) {
			out[obj] = true
		} else {
			nonStd[obj] = true
		}
	}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				return true
			case *ast.AssignStmt:
				for i, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok && i < len(x.Rhs) {
						bind(id, x.Rhs[i])
					}
				}
			case *ast.ValueSpec:
				for i, name := range x.Names {
					if i < len(x.Values) {
						bind(name, x.Values[i])
					}
				}
			}
			return true
		})
	}
	for obj := range nonStd {
		delete(out, obj)
	}
	stdLocalsMemo.byPass[pass] = out
	return out
}
