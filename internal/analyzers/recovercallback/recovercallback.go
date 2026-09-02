// Package recovercallback catches registry callbacks invoked with no
// recover in scope on a dispatch path that has no net.
//
// The bug class is one panic killing a whole process: a handler or
// gate obtained from a registry map or a struct field is app-supplied
// code, and the dispatch sites that run it are transport loops — a
// stdio JSON-RPC loop, a peer read loop, a goroutine spawned per
// frame — where net/http's per-request recover net does not exist.
// The 419-probe audit found this shape twice (both fixed b79942f7):
// moduleproto's Peer served inbound requests and notifications by
// calling the map-registered handler directly, so a panicking module
// handler crashed the host process instead of answering a paired error
// (probe TestHandlerPanicBecomesErrorResponse, fixed by runHandler's
// recover guard), and core/mcp's callTool evaluated a tool's Gate
// callback with no guard, so a panicking gate unwound the transport
// (probe TestPanickingGateFailsClosedEverywhere, fixed by
// checkToolGate's recover guard).
//
// "Dispatch path" is condition (a) of the shape, computed across the
// package rather than lexically: a function is on a dispatch path if
// it is reachable within the package from a goroutine literal, from a
// `go`/time.AfterFunc target, or from a function whose body contains a
// for-loop that blocks on input (a channel receive or a
// Read/Scan/Recv/Decode-style call). The lexical reading alone — "runs
// in a goroutine literal or is a method whose body contains the loop"
// — misses the real callTool site, whose loop (ServeStdio) sits three
// call frames above it; the transitive reading is what the probes
// actually exercised, so that is what ships.
//
// Postures it deliberately stays silent on, because they are not this
// bug: http.Handler-shaped callbacks — a value whose type is
// http.HandlerFunc or whose signature is func(http.ResponseWriter,
// *http.Request) — because net/http recovers handler panics per
// request, and likewise an http-handler-shaped function never seeds
// hotness even when its body loops (an SSE hold loop inside a handler
// still runs under the server's recover net); context.CancelFunc
// fields (never user code); named function calls (their guards are
// their own business); callbacks reached only from ordinary
// synchronous call sites, goroutine or no goroutine elsewhere in the
// package — reachability, not adjacency, is the test; and everything
// in _test.go, where a panic failing the test is the intended
// outcome.
package recovercallback

import (
	"go/ast"
	"go/types"
	"regexp"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "recovercallback",
	Doc:  "report registry callbacks invoked with no recover on a dispatch path that has none",
	Run:  run,
}

var readMethodName = regexp.MustCompile(`^(Read|Scan|Recv|Decode|Next|Accept)`)

func run(pass *analysis.Pass) (any, error) {
	g := buildGraph(pass)
	g.propagate()
	g.report()
	return nil, nil
}

// node is one function-ish unit: a declared function/method or a
// function literal.
type node struct {
	body        *ast.BlockStmt
	typ         types.Type // signature, when known
	owner       *node      // enclosing function for literals
	goLaunched  bool       // dispatched asynchronously: only its own recover counts
	hasRecover  bool
	readLoop    bool
	handlerShpd bool
	mapDerived  map[types.Object]bool // locals whose value came out of a map
	calls       []callbackCall
	edges       []*node
}

type callbackCall struct {
	call *ast.CallExpr
	desc string
}

type graph struct {
	pass  *analysis.Pass
	nodes []*node // declared functions/methods
	all   []*node // every node, literals included
	byObj map[types.Object]*node
	seeds []*node
	hot   map[*node]bool
}

func buildGraph(pass *analysis.Pass) *graph {
	g := &graph{pass: pass, byObj: map[types.Object]*node{}}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			obj := pass.TypesInfo.Defs[fn.Name]
			n := g.makeNode(fn.Body, obj.Type(), nil)
			if obj != nil {
				g.byObj[obj] = n
			}
			g.nodes = append(g.nodes, n)
			g.collectAll(n)
		}
	}
	for _, n := range g.nodes {
		g.link(pass, n)
	}
	return g
}

// collectAll flattens a node and its owned literals into g.all.
func (g *graph) collectAll(n *node) {
	g.all = append(g.all, n)
	for _, e := range n.edges {
		if e.owner == n {
			g.collectAll(e)
		}
	}
}

