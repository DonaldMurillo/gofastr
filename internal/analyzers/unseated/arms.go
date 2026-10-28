package unseated

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"golang.org/x/tools/go/analysis"
)

// RecheckAnalyzer is the principal re-check arm (see the package doc).
var RecheckAnalyzer = &analysis.Analyzer{
	Name: "unrechecked",
	Doc:  "reports a long-lived stream for a resolved principal whose delivery loops never re-check that principal, or whose connect-time authorization / tenant decision is never re-run on the delivery path",
	Run:  runRecheck,
}

// WriteDeadlineAnalyzer is the blocked-write arm (see the package doc).
var WriteDeadlineAnalyzer = &analysis.Analyzer{
	Name: "nowritedeadline",
	Doc:  "reports a client write inside a long-lived stream's park loop with no write deadline anywhere on the loop's path",
	Run:  runWriteDeadline,
}

const recheckMsg = "long-lived stream for a resolved principal delivers from this loop with no principal re-check on its path: a session revoked, a token expired, or a user deleted after connect keeps receiving (and, on a socket, keeps acting) until the client disconnects — call handler.RecheckPrincipal before each delivery and on an idle ticker and close on refusal (the framework/crud event stream and core/mcp GET spelling), or re-run the connect-time resolver inside the loop"

const decisionMsg = "connect-time decision %s is never re-run on this stream's delivery path: handler.RecheckPrincipal re-validates the identity, not this decision, so a principal whose access changes after connect (topic access removed, tenant left) keeps receiving — re-run it against the refreshed context before delivery and close or drop on refusal"

const deadlineMsg = "client write in a long-lived stream's park loop with no write deadline on its path: an evicted, revoked, or shutting-down stream blocked on a client that stopped reading never leaves (WriteTimeout 0 is the SSE-heavy app posture) — arm http.ResponseController.SetWriteDeadline (or conn.SetWriteDeadline) per write, or cut with a past deadline from whatever closes the stream (core/mcp's GET stream, core/a2a's sseStream.write, core/stream's SSEBroker cut)"

const staleDeadlineMsg = "write deadline armed on an http.ResponseController in a long-lived stream and never cleared: on HTTP/2 the deadline is a stream timer that resets the stream when it fires, write pending or not, so the stream dies in its first idle gap longer than the deadline — clear it once the write is flushed with rc.SetWriteDeadline(time.Time{}) (core/stream's SSEWriter, core/a2a's sseStream, core/mcp's GET stream)"

// resolverName: identity-reader spellings (nostore's vocabulary plus
// the harness claimsFrom).
var resolverName = regexp.MustCompile(`(?i)(currentuser|getuser$|getuser[^a]|sessionfrom|tokenscopes|requireowner|requireauth|verifytoken|readsession|getsession|loadsession|resolvesession|currentsession|allowsession|claimsfrom)`)

// identityFuncField: a func-typed field invoked with the request is a
// host-supplied resolver (a2a's Config.Owner, the SSE broker's
// Principal, rtc's Config.Authorize).
var identityFuncField = map[string]bool{
	"owner": true, "principal": true, "user": true, "caller": true,
	"identify": true, "identity": true, "authenticate": true,
	"subject": true, "authorize": true,
}

// tenantAccessor: the tenant read off a request context.
var tenantAccessor = regexp.MustCompile(`^(Get)?Tenant(ID)?$`)

// parkLoop is one park (a for loop blocking on input, or a channel
// range) in a family body.
type parkLoop struct {
	node  ast.Node
	body  *ast.BlockStmt
	reads bool // a conn-read pump
	fam   *family
}

// arms holds the widened arms' lazily computed facts over a graph.
type arms struct {
	*graph
	loops    map[*family][]*parkLoop
	closures map[*family]map[types.Object]*ast.FuncLit
	sseOpen  map[*family]bool
	has      map[string]map[*family]bool
}

