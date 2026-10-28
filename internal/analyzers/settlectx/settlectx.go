// Package settlectx catches an outcome write (ack, nack, settle, record,
// mark) made on the same cancellable context the unit of work it records
// just ran under.
//
// The bug: a worker runs a handler under ctx, the handler returns, and
// the worker records the outcome on that same ctx. ctx is the app's
// lifecycle context (cancelled by Shutdown before the drainers join) or a
// request's context (cancelled when the handler returns). When the
// cancel lands while the handler runs, the handler's effects happen, the
// outcome write fails at once with context.Canceled, and the work is
// re-run after the lease expires: at-least-once becomes a second email
// or a second charge on every deploy. Found by the 2026-10-27 bug bash,
// one ledger entry per surface:
//   - B29: DBQueue.workerLoop settles with q.fail(ctx, …) and
//     q.Ack(ctx, job) after q.runHandler(ctx, h, job)
//     (battery/queue/db.go:1220,1225), ctx the app context;
//   - B35: ProcessInbound writes the processed and failed marks with
//     store.UpdateEnvelope(ctx, …) after fn(ctx, *env)
//     (battery/webhook/inbound.go:432,446);
//   - U51: RunBulkJob checkpoints a chunk with store.Settle(ctx, …)
//     after u.runBulk(cctx, …) (framework/entityui/bulk.go:971);
//   - M19: runSeedsBody records the seed with recordSeeded(ctx, …) after
//     cfg.Seed(seedCtx, db) (framework/migrate/seed.go:270).
//
// The fix spelling is the outbox relay's settleContext
// (framework/outbox/relay.go:233): context.WithTimeout(
// context.WithoutCancel(ctx), settleTimeout). WithoutCancel keeps the
// values and drops the cancel; the timeout bounds the write so a dead
// database cannot hold Stop. The unbounded detach is its own failure
// (B27: battery/webhook's saveDelivery settled under a bare
// context.Background(), so a blocked UPDATE outlived Stop), and the rule
// reports it too when it follows a callback.
//
// Shape, within one function body (a declaration or a literal, each on
// its own):
//   - the WORK is a call that runs a caller-supplied callback under a
//     live context: a call of a func-typed value (a parameter, a field, a
//     map entry; not a local bound to a func literal in the same body),
//     or a call to a function that does so under the context parameter
//     it is handed. That second kind is computed to a fixed point inside
//     the package and exported as a runsCallback fact, so
//     q.runHandler → invokeRecovering → h(ctx, job) and RunBulkJob →
//     runBulk → runAppAction → runAction → a.Run(ctx, …) both count;
//   - the SETTLE is a later call (or a deferred one anywhere in the body)
//     whose only result is error, that takes a context resolving to the
//     same root the work ran under, and that is outcome-shaped: its name
//     leads with ack, nack, settle, complete, finish, checkpoint, record,
//     mark or fail (camel-case first word: recordSeeded, MarkDone), or it
//     writes a record whose Status/State field was set after the work
//     returned (B35's UpdateEnvelope(ctx, done) with done.Status set
//     between), or a composite literal with a Status/State key.
//
// "Same root" follows locals through their latest assignment: a
// derivation (WithValue, WithCancel, a tracer's Start, a principal
// attach) keeps its parent's root; r.Context() roots at r; a struct
// field holding a context is its own root. context.WithoutCancel ends
// the chain. A same-package or imported function that returns a context
// and calls WithoutCancel exports a detaches fact: with a
// WithTimeout/WithDeadline call in it too, it is the detached, bounded
// fix; without one, the unbounded-detach report applies.
//
// Silent postures, deliberately:
//   - the settle ctx is detached and bounded: WithTimeout/WithDeadline
//     over WithoutCancel, inline, through a reassigned local, or through
//     a detaches helper (the outbox settleContext);
//   - WithTimeout over context.Background(): bounded, and not the
//     shutdown bug (it drops the values, which is the caller's call);
//   - an outcome write before the work (the processing claim B35 makes
//     with MarkEnvelopeProcessing before fn), or a Status set before the
//     work: claiming is not recording an outcome;
//   - work that is the function's own code: a local func literal, a
//     declared function that never invokes a callback, an interface
//     method (a store call is not a handler call);
//   - a spawned `go fn(ctx)`: the work has not returned;
//   - a callback and a settle on different roots;
//   - a settle that is handed the same transaction (*sql.Tx or a type
//     named Tx) as the work: they commit or roll back together;
//   - outcome-named calls that return more than an error (a lookup, a
//     renderer) and calls with no outcome name and no status write;
//   - _test.go files.
//
// Out of reach, and so not in the rule: B25, where the work (a committed
// HTTP write) and the publish (webhook.Bridge's mgr.Publish(ctx, …),
// battery/webhook/bridge.go:110) sit in different functions joined by
// the event bus's EmitAsync, and B27 itself, whose saveDelivery has no
// context and no callback in its body.
package settlectx

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var Analyzer = &analysis.Analyzer{
	Name:      "settlectx",
	Doc:       "report outcome writes (ack, settle, record, mark) made on the same cancellable context a just-finished callback ran under",
	Run:       run,
	FactTypes: []analysis.Fact{(*runsCallback)(nil), (*detaches)(nil)},
}

