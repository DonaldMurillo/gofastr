// Package wrapwriter catches http.ResponseWriter wrappers that hide the
// connection from http.ResponseController, and middleware that changes
// response headers after the handler has already sent them.
//
// The bug, arm one (hidden writer): http.ResponseController reaches
// Flush, Hijack, SetReadDeadline, SetWriteDeadline and EnableFullDuplex
// by asserting the method on the writer it holds and, failing that, by
// calling `Unwrap() http.ResponseWriter` and trying again. A middleware
// type that wraps the writer to intercept Write or WriteHeader and has
// no Unwrap ends that walk: every ResponseController call behind it
// returns ErrNotSupported, which callers tend to discard. Found by the
// 2026-10-27 bug bash:
//   - P1: core/middleware/timeout.go:39 timeoutWriter has Flush and
//     Hijack but no Unwrap; core/stream/sse_broker.go:352 cuts an
//     evicted subscriber with rc.SetWriteDeadline, which fails through
//     it, and Timeout is in the default chain.
//   - P40: core/middleware/idempotency.go:412 idempotencyRecorder has
//     only WriteHeader and Write, so a streaming request carrying an
//     Idempotency-Key fails (core/a2a/server.go:877 refuses a writer
//     that is not a Flusher).
//   - B16: battery/cache/middleware.go:387 responseRecorder has no
//     Flush, Hijack or Unwrap, so SSE and websocket routes behind the
//     cache middleware fail.
//
// The fix spelling is the one every other core/middleware wrapper
// carries (cors, compress, logging, metrics, tracing):
// `func (w *T) Unwrap() http.ResponseWriter { return w.ResponseWriter }`,
// plus explicit Flush/Hijack where the wrapper must see them. A wrapper
// that buffers the whole response on purpose still needs a decision for
// streaming (pass through once the handler flushes, or bypass upgrades
// and event streams before wrapping); the rule reports it so that
// decision is made, it does not guess it was.
//
// Shape: a named struct type, outside _test.go files, with a field
// (embedded or named) whose type is an interface that implements
// http.ResponseWriter, that implements http.ResponseWriter itself (T
// or *T), that declares Write or WriteHeader on itself, and whose
// method set (the one that implements the interface) has no
// `Unwrap() http.ResponseWriter`, and whose values reach a handler in
// the same package: a T or *T is the writer argument of a dispatch
// (defined below), directly or through an interface variable it was
// assigned to. Reported at the type name.
//
// The bug, arm two (header after next): once the handler writes, the
// header map has been sent; Header().Set after next.ServeHTTP changes a
// map nobody reads. P42: core/middleware/tracing.go:90 runs
// prop.Inject(ctx, propagation.HeaderCarrier(ww.Header())) after
// next.ServeHTTP(ww, ...), so traceparent never reaches the client. The
// fix is to set the header before calling the handler, or in the
// wrapper's WriteHeader hook.
//
// Shape, within one function body: a call that dispatches to the next
// handler (a ServeHTTP(http.ResponseWriter, *http.Request) method, or
// a call through a variable of func(http.ResponseWriter, *http.Request)
// type such as `next(w, r)`) whose statement dominates, lexically, a
// mutation of the same writer's header: `<w>.Header().Set/Add/Del(...)`,
// an index assignment into `<w>.Header()`, or `<w>.Header()` converted
// to another type (propagation.HeaderCarrier(...), whose Set the
// propagator calls). "The
// same writer" is the variable passed to the handler, or the variable
// it wraps when the handler got a composite literal of a wrapper type
// that declares Unwrap (a pass-through says so). A header mutation in
// a closure deferred before the dispatch, in the same block, counts as
// after it.
//
// Silent postures, deliberately:
//   - a wrapper with Unwrap() http.ResponseWriter, on either receiver
//     of the implementing method set;
//   - a type that holds a writer but is not one (core/stream's
//     SSEWriter and ChunkedWriter, core/a2a's errWriter): nothing hands
//     it to a handler as a ResponseWriter;
//   - a field of a concrete type (logging's flushWriter embeds
//     *responseWriter, whose Unwrap is promoted);
//   - a type that declares neither Write nor WriteHeader: it intercepts
//     nothing;
//   - a wrapper handed only to declared functions, never to a handler
//     value (core/static's notFoundWriter goes to the package's own
//     serveFile, which never asks for the connection); and an exported
//     wrapper built only in other packages, which is a miss;
//   - header reads after the handler (Get, Values, Clone, a status
//     field): logging and metrics read what was sent; and the header
//     map passed as a plain call argument (the cache middleware's
//     isStoreable(rec, w.Header(), ...) reads it), so a copy helper
//     that writes into it is a miss;
//   - a header map bound to a local first (`h := w.Header()` then
//     `h.Set`): the rule follows the Header() call, not the map;
//   - a mutation on a different writer than the one the handler got
//     (the cache middleware hands the handler a recorder and sets
//     headers on the real writer before replaying the body), unless
//     the handler's writer is a declared pass-through of it;
//   - a dispatch inside a branch that does not dominate the mutation
//     (`if bypass { next.ServeHTTP(w, r); return }` then
//     `w.Header().Set(...)`): that header is set before the other
//     dispatch;
//   - a call to a declared function with the handler signature
//     (`writeJSON(w, r)`): the rule cannot tell a handler from a
//     helper, and only a handler value is the next link;
//   - _test.go files, for both arms: a test double never sits in a
//     production chain.
package wrapwriter

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name: "wrapwriter",
	Doc:  "report ResponseWriter wrappers with no Unwrap (ResponseController cannot reach the connection) and header mutations after the next handler ran",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	rw := responseWriterType(pass)
	if rw == nil {
		// The package never mentions net/http.
		return nil, nil
	}
	dispatched := dispatchedTypes(pass)
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		checkTypes(pass, f, rw, dispatched)
		for _, fn := range astx.AllFuncs(f) {
			checkBody(pass, astx.BodyOf(fn), rw)
		}
	}
	return nil, nil
}

