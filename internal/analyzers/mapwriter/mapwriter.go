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
	Name: "mapwriter",
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
		bound := boundExprs(pass, f)
		ast.Inspect(f, func(n ast.Node) bool {
			rng, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			if !rangesInMapOrder(pass, rng.X, bound, 0) {
				return true
			}
			// A range inside `if len(m) == 1 { ... }` over the same m
			// iterates once: order cannot be observed. "Same m" is the
			// same VARIABLE, not the same spelling: a shadowing
			// declaration inside the guard keeps the text identical
			// while the ranged map is a different, unbounded one.
			for _, g := range guarded {
				if rng.Pos() >= g.from && rng.Pos() <= g.to && g.matches(pass, rng.X) {
					return true
				}
			}
			ast.Inspect(rng.Body, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				if isOutputSink(pass, call.Fun, bound) {
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

// rangesInMapOrder reports whether ranging x visits entries in map order.
//
// A map does, obviously. So does maps.Keys(m) / maps.Values(m): those
// return an iterator that walks the map itself, and the prescribed fix is
// slices.Sorted(maps.Keys(m)) — dropping the slices.Sorted leaves the
// remediation looking applied while changing nothing. Ranging the result
// of slices.Sorted, or any other slice, is ordered and fine.
//
// The iterator does not stop walking the map when it reaches the range
// statement through an intermediate value: bound to a variable first
// (`keys := maps.Keys(m); for k := range keys`), or collected into a
// slice without sorting (`slices.Collect(maps.Keys(m))`). Both resolve
// back to the map-ordered source here.
func rangesInMapOrder(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr, depth int) bool {
	if depth > 8 {
		return false
	}
	if tv, ok := pass.TypesInfo.Types[x]; ok {
		if _, isMap := tv.Type.Underlying().(*types.Map); isMap {
			return true
		}
	}
	switch e := x.(type) {
	case *ast.ParenExpr:
		return rangesInMapOrder(pass, e.X, bound, depth+1)
	case *ast.Ident:
		if src, ok := bound[pass.TypesInfo.ObjectOf(e)]; ok {
			return rangesInMapOrder(pass, src, bound, depth+1)
		}
		return false
	case *ast.CallExpr:
		switch qualifiedCallee(pass, e.Fun) {
		case "maps.Keys", "maps.Values", "maps.All":
			return true
		case "slices.Collect", "slices.Values", "slices.All":
			// Collecting or re-iterating an unsorted map iterator keeps
			// map order; only slices.Sorted (and friends) impose one.
			if len(e.Args) == 1 {
				return rangesInMapOrder(pass, e.Args[0], bound, depth+1)
			}
		}
	}
	return false
}

// isOutputSink reports whether calling fun writes to an output sink:
// a write method on a writer-ish receiver, a package-level formatting
// sink, or either of those reached without selector syntax — bound to
// a variable before the loop (`write := sb.WriteString`,
// `fprintf := fmt.Fprintf`) or dot-imported.
func isOutputSink(pass *analysis.Pass, fun ast.Expr, bound map[types.Object]ast.Expr) bool {
	switch f := fun.(type) {
	case *ast.ParenExpr:
		return isOutputSink(pass, f.X, bound)
	case *ast.SelectorExpr:
		if writeMethods[f.Sel.Name] && isWriterish(pass, f.X) {
			return true
		}
		return writeFuncs[qualifiedFunc(pass, f)]
	case *ast.Ident:
		switch obj := pass.TypesInfo.Uses[f].(type) {
		case *types.Func:
			// Dot-imported package function.
			if obj.Pkg() != nil && writeFuncs[obj.Pkg().Name()+"."+obj.Name()] {
				return true
			}
		case *types.Var:
			if src, ok := bound[obj]; ok {
				return isOutputSink(pass, src, bound)
			}
		}
	}
	return false
}

// qualifiedCallee is qualifiedFunc for any callee expression, so a
// dot-imported or bound package function resolves the same way.
func qualifiedCallee(pass *analysis.Pass, fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return qualifiedFunc(pass, f)
	case *ast.Ident:
		if fn, ok := pass.TypesInfo.Uses[f].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Name() + "." + fn.Name()
		}
	}
	return ""
}

// boundExprs maps each local variable defined by a single-value
// `x := expr` / `var x = expr` / `x = expr` to that expr, so a sink or a
// range source hidden behind a variable can be resolved to its origin.
// Only the last binding in source order is kept; the analyzer is a
// gate, not a dataflow engine, and a variable rebound between the
// binding and the loop is the rare case it accepts missing.
func boundExprs(pass *analysis.Pass, f *ast.File) map[types.Object]ast.Expr {
	out := map[types.Object]ast.Expr{}
	bind := func(lhs ast.Expr, rhs ast.Expr) {
		id, ok := lhs.(*ast.Ident)
		if !ok {
			return
		}
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			out[obj] = rhs
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) == len(st.Rhs) {
				for i := range st.Lhs {
					bind(st.Lhs[i], st.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			if len(st.Names) == len(st.Values) {
				for i := range st.Names {
					bind(st.Names[i], st.Values[i])
				}
			}
		}
		return true
	})
	return out
}

// qualifiedFunc renders a selector as "pkg.Func", resolving the import
// through the type checker. Matching on the identifier text instead lets
// `import f "fmt"` walk past every sink in writeFuncs.
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

// guardRange is the span of an `if len(m) == 1` body plus the guarded
// expression: the variable it names when it is a plain identifier, or
// its printed form otherwise.
type guardRange struct {
	from, to token.Pos
	obj      types.Object
	expr     string
}

// matches reports whether x is the guarded expression: the same
// variable when the guard named one (so a shadowing declaration inside
// the guard does not inherit the exemption), else the same spelling.
func (g guardRange) matches(pass *analysis.Pass, x ast.Expr) bool {
	if g.obj != nil {
		id, ok := x.(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(id) == g.obj
	}
	return types.ExprString(x) == g.expr
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
		g := guardRange{
			from: ifStmt.Body.Pos(),
			to:   ifStmt.Body.End(),
			expr: types.ExprString(call.Args[0]),
		}
		if id, ok := call.Args[0].(*ast.Ident); ok {
			g.obj = pass.TypesInfo.ObjectOf(id)
		}
		out = append(out, g)
		return true
	})
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
		implementsWriter(t) || implementsWriter(types.NewPointer(t)) ||
		hasStringWriter(t) || hasStringWriter(types.NewPointer(t))
}

// hasStringWriter reports whether t has a WriteString(string) method:
// an output sink in everything but method set, which the io.Writer
// probe alone would miss.
func hasStringWriter(t types.Type) bool {
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "WriteString")
	fn, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params().Len() < 1 {
		return false
	}
	basic, ok := sig.Params().At(0).Type().Underlying().(*types.Basic)
	return ok && basic.Kind() == types.String
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
