// Package iounderlock catches database and network I/O made while a
// sync mutex is held.
//
// The bug: a mutex that guards a little in-memory state (a sweep
// timestamp, a cached key set, a discovery document) is held across the
// slow call that refreshes it. Every other caller of the mutex then
// waits on the network or the database: one slow or failing DELETE, one
// IdP that takes thirty seconds to answer, and every login or mint in the
// process queues behind it. callbackunderlock exempts named calls (its
// concern is app callbacks), so these slipped through:
//   - 5ade7a35: SQLMagicLinkTokenStore.sweepExpiredOnCreate held sweepMu
//     (Lock + defer Unlock) across the expiry DELETE, and a failed sweep
//     left lastSweep unadvanced, so the DELETE ran on every mint with every
//     mint queued on the mutex;
//   - ce72591b item 10: jwksCache.getKey held c.mu across fetchLocked, an
//     http.Client.Do of the JWKS document, and OIDCProvider.ensureDiscovery
//     held p.mu across the discovery fetch.
//
// It fires on a call made between a sync.Mutex/RWMutex Lock (or RLock)
// and its Unlock — a deferred Unlock holds the lock to the end of the
// body — that is:
//   - database/sql I/O on a *DB, *Conn, *Tx or *Stmt (Exec, Query,
//     QueryRow, Begin, Prepare, Ping and their Context forms);
//   - an *http.Client send (Do, Get, Post, Head, PostForm) or the
//     http.Get/Post/Head/PostForm sugar;
//   - a net.Dial* call or a *net.Dialer Dial/DialContext;
//   - a function carrying a doesIO fact: one whose own body (outside
//     nested literals and `go` launches) makes one of the calls above or
//     calls another doesIO function, computed to a fixed point inside the
//     package and exported across packages, so getKey → fetchLocked →
//     c.httpClient.Do counts.
//
// Every mutex held at the call is judged (acquisition order), not just
// the first: a lock taken earlier on another object must not hide the
// throttle mutex taken after it.
//
// Network I/O fires under any held mutex. Database I/O fires only when
// the mutex guards a throttle or TTL timestamp: between the acquisition
// and the I/O, a time.Time field of the mutex's owner (the root of the
// lock expression, s in s.sweepMu) is read in a comparison, a time.Time
// method, or time.Since/Until (`now.Sub(s.lastSweep) < interval`,
// `now.Before(s.nextSweep)`). That gate is the measured line, not a
// guess: the first cut fired 50 times over the tree, almost all of them
// mutexes that serialize the database work itself (SQLite single
// writers in the harness and kiln, framework/access's role transitions,
// schema-ensure and migration locks, an example app's engine lock). With
// the gate it fires twice, both the sweep shape: battery/auth's session
// sweep (the magic-link store's twin, fixed) and the durable scheduler's
// retention sweep (allow-marked: only its own loop takes that mutex).
//
// The fix spelling: claim the slot under the lock (`s.lastSweep = now`,
// a fetching flag) and do the I/O outside it, then publish the result
// under the lock; or serialize the I/O itself with a one-slot channel
// gate a waiter can leave at its own deadline (ce72591b's JWKS gate).
//
// Silent postures, deliberately:
//   - database I/O under a mutex with no throttle timestamp read before
//     it, or one read only after the I/O, or a timestamp on another
//     object (a job's startedAt): the mutex serializes the work, which is
//     a design choice, not the refresh-under-cache-lock bug;
//   - calls inside function literals that do not run in the region: a
//     closure handed to `go` or deferred, to time/context.AfterFunc
//     (which run it later on their own goroutine), or to sync.Once.Do
//     (once.Do's own serialization is the point), and one merely stored
//     or returned. A literal called in place or passed straight to any
//     other call IS the region — runNow(func(){ … }) runs under the
//     lock — as is a package-level function literal checked on its own;
//   - a mutex reached through an alias or a custom wrapper type: only
//     sync.Mutex/RWMutex selectors are tracked (callbackunderlock's
//     posture);
//   - interface methods (a store or executor interface): the
//     implementation behind it is unknown, and this repo's in-memory
//     stores sit behind the same interfaces as its SQL ones;
//   - deliberate serialization, such as a SQLite single-writer mutex
//     around a write: mark it `//gofastr:allow(iounderlock) <why>`;
//   - _test.go files.
package iounderlock

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var Analyzer = &analysis.Analyzer{
	Name:      "iounderlock",
	Doc:       "report database/network I/O made while a sync mutex is held",
	Run:       run,
	FactTypes: []analysis.Fact{(*doesIO)(nil)},
}