// makeNode computes the node's own facts, recursively creating child
// nodes for the function literals it directly owns.
func (g *graph) makeNode(body *ast.BlockStmt, typ types.Type, owner *node) *node {
	n := &node{
		body:       body,
		typ:        typ,
		owner:      owner,
		hasRecover: hasRecover(body),
		readLoop:   containsReadLoop(g.pass, body),
		mapDerived: mapDerivedVars(g.pass, body),
	}
	if sig, ok := typ.(*types.Signature); ok {
		n.handlerShpd = isHandlerShape(sig)
	}
	ast.Inspect(body, func(x ast.Node) bool {
		if lit, ok := x.(*ast.FuncLit); ok {
			child := g.makeNode(lit.Body, g.pass.TypesInfo.TypeOf(lit), n)
			n.edges = append(n.edges, child)
			return false
		}
		if call, ok := x.(*ast.CallExpr); ok {
			if desc, ok := g.callbackCallee(call, n); ok {
				n.calls = append(n.calls, callbackCall{call: call, desc: desc})
			}
		}
		return true
	})
	return n
}

// link resolves named calls to package-local functions and marks
// asynchronous dispatch (`go f()`, time.AfterFunc): the target stops
// being a synchronous edge and becomes a hot seed instead.
func (g *graph) link(pass *analysis.Pass, n *node) {
	var goCalls map[*ast.CallExpr]bool
	ast.Inspect(n.body, func(x ast.Node) bool {
		goStmt, ok := x.(*ast.GoStmt)
		if !ok {
			return true
		}
		if goCalls == nil {
			goCalls = map[*ast.CallExpr]bool{}
		}
		goCalls[goStmt.Call] = true
		g.markAsync(goStmt.Call.Fun, n)
		return true
	})
	ast.Inspect(n.body, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok || goCalls[call] {
			return true
		}
		if isAfterFunc(pass, call) && len(call.Args) > 0 {
			g.markAsync(call.Args[len(call.Args)-1], n)
			return true
		}
		if obj := g.callTarget(call.Fun); obj != nil {
			if target, ok := g.byObj[obj]; ok && target != n {
				n.edges = append(n.edges, target)
			}
		}
		return true
	})
}

// markAsync marks a `go`/AfterFunc target: the literal (matched by
// body identity) becomes a seed and loses its synchronous-parent
// recover credit; a package-local named function becomes a seed too.
func (g *graph) markAsync(fun ast.Expr, from *node) {
	if lit, ok := unparen(fun).(*ast.FuncLit); ok {
		for _, n := range g.all {
			if n.owner != nil && n.body == lit.Body {
				n.goLaunched = true
				g.seeds = append(g.seeds, n)
				return
			}
		}
		return
	}
	if obj := g.callTarget(fun); obj != nil {
		if target, ok := g.byObj[obj]; ok {
			target.goLaunched = true
			g.seeds = append(g.seeds, target)
		}
	}
}

func (g *graph) callTarget(fun ast.Expr) types.Object {
	var obj types.Object
	switch fun := unparen(fun).(type) {
	case *ast.Ident:
		obj = g.pass.TypesInfo.ObjectOf(fun)
	case *ast.SelectorExpr:
		if s, ok := g.pass.TypesInfo.Selections[fun]; ok {
			obj = s.Obj()
		}
	}
	if fn, ok := obj.(*types.Func); ok && fn.Pkg() == g.pass.Pkg {
		return obj
	}
	return nil
}

// callbackCallee reports whether call invokes a registry callback —
// through a func-typed struct field, a map element, or a local whose
// value came out of a map — and its printed description.
func (g *graph) callbackCallee(call *ast.CallExpr, n *node) (string, bool) {
	pass := g.pass
	switch fun := unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		s, ok := pass.TypesInfo.Selections[fun]
		if !ok || s.Kind() != types.FieldVal {
			return "", false
		}
		if !isCallbackType(s.Type()) {
			return "", false
		}
		return types.ExprString(fun), true
	case *ast.IndexExpr:
		// Only a map element counts; an IndexExpr over a function is
		// a generic instantiation (errors.AsType[T](err)), not a
		// callback lookup.
		if _, isMap := underlyingMap(pass.TypesInfo.TypeOf(fun.X)); !isMap {
			return "", false
		}
		if isCallbackType(pass.TypesInfo.TypeOf(fun)) {
			return types.ExprString(fun), true
		}
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(fun)
		if obj != nil && n.mapDerived[obj] {
			return fun.Name, true
		}
	}
	return "", false
}

