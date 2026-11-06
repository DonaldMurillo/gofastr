// Package recovercallback catches registry callbacks invoked with no
// recover in scope on a dispatch path that has no net.
//
// The bug class is one panic killing a whole process: a handler or
// gate obtained from a registry map, a struct field, or a local copied
// from one (`gate := t.Gate; gate()` — the nil-check-and-call spelling
// any careful author writes) is app-supplied code, and the dispatch
// sites that run it are transport loops — a stdio JSON-RPC loop, a
// peer read loop, a goroutine spawned per frame, a ticker-driven
// periodic dispatcher — where net/http's per-request recover net does
// not exist. The 419-probe audit found this shape twice (both fixed
// b79942f7): moduleproto's Peer served inbound requests and
// notifications by calling the map-registered handler directly, so a
// panicking module handler crashed the host process instead of
// answering a paired error (probe TestHandlerPanicBecomesErrorResponse,
// fixed by runHandler's recover guard), and core/mcp's callTool
// evaluated a tool's Gate callback with no guard, so a panicking gate
// unwound the transport (probe TestPanickingGateFailsClosedEverywhere,
// fixed by checkToolGate's recover guard). The 2026-09-02 round found
// the interface twin (cron runTick, event bridge, both still open):
// a method called through a module-declared interface — a LeaderElection
// installed via WithLeaderElection, a fanout.Fanout backend passed to
// AttachFanout — is host-supplied code exactly like a map handler, and
// so is a func value the host call RETURNS (Acquire's release func,
// invoked later on a bare goroutine; probes TestCronRedAcquirePanicIsolated,
// TestCronRedReleasePanicIsolated, bridge_red_test.go).
// The 2026-09-07 round added the generic twin (probe
// TestStateChannelRedPanicNet, still open): a SnapshotSource installed
// via NewStateChannel and dispatched from a channel-receive select
// loop is the same host-supplied extension point, but go/types hands
// selectors on generic instantiations per-instantiation method
// objects whose identity differs from the declared ones — the edge
// flood and the callback test both canonicalize through
// Named.Origin() (see originFunc) so a generic dispatcher is policed
// like any other.
//
// "Dispatch path" is condition (a) of the shape, computed across the
// package rather than lexically: a function is on a dispatch path if
// it is reachable within the package from a goroutine literal, from a
// `go`/time.AfterFunc target, or from a function whose body contains a
// for-loop that blocks on input (a channel receive — including a bare
// receive on a *time.Ticker/*time.Timer channel, `for { <-w.t.C; ... }`,
// which is a timer-driven dispatcher, not a coordination wait — or a
// Read/Scan/Recv/Decode-style call). A method call through an
// interface declared in this package adds edges to every package-local
// method with the same name and arity, so interface dispatch keeps the
// flood honest for the ordinary Go way to decouple a transport from
// its handlers. The lexical reading alone — "runs in a goroutine
// literal or is a method whose body contains the loop" — misses the
// real callTool site, whose loop (ServeStdio) sits three call frames
// above it; the transitive reading is what the probes actually
// exercised, so that is what ships.
//
// A recover counts when it runs on the panicking frame: the node's own
// deferred recover (the inline `defer func(){ recover() }()` literal,
// or `defer guard()` where the package-local guard function or method
// recovers — recover works when called directly by the deferred
// function, and the guard IS the deferred function), or the enclosing
// owner's for a literal that runs synchronously. A recover inside a
// nested function literal does NOT count: it runs on that literal's
// own stack (a goroutine's recover cannot catch the parent's panic).
//
// Interface-method callbacks: a call `recv.M(...)` counts when the
// method belongs to an interface DECLARED IN THIS MODULE (the
// extension points this repo hands to hosts: cron.LeaderElection,
// fanout.Fanout, queue and storage backends) and the receiver is a
// value held AS the extension point itself — a struct field the host
// installs, a registry map entry, a parameter the host passes, or a
// local one hop from one of those. The declaring package is judged by
// module path (the pass's module; a -module flag override exists for
// hermetic analysistest runs, where the driver reports none), so
// stdlib and third-party interfaces are not app callbacks no matter
// where they sit: io.Reader.Read is data plumbing, and a vendored
// driver's panic is that dependency's bug. A receiver obtained by
// TYPE ASSERTION (BatteryManager's `b.(BatteryLifecycle)` narrowing)
// stays quiet: that is an optional-capability probe of a value already
// held, and the boot/shutdown paths that use it (App.Start/Shutdown,
// battery OnStart/OnStop) are the coordinator's direct fix, not this
// rule's. A receiver the package CONSTRUCTED itself (a call result)
// stays quiet too: the package picked that implementation, and its
// panic is reached through the package-local edge flood like any other
// local call. The loop's own input calls — the Read/Scan/Recv/Decode/
// Next/Accept name family, the same set the read-loop detector uses —
// are the transport, not a registry callback (peerish's p.r.ReadFrame
// stays quiet however module-declared Reader is). The stdlib-backend
// spellings wrapper interfaces inherit — database/sql's QueryContext/
// QueryRowContext/ExecContext trio (framework/db.Executor exists so
// *sql.DB and *sql.Tx satisfy it) and the os/exec + io.Closer teardown
// family (RunningChild, schedulerStartStop) — are data access and
// resource teardown, not events: a panic there is a bug in one repo
// wrapper, the sql.DB.Query posture one hop removed. Accessor-shaped
// methods — zero parameters, one result (c.ID, agent.Info) — are value
// queries, not event callbacks.
//
// Returned funcs: a func-typed result of a callback call — `held,
// release, err := s.leader.Acquire(ctx)` then `release()` in a spawned
// goroutine — is host-supplied code by construction (the host's
// Acquire built it), so invoking it under the same dispatch/no-recover
// test fires. The release runs on its own goroutine with no recover,
// which is exactly the cron probe.
//
// Slot accessors: a local assigned from a package-local function whose
// body returns a non-cancel-named field or dereferences a pointer (the
// `*p` of an atomic.Pointer[func(...)] Load) holds the host's callback
// just as a local copied from the field does. island.Manager's
// heartbeat read its OnPresenceChange hook through presenceCallback()
// and called it bare on the heartbeat goroutine (2026-10-27 bug bash
// R9); the rule saw only a named function call. An accessor that
// returns a closure it built, or a method value, stays quiet.
//
// A callback expression that is itself a `go` target (go s.bus.Push(…),
// go r.onTick()) reports outright: it runs on its own goroutine, where
// no recover of the launching frame can reach it, whatever path the
// launcher sits on. A method VALUE taken from a host-installed
// extension-point slot (pub := b.backend.Publish; pub(ev)) is host code
// one hop from the field and is judged like the call through it.
// The 2026-11 lifecycle round fixed two blind spots core/a2a's run
// finalize and push drain goroutines slipped through (fixed by hand in
// 6fce08fe6). First, literal nodes link their own bodies: `go func(){
// ...; s.finalize(ctx, t, err) }()` is a hot seed, and its call to
// finalize is the literal's edge. Linking only the declarations, with a
// walk that did not stop at nested literals, attributed that call to
// startRun, which no dispatch path reached. The flood also stops at a
// protected node — a recover on a frame catches every panic its
// synchronous callees raise — which is the fix spelling 6fce08fe6 used:
// the goroutine literal's deferred recover covers finalize and all it
// calls. Second, an *http.Client send (Do/Get/Post/Head/PostForm)
// through a field the host can fill is a host callback: the method is
// concrete stdlib code, but it runs the host's Transport and its
// CheckRedirect. "Host can fill" means the field is exported, or some
// assignment in the package fills it from anything but a client the
// package built (an http.Client literal, http.DefaultClient, or a local
// only ever assigned one). That arm fires only on a path a goroutine
// the package launched reaches: a read loop the host calls on its own
// goroutine (a generated client's SSE watch) unwinds into the host.
//
// Postures it deliberately stays silent on, because they are not this
// bug: interface methods that are unexported (only this package can
// implement them, so the interface is a test seam over package code and
// the edge flood reaches the implementations — battery/service's
// toolRunner), interfaces declared in a main package (a command is the
// host and declares no extension point — examples/site's
// dashIslandPusher over *island.Manager), `func() *slog.Logger`
// accessor fields (logging plumbing, like the printf sinks), http.Handler-shaped callbacks — a value whose type is
// http.HandlerFunc or whose signature is func(http.ResponseWriter,
// *http.Request) — because net/http recovers handler panics per
// request, and likewise an http-handler-shaped function never seeds
// hotness even when its body loops (an SSE hold loop inside a handler
// still runs under the server's recover net); context.CancelFunc
// fields (never user code), including func(error)-shaped cancel-cause
// slots under a cancel-named field (cancelFn, cancelFunc — the
// context.WithCancelCause cancels kiln's AdapterStore stores) and
// locals copied from them; clock and printf-shaped fields (test seams
// and logging plumbing, not app callbacks) — including locals copied
// from them; interface methods on stdlib or third-party-declared
// interfaces, on assertion-narrowed or self-constructed receivers,
// input-name-family, data-access/teardown-spelling, and accessor-
// shaped methods (the paragraphs above); interface calls already under
// a recover on the frame (same test as every other callback); named
// function calls (their guards are their own business); callbacks
// reached only from ordinary synchronous call sites, goroutine or no
// goroutine elsewhere in the package — reachability, not adjacency, is
// the test; and everything in _test.go, where a panic failing the test
// is the intended outcome.
package recovercallback

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "recovercallback",
	Doc:  "report registry callbacks invoked with no recover on a dispatch path that has none",
	Run:  run,
}