func newArms(pass *analysis.Pass) *arms {
	a := &arms{
		graph:    buildGraph(pass),
		loops:    map[*family][]*parkLoop{},
		closures: map[*family]map[types.Object]*ast.FuncLit{},
		sseOpen:  map[*family]bool{},
		has:      map[string]map[*family]bool{},
	}
	for _, fam := range a.families {
		a.loops[fam] = collectLoops(pass, fam)
		a.closures[fam] = collectClosures(pass, fam)
		for _, body := range fam.bodies {
			ast.Inspect(body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && a.isStreamFunc(call, "NewSSEWriter") {
					a.sseOpen[fam] = true
				}
				return true
			})
		}
	}
	return a
}

// opens: the family holds an admission-shaped open, or builds a
// core/stream SSEWriter (whose header set lives in core/stream).
func (a *arms) opens(f *family) bool { return len(f.opens) > 0 || a.sseOpen[f] }

// famHas memoizes "some body of f holds a call satisfying pred".
func (a *arms) famHas(f *family, key string, pred func(*ast.CallExpr) bool) bool {
	m := a.has[key]
	if m == nil {
		m = map[*family]bool{}
		a.has[key] = m
	}
	if v, ok := m[f]; ok {
		return v
	}
	found := false
	for _, body := range f.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			if found {
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok && pred(call) {
				found = true
			}
			return !found
		})
	}
	m[f] = found
	return found
}

// floodHas: some family in f's flood satisfies famHas.
func (a *arms) floodHas(f *family, key string, pred func(*ast.CallExpr) bool) bool {
	for r := range a.floodOf(f) {
		if a.famHas(r, key, pred) {
			return true
		}
	}
	return false
}

// reaches reports whether executing root (a node inside fam) can reach
// a call satisfying pred: directly, through a closure fam binds to a
// name and calls, or through a package-local callee's flood.
func (a *arms) reaches(root ast.Node, fam *family, key string, pred func(*ast.CallExpr) bool) bool {
	seenLits := map[*ast.FuncLit]bool{}
	var walk func(n ast.Node) bool
	walk = func(n ast.Node) bool {
		found := false
		ast.Inspect(n, func(n ast.Node) bool {
			if found {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if pred(call) {
				found = true
				return false
			}
			if t := a.resolve(call); t != nil && a.floodHas(t, key, pred) {
				found = true
				return false
			}
			if id, ok := call.Fun.(*ast.Ident); ok {
				if lit := a.closures[fam][a.pass.TypesInfo.Uses[id]]; lit != nil && !seenLits[lit] {
					seenLits[lit] = true
					if walk(lit.Body) {
						found = true
						return false
					}
				}
			}
			return true
		})
		return found
	}
	return walk(root)
}

// inLoop: pos sits inside one of fam's park loops.
func (a *arms) inLoop(fam *family, pos token.Pos) bool {
	for _, l := range a.loops[fam] {
		if pos >= l.node.Pos() && pos < l.node.End() {
			return true
		}
	}
	return false
}

// ---- the principal re-check arm --------------------------------------

func runRecheck(pass *analysis.Pass) (any, error) {
	a := newArms(pass)
	reportedLoop := map[ast.Node]bool{}
	reportedCall := map[ast.Node]bool{}
	for _, entry := range a.families {
		reach := a.floodOf(entry)
		opened, resolved, rechecked := false, false, false
		var delivery, all []*parkLoop
		for r := range reach {
			if a.opens(r) {
				opened = true
			}
			if a.famHas(r, "resolver", a.resolverCall) {
				resolved = true
			}
			if a.famHas(r, "recheck", a.recheckCall) {
				rechecked = true
			}
			for _, l := range a.loops[r] {
				all = append(all, l)
				if l.reads || a.reaches(l.body, r, "anywrite", a.anyClientWrite) {
					delivery = append(delivery, l)
				}
			}
		}
		if !opened || !resolved || len(delivery) == 0 {
			continue
		}
		// Re-running the resolver inside any park loop of the stream is
		// a re-check too (the harness REST verifyToken per event, the
		// harness ws revocationWatch ticker).
		for _, l := range all {
			if !rechecked && a.reaches(l.body, l.fam, "resolver", a.resolverCall) {
				rechecked = true
			}
		}
		if !rechecked {
			for _, l := range delivery {
				if !reportedLoop[l.node] {
					reportedLoop[l.node] = true
					pass.Reportf(l.node.Pos(), "%s", recheckMsg)
				}
			}
			continue
		}
		// The identity is re-checked; every connect-time decision the
		// stream's own families make must be re-run on a loop path too.
		for r := range reach {
			if !a.opens(r) && !holdsLoop(delivery, r) {
				continue
			}
			for _, body := range r.bodies {
				ast.Inspect(body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || reportedCall[call] || a.inLoop(r, call.Pos()) {
						return true
					}
					name, ok := a.decisionName(call)
					if !ok {
						return true
					}
					rerun := false
					same := func(c *ast.CallExpr) bool { return astx.CalleeName(c.Fun) == name }
					for _, l := range all {
						if a.reaches(l.body, l.fam, "name:"+name, same) {
							rerun = true
							break
						}
					}
					if !rerun {
						reportedCall[call] = true
						pass.Reportf(call.Pos(), decisionMsg, name)
					}
					return true
				})
			}
		}
	}
	return nil, nil
}