// responseWriterType finds net/http.ResponseWriter among the package's
// imports, nil when net/http is not imported (directly or not).
func responseWriterType(pass *analysis.Pass) *types.Interface {
	var find func(p *types.Package, seen map[*types.Package]bool) *types.Interface
	find = func(p *types.Package, seen map[*types.Package]bool) *types.Interface {
		if seen[p] {
			return nil
		}
		seen[p] = true
		if p.Path() == "net/http" {
			if obj, ok := p.Scope().Lookup("ResponseWriter").(*types.TypeName); ok {
				if it, ok := obj.Type().Underlying().(*types.Interface); ok {
					return it
				}
			}
			return nil
		}
		for _, imp := range p.Imports() {
			if it := find(imp, seen); it != nil {
				return it
			}
		}
		return nil
	}
	return find(pass.Pkg, map[*types.Package]bool{})
}

// ---- arm one: wrappers with no Unwrap ---------------------------------

func checkTypes(pass *analysis.Pass, f *ast.File, rw *types.Interface, dispatched map[*types.Named]bool) {
	ast.Inspect(f, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		tn, ok := pass.TypesInfo.Defs[ts.Name].(*types.TypeName)
		if !ok || tn.IsAlias() {
			return true
		}
		named, ok := tn.Type().(*types.Named)
		if !ok {
			return true
		}
		st, ok := named.Underlying().(*types.Struct)
		if !ok || !wrapsWriter(st, rw) {
			return true
		}
		var mset *types.MethodSet
		switch {
		case types.Implements(named, rw):
			mset = types.NewMethodSet(named)
		case types.Implements(types.NewPointer(named), rw):
			mset = types.NewMethodSet(types.NewPointer(named))
		default:
			return true
		}
		if !declaresIntercept(named) || !dispatched[named] {
			return true
		}
		if hasUnwrap(mset) || hasUnwrap(types.NewMethodSet(types.NewPointer(named))) {
			return true
		}
		pass.Reportf(ts.Name.Pos(),
			"wrapwriter: %s wraps http.ResponseWriter and intercepts Write/WriteHeader with no Unwrap() http.ResponseWriter; http.ResponseController cannot reach Flush, Hijack or SetWriteDeadline through it, so SSE eviction, streaming and websocket upgrades behind it fail (add Unwrap, and decide streaming explicitly if it buffers)",
			ts.Name.Name)
		return true
	})
}

// dispatchedTypes collects the named types that reach a dispatch's
// writer argument anywhere in the package's non-test files: the
// argument's own static type (`next.ServeHTTP(rec, r)` with rec a
// *T), or, for an interface-typed variable, every T/*T value assigned
// to it (`w = &T{...}; next.ServeHTTP(w, r)`).
func dispatchedTypes(pass *analysis.Pass) map[*types.Named]bool {
	assigned := map[types.Object][]types.Type{}
	var args []ast.Expr
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if len(n.Lhs) == len(n.Rhs) {
					for i, lhs := range n.Lhs {
						if id, ok := lhs.(*ast.Ident); ok {
							if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
								assigned[obj] = append(assigned[obj], pass.TypesInfo.TypeOf(n.Rhs[i]))
							}
						}
					}
				}
			case *ast.ValueSpec:
				if len(n.Names) == len(n.Values) {
					for i, id := range n.Names {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							assigned[obj] = append(assigned[obj], pass.TypesInfo.TypeOf(n.Values[i]))
						}
					}
				}
			case *ast.CallExpr:
				if isDispatch(pass, n) {
					args = append(args, n.Args[0])
				}
			}
			return true
		})
	}
	out := map[*types.Named]bool{}
	add := func(t types.Type) {
		if t == nil {
			return
		}
		if n, ok := astx.Deref(t).(*types.Named); ok {
			out[n.Origin()] = true
		}
	}
	for _, a := range args {
		add(pass.TypesInfo.TypeOf(a))
		if id, ok := ast.Unparen(a).(*ast.Ident); ok {
			for _, t := range assigned[pass.TypesInfo.ObjectOf(id)] {
				add(t)
			}
		}
	}
	return out
}

