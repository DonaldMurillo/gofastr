// Package callbackunderlock catches calls through func-typed values
// while a sync mutex is held.
//
// The bug class is a wedged registry: an app-supplied callback (a
// per-tool gate, a subscriber hook) invoked between Lock and Unlock
// can block for as long as it likes — while holding the lock every
// other reader needs — or panic past a plain (non-deferred) Unlock and
// leave the mutex locked forever. Go's RWMutex is writer-preferring,
// so one queued writer turns a slow callback into a full stop of every
// reader. The 419-probe audit found this shape in every MCP listing
// path (probe TestListingGateNeverBlocksRegistry, fixed b79942f7):
// listTools, handlePromptsList and handleResourcesTemplatesList all
// evaluated per-caller gates inside the registry read lock; the fix
// snapshots under the lock and evaluates gates outside it. This rule
// fires on the shape, not those sites: any call whose callee is a
// func-typed struct field or map element, lexically between a
// sync.Mutex/sync.RWMutex acquisition on the same receiver and its
// release (including the whole tail of the function when the release
// is deferred).
//
// Postures it deliberately stays silent on, because they are not this
// bug or are a different one: calls to NAMED functions and methods
// (they may take the lock themselves — locking twice is a deadlock
// bug, not a callback-under-lock bug, and is not diagnosable from
// shape); calls through locals and parameters — EXCEPT locals whose
// every binding is map-derived (`v, ok := m[k]`, `for _, v := range m`),
// which are registry callbacks by construction — because the repo's
// own convention copies a func field to a local and calls THAT under
// the lock precisely to run framework-controlled, non-blocking gates
// there (core/mcp listTools' callGate snapshot), and "which callbacks
// the framework allows to block its registry" is not visible from
// shape; a mutex reached through an alias (`m := &s.mu; m.Lock()`) or
// a custom wrapper type with its own Lock method (only sync.Mutex and
// sync.RWMutex selectors count); calls inside nested function literals
// (a closure deferred or handed to `go` does not run in the linear
// region, and a synchronous comparator's own lock behavior is its
// business); and context.CancelFunc fields (moduleproto's cancel
// slots): CancelFunc never re-enters user code — it is documented
// safe to call concurrently and only touches context internals — so
// holding p.mu across it cannot deadlock on a callback that re-takes
// the lock; and the deliberately-serialized one-shot — a callback on
// the SAME object as the mutex, called at most once outside any loop,
// under a DEFERRED release (battery/setup's run-one-step-under-lock,
// where the lock IS the exactly-once guarantee, and the style cache's
// compute-under-entry-lock) — because a panic unwinds through the
// deferred unlock and the blocked parties are the same object's own
// callers, not a shared registry's every reader.
package callbackunderlock

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "callbackunderlock",
	Doc:  "report calls through func-typed fields/map elements while a sync mutex is held",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkBody(pass, fn, fn.Body)
		}
	}
	return nil, nil
}

// lockEvent is one acquisition or release, in source order. key is the
// printed receiver expression ("s.mu"), so Lock and Unlock on the same
// mutex pair up and different mutexes in one function stay separate.
type lockEvent struct {
	pos  token.Pos
	key  string
	lock bool // true: Lock/RLock; false: Unlock/RUnlock
}

func checkBody(pass *analysis.Pass, fn *ast.FuncDecl, body *ast.BlockStmt) {
	events, calls, deferredRelease := scanLocks(pass, body)
	mapDerived := mapDerivedBindings(pass, body)
	recv := receiverName(fn)
	inLoop, ownBatch := callsInsideLoops(pass, body, recv)
	params := paramObjects(pass, fn)

	for i := range calls {
		call := &calls[i]
		_, desc, ok := funcValueCallee(pass, call, mapDerived)
		if !ok {
			continue
		}
		key, held := heldAt(events, call.Pos())
		if !held {
			continue
		}
		if serializedOneShot(deferredRelease, inLoop, ownBatch, key, desc, call, params) {
			continue
		}
		pass.Reportf(call.Pos(),
			"callbackunderlock: %s is called while %s is held; a blocking or panicking callback wedges every other user of the mutex (snapshot under the lock, invoke outside)",
			desc, key)
	}
}