// doesIO marks a function whose own body performs database or network
// I/O, directly or through another doesIO function. Via names the I/O.
type doesIO struct {
	Via string
	Net bool // network I/O (an HTTP send, a dial), not database I/O
}

func (*doesIO) AFact() {}

func (f *doesIO) String() string { return fmt.Sprintf("doesIO(%s, net=%t)", f.Via, f.Net) }

// ioKind is what a call does: Via names it ("" for no I/O), Net says
// whether it is network I/O.
type ioKind struct {
	Via string
	Net bool
}

func run(pass *analysis.Pass) (any, error) {
	var fns []*ast.FuncDecl
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				fns = append(fns, fn)
			}
		}
	}
	a := &analyzer{pass: pass, io: map[*types.Func]ioKind{}}
	a.collect(fns)
	for _, obj := range slices.SortedFunc(maps.Keys(a.io), func(x, y *types.Func) int { return int(x.Pos() - y.Pos()) }) {
		k := a.io[obj]
		pass.ExportObjectFact(obj, &doesIO{Via: k.Via, Net: k.Net})
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, fn := range astx.AllFuncs(f) {
			a.checkBody(astx.BodyOf(fn))
		}
	}
	return nil, nil
}

type analyzer struct {
	pass *analysis.Pass
	io   map[*types.Func]ioKind
}

// linearCalls visits the calls body makes itself: not launched with
// `go`, not deferred, and not inside a function literal other than one
// called in place or passed straight to a call — a synchronous closure
// argument runs in this region. A literal handed to `go` or defer, or
// to a function that runs it later (time/context.AfterFunc) or
// serializes it itself (sync.Once.Do), does not.
func (a *analyzer) linearCalls(body *ast.BlockStmt, visit func(*ast.CallExpr)) {
	launched := map[*ast.FuncLit]bool{} // inside a go/defer call's subtree
	runsHere := map[*ast.FuncLit]bool{} // called in place, or a direct call argument
	var async []ast.Expr
	for _, stmt := range body.List {
		collectLiterals(a.pass, stmt, launched, runsHere, &async)
	}
	for _, call := range async {
		ast.Inspect(call, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				launched[lit] = true
			}
			return true
		})
	}
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return !launched[n] && runsHere[n]
		case *ast.GoStmt:
			for _, arg := range n.Call.Args {
				ast.Inspect(arg, walk)
			}
			return false
		case *ast.DeferStmt:
			for _, arg := range n.Call.Args {
				ast.Inspect(arg, walk)
			}
			return false
		case *ast.CallExpr:
			visit(n)
		}
		return true
	}
	ast.Inspect(body, walk)
}

// collectLiterals gathers the function literals of stmt's subtree:
// launched collects the `go`/defer call subtrees (marked wholesale,
// arguments included — whether a callee runs a passed closure here is
// unknowable, and the quiet direction is right), runsHere the literals
// that run synchronously in this region: called in place, or a direct
// argument of a call that is not one of the run-it-later spellings.
func collectLiterals(pass *analysis.Pass, stmt ast.Stmt, launched, runsHere map[*ast.FuncLit]bool, async *[]ast.Expr) {
	ast.Inspect(stmt, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GoStmt:
			*async = append(*async, n.Call)
			return false
		case *ast.DeferStmt:
			*async = append(*async, n.Call)
			return false
		case *ast.CallExpr:
			if lit, ok := ast.Unparen(n.Fun).(*ast.FuncLit); ok {
				runsHere[lit] = true
			}
			if !runsItLater(pass, n) {
				for _, arg := range n.Args {
					if lit, ok := ast.Unparen(arg).(*ast.FuncLit); ok {
						runsHere[lit] = true
					}
				}
			}
		}
		return true
	})
}

// runsItLater: a call that does NOT run its function argument in this
// region: time.AfterFunc and context.AfterFunc run it on their own
// goroutine, sync.Once.Do serializes and runs it under its own lock.
func runsItLater(pass *analysis.Pass, call *ast.CallExpr) bool {
	fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "time", "context":
		return fn.Name() == "AfterFunc"
	case "sync":
		return fn.Name() == "Do" && recvName(fn) == "Once"
	}
	return false
}

// collect computes the package's doesIO functions to a fixed point.
func (a *analyzer) collect(fns []*ast.FuncDecl) {
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			obj, ok := a.pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok {
				continue
			}
			if _, done := a.io[obj]; done {
				continue
			}
			var k ioKind
			a.linearCalls(fn.Body, func(call *ast.CallExpr) {
				// Network I/O wins: it is the arm that fires under any lock.
				if c := a.ioOf(call); c.Via != "" && (k.Via == "" || c.Net && !k.Net) {
					k = c
				}
			})
			if k.Via != "" {
				a.io[obj] = k
				changed = true
			}
		}
	}
}