func holdsLoop(loops []*parkLoop, f *family) bool {
	for _, l := range loops {
		if l.fam == f {
			return true
		}
	}
	return false
}

// recheckCall: handler.RecheckPrincipal.
func (a *arms) recheckCall(call *ast.CallExpr) bool {
	fn, ok := astx.CalleeFunc(a.pass, call.Fun)
	return ok && fn.Name() == "RecheckPrincipal" && fn.Pkg() != nil &&
		strings.HasSuffix(fn.Pkg().Path(), "core/handler")
}

// resolverCall: a call that resolves or authorizes the caller.
func (a *arms) resolverCall(call *ast.CallExpr) bool {
	name := astx.CalleeName(call.Fun)
	if name == "" || name == "RecheckPrincipal" {
		return false
	}
	if resolverName.MatchString(name) || strings.Contains(strings.ToLower(name), "authorize") {
		return true
	}
	if astx.PkgFuncName(a.pass, call.Fun) == "auth.Verify" {
		return true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !identityFuncField[strings.ToLower(name)] {
		return false
	}
	if v, isVar := a.pass.TypesInfo.Uses[sel.Sel].(*types.Var); !isVar || !v.IsField() {
		return false
	}
	for _, arg := range call.Args {
		if t := typeOf(a.pass, arg); t != nil && astx.IsNamed(astx.Deref(t), "net/http", "Request") {
			return true
		}
	}
	return false
}

// decisionName: a connect-time decision the identity re-check does not
// re-run — an authorize-named call, or a tenant read off a context.
func (a *arms) decisionName(call *ast.CallExpr) (string, bool) {
	name := astx.CalleeName(call.Fun)
	if name == "" {
		return "", false
	}
	if strings.Contains(strings.ToLower(name), "authorize") {
		return name, true
	}
	if tenantAccessor.MatchString(name) {
		for _, arg := range call.Args {
			if t := typeOf(a.pass, arg); t != nil && astx.IsNamed(t, "context", "Context") {
				return name, true
			}
		}
	}
	return "", false
}

// ---- the blocked-write arm -------------------------------------------

func runWriteDeadline(pass *analysis.Pass) (any, error) {
	a := newArms(pass)
	reported := map[ast.Node]bool{}
	for _, entry := range a.families {
		reach := a.floodOf(entry)
		opened, parks := false, false
		for r := range reach {
			opened = opened || a.opens(r)
			parks = parks || len(a.loops[r]) > 0
		}
		if !opened {
			continue
		}
		if parks {
			for r := range reach {
				for _, site := range a.armedDeadlines(r) {
					if !reported[site.call] && !a.clearedAfter(r, site.call, site.body, reach, map[*family]bool{}) {
						reported[site.call] = true
						pass.Reportf(site.call.Pos(), "%s", staleDeadlineMsg)
					}
				}
			}
		}
		for r := range reach {
			loops := a.loops[r]
			if len(loops) == 0 || a.floodHas(r, "deadline", deadlineCall) {
				continue
			}
			for _, l := range loops {
				if reported[l.node] {
					continue
				}
				if site := a.firstWrite(l); site != nil {
					reported[l.node] = true
					pass.Reportf(site.Pos(), "%s", deadlineMsg)
				}
			}
		}
	}
	return nil, nil
}

// firstWrite returns the first call in the loop body (source order)
// that reaches an unbounded client write, or nil.
func (a *arms) firstWrite(l *parkLoop) *ast.CallExpr {
	var site *ast.CallExpr
	ast.Inspect(l.body, func(n ast.Node) bool {
		if site != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if a.reaches(call, l.fam, "rawwrite", a.unboundedWrite) {
			site = call
			return false
		}
		return true
	})
	return site
}

// deadlineCall: a write deadline armed on anything — a ResponseController,
// a net.Conn, the broker's cut closure.
func deadlineCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "SetWriteDeadline" || sel.Sel.Name == "SetDeadline")
}