// moduleFlag overrides the module an interface must be declared in to
// count as an extension point. go vet populates pass.Module;
// analysistest's fixture loader reports an empty module path, so
// hermetic runs set this flag explicitly.
var moduleFlag *string

func init() {
	// Registered in init to keep the var dependency graph acyclic.
	moduleFlag = Analyzer.Flags.String("module", "", "module path override when the driver reports no module (test fixtures)")
}

var readMethodName = regexp.MustCompile(`^(Read|Scan|Recv|Decode|Next|Accept)`)

// plumbingName matches the method spellings the repo's wrapper
// interfaces inherit from stdlib backends: database/sql's
// QueryContext/QueryRowContext/ExecContext trio (framework/db.Executor
// exists so *sql.DB and *sql.Tx both satisfy it; crud's eager loader,
// migrate, and the durable queue all take it as a parameter), and the
// os/exec + io.Closer teardown family (RunningChild wraps an exec.Cmd;
// schedulerStartStop narrows the queues' Close). A panic there is a
// bug in one repo wrapper, not host callback code — the sql.DB.Query
// posture one hop removed.
var plumbingName = regexp.MustCompile(`^(QueryContext|QueryRowContext|ExecContext|Wait|Kill|CloseStdin|Signal|Close)$`)

func run(pass *analysis.Pass) (any, error) {
	g := buildGraph(pass)
	g.propagate()
	g.report()
	return nil, nil
}

