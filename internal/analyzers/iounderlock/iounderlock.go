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
//   - calls inside nested function literals: a sync.Once body, a
//     closure handed to `go` or deferred, or a comparator runs outside
//     the linear region (and once.Do's own serialization is the point);
//   - `go` launches and deferred calls under the lock: the launch does
//     not block, and the deferred call runs at exit;
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
	for _, fn := range fns {
		if strings.HasSuffix(pass.Fset.Position(fn.Pos()).Filename, "_test.go") {
			continue
		}
		a.checkBody(fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				a.checkBody(lit.Body)
			}
			return true
		})
	}
	return nil, nil
}

type analyzer struct {
	pass *analysis.Pass
	io   map[*types.Func]ioKind
}

// linearCalls visits the calls body makes itself: not inside nested
// literals, not launched with `go`, not deferred.
func linearCalls(body *ast.BlockStmt, visit func(*ast.CallExpr)) {
	skip := map[*ast.CallExpr]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.GoStmt:
			skip[n.Call] = true
		case *ast.DeferStmt:
			skip[n.Call] = true
		case *ast.CallExpr:
			if !skip[n] {
				visit(n)
			}
		}
		return true
	})
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
			linearCalls(fn.Body, func(call *ast.CallExpr) {
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
	linearCalls(body, func(call *ast.CallExpr) {
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
		key, since, held := heldAt(events, c.call.Pos())
		if !held {
			continue
		}
		if !c.kind.Net && !checkedBetween(checks, key, since, c.call.Pos()) {
			// Database I/O under a mutex that serializes the work
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

// heldAt reports a mutex held at pos: the last event for its key before
// pos is an acquisition. Branches flatten by source position, which can
// only under-report (a release on one branch reads as a release). A
// deferred Unlock never appears as an event, so Lock + defer Unlock
// holds to the end of the body.
func heldAt(events []lockEvent, pos token.Pos) (string, token.Pos, bool) {
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
	for _, key := range order {
		if e := last[key]; e.lock {
			return key, e.pos, true
		}
	}
	return "", token.NoPos, false
}

func calleeName(info *types.Info, call *ast.CallExpr) string {
	if fn, ok := typeutil.Callee(info, call).(*types.Func); ok {
		return fn.Name()
	}
	return types.ExprString(call.Fun)
}
