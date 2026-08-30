// Package discardmutator catches security-state mutations whose result
// is discarded right where the handler acknowledges success. The shape:
// `_ = store.Delete(x)` — or a bare `store.Delete(x)` statement, or
// `_, _ =` — where the method is Delete, Revoke, Mark* (MarkRevoked,
// MarkUsed, ...), Burn, Purge, Invalidate, Expire, or Reset, the
// receiver expression text looks like state
// ((?i)store|session|token|verifier|manager|registry|cache), and later
// in the same function's statement order a WriteHeader, Write, or
// http.Redirect lands on a receiver that resolves to
// net/http.ResponseWriter with a 2xx/3xx constant (Write implies 200).
// Real instance: battery/auth/core.go:354 (logout) discarded
// c.mgr.SessionStore().Delete(...) and then wrote 303 or 204 — a failed
// delete answered "logged out" while the session survived server-side.
//
// Lane: vettool (type-aware), not the pattern lane. Two identifications
// need types.Info: (1) the write receiver must RESOLVE to
// net/http.ResponseWriter — a parameter named w can be a *bytes.Buffer
// or a log writer, and a real ResponseWriter can be named rw or resp,
// so name matching both over- and under-matches; (2) the bare-statement
// form only discards something when the callee's signature returns a
// value, which only the type graph knows. Import aliases and wrapper
// types defeat string rules; that bypass class is the lane
// justification (cf. fmtformat).
//
// Sanctioned postures that stay silent:
//   - discard followed by return, by an error write (http.Error, a
//     helper, WriteHeader with a 4xx/5xx constant), or by no write;
//   - receivers that are not state-shaped: _ = logger.Warn(...),
//     _ = fmt.Fprintf(w, ...) (package selector), _ = counter.Reset();
//   - mutator-named methods on non-state receivers (builder.Reset);
//   - void mutators called bare (nothing is discarded);
//   - non-constant status codes (success cannot be proven);
//   - janitors/background sweeps with no ResponseWriter in scope.
//
// Order is over-approximated: a diagnostic fires when a success write
// appears anywhere after the discard in the same function body —
// branches, loops, and sibling blocks included; the write may sit in a
// a branch the discard never reaches. Writes inside nested closures and
// in other functions do not count (intra-procedural), and a write
// before the discard does not count. One diagnostic per discard site.
package discardmutator

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

const Doc = "report discarded security-state mutations (store Delete/Revoke/Mark*/Burn/Purge/Invalidate/Expire/Reset) followed by a success response"

var Analyzer = &analysis.Analyzer{
	Name: "discardmutator",
	Doc:  Doc,
	Run:  run,
}

// receiverRe matches the receiver expression text: the store-ish
// objects whose mutation is security state.
var receiverRe = regexp.MustCompile(`(?i)(store|session|token|verifier|manager|registry|cache)`)

func run(pass *analysis.Pass) (any, error) {
	rw := responseWriterIface(pass)
	var bodies []*ast.BlockStmt
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					bodies = append(bodies, fn.Body)
				}
			case *ast.FuncLit:
				bodies = append(bodies, fn.Body)
			}
			return true
		})
	}
	for _, b := range bodies {
		scanBody(pass, b, rw)
	}
	return nil, nil
}

type discardSite struct {
	pos  token.Pos
	what string
}

// scanBody reports discards in one function body (FuncDecl or FuncLit)
// that are followed by a success write in position order.
func scanBody(pass *analysis.Pass, body *ast.BlockStmt, rw *types.Interface) {
	var discards []discardSite
	var writes []token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false // nested closure: separate function scope
		}
		switch st := n.(type) {
		case *ast.AssignStmt:
			if !allBlank(st) {
				return true
			}
			for _, rhs := range st.Rhs {
				if what, ok := mutatorCall(pass, rhs); ok {
					discards = append(discards, discardSite{pos: st.Pos(), what: what})
				}
			}
		case *ast.ExprStmt:
			call, ok := st.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			if what, ok := mutatorCall(pass, st.X); ok && callReturnsValue(pass, call) {
				discards = append(discards, discardSite{pos: st.Pos(), what: what})
			}
		case *ast.CallExpr:
			if successWrite(pass, st, rw) {
				writes = append(writes, st.Pos())
			}
		}
		return true
	})
	for _, d := range discards {
		for _, w := range writes {
			if w > d.pos {
				pass.Reportf(d.pos, "discardmutator: discarded result of %s is followed by a success response; a failed mutation would be acknowledged as success", d.what)
				break
			}
		}
	}
}