// serializedOneShot recognizes the deliberately-serialized pattern this
// rule stays out of: the callback belongs to the SAME object as the
// mutex, is invoked at most once (not inside a loop), and the release
// is deferred — the setup runner runs one step under the lock because
// the lock is the exactly-once guarantee, and the style cache computes
// under the entry lock for the same reason. A panic there unwinds
// through the deferred unlock; nothing is wedged. The shape this rule
// exists for is the SHARED registry: per-item callbacks in a loop
// (mcp's listing gates, cron's per-job gate) or any explicit lock/
// unlock span a panicking callback can skip past.
func serializedOneShot(deferredRelease map[string]bool, inLoop, ownBatch map[token.Pos]bool, key, desc string, call *ast.CallExpr, params map[string]bool) bool {
	if !deferredRelease[key] {
		return false
	}
	if ownBatch[call.Pos()] {
		// The loop iterates the owner's own configured batch
		// (r.cfg.Steps), not a shared registry: the batch runs to
		// completion under the lock by design.
		return true
	}
	if inLoop[call.Pos()] {
		// Any other loop is a registry walk — the shape this rule
		// exists for. It fires.
		return false
	}
	lockRoot, _, ok := strings.Cut(key, ".")
	if !ok || lockRoot == "" {
		return false
	}
	calleeRoot, _, _ := strings.Cut(desc, ".")
	return calleeRoot == lockRoot || params[calleeRoot]
}

// rootIdent is the leftmost identifier of a selector chain (r.cfg.Steps
// → r); "" when the expression is not rooted at a plain name.
func rootIdent(e ast.Expr) string {
	for {
		switch x := unparen(e).(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			e = x.X
		default:
			return ""
		}
	}
}

// receiverName is the method receiver's name, "" for a plain function.
func receiverName(fn *ast.FuncDecl) string {
	if fn == nil || fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	names := fn.Recv.List[0].Names
	if len(names) == 0 {
		return ""
	}
	return names[0].Name
}

// paramObjects collects the enclosing function's parameter names
// (receiver included).
func paramObjects(pass *analysis.Pass, fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	if fn == nil {
		return out
	}
	if fn.Recv != nil {
		for _, f := range fn.Recv.List {
			for _, n := range f.Names {
				out[n.Name] = true
			}
		}
	}
	if fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			for _, n := range f.Names {
				out[n.Name] = true
			}
		}
	}
	return out
}

// callsInsideLoops marks every call position lexically inside a loop,
// and separately the calls inside an "own batch" range: a range over a
// non-map value rooted at the function's receiver (r.cfg.Steps) — the
// owner's configured batch, not a shared registry. Registries in this
// repo are maps (tools, prompts, handlers, rules); a map range stays a
// registry walk no matter whose field it is.
func callsInsideLoops(pass *analysis.Pass, body *ast.BlockStmt, recv string) (map[token.Pos]bool, map[token.Pos]bool) {
	inLoop := map[token.Pos]bool{}
	ownBatch := map[token.Pos]bool{}
	mark := func(own bool, stmt ast.Stmt) {
		ast.Inspect(stmt, func(x ast.Node) bool {
			if call, ok := x.(*ast.CallExpr); ok {
				inLoop[call.Pos()] = true
				if own {
					ownBatch[call.Pos()] = true
				}
			}
			if _, ok := x.(*ast.FuncLit); ok {
				return false
			}
			return true
		})
	}
	ast.Inspect(body, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ForStmt:
			mark(false, x)
			return false
		case *ast.RangeStmt:
			isMap := isMapTyped(pass, x.X)
			mark(!isMap && recv != "" && rootIdent(x.X) == recv, x)
			return false
		}
		return true
	})
	return inLoop, ownBatch
}

// scanLocks collects lock/unlock events and every other call, in
// source order, not descending into function literals: a closure
// handed to `go` or defer escapes the region, and a synchronous
// comparator is its own scope. If statements and loops are flattened
// by source position, the conservative order for "is the lock held
// HERE": a release that only happens on one branch still reads as a
// release, which can only under-report, never over-report.
func scanLocks(pass *analysis.Pass, body *ast.BlockStmt) ([]lockEvent, []ast.CallExpr, map[string]bool) {
	var events []lockEvent
	var calls []ast.CallExpr
	deferredRelease := map[string]bool{}
	// A release inside `defer` or `go` does not happen at its
	// statement: the deferred Unlock fires at function exit (so the
	// lock stays held through the tail of the body), and a
	// `go mu.Unlock()` is not a release this rule can place.
	async := map[token.Pos]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.DeferStmt:
			async[n.Call.Pos()] = true
			if sel, ok := unparen(n.Call.Fun).(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Unlock", "RUnlock":
					if s, ok := pass.TypesInfo.Selections[sel]; ok {
						if obj := s.Obj(); obj.Pkg() != nil && obj.Pkg().Path() == "sync" {
							deferredRelease[types.ExprString(sel.X)] = true
						}
					}
				}
			}
		case *ast.GoStmt:
			async[n.Call.Pos()] = true
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			calls = append(calls, *call)
			return true
		}
		switch sel.Sel.Name {
		case "Lock", "RLock", "Unlock", "RUnlock":
		default:
			calls = append(calls, *call)
			return true
		}
		s, ok := pass.TypesInfo.Selections[sel]
		if !ok {
			calls = append(calls, *call)
			return true
		}
		obj := s.Obj()
		if obj.Pkg() == nil || obj.Pkg().Path() != "sync" {
			// Not sync's own method: a wrapper type redefining
			// Lock/Unlock is out of scope.
			calls = append(calls, *call)
			return true
		}
		if async[call.Pos()] {
			// The deferred/go release: not an event. A deferred
			// Lock would be too, but that shape cannot guard the
			// statements above it anyway.
			return true
		}
		events = append(events, lockEvent{
			pos:  call.Pos(),
			key:  types.ExprString(sel.X),
			lock: sel.Sel.Name == "Lock" || sel.Sel.Name == "RLock",
		})
		return true
	})
	return events, calls, deferredRelease
}

