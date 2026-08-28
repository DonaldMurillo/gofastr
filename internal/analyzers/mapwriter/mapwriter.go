// Package mapwriter catches nondeterministic SSR output at its source:
// ranging over a Go map while writing into an output builder emits
// attributes/markup in a different order every render. The class has
// bitten real code (chart SVG attribute appends in PR #261's review);
// the fix is always the same — iterate slices.Sorted(maps.Keys(m)),
// which ranges a slice and never trips this check.
package mapwriter

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrmapwriter",
	Doc:  "forbids ranging over a map while writing to a Builder/Buffer/Writer (nondeterministic SSR output); iterate slices.Sorted(maps.Keys(m)) instead",
	Run:  run,
}

// writeMethods are the sink methods whose call inside a map-range body
// makes iteration order observable in output.
var writeMethods = map[string]bool{
	"WriteString": true, "WriteByte": true, "WriteRune": true, "Write": true,
}

// writeFuncs are package-level sinks with a writer first argument.
var writeFuncs = map[string]bool{
	"fmt.Fprintf": true, "fmt.Fprintln": true, "fmt.Fprint": true,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		filename := pass.Fset.Position(f.Pos()).Filename
		if strings.HasSuffix(filename, "_test.go") {
			continue
		}
		guarded := singleEntryGuards(pass, f)
		ast.Inspect(f, func(n ast.Node) bool {
			rng, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			tv, ok := pass.TypesInfo.Types[rng.X]
			if !ok {
				return true
			}
			if _, isMap := tv.Type.Underlying().(*types.Map); !isMap {
				return true
			}
			// A range inside `if len(m) == 1 { ... }` over the same m
			// iterates once: order cannot be observed.
			for _, g := range guarded {
				if rng.Pos() >= g.from && rng.Pos() <= g.to && types.ExprString(rng.X) == g.expr {
					return true
				}
			}
			ast.Inspect(rng.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if writeMethods[sel.Sel.Name] && isWriterish(pass, sel.X) {
					pass.Reportf(call.Pos(),
						"writing to output while ranging a map: iteration order is random per render; iterate slices.Sorted(maps.Keys(m)) instead")
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); ok && writeFuncs[pkg.Name+"."+sel.Sel.Name] {
					pass.Reportf(call.Pos(),
						"writing to output while ranging a map: iteration order is random per render; iterate slices.Sorted(maps.Keys(m)) instead")
				}
				return true
			})
			return true
		})
	}
	return nil, nil
}

// guardRange is the span of an `if len(m) == 1` body plus the guarded
// expression's printed form.
type guardRange struct {
	from, to token.Pos
	expr     string
}

// singleEntryGuards collects the bodies of `if len(m) == 1 { ... }`
// statements: a map range inside one iterates exactly once, so
// iteration order is unobservable there.
func singleEntryGuards(pass *analysis.Pass, f *ast.File) []guardRange {
	var out []guardRange
	ast.Inspect(f, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		bin, ok := ifStmt.Cond.(*ast.BinaryExpr)
		if !ok || bin.Op != token.EQL {
			return true
		}
		lenCall, lit := bin.X, bin.Y
		if _, ok := lit.(*ast.BasicLit); !ok {
			lenCall, lit = bin.Y, bin.X
		}
		basic, ok := lit.(*ast.BasicLit)
		if !ok || basic.Value != "1" {
			return true
		}
		call, ok := lenCall.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return true
		}
		if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "len" {
			return true
		}
		out = append(out, guardRange{
			from: ifStmt.Body.Pos(),
			to:   ifStmt.Body.End(),
			expr: types.ExprString(call.Args[0]),
		})
		return true
	})
	_ = pass
	return out
}

// isWriterish reports whether the receiver is a strings.Builder,
// bytes.Buffer, or something implementing io.Writer — the sinks where
// ordered output matters. Plain map/slice mutation inside a range is
// fine (order-insensitive) and stays unflagged.
func isWriterish(pass *analysis.Pass, recv ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[recv]
	if !ok {
		return false
	}
	t := tv.Type
	for {
		ptr, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = ptr.Elem()
	}
	name := t.String()
	return strings.HasSuffix(name, "strings.Builder") ||
		strings.HasSuffix(name, "bytes.Buffer") ||
		implementsWriter(t) || implementsWriter(types.NewPointer(t))
}

var writerIface = func() *types.Interface {
	// io.Writer built by hand so the analyzer needs no import lookups.
	sig := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(0, nil, "p", types.NewSlice(types.Typ[types.Byte]))),
		types.NewTuple(
			types.NewVar(0, nil, "n", types.Typ[types.Int]),
			types.NewVar(0, nil, "err", types.Universe.Lookup("error").Type()),
		), false)
	m := types.NewFunc(0, nil, "Write", sig)
	return types.NewInterfaceType([]*types.Func{m}, nil).Complete()
}()

func implementsWriter(t types.Type) bool {
	return types.Implements(t, writerIface)
}
