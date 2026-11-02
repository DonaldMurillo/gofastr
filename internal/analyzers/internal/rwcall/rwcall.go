// Package rwcall answers one question for the response-lifecycle
// analyzers (recoverwrite, doublewrite): does this call write an HTTP
// response, and to which writer? It decides nothing about when a write
// is wrong; the analyzers pick that.
//
// A call writes a response when it is:
//   - x.WriteHeader(...) or x.Write(...) on a value x that is an
//     http.ResponseWriter (its method set holds Header() http.Header and
//     WriteHeader(int));
//   - http.Error, http.Redirect, http.NotFound, http.ServeContent,
//     http.ServeFile or http.ServeFileFS with x as the writer argument;
//   - fmt.Fprint/Fprintf/Fprintln, io.WriteString or io.Copy into x;
//   - json.NewEncoder(x).Encode(...);
//   - a call to a function that writes its own writer parameter (a
//     respond helper): the analyzers compute that set to a fixed point
//     inside the package with Writers and export it as a fact, so
//     handler.WriteError and i18n.HTTPError count across packages.
package rwcall

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

// IsWriter reports whether t is an http.ResponseWriter by its method set.
func IsWriter(t types.Type) bool {
	if t == nil {
		return false
	}
	ms := types.NewMethodSet(t)
	if _, isPtr := t.Underlying().(*types.Pointer); !isPtr {
		if _, isIface := t.Underlying().(*types.Interface); !isIface {
			ms = types.NewMethodSet(types.NewPointer(t))
		}
	}
	h := ms.Lookup(nil, "Header")
	wh := ms.Lookup(nil, "WriteHeader")
	if h == nil || wh == nil {
		return false
	}
	sig, ok := h.Obj().Type().(*types.Signature)
	if !ok || sig.Results().Len() != 1 || !astx.IsNamed(sig.Results().At(0).Type(), "net/http", "Header") {
		return false
	}
	sig, ok = wh.Obj().Type().(*types.Signature)
	return ok && sig.Params().Len() == 1
}

// writerFirst are the package functions whose first argument is the
// response writer they write.
var writerFirst = map[string]bool{
	"net/http.Error":        true,
	"net/http.Redirect":     true,
	"net/http.NotFound":     true,
	"net/http.ServeContent": true,
	"net/http.ServeFile":    true,
	"net/http.ServeFileFS":  true,
	"fmt.Fprint":            true,
	"fmt.Fprintf":           true,
	"fmt.Fprintln":          true,
	"io.WriteString":        true,
	"io.Copy":               true,
	"io.CopyN":              true,
}

// HelperIndex returns, for a callee, the argument indexes it writes as a
// response writer (from the analyzer's fact or local computation).
type HelperIndex func(fn *types.Func) []int

// Target returns the writer expression a call writes a response to, nil
// when the call writes none.
func Target(pass *analysis.Pass, call *ast.CallExpr, helpers HelperIndex) ast.Expr {
	fun := ast.Unparen(call.Fun)
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		switch sel.Sel.Name {
		case "WriteHeader", "Write":
			if _, isMethod := pass.TypesInfo.Selections[sel]; isMethod && IsWriter(pass.TypesInfo.TypeOf(sel.X)) {
				return sel.X
			}
		case "Encode":
			if inner, ok := ast.Unparen(sel.X).(*ast.CallExpr); ok && len(inner.Args) == 1 &&
				pathflow.QualifiedFunc(pass, ast.Unparen(inner.Fun)) == "encoding/json.NewEncoder" &&
				IsWriter(pass.TypesInfo.TypeOf(inner.Args[0])) {
				return inner.Args[0]
			}
		}
	}
	if writerFirst[pathflow.QualifiedFunc(pass, fun)] && len(call.Args) > 0 && IsWriter(pass.TypesInfo.TypeOf(call.Args[0])) {
		return call.Args[0]
	}
	if fn, ok := astx.CalleeFunc(pass, fun); ok && helpers != nil {
		for _, i := range helpers(fn.Origin()) {
			if i < len(call.Args) {
				return call.Args[i]
			}
		}
	}
	return nil
}

// Writers computes, to a fixed point over the package's function
// declarations, the writer-parameter indexes each function writes,
// seeded by imported facts through imported.
func Writers(pass *analysis.Pass, imported HelperIndex) map[*types.Func][]int {
	out := map[*types.Func][]int{}
	var decls []*ast.FuncDecl
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
			}
		}
	}
	lookup := func(fn *types.Func) []int {
		if idx, ok := out[fn]; ok {
			return idx
		}
		if imported != nil {
			return imported(fn)
		}
		return nil
	}
	for changed := true; changed; {
		changed = false
		for _, fd := range decls {
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			params := map[types.Object]int{}
			i := 0
			for _, field := range fd.Type.Params.List {
				names := field.Names
				if len(names) == 0 {
					i++
					continue
				}
				for _, name := range names {
					if obj := pass.TypesInfo.Defs[name]; obj != nil && IsWriter(obj.Type()) {
						params[obj] = i
					}
					i++
				}
			}
			if len(params) == 0 {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				t := Target(pass, call, lookup)
				id, ok := ast.Unparen(nonNil(t)).(*ast.Ident)
				if !ok {
					return true
				}
				idx, ok := params[pass.TypesInfo.Uses[id]]
				if !ok || slices.Contains(out[fn], idx) {
					return true
				}
				out[fn] = append(out[fn], idx)
				slices.Sort(out[fn])
				changed = true
				return true
			})
		}
	}
	return out
}

func nonNil(e ast.Expr) ast.Expr {
	if e == nil {
		return &ast.BadExpr{}
	}
	return e
}