// heldAt reports the mutex key held at pos, if any: the last event for
// some key before pos is an acquisition. A deferred release never
// appears as an event (it happens at function exit), so Lock+defer
// Unlock holds through the end of the body, which is the rule's intent.
func heldAt(events []lockEvent, pos token.Pos) (string, bool) {
	last := map[string]bool{}
	for _, e := range events {
		if e.pos >= pos {
			break
		}
		last[e.key] = e.lock
	}
	for key, locked := range last {
		if locked {
			return key, true
		}
	}
	return "", false
}

// funcValueCallee classifies the callee if it is a func-typed VALUE —
// a struct field selection, a map element index, or a local bound
// only from map values — rather than a named function/method.
// Everything else is not this rule's shape.
func funcValueCallee(pass *analysis.Pass, call *ast.CallExpr, mapDerived map[types.Object]bool) (types.Object, string, bool) {
	switch fun := unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		s, ok := pass.TypesInfo.Selections[fun]
		if !ok || s.Kind() != types.FieldVal {
			return nil, "", false
		}
		if !isSignature(s.Type()) {
			return nil, "", false
		}
		if field, ok := s.Obj().(*types.Var); ok && (isCancelFunc(field.Type()) || isClockFunc(field.Type())) {
			return nil, "", false
		}
		return s.Obj(), types.ExprString(fun), true
	case *ast.IndexExpr:
		// Only a map element counts; an IndexExpr over a function is
		// a generic instantiation (errors.AsType[T](err)), not a
		// callback lookup.
		if !isMapTyped(pass, fun.X) {
			return nil, "", false
		}
		if isSignature(pass.TypesInfo.TypeOf(fun)) {
			return nil, types.ExprString(fun), true
		}
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(fun)
		if obj != nil && mapDerived[obj] {
			return obj, fun.Name, true
		}
	}
	return nil, "", false
}

// mapDerivedBindings collects variables whose value comes out of a
// map: `v, ok := m[k]`, `v := m[k]`, and the value variable of a
// `for _, v := range m`. Those are registry callbacks even when called
// through the local's name.
func mapDerivedBindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.RangeStmt:
			if !isMapTyped(pass, n.X) {
				return true
			}
			if id, ok := n.Value.(*ast.Ident); ok {
				if obj := pass.TypesInfo.Defs[id]; obj != nil {
					out[obj] = true
				}
			}
		case *ast.AssignStmt:
			var ids []*ast.Ident
			for _, lhs := range n.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					ids = append(ids, id)
				}
			}
			for i, rhs := range n.Rhs {
				idx, ok := unparen(rhs).(*ast.IndexExpr)
				if !ok || !isMapTyped(pass, idx.X) {
					continue
				}
				if i < len(ids) {
					if obj := pass.TypesInfo.Defs[ids[i]]; obj != nil {
						out[obj] = true
					}
				}
			}
		}
		return true
	})
	return out
}

func isMapTyped(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Map)
	return ok
}

func isSignature(t types.Type) bool {
	if t == nil {
		return false
	}
	// Named function types (`type Stage func(...)`) carry their
	// signature in the underlying type; a func field declared with one
	// is as much a callback as an inline signature.
	_, ok := t.Underlying().(*types.Signature)
	return ok
}

// isClockFunc: an injected func() time.Time — a test seam over
// time.Now, incapable of blocking on app code or panicking.
func isClockFunc(t types.Type) bool {
	if t == nil {
		return false
	}
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	n, ok := sig.Results().At(0).Type().(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "time" && n.Obj().Name() == "Time"
}

func isCancelFunc(t types.Type) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "context" && obj.Name() == "CancelFunc"
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}