// mapDerivedVars records the variables whose value comes out of a
// map: `v, ok := m[k]` and the value variable of `for _, v := range m`.
func mapDerivedVars(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.FuncLit:
			return false
		case *ast.RangeStmt:
			if id, ok := x.Value.(*ast.Ident); ok {
				if obj := pass.TypesInfo.Defs[id]; obj != nil {
					out[obj] = true
				}
			}
		case *ast.AssignStmt:
			for i, rhs := range x.Rhs {
				idx, ok := unparen(rhs).(*ast.IndexExpr)
				if !ok || i >= len(x.Lhs) {
					continue
				}
				if _, isMap := underlyingMap(pass.TypesInfo.TypeOf(idx.X)); !isMap {
					continue
				}
				if id, ok := x.Lhs[i].(*ast.Ident); ok {
					if obj := pass.TypesInfo.Defs[id]; obj != nil {
						out[obj] = true
					}
				}
			}
		}
		return true
	})
	return out
}

// propagate floods hotness from the seeds along synchronous call
// edges. Seeds: goroutine/AfterFunc targets, and any node whose body
// blocks on input in a loop — unless it is http-handler-shaped.
func (g *graph) propagate() {
	g.hot = map[*node]bool{}
	var queue []*node
	add := func(n *node) {
		if !g.hot[n] {
			g.hot[n] = true
			queue = append(queue, n)
		}
	}
	for _, n := range g.all {
		if n.readLoop && !n.handlerShpd {
			add(n)
		}
	}
	for _, s := range g.seeds {
		add(s)
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, e := range n.edges {
			add(e)
		}
	}
}

func (g *graph) report() {
	for _, n := range g.all {
		if !g.hot[n] || g.protected(n) {
			continue
		}
		for _, c := range n.calls {
			g.pass.Reportf(c.call.Pos(),
				"recovercallback: %s is invoked with no recover in scope; it sits on a dispatch path with no per-request net (goroutine/read loop), so a panicking callback kills the process",
				c.desc)
		}
	}
}

// protected: the node's own deferred recover, or — for a literal that
// runs synchronously inside its owner — the owner's (same goroutine).
func (g *graph) protected(n *node) bool {
	if n.hasRecover {
		return true
	}
	return !n.goLaunched && n.owner != nil && g.protected(n.owner)
}

func hasRecover(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(x ast.Node) bool {
		def, ok := x.(*ast.DeferStmt)
		if !ok {
			return true
		}
		ast.Inspect(def.Call, func(y ast.Node) bool {
			if call, ok := y.(*ast.CallExpr); ok {
				if id, ok := unparen(call.Fun).(*ast.Ident); ok && id.Name == "recover" {
					found = true
				}
			}
			return !found
		})
		return !found
	})
	return found
}

// containsReadLoop reports whether body has a for (or channel-range)
// loop that blocks on input: a Read/Scan/Recv/Decode-style call in the
// condition, a channel range, a receive ASSIGNED in the body, or a
// select inside the loop. A bare discard receive (`<-ready`) alone
// does not count — that is a coordination wait, not a dispatch.
func containsReadLoop(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.FuncLit:
			return false
		case *ast.RangeStmt:
			if _, isChan := underlyingChan(pass.TypesInfo.TypeOf(x.X)); isChan {
				found = true
			}
		case *ast.ForStmt:
			if forLoopReads(pass, x) {
				found = true
			}
		}
		return !found
	})
	return found
}