// node is one function-ish unit: a declared function/method or a
// function literal.
type node struct {
	body         *ast.BlockStmt
	typ          types.Type // signature, when known
	owner        *node      // enclosing function for literals
	goLaunched   bool       // dispatched asynchronously: only its own recover counts
	hasRecover   bool
	readLoop     bool
	handlerShpd  bool
	params       map[types.Object]bool // signature params (+ receiver), for host-supplied receiver tests
	derived      map[types.Object]bool // locals whose value came out of a map, a func-typed field, or a callback call's func result
	derivedIface map[types.Object]bool // locals one hop from an interface slot (field/param/map entry)
	calls        []callbackCall
	edges        []*node
}

type callbackCall struct {
	call *ast.CallExpr
	desc string
	// asyncOnly: an *http.Client send through a host-filled field. It
	// fires only on a path a goroutine the package launched reaches: a
	// read loop the host calls synchronously (a generated client's SSE
	// watch) runs the host's Transport on the host's own goroutine.
	asyncOnly bool
	// launched: the call itself is a `go` target. It runs on its own
	// goroutine, where no recover of ours can reach it, hot or not.
	launched bool
}

type graph struct {
	pass    *analysis.Pass
	modPath string  // module an interface must live in to be an extension point ("" = unknown)
	nodes   []*node // declared functions/methods
	all     []*node // every node, literals included
	byObj   map[types.Object]*node
	decls   map[types.Object]*ast.FuncDecl // every package-local declaration, for slotAccessor
	seeds   []*node
	hot     map[*node]bool
	async   map[*node]bool // hot from a goroutine/AfterFunc seed, read loops aside
	// hostClients: the *http.Client fields a host-supplied value can
	// fill (see hostClientFields).
	hostClients map[*types.Var]bool
}

func buildGraph(pass *analysis.Pass) *graph {
	g := &graph{pass: pass, byObj: map[types.Object]*node{}, decls: map[types.Object]*ast.FuncDecl{}, modPath: *moduleFlag, hostClients: hostClientFields(pass)}
	if g.modPath == "" && pass.Module != nil {
		g.modPath = pass.Module.Path
	}
	// Declarations first: a node's derived locals may come from an
	// accessor declared later in the package.
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				if obj := pass.TypesInfo.Defs[fn.Name]; obj != nil {
					g.decls[obj] = fn
				}
			}
		}
	}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
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
	// Every node links, literals included: a goroutine literal is a hot
	// seed, and the named calls in its body are ITS edges. Linking only
	// the declarations attributed `go func(){ s.finalize(...) }()`'s
	// call to the enclosing declaration, which no dispatch path reached,
	// so core/a2a's run-finalize goroutine was never flagged (fixed by
	// hand in 6fce08fe6).
	for _, n := range g.all {
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
		params:     paramObjects(typ),
	}
	n.derived = g.derivedVars(body, n)
	n.derivedIface = g.derivedIfaceVars(body, n)
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
				sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
				n.calls = append(n.calls, callbackCall{call: call, desc: desc, asyncOnly: isSel && g.hostClientCall(sel)})
			}
		}
		return true
	})
	return n
}

// paramObjects collects a signature's parameter variables (and method
// receiver): the values the HOST or the caller hands this function.
func paramObjects(typ types.Type) map[types.Object]bool {
	out := map[types.Object]bool{}
	sig, ok := typ.(*types.Signature)
	if !ok {
		return out
	}
	for i := range sig.Params().Len() {
		out[sig.Params().At(i)] = true
	}
	if sig.Recv() != nil {
		out[sig.Recv()] = true
	}
	return out
}