const advice = "record the outcome under a detached, bounded context: context.WithTimeout(context.WithoutCancel(ctx), d)"

// runsCallback marks a function that invokes a caller-supplied callback
// (a func-typed value) under the context parameters at these argument
// indexes, directly or through another runsCallback function.
type runsCallback struct{ Index []int }

func (*runsCallback) AFact() {}

func (f *runsCallback) String() string { return fmt.Sprintf("runsCallback%v", f.Index) }

// detaches marks a function that returns a context and calls
// context.WithoutCancel; Bounded when it also calls context.WithTimeout
// or WithDeadline (the outbox settleContext helper).
type detaches struct{ Bounded bool }

func (*detaches) AFact() {}

func (f *detaches) String() string { return fmt.Sprintf("detaches(bounded=%t)", f.Bounded) }

func run(pass *analysis.Pass) (any, error) {
	var fns []*ast.FuncDecl
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				fns = append(fns, fn)
			}
		}
	}
	a := &analyzer{
		pass:      pass,
		runs:      map[*types.Func]map[int]bool{},
		detaching: map[*types.Func]bool{},
	}
	a.collectDetaching(fns)
	a.collectRuns(fns)
	for _, obj := range slices.SortedFunc(maps.Keys(a.detaching), byPos) {
		pass.ExportObjectFact(obj, &detaches{Bounded: a.detaching[obj]})
	}
	for _, obj := range slices.SortedFunc(maps.Keys(a.runs), byPos) {
		pass.ExportObjectFact(obj, &runsCallback{Index: slices.Sorted(maps.Keys(a.runs[obj]))})
	}
	for _, fn := range fns {
		if strings.HasSuffix(pass.Fset.Position(fn.Pos()).Filename, "_test.go") {
			continue
		}
		r := a.newResolver(fn.Body)
		a.checkBody(r, fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				a.checkBody(r, lit.Body)
			}
			return true
		})
	}
	return nil, nil
}

func byPos(a, b *types.Func) int { return int(a.Pos() - b.Pos()) }

type analyzer struct {
	pass      *analysis.Pass
	runs      map[*types.Func]map[int]bool
	detaching map[*types.Func]bool // value: bounded
}

// ---- context classification ----

type kind int

const (
	unknown  kind = iota
	live          // descends from a cancellable root
	detached      // context.WithoutCancel on the chain
	fresh         // context.Background / context.TODO
)

type state struct {
	kind    kind
	root    types.Object // live only
	bounded bool         // detached or fresh: a timeout/deadline on the chain
}

type assign struct {
	end token.Pos
	rhs ast.Expr
}