// controllerDeadline: call is SetWriteDeadline on an
// http.ResponseController, returning its argument.
func (a *arms) controllerDeadline(call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "SetWriteDeadline" || len(call.Args) != 1 {
		return nil, false
	}
	if !astx.IsNamed(astx.Deref(typeOf(a.pass, sel.X)), "net/http", "ResponseController") {
		return nil, false
	}
	return call.Args[0], true
}

// clearedDeadline: rc.SetWriteDeadline(time.Time{}) on a
// ResponseController.
func (a *arms) clearedDeadline(call *ast.CallExpr) bool {
	arg, ok := a.controllerDeadline(call)
	if !ok {
		return false
	}
	lit, ok := ast.Unparen(arg).(*ast.CompositeLit)
	return ok && len(lit.Elts) == 0 && astx.IsNamed(typeOf(a.pass, lit), "time", "Time")
}

// armedSite is a deadline armed in the future, with the body that
// holds it.
type armedSite struct {
	call *ast.CallExpr
	body ast.Node
}

// clearedAfter: a call after the armed site, in the body that holds
// it, clears the deadline: directly, or through a closure or a
// package-local callee that does. A clear earlier in the handler (the
// inherited deadline wiped once before the loop) leaves every later
// arm standing through the idle gap. When the arm sits in a helper
// with no clear of its own (core/stream's armDeadline), every call of
// that helper in the stream's reach must be followed by one instead.
func (a *arms) clearedAfter(f *family, site ast.Node, body ast.Node, reach map[*family]bool, seen map[*family]bool) bool {
	if a.clearFollows(f, site, body) {
		return true
	}
	if seen[f] {
		return false
	}
	seen[f] = true
	callers := 0
	for g := range reach {
		for _, gb := range g.bodies {
			ok := true
			ast.Inspect(gb, func(n ast.Node) bool {
				call, isCall := n.(*ast.CallExpr)
				if !isCall || !ok || a.resolve(call) != f {
					return ok
				}
				callers++
				if !a.clearedAfter(g, call, gb, reach, seen) {
					ok = false
				}
				return true
			})
			if !ok {
				return false
			}
		}
	}
	return callers > 0
}

// clearFollows: a call after site, inside body, reaches a clear.
func (a *arms) clearFollows(f *family, site ast.Node, body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() <= site.End() {
			return true
		}
		if a.reaches(call, f, "cleardeadline", a.clearedDeadline) {
			found = true
			return false
		}
		return true
	})
	return found
}

