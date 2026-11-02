// Package readsrc finds the reader a whole-body read consumes. It is
// the walk unboundedbody (an inbound request body) and unboundedresp
// (an *http.Response body) share: each asks whether io.ReadAll,
// json.NewDecoder and the rest read a Body to EOF, and a read can name
// that Body through a local (body := r.Body) or a pass-through wrapper
// (bufio.NewReader(r.Body)). The analyzers keep their own sinks, their
// own test for which Body counts and their own postures; this package
// only resolves the reader.
package readsrc

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
)

// SingleBindings maps each local bound exactly once in f, by a
// single-value assignment or declaration, to its value: the alias a read
// may name in place of a Body. A local bound more than once is left out,
// since which binding the read sees is flow-dependent.
func SingleBindings(pass *analysis.Pass, f *ast.File) map[types.Object]ast.Expr {
	out := map[types.Object]ast.Expr{}
	count := map[types.Object]int{}
	bind := func(id *ast.Ident, v ast.Expr) {
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil && id.Name != "_" {
			count[obj]++
			out[obj] = v
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) == len(st.Rhs) {
				for i, lhs := range st.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						bind(id, st.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			if len(st.Names) == len(st.Values) {
				for i, id := range st.Names {
					bind(id, st.Values[i])
				}
			}
		}
		return true
	})
	for obj, n := range count {
		if n > 1 {
			delete(out, obj)
		}
	}
	return out
}

// Source sees through what does not bound a read: parentheses, a local
// in aliases (SingleBindings), and the pass-through wrappers
// bufio.NewReader / NewReaderSize and io.TeeReader, each of which reads
// its source to EOF on demand. A limiter is not unwrapped: a read
// through io.LimitReader is bounded, so it is what Source returns.
func Source(pass *analysis.Pass, e ast.Expr, aliases map[types.Object]ast.Expr) ast.Expr {
	for range 8 {
		e = ast.Unparen(e)
		switch x := e.(type) {
		case *ast.Ident:
			b, ok := aliases[pass.TypesInfo.ObjectOf(x)]
			if !ok {
				return e
			}
			e = b
		case *ast.CallExpr:
			switch astx.PkgFuncName(pass, x.Fun) {
			case "bufio.NewReader", "bufio.NewReaderSize", "io.TeeReader":
				if len(x.Args) == 0 {
					return e
				}
				e = x.Args[0]
			default:
				return e
			}
		default:
			return e
		}
	}
	return e
}