// wrapsWriter: st has a field whose type is an interface implementing
// http.ResponseWriter.
func wrapsWriter(st *types.Struct, rw *types.Interface) bool {
	for i := range st.NumFields() {
		ft := st.Field(i).Type()
		if _, ok := ft.Underlying().(*types.Interface); ok && types.Implements(ft, rw) {
			return true
		}
	}
	return false
}

// declaresIntercept: Write or WriteHeader is declared on the type
// itself (either receiver), not promoted from an embedded field.
func declaresIntercept(named *types.Named) bool {
	for i := range named.NumMethods() {
		switch named.Method(i).Name() {
		case "Write", "WriteHeader":
			return true
		}
	}
	return false
}

// hasUnwrap: the method set holds Unwrap() http.ResponseWriter.
func hasUnwrap(mset *types.MethodSet) bool {
	for i := range mset.Len() {
		fn, ok := mset.At(i).Obj().(*types.Func)
		if !ok || fn.Name() != "Unwrap" {
			continue
		}
		sig := fn.Type().(*types.Signature)
		if sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
			astx.IsNamed(sig.Results().At(0).Type(), "net/http", "ResponseWriter") {
			return true
		}
	}
	return false
}

// ---- arm two: header mutation after the next handler -----------------

// dispatch is one call to the next handler.
type dispatch struct {
	call   *ast.CallExpr
	stmt   ast.Stmt
	writer types.Object // the variable passed as the writer
	alias  types.Object // the writer it passes through to, or nil
}

func checkBody(pass *analysis.Pass, body *ast.BlockStmt, rw *types.Interface) {
	if body == nil {
		return
	}
	parents := dominance.Parents(body)
	passThrough := passThroughBindings(pass, body)
	var dispatches []dispatch
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isDispatch(pass, call) {
			return true
		}
		id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
		if !ok {
			return true
		}
		obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok {
			return true
		}
		st, ok := parents[call].(ast.Stmt)
		if !ok {
			return true
		}
		dispatches = append(dispatches, dispatch{call: call, stmt: st, writer: obj, alias: passThrough[obj]})
		return true
	})
	if len(dispatches) == 0 {
		return
	}
	reported := map[token.Pos]bool{}
	report := func(at ast.Node) {
		if reported[at.Pos()] {
			return
		}
		reported[at.Pos()] = true
		pass.Reportf(at.Pos(),
			"wrapwriter: response header changed after the next handler ran; once the handler writes, the header is already sent and this change never reaches the client (set it before calling the handler, or in the wrapper's WriteHeader)")
	}
	// Mutations in the body proper, dominated by a dispatch statement.
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			lit, ok := ast.Unparen(n.Call.Fun).(*ast.FuncLit)
			if !ok {
				return true
			}
			// A deferred closure runs after every later dispatch in the
			// same block.
			for _, d := range dispatches {
				if parents[d.stmt] != parents[n] || d.stmt.Pos() < n.Pos() {
					continue
				}
				for _, m := range headerMutations(pass, lit.Body) {
					if matchesWriter(pass, m, d) {
						report(m)
					}
				}
			}
			return false
		case *ast.CallExpr:
			if !isHeaderCall(pass, n) || !mutates(pass, n, parents) {
				return true
			}
			for _, d := range dispatches {
				if matchesWriter(pass, n, d) && dominates(d.stmt, n, parents, body) {
					report(n)
				}
			}
		}
		return true
	})
}

// isDispatch: call is X.ServeHTTP(w, r) with the handler signature, or
// a call through a func-typed variable of that signature.
func isDispatch(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) != 2 {
		return false
	}
	fun := ast.Unparen(call.Fun)
	if sel, ok := fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ServeHTTP" {
		fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
		return ok && handlerSig(fn.Type())
	}
	var obj types.Object
	switch f := fun.(type) {
	case *ast.Ident:
		obj = pass.TypesInfo.Uses[f]
	case *ast.SelectorExpr:
		obj = pass.TypesInfo.Uses[f.Sel]
	}
	v, ok := obj.(*types.Var)
	return ok && handlerSig(v.Type())
}