// resolver answers "what context does this expression hold here" for one
// top-level function body, nested literals included.
type resolver struct {
	a       *analyzer
	assigns map[types.Object][]assign
	// closures are locals bound to a func literal in this body: the
	// function's own code, not a caller-supplied callback.
	closures map[types.Object]bool
}

func (a *analyzer) newResolver(body *ast.BlockStmt) *resolver {
	r := &resolver{a: a, assigns: map[types.Object][]assign{}, closures: map[types.Object]bool{}}
	info := a.pass.TypesInfo
	add := func(id *ast.Ident, rhs ast.Expr, end token.Pos) {
		obj := info.Defs[id]
		if obj == nil {
			obj = info.Uses[id]
		}
		if obj == nil {
			return
		}
		if _, ok := ast.Unparen(rhs).(*ast.FuncLit); ok && rhs != nil {
			r.closures[obj] = true
		}
		if !isContext(obj.Type()) {
			return
		}
		r.assigns[obj] = append(r.assigns[obj], assign{end: end, rhs: rhs})
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				id, ok := ast.Unparen(lhs).(*ast.Ident)
				if !ok {
					continue
				}
				var rhs ast.Expr
				if len(n.Rhs) == len(n.Lhs) {
					rhs = n.Rhs[i]
				} else if len(n.Rhs) == 1 {
					rhs = n.Rhs[0]
				}
				add(id, rhs, n.End())
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				var rhs ast.Expr
				if len(n.Values) == len(n.Names) {
					rhs = n.Values[i]
				} else if len(n.Values) == 1 {
					rhs = n.Values[0]
				}
				add(id, rhs, n.End())
			}
		}
		return true
	})
	return r
}

// classify resolves the context e holds, following the latest assignment
// to a local before e (straight-line: branches are not split).
func (r *resolver) classify(e ast.Expr, depth int) state {
	if depth > 10 || e == nil {
		return state{}
	}
	info := r.a.pass.TypesInfo
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj := info.Uses[e]
		if obj == nil {
			obj = info.Defs[e]
		}
		v, ok := obj.(*types.Var)
		if !ok || !isContext(v.Type()) {
			return state{}
		}
		var last *assign
		for i := range r.assigns[obj] {
			as := &r.assigns[obj][i]
			if as.end <= e.Pos() && (last == nil || as.end > last.end) {
				last = as
			}
		}
		if last == nil {
			// A parameter or a free variable: a root of its own.
			return state{kind: live, root: obj}
		}
		return r.classify(last.rhs, depth+1)
	case *ast.SelectorExpr:
		// A context held in a struct field (w.ctx): the field is the root.
		if sel, ok := info.Selections[e]; ok && sel.Kind() == types.FieldVal && isContext(sel.Obj().Type()) {
			return state{kind: live, root: sel.Obj()}
		}
		return state{}
	case *ast.CallExpr:
		fn := typeutil.StaticCallee(info, e)
		if fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "context" {
			switch fn.Name() {
			case "WithoutCancel":
				return state{kind: detached}
			case "Background", "TODO":
				return state{kind: fresh}
			case "WithTimeout", "WithDeadline", "WithTimeoutCause", "WithDeadlineCause":
				if len(e.Args) > 0 {
					s := r.classify(e.Args[0], depth+1)
					if s.kind == detached || s.kind == fresh {
						s.bounded = true
					}
					return s
				}
			}
		}
		if fn != nil {
			if ok, bounded := r.a.detachState(fn); ok {
				return state{kind: detached, bounded: bounded}
			}
		}
		if !returnsContext(info.TypeOf(e)) {
			return state{}
		}
		// A derivation (WithValue, WithCancel, a tracer's Start, a
		// principal attach): the result carries its parent's cancellation.
		for _, arg := range e.Args {
			if t := info.TypeOf(arg); t != nil && isContext(t) {
				return r.classify(arg, depth+1)
			}
		}
		// r.Context(): the request is the root.
		if s, ok := ast.Unparen(e.Fun).(*ast.SelectorExpr); ok && s.Sel.Name == "Context" && len(e.Args) == 0 {
			if id, ok := ast.Unparen(s.X).(*ast.Ident); ok {
				if obj := info.Uses[id]; obj != nil {
					return state{kind: live, root: obj}
				}
			}
		}
		return state{}
	}
	return state{}
}