// ioOf names the I/O call performs; a zero ioKind when it performs none.
func (a *analyzer) ioOf(call *ast.CallExpr) ioKind {
	fn, ok := typeutil.Callee(a.pass.TypesInfo, call).(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ioKind{}
	}
	if d := directIO(fn); d.Via != "" {
		return d
	}
	fn = fn.Origin()
	if k, ok := a.io[fn]; ok {
		return ioKind{Via: fn.Name() + " → " + k.Via, Net: k.Net}
	}
	if fn.Pkg() == a.pass.Pkg {
		return ioKind{}
	}
	var f doesIO
	if a.pass.ImportObjectFact(fn, &f) {
		return ioKind{Via: fn.Pkg().Name() + "." + fn.Name() + " → " + f.Via, Net: f.Net}
	}
	return ioKind{}
}

var sqlMethods = map[string]bool{
	"Exec": true, "ExecContext": true, "Query": true, "QueryContext": true,
	"QueryRow": true, "QueryRowContext": true, "Begin": true, "BeginTx": true,
	"Prepare": true, "PrepareContext": true, "Ping": true, "PingContext": true,
}

var sqlRecv = map[string]bool{"DB": true, "Conn": true, "Tx": true, "Stmt": true}

var clientSend = map[string]bool{"Do": true, "Get": true, "Post": true, "Head": true, "PostForm": true}

// directIO names a standard-library call that does database or network
// I/O.
func directIO(fn *types.Func) ioKind {
	path := fn.Pkg().Path()
	recv := recvName(fn)
	switch {
	case path == "database/sql" && sqlRecv[recv] && sqlMethods[fn.Name()]:
		return ioKind{Via: fmt.Sprintf("(*sql.%s).%s", recv, fn.Name())}
	case path == "net/http" && recv == "Client" && clientSend[fn.Name()]:
		return ioKind{Via: "(*http.Client)." + fn.Name(), Net: true}
	case path == "net/http" && recv == "" && clientSend[fn.Name()] && fn.Name() != "Do":
		return ioKind{Via: "http." + fn.Name(), Net: true}
	case path == "net" && recv == "" && strings.HasPrefix(fn.Name(), "Dial"):
		return ioKind{Via: "net." + fn.Name(), Net: true}
	case path == "net" && recv == "Dialer" && strings.HasPrefix(fn.Name(), "Dial"):
		return ioKind{Via: "(*net.Dialer)." + fn.Name(), Net: true}
	}
	return ioKind{}
}

// recvName is the receiver's type name for a concrete method, "" for a
// package function or an interface method.
func recvName(fn *types.Func) string {
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return ""
	}
	t := sig.Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return ""
	}
	if _, isIface := n.Underlying().(*types.Interface); isIface {
		return ""
	}
	return n.Obj().Name()
}

// lockEvent is one acquisition or release, in source order, keyed by the
// printed mutex expression ("s.mu").
type lockEvent struct {
	pos  token.Pos
	key  string
	lock bool
}

// checkBody reports I/O calls made while a mutex is held in body (not in
// its nested literals, which are checked on their own).
func (a *analyzer) checkBody(body *ast.BlockStmt) {
	var events []lockEvent
	type ioCall struct {
		call *ast.CallExpr
		kind ioKind
	}
	var ios []ioCall
	a.linearCalls(body, func(call *ast.CallExpr) {
		if key, lock, ok := a.mutexOp(call); ok {
			events = append(events, lockEvent{pos: call.Pos(), key: key, lock: lock})
			return
		}
		if k := a.ioOf(call); k.Via != "" {
			ios = append(ios, ioCall{call, k})
		}
	})
	if len(events) == 0 {
		return
	}
	checks := a.timestampChecks(body)
	for _, c := range ios {
		held := heldLocks(events, c.call.Pos())
		if len(held) == 0 {
			continue
		}
		// Network I/O fires under any held mutex. Database I/O fires
		// under the first held mutex (acquisition order) whose owner's
		// throttle timestamp was read between its acquisition and the
		// call — a lock taken earlier on another object must not hide
		// the throttle mutex taken after it.
		var key string
		for _, lk := range held {
			if c.kind.Net || checkedBetween(checks, lk.key, lk.pos, c.call.Pos()) {
				key = lk.key
				break
			}
		}
		if key == "" {
			// Database I/O under mutexes that serialize the work
			// itself (a SQLite single writer, a role transition, a
			// schema ensure) is a design choice. The bug is the mutex
			// that guards a throttle or TTL timestamp, held across the
			// slow refresh it gates.
			continue
		}
		what := "a throttle timestamp the I/O refreshes"
		if c.kind.Net {
			what = "in-process state"
		}
		a.pass.Reportf(c.call.Pos(), "iounderlock: %s does I/O (%s) while %s, which guards %s, is held; every other user of the mutex waits on it: claim the slot under the lock and do the I/O outside it, or gate it with a one-slot channel",
			calleeName(a.pass.TypesInfo, c.call), c.kind.Via, key, what)
	}
}