// handlerSig: t is (or has underlying) func(http.ResponseWriter,
// *http.Request) with no results.
func handlerSig(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 2 || sig.Results().Len() != 0 {
		return false
	}
	p1 := sig.Params().At(1).Type()
	ptr, ok := p1.(*types.Pointer)
	return ok && astx.IsNamed(sig.Params().At(0).Type(), "net/http", "ResponseWriter") &&
		astx.IsNamed(ptr.Elem(), "net/http", "Request")
}

// passThroughBindings maps `ww := &T{...: w}` (or T{...}) to w when T
// declares Unwrap() http.ResponseWriter: a pass-through wrapper shares
// the wrapped writer's header map.
func passThroughBindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]types.Object {
	out := map[types.Object]types.Object{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, rhs := range as.Rhs {
			id, ok := as.Lhs[i].(*ast.Ident)
			if !ok {
				continue
			}
			lhs := pass.TypesInfo.ObjectOf(id)
			e := ast.Unparen(rhs)
			if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
				e = ast.Unparen(u.X)
			}
			lit, ok := e.(*ast.CompositeLit)
			if !ok || lhs == nil {
				continue
			}
			t := pass.TypesInfo.TypeOf(lit)
			if t == nil || !hasUnwrap(types.NewMethodSet(types.NewPointer(t))) {
				continue
			}
			for _, elt := range lit.Elts {
				v := elt
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					v = kv.Value
				}
				vid, ok := ast.Unparen(v).(*ast.Ident)
				if !ok {
					continue
				}
				if obj, ok := pass.TypesInfo.ObjectOf(vid).(*types.Var); ok && astx.IsNamed(obj.Type(), "net/http", "ResponseWriter") {
					out[lhs] = obj
				}
			}
		}
		return true
	})
	return out
}

// isHeaderCall: call is <x>.Header() with x a ResponseWriter-ish value
// (anything whose Header method returns http.Header).
func isHeaderCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Header" || len(call.Args) != 0 {
		return false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok {
		return false
	}
	sig := fn.Type().(*types.Signature)
	return sig.Recv() != nil && sig.Results().Len() == 1 &&
		astx.IsNamed(sig.Results().At(0).Type(), "net/http", "Header")
}

// headerMutations collects the mutating Header() calls in a body.
func headerMutations(pass *analysis.Pass, body *ast.BlockStmt) []*ast.CallExpr {
	parents := dominance.Parents(body)
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isHeaderCall(pass, call) && mutates(pass, call, parents) {
			out = append(out, call)
		}
		return true
	})
	return out
}

// mutates: the Header() result is the receiver of Set/Add/Del, the map
// of an index assignment, or the operand of a conversion.
func mutates(pass *analysis.Pass, hc *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	p := parents[hc]
	for {
		if pe, ok := p.(*ast.ParenExpr); ok {
			p = parents[pe]
			continue
		}
		break
	}
	switch p := p.(type) {
	case *ast.SelectorExpr:
		switch p.Sel.Name {
		case "Set", "Add", "Del":
			_, ok := parents[p].(*ast.CallExpr)
			return ok
		}
	case *ast.IndexExpr:
		as, ok := parents[p].(*ast.AssignStmt)
		if !ok {
			return false
		}
		for _, lhs := range as.Lhs {
			if lhs == p {
				return true
			}
		}
	case *ast.CallExpr:
		// A conversion (propagation.HeaderCarrier(w.Header())) hands
		// the map to a writer type. A plain call argument stays quiet:
		// isStoreable(rec, w.Header()) reads it.
		if tv, ok := pass.TypesInfo.Types[p.Fun]; ok && tv.IsType() {
			return true
		}
	}
	return false
}

// matchesWriter: the Header() receiver is the dispatched writer or the
// writer it passes through to.
func matchesWriter(pass *analysis.Pass, hc *ast.CallExpr, d dispatch) bool {
	sel := ast.Unparen(hc.Fun).(*ast.SelectorExpr)
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(id)
	return obj != nil && (obj == d.writer || (d.alias != nil && obj == d.alias))
}

// dominates: the dispatch statement sits in a statement list that
// lexically precedes node on its path up to body.
func dominates(st ast.Stmt, node ast.Node, parents map[ast.Node]ast.Node, body ast.Node) bool {
	for _, list := range dominance.Prefix(node, parents, body) {
		for _, s := range list {
			if s == st {
				return true
			}
		}
	}
	return false
}