func isContext(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "context" && n.Obj().Name() == "Context"
}

func returnsContext(t types.Type) bool {
	if t == nil {
		return false
	}
	if tup, ok := t.(*types.Tuple); ok {
		for v := range tup.Variables() {
			if isContext(v.Type()) {
				return true
			}
		}
		return false
	}
	return isContext(t)
}

// ---- facts ----

// detachState reports whether fn returns a detached context and whether
// that context is bounded.
func (a *analyzer) detachState(fn *types.Func) (isDetach, bounded bool) {
	fn = fn.Origin()
	if b, ok := a.detaching[fn]; ok {
		return true, b
	}
	if fn.Pkg() == a.pass.Pkg {
		return false, false
	}
	var f detaches
	if !a.pass.ImportObjectFact(fn, &f) {
		return false, false
	}
	return true, f.Bounded
}

// collectDetaching finds the local functions that return a context built
// on context.WithoutCancel, and whether a timeout or deadline bounds it.
func (a *analyzer) collectDetaching(fns []*ast.FuncDecl) {
	for _, fn := range fns {
		obj, ok := a.pass.TypesInfo.Defs[fn.Name].(*types.Func)
		if !ok || !returnsContext(obj.Signature().Results()) {
			continue
		}
		var detach, bound bool
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			f := typeutil.StaticCallee(a.pass.TypesInfo, c)
			if f == nil || f.Pkg() == nil || f.Pkg().Path() != "context" {
				return true
			}
			switch f.Name() {
			case "WithoutCancel":
				detach = true
			case "WithTimeout", "WithDeadline", "WithTimeoutCause", "WithDeadlineCause":
				bound = true
			}
			return true
		})
		if detach {
			a.detaching[obj] = bound
		}
	}
}

// runsIndexes returns the argument indexes at which fn invokes a callback
// under the passed context.
func (a *analyzer) runsIndexes(fn *types.Func) map[int]bool {
	fn = fn.Origin()
	if idx, ok := a.runs[fn]; ok {
		return idx
	}
	if fn.Pkg() == a.pass.Pkg {
		return nil
	}
	var f runsCallback
	if !a.pass.ImportObjectFact(fn, &f) {
		return nil
	}
	out := map[int]bool{}
	for _, i := range f.Index {
		out[i] = true
	}
	return out
}

// collectRuns computes, to a fixed point, the local functions that run a
// callback under one of their context parameters.
func (a *analyzer) collectRuns(fns []*ast.FuncDecl) {
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			obj, ok := a.pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok {
				continue
			}
			params := map[types.Object]int{}
			i := 0
			for _, field := range fn.Type.Params.List {
				if len(field.Names) == 0 {
					i++
					continue
				}
				for _, id := range field.Names {
					if po := a.pass.TypesInfo.Defs[id]; po != nil && isContext(po.Type()) {
						params[po] = i
					}
					i++
				}
			}
			if len(params) == 0 {
				continue
			}
			r := a.newResolver(fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, root := range a.workRoots(r, call) {
					pi, ok := params[root]
					if !ok || a.runs[obj][pi] {
						continue
					}
					if a.runs[obj] == nil {
						a.runs[obj] = map[int]bool{}
					}
					a.runs[obj][pi] = true
					changed = true
				}
				return true
			})
		}
	}
}

// workRoots returns the live context roots under which call runs a
// callback: a call of a func-typed value with a live context argument,
// or a call to a runsCallback function at one of its indexes.
func (a *analyzer) workRoots(r *resolver, call *ast.CallExpr) []types.Object {
	info := a.pass.TypesInfo
	var idx map[int]bool
	if isFuncValueCall(info, call) && !r.isClosure(call) {
		idx = nil // every context argument counts
	} else if fn := typeutil.StaticCallee(info, call); fn != nil {
		idx = a.runsIndexes(fn)
		if len(idx) == 0 {
			return nil
		}
	} else {
		return nil
	}
	var roots []types.Object
	for i, arg := range call.Args {
		if idx != nil && !idx[i] {
			continue
		}
		t := info.TypeOf(arg)
		if t == nil || !isContext(t) {
			continue
		}
		if s := r.classify(arg, 0); s.kind == live {
			roots = append(roots, s.root)
		}
	}
	return roots
}