// armedDeadlines: the ResponseController deadlines f arms in the future.
// A clear is not one, and neither is a past-deadline cut
// (rc.SetWriteDeadline(time.Now()), the SSEBroker's eviction), which is
// meant to end the stream.
func (a *arms) armedDeadlines(f *family) []armedSite {
	var sites []armedSite
	for _, body := range f.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || a.clearedDeadline(call) {
				return true
			}
			arg, ok := a.controllerDeadline(call)
			if !ok {
				return true
			}
			if now, ok := ast.Unparen(arg).(*ast.CallExpr); ok && astx.PkgFuncName(a.pass, now.Fun) == "time.Now" {
				return true
			}
			sites = append(sites, armedSite{call: call, body: body})
			return true
		})
	}
	return sites
}

var rawWriteMethod = map[string]bool{
	"Write": true, "WriteString": true, "WriteByte": true, "WriteRune": true,
	"Flush": true, "ReadFrom": true,
}

var sseWriterMethod = map[string]bool{
	"WriteEvent": true, "WriteData": true, "WriteComment": true,
	"WriteError": true, "Flush": true, "SetRetry": true,
}

var wsWriteMethod = map[string]bool{
	"Write": true, "WriteString": true, "WriteJSON": true,
}

// unboundedWrite: a write to the client that nothing bounds by itself —
// onto the ResponseWriter (directly, via its Flusher or a
// ResponseController, or through fmt/io/json helpers), or a hijacked
// conn or its bufio wrapper. A core/stream WebSocketConn write is NOT
// one: its writeFrame arms WSConfig's per-frame WriteTimeout (10s by
// default). Nor is a core/stream SSEWriter write, which arms a fresh
// 10s deadline through http.ResponseController each time.
func (a *arms) unboundedWrite(call *ast.CallExpr) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		t := typeOf(a.pass, sel.X)
		name := sel.Sel.Name
		if rawWriteMethod[name] && clientWriter(t) {
			return true
		}
		if name == "Encode" {
			if inner, ok := ast.Unparen(sel.X).(*ast.CallExpr); ok && astx.PkgFuncName(a.pass, inner.Fun) == "json.NewEncoder" && len(inner.Args) == 1 {
				return clientWriter(typeOf(a.pass, inner.Args[0]))
			}
		}
	}
	switch astx.PkgFuncName(a.pass, call.Fun) {
	case "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln", "io.WriteString", "io.Copy", "io.CopyN", "io.CopyBuffer":
		return len(call.Args) > 0 && clientWriter(typeOf(a.pass, call.Args[0]))
	}
	return false
}

// anyClientWrite: an unbounded write, or a core/stream WebSocketConn or
// SSEWriter write — delivery either way, for the re-check arm.
func (a *arms) anyClientWrite(call *ast.CallExpr) bool {
	if a.unboundedWrite(call) {
		return true
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	t := typeOf(a.pass, sel.X)
	return wsWriteMethod[sel.Sel.Name] && streamType(t, "WebSocketConn") ||
		sseWriterMethod[sel.Sel.Name] && streamType(t, "SSEWriter")
}

// clientWriter: the response writer, its Flusher, a ResponseController,
// a hijacked conn (anything carrying SetWriteDeadline), or a bufio
// writer over one.
func clientWriter(t types.Type) bool {
	if t == nil {
		return false
	}
	if astx.IsNamed(t, "net/http", "ResponseWriter") || astx.IsNamed(t, "net/http", "Flusher") {
		return true
	}
	d := astx.Deref(t)
	if astx.IsNamed(d, "net/http", "ResponseController") || astx.IsNamed(d, "bufio", "Writer") || astx.IsNamed(d, "bufio", "ReadWriter") {
		return true
	}
	if streamType(t, "WebSocketConn") {
		return false
	}
	obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "SetWriteDeadline")
	_, isFunc := obj.(*types.Func)
	return isFunc
}