// allBlank reports whether st assigns every result to `_`.
func allBlank(st *ast.AssignStmt) bool {
	if len(st.Lhs) == 0 {
		return false
	}
	for _, lhs := range st.Lhs {
		id, ok := lhs.(*ast.Ident)
		if !ok || id.Name != "_" {
			return false
		}
	}
	return true
}

// mutatorCall reports whether e is x.M(...) with M a security-state
// mutator and the receiver expression text store-shaped; what is
// "recv.M".
func mutatorCall(pass *analysis.Pass, e ast.Expr) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	if !isMutator(sel.Sel.Name) {
		return "", false
	}
	text := types.ExprString(sel.X)
	if !receiverRe.MatchString(text) {
		return "", false
	}
	return text + "." + sel.Sel.Name, true
}

func isMutator(name string) bool {
	switch name {
	case "Delete", "Revoke", "Burn", "Purge", "Invalidate", "Expire", "Reset":
		return true
	}
	return strings.HasPrefix(name, "Mark")
}

// callReturnsValue reports whether the bare statement form discards
// anything: only a call with results drops a value.
func callReturnsValue(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn := callee(pass, call)
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Results().Len() > 0
}

// callee resolves the *types.Func called by call, package function or
// method (interface methods included).
func callee(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		if fn, ok := pass.TypesInfo.Uses[fun].(*types.Func); ok {
			return fn
		}
	case *ast.SelectorExpr:
		if fn, ok := pass.TypesInfo.Uses[fun.Sel].(*types.Func); ok {
			return fn
		}
	}
	return nil
}

// successWrite reports whether call writes a success response: a
// WriteHeader or Write method on a net/http.ResponseWriter receiver,
// or http.Redirect with a success status onto one.
func successWrite(pass *analysis.Pass, call *ast.CallExpr, rw *types.Interface) bool {
	if fn := callee(pass, call); fn != nil && fn.Pkg() != nil &&
		fn.Pkg().Path() == "net/http" && fn.Name() == "Redirect" {
		if len(call.Args) == 4 && implementsRW(pass.TypesInfo.TypeOf(call.Args[0]), rw) {
			return successCode(pass, call.Args[3])
		}
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !implementsRW(pass.TypesInfo.TypeOf(sel.X), rw) {
		return false
	}
	switch sel.Sel.Name {
	case "WriteHeader":
		return len(call.Args) == 1 && successCode(pass, call.Args[0])
	case "Write":
		return len(call.Args) == 1
	}
	return false
}

// successCode reports whether e is a constant status in [200, 399];
// 3xx included because post-mutation redirects acknowledge success.
func successCode(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return false
	}
	v, ok := constant.Int64Val(tv.Value)
	return ok && v >= 200 && v <= 399
}

// responseWriterIface resolves net/http.ResponseWriter through the
// analyzed package's type information, aliases included.
func responseWriterIface(pass *analysis.Pass) *types.Interface {
	for _, imp := range pass.Pkg.Imports() {
		if imp.Path() != "net/http" {
			continue
		}
		tn, ok := imp.Scope().Lookup("ResponseWriter").(*types.TypeName)
		if !ok {
			continue
		}
		if it, ok := tn.Type().Underlying().(*types.Interface); ok {
			return it
		}
	}
	return nil
}

func implementsRW(t types.Type, rw *types.Interface) bool {
	if t == nil || rw == nil {
		return false
	}
	if types.Implements(t, rw) {
		return true
	}
	if _, ok := t.(*types.Interface); ok {
		return false
	}
	return types.Implements(types.NewPointer(t), rw)
}