func forLoopReads(pass *analysis.Pass, loop *ast.ForStmt) bool {
	if call, ok := unparen(loop.Cond).(*ast.CallExpr); ok && loop.Cond != nil {
		if sel, ok := unparen(call.Fun).(*ast.SelectorExpr); ok && readMethodName.MatchString(sel.Sel.Name) {
			return true
		}
	}
	reads := false
	ast.Inspect(loop.Body, func(y ast.Node) bool {
		switch y := y.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ExprStmt:
			// A bare DISCARD receive (`<-ready`) does not make a loop
			// a dispatcher: that is a coordination wait (the cached
			// resolver's singleflight), and every real dispatch loop
			// in this repo selects or reads instead.
			_ = y
		case *ast.AssignStmt:
			for _, rhs := range y.Rhs {
				if u, ok := unparen(rhs).(*ast.UnaryExpr); ok && isReceive(u) {
					reads = true
				}
				if call, ok := unparen(rhs).(*ast.CallExpr); ok {
					if sel, ok := unparen(call.Fun).(*ast.SelectorExpr); ok && readMethodName.MatchString(sel.Sel.Name) {
						reads = true // for { f, err := r.ReadFrame() }
					}
				}
			}
		case *ast.SelectStmt:
			reads = true // a select inside a loop is a wait loop
		}
		return !reads
	})
	return reads
}

func isReceive(u *ast.UnaryExpr) bool { return u.Op.String() == "<-" }

func underlyingMap(t types.Type) (*types.Map, bool) {
	if t == nil {
		return nil, false
	}
	m, ok := t.Underlying().(*types.Map)
	return m, ok
}

func underlyingChan(t types.Type) (*types.Chan, bool) {
	if t == nil {
		return nil, false
	}
	c, ok := t.Underlying().(*types.Chan)
	return c, ok
}

// isCallbackType: a func-typed value that is neither an http.Handler,
// a CancelFunc, nor infrastructure. Infrastructure shapes: an injected
// `func() time.Time` (nowFn, now, clock fields all over the repo) is a
// test seam over time.Now, and a printf sink
// (`func(format string, args ...any)` — the Logger/logf fields the
// webhook manager, static builder and process supervisor inject) is
// logging plumbing; Go's own log/slog does not recover handler panics
// either, so demanding a net around every log line exceeds the
// language's own posture. Neither is app callback code, however much
// either looks like one.
func isCallbackType(t types.Type) bool {
	if t == nil {
		return false
	}
	if isHandlerType(t) || isCancelFunc(t) || isClockType(t) || isPrintfType(t) {
		return false
	}
	_, ok := t.Underlying().(*types.Signature)
	return ok
}

func isPrintfType(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || !sig.Variadic() || sig.Params().Len() != 2 || sig.Results().Len() != 0 {
		return false
	}
	if !isString(sig.Params().At(0).Type()) {
		return false
	}
	sl, ok := sig.Params().At(1).Type().Underlying().(*types.Slice)
	if !ok {
		return false
	}
	e, ok := sl.Elem().Underlying().(*types.Interface)
	return ok && e.Empty()
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isClockType(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	return isNamed(sig.Results().At(0).Type(), "time", "Time")
}

func isHandlerType(t types.Type) bool {
	if n, ok := t.(*types.Named); ok {
		obj := n.Obj()
		if obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == "HandlerFunc" {
			return true
		}
	}
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		return false
	}
	return isHandlerShape(sig)
}

func isHandlerShape(sig *types.Signature) bool {
	if sig.Params().Len() != 2 || sig.Results().Len() != 0 {
		return false
	}
	p0, p1 := sig.Params().At(0), sig.Params().At(1)
	if !isNamed(p0.Type(), "net/http", "ResponseWriter") {
		return false
	}
	ptr, ok := p1.Type().(*types.Pointer)
	if !ok {
		return false
	}
	return isNamed(ptr.Elem(), "net/http", "Request")
}

func isNamed(t types.Type, pkgPath, name string) bool {
	n, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

func isCancelFunc(t types.Type) bool {
	return isNamed(t, "context", "CancelFunc")
}

func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	name := pass.Fset.Position(f.Pos()).Filename
	return len(name) >= 8 && name[len(name)-8:] == "_test.go"
}

func isAfterFunc(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	return ok && pkg.Imported().Path() == "time" && sel.Sel.Name == "AfterFunc"
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