// streamType: t (or *t) is core/stream's named type name.
func streamType(t types.Type, name string) bool {
	named := astx.NamedOf(t)
	return named != nil && named.Obj() != nil && named.Obj().Name() == name &&
		named.Obj().Pkg() != nil && strings.HasSuffix(named.Obj().Pkg().Path(), "core/stream")
}

// isStreamFunc: call targets core/stream's function name.
func (a *arms) isStreamFunc(call *ast.CallExpr, name string) bool {
	fn, ok := astx.CalleeFunc(a.pass, call.Fun)
	return ok && fn.Name() == name && fn.Pkg() != nil && strings.HasSuffix(fn.Pkg().Path(), "core/stream") &&
		fn.Type().(*types.Signature).Recv() == nil
}

// ---- per-family collection -------------------------------------------

// collectLoops returns fam's outermost park loops, by the seat arm's
// park test (bodyParks / forLoopParks), each body visited once.
func collectLoops(pass *analysis.Pass, fam *family) []*parkLoop {
	chanParams := chanParamsOf(pass, fam.decl)
	var out []*parkLoop
	for _, body := range fam.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			switch st := n.(type) {
			case *ast.FuncLit:
				return false // its own body in fam.bodies
			case *ast.RangeStmt:
				if _, ok := underlyingChan(typeOf(pass, st.X)); ok {
					if id, isIdent := st.X.(*ast.Ident); isIdent {
						if obj := pass.TypesInfo.Uses[id]; obj != nil && chanParams[obj] {
							return true // caller-driven pump
						}
					}
					out = append(out, &parkLoop{node: st, body: st.Body, fam: fam})
					return false
				}
			case *ast.ForStmt:
				if forLoopParks(pass, st) {
					out = append(out, &parkLoop{node: st, body: st.Body, fam: fam, reads: loopReads(st.Body)})
					return false
				}
			}
			return true
		})
	}
	return out
}

// loopReads: the loop blocks on a conn-read call.
func loopReads(body *ast.BlockStmt) bool {
	reads := false
	ast.Inspect(body, func(n ast.Node) bool {
		if reads {
			return false
		}
		switch e := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok && readLoopName.MatchString(sel.Sel.Name) {
				reads = true
			}
		}
		return !reads
	})
	return reads
}

// collectClosures maps a name bound to a function literal (x := func…,
// var x = func…) to that literal, so a call through the name inside a
// loop is followed into the literal.
func collectClosures(pass *analysis.Pass, fam *family) map[types.Object]*ast.FuncLit {
	out := map[types.Object]*ast.FuncLit{}
	for _, body := range fam.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			switch st := n.(type) {
			case *ast.AssignStmt:
				if len(st.Lhs) != len(st.Rhs) {
					return true
				}
				for i, rhs := range st.Rhs {
					lit, ok := rhs.(*ast.FuncLit)
					id, isIdent := st.Lhs[i].(*ast.Ident)
					if ok && isIdent {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							out[obj] = lit
						}
					}
				}
			case *ast.ValueSpec:
				if len(st.Names) != len(st.Values) {
					return true
				}
				for i, v := range st.Values {
					if lit, ok := v.(*ast.FuncLit); ok {
						if obj := pass.TypesInfo.ObjectOf(st.Names[i]); obj != nil {
							out[obj] = lit
						}
					}
				}
			}
			return true
		})
	}
	return out
}

// chanParamsOf: decl's channel-typed parameters.
func chanParamsOf(pass *analysis.Pass, decl *ast.FuncDecl) map[types.Object]bool {
	out := map[types.Object]bool{}
	if decl.Type.Params == nil {
		return out
	}
	for _, field := range decl.Type.Params.List {
		for _, id := range field.Names {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				if _, ok := obj.Type().Underlying().(*types.Chan); ok {
					out[obj] = true
				}
			}
		}
	}
	return out
}