// link resolves named calls to package-local functions and marks
// asynchronous dispatch (`go f()`, time.AfterFunc): the target stops
// being a synchronous edge and becomes a hot seed instead.
//
// Each walk stops at a nested function literal: the literal is its own
// node and links its own body, so a call inside it is an edge of the
// literal, never of the function that encloses it.
func (g *graph) link(pass *analysis.Pass, n *node) {
	var goCalls map[*ast.CallExpr]bool
	ast.Inspect(n.body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		goStmt, ok := x.(*ast.GoStmt)
		if !ok {
			return true
		}
		if goCalls == nil {
			goCalls = map[*ast.CallExpr]bool{}
		}
		goCalls[goStmt.Call] = true
		// A callback expression launched with `go` runs on its own
		// goroutine: no recover of this frame can reach it, so it is
		// reported whatever this function's own path is.
		for i := range n.calls {
			if n.calls[i].call == goStmt.Call {
				n.calls[i].launched = true
			}
		}
		g.markAsync(goStmt.Call.Fun, n)
		return true
	})
	// R5: a deferred package-local guard whose body calls recover()
	// DIRECTLY protects this node exactly like the inline literal —
	// recover works when called directly by the deferred function, and
	// the guard IS the deferred function. A guard whose OWN body defers
	// its recover does not: that nested recover runs on the guard's
	// frame and cannot catch a panic from this node, so only
	// directRecover counts here.
	ast.Inspect(n.body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		def, ok := x.(*ast.DeferStmt)
		if !ok {
			return true
		}
		if target, ok := g.byObj[g.callTarget(def.Call.Fun)]; ok {
			if directRecover(target.body) {
				n.hasRecover = true
			}
		}
		return true
	})
	ast.Inspect(n.body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		call, ok := x.(*ast.CallExpr)
		if !ok || goCalls[call] {
			return true
		}
		if isAfterFunc(pass, call) && len(call.Args) > 0 {
			g.markAsync(call.Args[len(call.Args)-1], n)
			return true
		}
		if obj := g.callTarget(call.Fun); obj != nil {
			if target, ok := g.byObj[obj]; ok {
				if target != n {
					n.edges = append(n.edges, target)
				}
			} else {
				// R1: a method call through an interface declared in
				// this package resolves to the interface's method, not
				// any implementation. Add an edge to every
				// package-local method with the same name and arity —
				// a cheap dispatch approximation that keeps the
				// reachability flood honest for the ordinary Go way to
				// decouple a transport from its handlers.
				for _, target := range g.interfaceImplementations(obj.(*types.Func)) {
					if target != n {
						n.edges = append(n.edges, target)
					}
				}
			}
		}
		return true
	})
}

// interfaceImplementations: the package-local method nodes with the
// same name and parameter count as the interface method m, when m
// belongs to an interface declared in this package.
func (g *graph) interfaceImplementations(m *types.Func) []*node {
	sig, ok := m.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return nil
	}
	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok {
		return nil
	}
	if _, ok := named.Underlying().(*types.Interface); !ok {
		return nil
	}
	if p := named.Obj().Pkg(); p == nil || p.Path() != g.pass.Pkg.Path() {
		return nil
	}
	var out []*node
	for obj, target := range g.byObj {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Name() != m.Name() {
			continue
		}
		s2, ok := fn.Type().(*types.Signature)
		if !ok || s2.Recv() == nil || s2.Params().Len() != sig.Params().Len() {
			continue
		}
		out = append(out, target)
	}
	return out
}

// markAsync marks a `go`/AfterFunc target: the literal (matched by
// body identity) becomes a seed and loses its synchronous-parent
// recover credit; a package-local named function becomes a seed too.
func (g *graph) markAsync(fun ast.Expr, from *node) {
	if lit, ok := ast.Unparen(fun).(*ast.FuncLit); ok {
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
	switch fun := ast.Unparen(fun).(type) {
	case *ast.Ident:
		obj = g.pass.TypesInfo.ObjectOf(fun)
	case *ast.SelectorExpr:
		if s, ok := g.pass.TypesInfo.Selections[fun]; ok {
			obj = s.Obj()
		}
	}
	if fn, ok := obj.(*types.Func); ok && fn.Pkg() == g.pass.Pkg {
		return originFunc(fn)
	}
	return nil
}

// originFunc canonicalizes a method selected through a GENERIC
// instantiation to the declared method. The 2026-09-07 round found
// core/stream.StateChannel quiet against this rule's own shape: a
// selector on a receiver whose type is a generic instantiation
// (Run's `c.runSnapshot(job)` inside the generic type's own methods)
// yields a per-instantiation *types.Func whose object identity differs
// from the Defs object the node table is keyed by, so every edge out
// of the dispatch loop silently vanished and the callbacks under it
// never went hot. Mapping through the receiver's Named.Origin() (a
// no-op for non-generic receivers and for origin methods themselves)
// restores identity; method identity, not signature, is what the
// byObj table is keyed on.
func originFunc(fn *types.Func) *types.Func {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return fn
	}
	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok {
		return fn
	}
	orig := named.Origin()
	if orig == named {
		return fn
	}
	for i := range orig.NumMethods() {
		if m := orig.Method(i); m.Name() == fn.Name() {
			return m
		}
	}
	return fn
}

// callbackCallee reports whether call invokes a registry callback —
// through a func-typed struct field, a map element, a local whose
// value came out of a map or was copied from a func-typed field (the
// nil-check-and-call spelling any careful author writes), a method on
// a module-declared interface held in a slot the host fills, or a func
// result of one of those host calls — and its printed description.
func (g *graph) callbackCallee(call *ast.CallExpr, n *node) (string, bool) {
	pass := g.pass
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		if s, ok := pass.TypesInfo.Selections[fun]; ok && s.Kind() == types.FieldVal {
			if isCallbackType(s.Type()) && !isCancelNamed(s.Obj().Name()) {
				return types.ExprString(fun), true
			}
			return "", false
		}
		if g.hostClientCall(fun) {
			return types.ExprString(fun), true
		}
		return g.ifaceMethodCallee(fun, n)
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
		if obj != nil && g.derivedIn(obj, n, func(m *node) map[types.Object]bool { return m.derived }) {
			return fun.Name, true
		}
	}
	return "", false
}