// tsCheck is a read of a time.Time field rooted at root, in a comparison
// or a time method (`now.Sub(s.lastSweep) < interval`,
// `now.Before(s.nextSweep)`, `c.fetchedAt.IsZero()`).
type tsCheck struct {
	pos  token.Pos
	root string
}

// timestampChecks finds the throttle/TTL reads in body: a time.Time
// struct field used as the receiver or argument of a time.Time method, or
// as an operand of a comparison.
func (a *analyzer) timestampChecks(body *ast.BlockStmt) []tsCheck {
	info := a.pass.TypesInfo
	var out []tsCheck
	field := func(e ast.Expr) (string, bool) {
		sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		s, ok := info.Selections[sel]
		if !ok || s.Kind() != types.FieldVal || !isTime(s.Type()) {
			return "", false
		}
		return rootOf(sel), true
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			sel, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr)
			if !ok {
				return true
			}
			fn, ok := typeutil.Callee(info, n).(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "time" {
				return true
			}
			// A time.Time method (now.Sub(s.last), s.next.IsZero()) or
			// time.Since/Until(s.last).
			if recvName(fn) != "Time" && fn.Name() != "Since" && fn.Name() != "Until" {
				return true
			}
			for _, e := range append([]ast.Expr{sel.X}, n.Args...) {
				if root, ok := field(e); ok {
					out = append(out, tsCheck{n.Pos(), root})
				}
			}
		case *ast.BinaryExpr:
			switch n.Op {
			case token.LSS, token.GTR, token.LEQ, token.GEQ, token.EQL, token.NEQ:
				for _, e := range []ast.Expr{n.X, n.Y} {
					if root, ok := field(e); ok {
						out = append(out, tsCheck{n.Pos(), root})
					}
				}
			}
		}
		return true
	})
	return out
}

// checkedBetween reports a timestamp check on the mutex owner (the root
// of key, "s" in "s.sweepMu") between the acquisition and the I/O.
func checkedBetween(checks []tsCheck, key string, from, to token.Pos) bool {
	root, _, _ := strings.Cut(key, ".")
	for _, c := range checks {
		if c.root == root && c.pos > from && c.pos < to {
			return true
		}
	}
	return false
}

func isTime(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "time" && n.Obj().Name() == "Time"
}

// rootOf is the leftmost identifier of a selector chain (s.a.b → s).
func rootOf(e ast.Expr) string {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			e = x.X
		default:
			return ""
		}
	}
}

// mutexOp recognizes sync.Mutex/RWMutex Lock, RLock, Unlock and RUnlock.
func (a *analyzer) mutexOp(call *ast.CallExpr) (key string, lock, ok bool) {
	sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSel {
		return "", false, false
	}
	switch sel.Sel.Name {
	case "Lock", "RLock", "Unlock", "RUnlock":
	default:
		return "", false, false
	}
	s, found := a.pass.TypesInfo.Selections[sel]
	if !found || s.Obj().Pkg() == nil || s.Obj().Pkg().Path() != "sync" {
		return "", false, false
	}
	return types.ExprString(sel.X), sel.Sel.Name == "Lock" || sel.Sel.Name == "RLock", true
}

// heldLocks reports the mutexes held at pos, in acquisition order: the
// last event for each key before pos is an acquisition. Branches
// flatten by source position, which can only under-report (a release
// on one branch reads as a release). A deferred Unlock never appears
// as an event, so Lock + defer Unlock holds to the end of the body.
func heldLocks(events []lockEvent, pos token.Pos) []lockEvent {
	last := map[string]lockEvent{}
	var order []string
	for _, e := range events {
		if e.pos >= pos {
			break
		}
		if _, seen := last[e.key]; !seen {
			order = append(order, e.key)
		}
		last[e.key] = e
	}
	var held []lockEvent
	for _, key := range order {
		if e := last[key]; e.lock {
			held = append(held, e)
		}
	}
	return held
}

func calleeName(info *types.Info, call *ast.CallExpr) string {
	if fn, ok := typeutil.Callee(info, call).(*types.Func); ok {
		return fn.Name()
	}
	return types.ExprString(call.Fun)
}