// isClosure reports whether call invokes a local bound to a func literal
// in this body.
func (r *resolver) isClosure(call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	return ok && r.closures[r.a.pass.TypesInfo.Uses[id]]
}

// isFuncValueCall reports whether call invokes a func-typed value (a
// parameter, local, or field holding a callback) rather than a declared
// function or method.
func isFuncValueCall(info *types.Info, call *ast.CallExpr) bool {
	tv, ok := info.Types[call.Fun]
	if !ok || tv.IsType() || tv.IsBuiltin() {
		return false
	}
	if _, ok := tv.Type.Underlying().(*types.Signature); !ok {
		return false
	}
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		_, isVar := info.Uses[f].(*types.Var)
		return isVar
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[f]; ok {
			return sel.Kind() == types.FieldVal
		}
		_, isVar := info.Uses[f.Sel].(*types.Var)
		return isVar
	case *ast.IndexExpr:
		// handlers[name](ctx, …)
		_, isFunc := typeutil.Callee(info, call).(*types.Func)
		return !isFunc
	}
	return false
}

// ---- detection ----

type work struct {
	end   token.Pos
	roots []types.Object
	call  *ast.CallExpr
}

type candidate struct {
	call     *ast.CallExpr
	fn       *types.Func
	deferred bool
}

// checkBody walks one function body (not its nested literals) and reports
// a settle call whose context is the same live root a callback ran under
// earlier in the body, or a detached context with no bound.
func (a *analyzer) checkBody(r *resolver, body *ast.BlockStmt) {
	info := a.pass.TypesInfo
	var works []work
	var cands []candidate
	var visit func(n ast.Node, deferred, spawned bool)
	visit = func(n ast.Node, deferred, spawned bool) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.DeferStmt:
				visit(n.Call, true, false)
				return false
			case *ast.GoStmt:
				// The spawned call has not returned when the next
				// statement runs: neither the work nor its settle.
				return false
			case *ast.CallExpr:
				if roots := a.workRoots(r, n); len(roots) > 0 {
					if !deferred && !spawned {
						works = append(works, work{end: n.End(), roots: roots, call: n})
					}
					return true
				}
				fn := typeutil.StaticCallee(info, n)
				if fn == nil {
					if f, ok := typeutil.Callee(info, n).(*types.Func); ok {
						fn = f // interface method
					}
				}
				if fn != nil && errorOnly(fn) {
					cands = append(cands, candidate{call: n, fn: fn, deferred: deferred})
				}
			}
			return true
		})
	}
	visit(body, false, false)
	if len(works) == 0 {
		return
	}
	statusAt := statusWrites(info, body)
	for _, c := range cands {
		// Nearest work first, so the report names the callback that
		// just returned.
		for _, w := range slices.Backward(works) {
			if !c.deferred && w.end > c.call.Pos() {
				continue
			}
			if a.consider(r, c, w, statusAt) {
				break
			}
		}
	}
}