// ifaceMethodCallee handles the extension-point leg: recv.M(...) where
// M is a method of an interface declared in this module (cron's
// LeaderElection, fanout.Fanout — the interfaces whose implementations
// are host code) and recv is a value held AS the extension point: a
// field the host installs, a registry map entry, a parameter the host
// passes, or a local one hop from one of those.
func (g *graph) ifaceMethodCallee(sel *ast.SelectorExpr, n *node) (string, bool) {
	if !g.extensionPointMethod(sel) {
		return "", false
	}
	if !g.hostSuppliedIface(sel.X, n) {
		return "", false
	}
	return types.ExprString(sel), true
}

// extensionPointMethod: sel names a method of an interface declared in
// this module that counts as a registry callback — every gate of
// ifaceMethodCallee short of the receiver test, so a method VALUE
// taken from a host-supplied slot (pub := b.backend.Publish; pub(ev))
// is judged the same way as the call through it.
func (g *graph) extensionPointMethod(sel *ast.SelectorExpr) bool {
	s, ok := g.pass.TypesInfo.Selections[sel]
	if !ok {
		return false
	}
	fn, ok := s.Obj().(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type()
	if ptr, ok := recv.(*types.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok {
		return false // an unnamed inline interface has no declaring package to judge
	}
	if _, ok := named.Underlying().(*types.Interface); !ok {
		return false // concrete method: package-local code, reached through the edge flood
	}
	if !g.isModulePkg(named.Obj().Pkg()) {
		return false // stdlib or third-party interface: data plumbing, not an app callback
	}
	if named.Obj().Pkg().Name() == "main" {
		// A command declares no extension point: it IS the host, and an
		// interface it declares is a seam over its own wiring
		// (examples/site's dashIslandPusher over *island.Manager).
		return false
	}
	if !fn.Exported() {
		// An unexported method can only be implemented inside this
		// package: the interface is a test seam over package code
		// (battery/service's toolRunner over exec), and the edge flood
		// already reaches every implementation.
		return false
	}
	if readMethodName.MatchString(fn.Name()) {
		return false // the loop's own input plumbing (Read/Scan/Recv/...), not a registry callback
	}
	if plumbingName.MatchString(fn.Name()) {
		return false // the data-access/teardown spellings inherited from stdlib backends
	}
	if sig.Params().Len() == 0 && sig.Results().Len() == 1 {
		return false // accessor-shaped (c.ID, agent.Info): a value query, not an event callback
	}
	return true
}

// hostSuppliedIface: the receiver expression holds the interface as a
// host-filled slot — a struct field, a registry map entry, a parameter
// (of this function or one it closes over), or a local one hop from
// those. A type-assertion local (BatteryManager's optional-lifecycle
// narrowing) and a self-constructed value (a call result) are not: the
// first is the boot/shutdown path the coordinator fixes directly, the
// second is a package-chosen implementation whose panic the edge flood
// already reaches.
func (g *graph) hostSuppliedIface(x ast.Expr, n *node) bool {
	switch v := ast.Unparen(x).(type) {
	case *ast.SelectorExpr:
		if s, ok := g.pass.TypesInfo.Selections[v]; ok && s.Kind() == types.FieldVal {
			_, isField := s.Obj().(*types.Var)
			return isField
		}
	case *ast.Ident:
		obj := g.pass.TypesInfo.ObjectOf(v)
		if obj == nil {
			return false
		}
		for m := n; m != nil; m = m.owner {
			if m.params[obj] {
				return true
			}
		}
		return g.derivedIn(obj, n, func(m *node) map[types.Object]bool { return m.derivedIface })
	case *ast.IndexExpr:
		_, isMap := underlyingMap(g.pass.TypesInfo.TypeOf(v.X))
		return isMap
	}
	return false
}

// derivedIn walks the owner chain: a local marked derived in an
// ancestor function is still that value when a closure captures it —
// Acquire's release func is defined in runTick and invoked in a
// goroutine literal two levels down.
func (g *graph) derivedIn(obj types.Object, n *node, set func(*node) map[types.Object]bool) bool {
	for m := n; m != nil; m = m.owner {
		if set(m)[obj] {
			return true
		}
	}
	return false
}

// isModulePkg: pkg is the analyzed package or lives in its module —
// the repo whose extension points this rule polices. Stdlib ("io"),
// vendored, and third-party packages fail this test. With no module
// resolvable (a driver that reports none) only same-package interfaces
// qualify.
func (g *graph) isModulePkg(pkg *types.Package) bool {
	if pkg == nil {
		return false
	}
	if pkg.Path() == g.pass.Pkg.Path() {
		return true
	}
	if g.modPath == "" {
		return false
	}
	return pkg.Path() == g.modPath || strings.HasPrefix(pkg.Path(), g.modPath+"/")
}

var cancelFieldName = regexp.MustCompile(`(?i)^cancel`)

// isCancelNamed: a func-typed field whose name says cancel (cancel,
// cancelFn, cancelFunc, cancelTurn, ...). The cancel-cause slots the
// repo stores (context.WithCancelCause cancels — kiln's AdapterStore
// cancelFn) are context plumbing exactly like context.CancelFunc:
// they never run app code, however func(error)-shaped they look, and
// locals copied from them inherit that.
func isCancelNamed(name string) bool {
	return cancelFieldName.MatchString(name)
}

// derivedVars records the variables whose value comes out of a map
// (`v, ok := m[k]`, the value variable of `for _, v := range m`), is
// copied from a callback-typed struct field (`gate := t.Gate`), or is a
// func-typed result of a callback call (`held, release, err :=
// leader.Acquire(ctx)` — the host built that func, so calling it is
// calling host code). Those are registry callbacks even when called
// through the local's name; infrastructure-shaped fields (printf
// loggers, handlers, clocks, CancelFuncs) copied to a local stay
// plumbing.
func (g *graph) derivedVars(body *ast.BlockStmt, n *node) map[types.Object]bool {
	pass := g.pass
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
				if i >= len(x.Lhs) {
					continue
				}
				if idx, ok := ast.Unparen(rhs).(*ast.IndexExpr); ok {
					if _, isMap := underlyingMap(pass.TypesInfo.TypeOf(idx.X)); !isMap {
						continue
					}
					if id, ok := x.Lhs[i].(*ast.Ident); ok {
						if obj := pass.TypesInfo.Defs[id]; obj != nil {
							out[obj] = true
						}
					}
					continue
				}
				if sel, ok := ast.Unparen(rhs).(*ast.SelectorExpr); ok {
					if s, ok := pass.TypesInfo.Selections[sel]; ok {
						// A func-typed field, or a method VALUE off a
						// host-installed extension-point slot: `pub :=
						// b.backend.Publish` holds the host's code one
						// hop from the field, exactly like the field.
						if s.Kind() == types.FieldVal && isCallbackType(s.Type()) && !isCancelNamed(s.Obj().Name()) ||
							s.Kind() == types.MethodVal && isCallbackType(s.Type()) && g.extensionPointMethod(sel) && g.hostSuppliedIface(sel.X, n) {
							if id, ok := x.Lhs[i].(*ast.Ident); ok {
								if obj := pass.TypesInfo.Defs[id]; obj != nil {
									out[obj] = true
								}
							}
						}
					}
					continue
				}
				if call, ok := ast.Unparen(rhs).(*ast.CallExpr); ok {
					// A func result of a host call: `release` out of
					// Acquire. Multi-value assigns map every func-typed
					// left side to the one call on the right.
					if _, ok := g.callbackCallee(call, n); ok || g.slotAccessor(call) {
						for _, lhs := range x.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								if obj := pass.TypesInfo.Defs[id]; obj != nil && isCallbackType(obj.Type()) {
									out[obj] = true
								}
							}
						}
					}
				}
			}
		}
		return true
	})
	return out
}

// slotAccessor reports whether call reaches a package-local accessor of
// a host-installed callback slot: a function whose body returns a field
// (not a cancel-named one) or dereferences a pointer, the way an
// atomic.Pointer[func(...)] slot is read. A local assigned from one holds
// the host's callback exactly as a local copied from the field does:
// island.Manager's heartbeat read its OnPresenceChange hook through
// presenceCallback() and called it bare (2026-10-27 bug bash R9). The
// caller marks only callback-typed locals, so the result type needs no
// check here. An accessor that builds and returns its own closure returns
// neither shape and stays quiet, and a return inside a nested literal
// answers for that literal, not the accessor.
func (g *graph) slotAccessor(call *ast.CallExpr) bool {
	fn := g.decls[g.callTarget(call.Fun)]
	if fn == nil {
		return false
	}
	found := false
	ast.Inspect(fn.Body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok || found {
			return false
		}
		ret, ok := x.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, res := range ret.Results {
			switch r := ast.Unparen(res).(type) {
			case *ast.SelectorExpr:
				s, ok := g.pass.TypesInfo.Selections[r]
				found = found || ok && s.Kind() == types.FieldVal && !isCancelNamed(s.Obj().Name())
			case *ast.StarExpr:
				found = true
			}
		}
		return true
	})
	return found
}