// consider reports c against the work w ran before it, and reports
// whether it did.
func (a *analyzer) consider(r *resolver, c candidate, w work, statusAt map[types.Object][]token.Pos) bool {
	info := a.pass.TypesInfo
	settle := settleName(c.fn.Name()) || recordsStatus(info, c.call, w.end, statusAt)
	for _, arg := range c.call.Args {
		t := info.TypeOf(arg)
		if t == nil || !isContext(t) {
			continue
		}
		s := r.classify(arg, 0)
		switch {
		case s.kind == live && slices.Contains(w.roots, s.root):
			if !settle || sharesTx(info, c.call, w.call) {
				return false
			}
			a.pass.Reportf(c.call.Pos(), "%s records an outcome on the context the callback on line %d ran under; once shutdown or the request's end cancels it, the work has happened, this write fails, and the work runs again: %s",
				c.fn.Name(), a.pass.Fset.Position(w.call.Pos()).Line, advice)
			return true
		case (s.kind == detached || s.kind == fresh) && !s.bounded && settle:
			what := "context.WithoutCancel with no timeout"
			if s.kind == fresh {
				what = "context.Background()"
			}
			a.pass.Reportf(c.call.Pos(), "%s records an outcome under %s; a blocked write outlives shutdown: %s",
				c.fn.Name(), what, advice)
			return true
		}
	}
	return false
}

// statusWrites indexes `x.Status = …` / `x.State = …` assignments in body
// by the variable x.
func statusWrites(info *types.Info, body *ast.BlockStmt) map[types.Object][]token.Pos {
	out := map[types.Object][]token.Pos{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
			if !ok || !statusField(sel.Sel.Name) {
				continue
			}
			if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok {
				if obj := info.Uses[id]; obj != nil {
					out[obj] = append(out[obj], as.Pos())
				}
			}
		}
		return true
	})
	return out
}

func statusField(name string) bool { return name == "Status" || name == "State" }

// recordsStatus reports whether call writes a record whose Status/State
// was set after the work ended: a variable assigned `v.Status = …` between
// the work and the call, or a composite literal with a Status key.
func recordsStatus(info *types.Info, call *ast.CallExpr, after token.Pos, statusAt map[types.Object][]token.Pos) bool {
	for _, arg := range call.Args {
		arg = ast.Unparen(arg)
		if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.AND {
			arg = ast.Unparen(u.X)
		}
		switch arg := arg.(type) {
		case *ast.Ident:
			for _, p := range statusAt[info.Uses[arg]] {
				if p > after && p < call.Pos() {
					return true
				}
			}
		case *ast.CompositeLit:
			for _, el := range arg.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && statusField(k.Name) {
						return true
					}
				}
			}
		}
	}
	return false
}

// sharesTx reports whether the settle call and the work were handed the
// same transaction: they commit or roll back together.
func sharesTx(info *types.Info, settle, w *ast.CallExpr) bool {
	txs := map[types.Object]bool{}
	collect := func(c *ast.CallExpr, add bool) bool {
		exprs := slices.Clone(c.Args)
		if s, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr); ok {
			exprs = append(exprs, s.X)
		}
		for _, e := range exprs {
			id, ok := ast.Unparen(e).(*ast.Ident)
			if !ok || !isTx(info.TypeOf(id)) {
				continue
			}
			obj := info.Uses[id]
			if add {
				txs[obj] = true
			} else if txs[obj] {
				return true
			}
		}
		return false
	}
	collect(w, true)
	return collect(settle, false)
}

// isTx matches *sql.Tx and any named type called Tx (a driver or
// framework transaction handle).
func isTx(t types.Type) bool {
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Name() == "Tx"
}

// errorOnly reports whether fn's only result is an error: the signature
// of a write whose outcome is success or failure.
func errorOnly(fn *types.Func) bool {
	res := fn.Signature().Results()
	return res.Len() == 1 && types.Identical(res.At(0).Type(), types.Universe.Lookup("error").Type())
}

// settleVerbs are the leading words of an outcome-recording call.
var settleVerbs = map[string]bool{
	"ack": true, "nack": true, "settle": true, "complete": true, "finish": true,
	"checkpoint": true, "record": true, "mark": true, "fail": true,
}

func settleName(name string) bool {
	return settleVerbs[firstWord(name)]
}

// firstWord returns the leading camel-case word of name, lowercased.
func firstWord(name string) string {
	rs := []rune(name)
	end := len(rs)
	for i := 1; i < len(rs); i++ {
		if unicode.IsUpper(rs[i]) {
			end = i
			break
		}
	}
	return strings.ToLower(string(rs[:end]))
}