// derivedIfaceVars records locals one hop from an interface slot the
// host fills: `be := f` (a parameter), `le := s.leader` (a field), or
// `b := s.backends[name]` (a registry entry). Type assertions and call
// results are deliberately absent — see hostSuppliedIface.
func (g *graph) derivedIfaceVars(body *ast.BlockStmt, n *node) map[types.Object]bool {
	pass := g.pass
	out := map[types.Object]bool{}
	ast.Inspect(body, func(x ast.Node) bool {
		if x, ok := x.(*ast.AssignStmt); ok {
			for i, rhs := range x.Rhs {
				if i >= len(x.Lhs) {
					continue
				}
				id, ok := x.Lhs[i].(*ast.Ident)
				if !ok {
					continue
				}
				obj := pass.TypesInfo.Defs[id]
				if obj == nil {
					continue
				}
				switch ast.Unparen(rhs).(type) {
				case *ast.SelectorExpr:
					sel := ast.Unparen(rhs).(*ast.SelectorExpr)
					if s, ok := pass.TypesInfo.Selections[sel]; ok && s.Kind() == types.FieldVal {
						out[obj] = true
					}
				case *ast.Ident:
					other := pass.TypesInfo.ObjectOf(ast.Unparen(rhs).(*ast.Ident))
					if other != nil {
						for m := n; m != nil; m = m.owner {
							if m.params[other] {
								out[obj] = true
							}
						}
					}
				case *ast.IndexExpr:
					idx := ast.Unparen(rhs).(*ast.IndexExpr)
					if _, isMap := underlyingMap(pass.TypesInfo.TypeOf(idx.X)); isMap {
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
	var loops []*node
	for _, n := range g.all {
		if n.readLoop && !n.handlerShpd {
			loops = append(loops, n)
		}
	}
	g.hot = g.flood(append(loops, g.seeds...))
	g.async = g.flood(g.seeds)
}

func (g *graph) flood(seeds []*node) map[*node]bool {
	hot := map[*node]bool{}
	var queue []*node
	add := func(n *node) {
		if !hot[n] {
			hot[n] = true
			queue = append(queue, n)
		}
	}
	for _, s := range seeds {
		add(s)
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if g.protected(n) {
			// A recover on this frame catches every panic its
			// synchronous callees raise: the flood stops here. A
			// goroutine it launches is a seed of its own. This is the
			// a2a fix spelling (6fce08fe6): the run goroutine's deferred
			// recover covers finalize and everything finalize calls.
			continue
		}
		for _, e := range n.edges {
			add(e)
		}
	}
	return hot
}

func (g *graph) report() {
	for _, n := range g.all {
		for _, c := range n.calls {
			if c.launched {
				// A callback on its own goroutine has no recover by
				// construction: the enclosing function's deferred
				// recover runs on a different stack, hot or not.
				g.pass.Reportf(c.call.Pos(),
					"recovercallback: %s is invoked with no recover in scope; it is launched on its own goroutine, where a panicking callback kills the process",
					c.desc)
				continue
			}
			if !g.hot[n] || g.protected(n) {
				continue
			}
			if c.asyncOnly && !g.async[n] {
				continue
			}
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
		switch x := x.(type) {
		case *ast.FuncLit:
			// A recover deferred inside a nested literal runs on that
			// literal's own stack — a goroutine's recover cannot catch
			// a panic in this body. Deferred literals are visited from
			// their DeferStmt below.
			return false
		case *ast.DeferStmt:
			ast.Inspect(x.Call, func(y ast.Node) bool {
				if call, ok := y.(*ast.CallExpr); ok {
					if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && id.Name == "recover" {
						found = true
					}
				}
				return !found
			})
		}
		return !found
	})
	return found
}

// directRecover: a recover() call in the node's own linear body, not
// under a defer and not inside a nested literal. Useless for the node
// itself, but effective when the node is the DEFERRED function —
// that is the named-guard spelling (defer guard()).
func directRecover(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(x ast.Node) bool {
		if _, ok := x.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := x.(*ast.CallExpr); ok {
			if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && id.Name == "recover" {
				found = true
			}
		}
		return !found
	})
	return found
}

// containsReadLoop reports whether body has a for (or channel-range)
// loop that blocks on input: a Read/Scan/Recv/Decode-style call in the
// condition, a channel range, a receive ASSIGNED in the body, a
// select inside the loop, or a bare receive on the channel of a
// *time.Ticker/*time.Timer (`<-w.t.C`). A bare discard receive on an
// ordinary channel (`<-ready`) alone does not count — that is a
// coordination wait, not a dispatch.
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
	if call, ok := ast.Unparen(loop.Cond).(*ast.CallExpr); ok && loop.Cond != nil {
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && readMethodName.MatchString(sel.Sel.Name) {
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
			// in this repo selects or reads instead — UNLESS the
			// channel is a ticker's or timer's .C, which no
			// coordination wait looks like: that is a timer-driven
			// dispatcher whose callback panic kills the process.
			if u, ok := ast.Unparen(y.X).(*ast.UnaryExpr); ok && isReceive(u) && isTimerChan(pass, u.X) {
				reads = true
			}
		case *ast.AssignStmt:
			for _, rhs := range y.Rhs {
				if u, ok := ast.Unparen(rhs).(*ast.UnaryExpr); ok && isReceive(u) {
					reads = true
				}
				if call, ok := ast.Unparen(rhs).(*ast.CallExpr); ok {
					if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && readMethodName.MatchString(sel.Sel.Name) {
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

// isTimerChan: `<-w.t.C` — the channel of a *time.Ticker or
// *time.Timer. A periodic dispatch loop written over a ticker is a
// timer-driven dispatcher; no singleflight coordination wait receives
// on a .C selector.
func isTimerChan(pass *analysis.Pass, e ast.Expr) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "C" {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	return astx.IsNamed(t, "time", "Ticker") || astx.IsNamed(t, "time", "Timer")
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
	if isHandlerType(t) || isCancelFunc(t) || isClockType(t) || isPrintfType(t) || isLoggerAccessor(t) {
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

// isLoggerAccessor: `func() *slog.Logger`, the owning App's logger
// handed down as a getter (BatteryManager.logger). Logging plumbing,
// like the printf sinks.
func isLoggerAccessor(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	ptr, ok := sig.Results().At(0).Type().(*types.Pointer)
	return ok && astx.IsNamed(ptr.Elem(), "log/slog", "Logger")
}

func isClockType(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	return astx.IsNamed(sig.Results().At(0).Type(), "time", "Time")
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
	if !astx.IsNamed(p0.Type(), "net/http", "ResponseWriter") {
		return false
	}
	ptr, ok := p1.Type().(*types.Pointer)
	if !ok {
		return false
	}
	return astx.IsNamed(ptr.Elem(), "net/http", "Request")
}

func isCancelFunc(t types.Type) bool {
	return astx.IsNamed(t, "context", "CancelFunc")
}

func isAfterFunc(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
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

// clientSendMethods: the *http.Client methods that run the client's
// Transport and CheckRedirect.
var clientSendMethods = map[string]bool{"Do": true, "Get": true, "Post": true, "Head": true, "PostForm": true}

// hostClientCall: `p.client.Do(req)` where p.client is an *http.Client
// field the host can fill. The method is concrete stdlib code, but it
// runs the host's Transport (a RoundTripper the host wrote) and its
// CheckRedirect: host code by construction, the PushOptions.Client
// delivery core/a2a's push drain ran bare until 6fce08fe6. A client the
// package builds itself from a literal runs net/http's own transport and
// stays quiet.
func (g *graph) hostClientCall(sel *ast.SelectorExpr) bool {
	if !clientSendMethods[sel.Sel.Name] {
		return false
	}
	s, ok := g.pass.TypesInfo.Selections[sel]
	if !ok || s.Kind() != types.MethodVal || !isHTTPClient(s.Recv()) {
		return false
	}
	recv, ok := ast.Unparen(sel.X).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	rs, ok := g.pass.TypesInfo.Selections[recv]
	if !ok || rs.Kind() != types.FieldVal {
		return false
	}
	v, ok := rs.Obj().(*types.Var)
	return ok && g.hostClients[v]
}

func isHTTPClient(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	return astx.IsNamed(t, "net/http", "Client")
}

// hostClientFields collects the *http.Client struct fields declared in
// this package that a host-supplied value can fill: an exported field
// (the host sets it directly, an options struct's Client), or one
// assigned anywhere in the package — keyed composite literal or field
// assignment — from something other than a client the package builds:
// a parameter, a call result, another struct's field. A field filled
// only by `&http.Client{...}` literals or http.DefaultClient, directly
// or through a local every assignment of which is one, runs net/http's
// own code and is not host code.
func hostClientFields(pass *analysis.Pass) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok {
				ast.Inspect(gd, func(x ast.Node) bool {
					if fld, ok := x.(*ast.Field); ok {
						for _, name := range fld.Names {
							if v, ok := pass.TypesInfo.Defs[name].(*types.Var); ok && v.IsField() && v.Exported() && isHTTPClient(v.Type()) {
								out[v] = true
							}
						}
					}
					return true
				})
				continue
			}
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(x ast.Node) bool {
				switch x := x.(type) {
				case *ast.CompositeLit:
					for _, el := range x.Elts {
						kv, ok := el.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						id, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok && v.IsField() && isHTTPClient(v.Type()) && !builtClient(pass, fd.Body, kv.Value, 0) {
							out[v] = true
						}
					}
				case *ast.AssignStmt:
					if len(x.Lhs) != len(x.Rhs) {
						return true
					}
					for i, lhs := range x.Lhs {
						sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
						if !ok {
							continue
						}
						rs, ok := pass.TypesInfo.Selections[sel]
						if !ok || rs.Kind() != types.FieldVal {
							continue
						}
						if v, ok := rs.Obj().(*types.Var); ok && isHTTPClient(v.Type()) && !builtClient(pass, fd.Body, x.Rhs[i], 0) {
							out[v] = true
						}
					}
				}
				return true
			})
		}
	}
	return out
}

// builtClient: e is a client the package built — an http.Client
// composite literal (addressed or not), http.DefaultClient, nil, or a
// local of the same function every assignment of which is one of those.
// A local with no assignment in the body is a parameter or a capture:
// the caller's value, so not built.
func builtClient(pass *analysis.Pass, body *ast.BlockStmt, e ast.Expr, depth int) bool {
	e = ast.Unparen(e)
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = ast.Unparen(u.X)
	}
	switch e := e.(type) {
	case *ast.CompositeLit:
		return isHTTPClient(pass.TypesInfo.TypeOf(e))
	case *ast.SelectorExpr:
		obj, ok := pass.TypesInfo.ObjectOf(e.Sel).(*types.Var)
		return ok && obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == "DefaultClient"
	case *ast.Ident:
		if e.Name == "nil" {
			return true
		}
		v, ok := pass.TypesInfo.ObjectOf(e).(*types.Var)
		if !ok || v.IsField() || depth > 2 || v.Pkg() == nil || v.Parent() == v.Pkg().Scope() {
			return false
		}
		srcs := 0
		allBuilt := true
		ast.Inspect(body, func(x ast.Node) bool {
			switch x := x.(type) {
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || pass.TypesInfo.ObjectOf(id) != v {
						continue
					}
					srcs++
					if len(x.Lhs) != len(x.Rhs) {
						allBuilt = false
						continue
					}
					allBuilt = allBuilt && builtClient(pass, body, x.Rhs[i], depth+1)
				}
			case *ast.ValueSpec:
				for i, name := range x.Names {
					if pass.TypesInfo.ObjectOf(name) == v && i < len(x.Values) {
						srcs++
						allBuilt = allBuilt && builtClient(pass, body, x.Values[i], depth+1)
					}
				}
			}
			return true
		})
		return srcs > 0 && allBuilt
	}
	return false
}
