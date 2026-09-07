// Package unseated catches a long-lived stream surface that admits a
// connection without any seat acquisition reachable on its open path.
//
// The bug class is one low-privilege principal parking unbounded
// concurrent streams: every stream surface in this repo costs a handler
// goroutine, a buffered channel, and (for websockets) a read pump, a
// keepalive loop, and a write pump for the life of the connection, so
// an admission path with no per-principal cap is a memory/fd
// exhaustion target for a single caller. The house policy is one
// number — core/stream/seats.go defaultSeatsPerPrincipal (16) with a
// SeatOverflowPolicy (refuse at connect, or evict the caller's oldest)
// — and earlier rounds pinned it for the SSE broker (core/stream/
// sse_broker.go Subscribe), the MCP notification stream (core/mcp/
// transport.go sseGetHandler via sseSeatKey/addSSESubscriber), and the
// CRUD event stream (framework/crud/eventstream_seats.go via
// seatRegistry().admit). The round-5 red probes found the unseated
// members one family at a time (battery/rtc seat_flood, core/a2a
// stream_seats, harness control rest/ws seat_cap, webmcp-remote-assist
// ws_seats, kiln/live events_seats — 300 dials, 64 streams, 33
// upgrades, all admitted, zero refusals); this rule is the family
// enumeration so the next surface cannot reopen quietly.
//
// Shape: a handler path that OPENS a long-lived stream — sets
// Content-Type "text/event-stream" on the response writer, calls a
// websocket Upgrade, or takes the connection with http.Hijacker.Hijack
// — and from which a PARK is reachable within the package: a for/select
// loop blocking on a channel receive or ctx.Done(), a channel range, or
// a conn.Read-style read loop. Both the open and the park are computed
// over the handler's package-local call flood, because neither has to
// sit in the handler itself: core/a2a's handlers open through the
// newSSEStream helper and park inside forwardEvents/pollEvents, and the
// harness ws handler parks in a goroutine run loop — reachability, not
// adjacency, is the test (the lesson recovercallback's dispatch-path
// flood learned). A family fires when its flood holds an open site, a
// park, and no seat; the report lands on the open site.
//
// A seat acquisition anywhere on the flood credits the surface: a call
// whose name says seat (seatsFor, spliceSeatLocked, sseSeatKey,
// seatRegistry, Seats.Acquire — the repo's spellings), a call on a
// value of a core/stream type whose own name says seat, or a
// len(counter) compared against a non-zero cap where the counter is a
// seat/subs/streams/peers-named map (the hand-rolled admission shape;
// a comparison against zero is an emptiness probe — rtc's peerGone
// roster cleanup — not a bound). A seat on one arm of a dispatcher
// flood credits the whole flood: the admission decision that matters
// is the one the dispatcher's own package makes, wherever in it the
// decision sits.
//
// Postures it deliberately stays silent on, because they are not this
// bug: a Hijack call inside a function named Hijack or Upgrade — the
// ResponseWriter-wrapper passthroughs (core/middleware, dev
// htmlinject, webmcp observer) and core/stream's own Upgrade, whose
// hijack is the mechanism rather than an admission decision (its
// CALLERS are where this rule reports); an upgrade that only CLOSES
// its connection (rtc's refuse upgrades a refused join so the browser
// can read the close code, then closes it immediately — a refusal, not
// an admission; a deferred Close is cleanup and does not count); an
// open with no park reachable (core/mcp ssePostHandler writes one
// event and returns; core/stream's Upgrade hands the socket back — the
// caller parks or does not); a park that is solely a range over the
// parking function's own channel-typed parameter (handler.SSEStream is
// a caller-driven pump: the channel's owner, one frame up, owns the
// admission decision); surfaces with no open spelling at all
// (framework/crud's EventStream opens through core/stream's SSEWriter,
// whose header set lives in another package, and seats itself at its
// own call site); and everything in _test.go, where a held stream is a
// fixture, not an attack.
package unseated

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "unseated",
	Doc:  "reports a long-lived stream surface (text/event-stream, websocket Upgrade, Hijack) with a park loop reachable in the package and no seat acquisition on the path",
	Run:  run,
}

const unseatedMsg = "long-lived stream opened here with a park loop reachable in this package and no seat acquisition on the path: one principal can hold unbounded concurrent streams (a goroutine, a buffered channel, and per-socket pumps each) — admit through core/stream's seat policy (defaultSeatsPerPrincipal, SeatOverflowRefuse or SeatOverflowEvictOldest), the posture core/stream, core/mcp, and framework/crud already pin"

// readLoopName matches the conn-read family: a for loop blocking on
// one of these calls is a read pump. Next/Scan/Decode are deliberately
// absent — a scanner or rows.Next loop is input processing, not a held
// connection.
var readLoopName = regexp.MustCompile(`^(Read|ReadFrame|ReadMessage|ReadPacket|ReadJSON|Recv|Receive)$`)

// counterMapName matches the map names the hand-rolled admission shape
// counts into: seat tables, subscriber registries, stream and peer
// rosters.
var counterMapName = regexp.MustCompile(`(?i)(seat|subs|stream|peer)`)

// family is one analysis unit: a function declaration plus every
// function literal it owns (a handler-returning method is one surface,
// not two).
// openSite is one admission-shaped call plus, when its result is
// assigned to a variable, that variable (for the refusal test).
type openSite struct {
	call   *ast.CallExpr
	result types.Object // nil when the open result is discarded
}

type family struct {
	decl    *ast.FuncDecl
	bodies  []*ast.BlockStmt // decl body + owned literals, in order
	callees map[*family]bool
	opens   []openSite
	closed  map[types.Object]bool // open results closed immediately (not deferred)
	parks   bool
	seats   bool
}

func run(pass *analysis.Pass) (any, error) {
	funcs := map[string][]*ast.FuncDecl{}   // top-level funcs by name
	methods := map[string][]*ast.FuncDecl{} // "Recv.Method" by name
	var families []*family

	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fam := &family{decl: fd, callees: map[*family]bool{}}
			fam.bodies = append(fam.bodies, fd.Body)
			collectLiterals(fd.Body, &fam.bodies)
			families = append(families, fam)
			if fd.Recv == nil {
				funcs[fd.Name.Name] = append(funcs[fd.Name.Name], fd)
			} else if base := recvBaseName(fd); base != "" {
				methods[base+"."+fd.Name.Name] = append(methods[base+"."+fd.Name.Name], fd)
			}
		}
	}

	byDecl := map[*ast.FuncDecl]*family{}
	for _, fam := range families {
		byDecl[fam.decl] = fam
	}

	for _, fam := range families {
		scanFamily(pass, fam)
		for _, body := range fam.bodies {
			ast.Inspect(body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if target := resolveCall(pass, call.Fun, funcs, methods); target != nil {
					if t := byDecl[target]; t != nil && t != fam {
						fam.callees[t] = true
					}
				}
				return true
			})
		}
	}

	// A family fires when its flood holds an open site, a park, and no
	// seat; the report lands on the open site, deduplicated per site.
	// An open whose owning family closes its result immediately AND has
	// no park anywhere in its own flood is a refusal (rtc's refuse
	// upgrades so the browser can read the close code, then closes) and
	// never fires; a family that parks keeps its open even when an
	// error branch closes the connection early (rtc Serve, harness ws).
	floods := map[*family]map[*family]bool{}
	floodOf := func(f *family) map[*family]bool {
		if floods[f] == nil {
			floods[f] = flood(f)
		}
		return floods[f]
	}
	parksAnywhere := func(f *family) bool {
		for r := range floodOf(f) {
			if r.parks {
				return true
			}
		}
		return false
	}

	reported := map[ast.Node]bool{}
	for _, fam := range families {
		reach := floodOf(fam)
		parked, seated := false, false
		for r := range reach {
			if r.parks {
				parked = true
			}
			if r.seats {
				seated = true
			}
		}
		if !parked || seated {
			continue
		}
		for r := range reach {
			ownerParks := parksAnywhere(r)
			for _, site := range r.opens {
				if site.result != nil && r.closed != nil && r.closed[site.result] && !ownerParks {
					continue // refusal: upgraded only to be closed
				}
				if !reported[site.call] {
					reported[site.call] = true
					pass.Reportf(site.call.Pos(), "%s", unseatedMsg)
				}
			}
		}
	}
	return nil, nil
}

// scanFamily computes the family's own facts: open sites (with their
// result variables), immediate closes of those results, park presence,
// seat credit.
func scanFamily(pass *analysis.Pass, fam *family) {
	name := fam.decl.Name.Name
	chanParams := map[types.Object]bool{}
	if fam.decl.Type.Params != nil {
		for _, field := range fam.decl.Type.Params.List {
			for _, id := range field.Names {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					if _, ok := obj.Type().Underlying().(*types.Chan); ok {
						chanParams[obj] = true
					}
				}
			}
		}
	}

	// Result variables of Upgrade/Hijack calls, for the refusal test.
	openResult := map[*ast.CallExpr]types.Object{}
	for _, body := range fam.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, rhs := range assign.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok || i >= len(assign.Lhs) || !isUpgradeOrHijack(call) {
					continue
				}
				if id, ok := assign.Lhs[i].(*ast.Ident); ok {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						openResult[call] = obj
					}
				}
			}
			return true
		})
	}
	if len(openResult) > 0 {
		fam.closed = map[types.Object]bool{}
		for _, body := range fam.bodies {
			ast.Inspect(body, func(n ast.Node) bool {
				switch st := n.(type) {
				case *ast.DeferStmt:
					return true // deferred close is cleanup, not refusal
				case *ast.ExprStmt:
					if call, ok := st.X.(*ast.CallExpr); ok {
						markImmediateClose(pass, call, fam.closed)
					}
				case *ast.AssignStmt:
					for _, rhs := range st.Rhs {
						if call, ok := rhs.(*ast.CallExpr); ok {
							markImmediateClose(pass, call, fam.closed)
						}
					}
				}
				return true
			})
		}
	}

	for _, body := range fam.bodies {
		if !fam.parks {
			fam.parks = bodyParks(pass, body, chanParams)
		}
	}
	for _, body := range fam.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.CallExpr:
				if isOpenSite(pass, e, name) {
					fam.opens = append(fam.opens, openSite{call: e, result: openResult[e]})
				}
				if seatCall(pass, e) {
					fam.seats = true
				}
			case *ast.SelectorExpr:
				if seatSelector(pass, e) {
					fam.seats = true
				}
			case *ast.BinaryExpr:
				if counterCompare(pass, e) {
					fam.seats = true
				}
			}
			return true
		})
	}
}

// markImmediateClose records that a Close-named call on an open result
// runs straight-line (a refusal closing the socket it just upgraded).
func markImmediateClose(pass *analysis.Pass, call *ast.CallExpr, closed map[types.Object]bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !strings.HasPrefix(sel.Sel.Name, "Close") {
		return
	}
	if id, ok := sel.X.(*ast.Ident); ok {
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			closed[obj] = true
		}
	}
}

func isUpgradeOrHijack(call *ast.CallExpr) bool {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name == "Upgrade" || fun.Name == "Hijack"
	case *ast.SelectorExpr:
		return fun.Sel.Name == "Upgrade" || fun.Sel.Name == "Hijack"
	}
	return false
}

// isOpenSite reports whether call is an admission-shaped open: the
// text/event-stream content-type set, a websocket Upgrade, or a Hijack
// outside the functions that exist to forward it.
func isOpenSite(pass *analysis.Pass, call *ast.CallExpr, familyName string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "Upgrade" {
			return true
		}
		return false
	}
	switch sel.Sel.Name {
	case "Set", "Add":
		if len(call.Args) >= 2 && headerValueIs(call.Args[0], "content-type") && litContains(call.Args[1], "text/event-stream") {
			return true
		}
	case "Upgrade":
		return true
	case "Hijack":
		// The wrapper passthroughs (every middleware Hijack()) and the
		// websocket library's own Upgrade exist to forward or implement
		// the hijack; the admission decision belongs to their callers.
		return familyName != "Hijack" && familyName != "Upgrade"
	}
	return false
}

func headerValueIs(e ast.Expr, want string) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return strings.EqualFold(strings.Trim(lit.Value, "\"'`"), want)
}

func litContains(e ast.Expr, want string) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return strings.Contains(strings.Trim(lit.Value, "\"'`"), want)
}

// seatCall: the callee name says seat (seatsFor, spliceSeatLocked,
// sseSeatKey, seatRegistry, Seats.Acquire), or the call is on a
// core/stream value whose type name says seat.
func seatCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		if strings.Contains(strings.ToLower(fun.Sel.Name), "seat") {
			return true
		}
		if tv, ok := pass.TypesInfo.Types[fun.X]; ok && fromStreamPkg(tv.Type) && typeSaysSeat(tv.Type) {
			return true
		}
		return false
	default:
		return false
	}
	return strings.Contains(strings.ToLower(name), "seat")
}

// seatSelector: a value of a core/stream seat-named type read directly
// (the Seats registry field), the non-call half of the same credit.
func seatSelector(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	if strings.Contains(strings.ToLower(sel.Sel.Name), "seat") {
		return false // call-shaped names are credited by seatCall
	}
	if tv, ok := pass.TypesInfo.Types[sel.X]; ok {
		return fromStreamPkg(tv.Type) && typeSaysSeat(tv.Type)
	}
	return false
}

func fromStreamPkg(t types.Type) bool {
	named := namedOf(t)
	return named != nil && named.Obj() != nil && named.Obj().Pkg() != nil &&
		strings.HasSuffix(named.Obj().Pkg().Path(), "core/stream")
}

func typeSaysSeat(t types.Type) bool {
	named := namedOf(t)
	return named != nil && named.Obj() != nil &&
		strings.Contains(strings.ToLower(named.Obj().Name()), "seat")
}

func namedOf(t types.Type) *types.Named {
	if named, ok := t.(*types.Named); ok {
		return named
	}
	if p, ok := t.(*types.Pointer); ok {
		if named, ok := p.Elem().(*types.Named); ok {
			return named
		}
	}
	return nil
}

// counterCompare: `len(m[k]) >= cap` where m is a seat/subs/streams/
// peers-named map — the hand-rolled per-principal admission shape,
// which credits the surface exactly like a named seat call.
func counterCompare(pass *analysis.Pass, be *ast.BinaryExpr) bool {
	switch be.Op {
	case token.GEQ, token.GTR, token.EQL, token.LSS, token.LEQ:
	default:
		return false
	}
	l, ok := unparen(be.X).(*ast.CallExpr)
	if !ok || !isLen(l) {
		l, ok = unparen(be.Y).(*ast.CallExpr)
		if !ok || !isLen(l) {
			return false
		}
	}
	// A comparison against zero is an emptiness probe, not a cap:
	// `len(m) == 0` / `> 0` guards roster cleanup (rtc's peerGone),
	// which is not an admission bound.
	other := be.Y
	if unparen(be.X) != ast.Expr(l) {
		other = be.X // len sits on the right; the bound is on the left
	}
	if lit, ok := other.(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value == "0" {
		return false
	}
	base := indexBase(l.Args[0])
	return base != "" && counterMapName.MatchString(base)
}

// bodyParks reports whether body holds a park: a for/select loop
// blocking on a channel receive or ctx.Done(), a channel range, or a
// conn-read loop. A park that is ONLY a range over one of the unit's
// own channel parameters is a caller-driven pump, not a held stream.
func bodyParks(pass *analysis.Pass, body *ast.BlockStmt, chanParams map[types.Object]bool) bool {
	parked := false
	ast.Inspect(body, func(n ast.Node) bool {
		if parked {
			return false
		}
		switch st := n.(type) {
		case *ast.FuncLit:
			return false // separate surface: its parks are its own
		case *ast.RangeStmt:
			if _, ok := underlyingChan(typeOf(pass, st.X)); ok {
				if id, isIdent := st.X.(*ast.Ident); isIdent {
					if obj := pass.TypesInfo.Uses[id]; obj != nil && chanParams[obj] {
						return true // param-channel range: not a park by itself
					}
				}
				parked = true
				return false
			}
		case *ast.ForStmt:
			if forLoopParks(pass, st) {
				parked = true
				return false
			}
		}
		return true
	})
	return parked
}

// forLoopParks: the loop's body blocks on input — a select with a
// receive case, a receive assignment, or a conn-read call.
func forLoopParks(pass *analysis.Pass, loop *ast.ForStmt) bool {
	blocks := false
	ast.Inspect(loop.Body, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.SelectStmt:
			for _, cl := range e.Body.List {
				comm, ok := cl.(*ast.CommClause)
				if !ok || comm.Comm == nil {
					continue
				}
				if receiveShaped(comm.Comm) {
					blocks = true
					return false
				}
			}
		case *ast.AssignStmt:
			for _, rhs := range e.Rhs {
				if recv, ok := rhs.(*ast.UnaryExpr); ok && recv.Op == token.ARROW {
					blocks = true
					return false
				}
			}
		case *ast.ExprStmt:
			if recv, ok := e.X.(*ast.UnaryExpr); ok && recv.Op == token.ARROW {
				blocks = true
				return false
			}
		case *ast.CallExpr:
			if sel, ok := e.Fun.(*ast.SelectorExpr); ok && readLoopName.MatchString(sel.Sel.Name) {
				blocks = true
				return false
			}
		}
		return true
	})
	return blocks
}

// receiveShaped: a comm clause whose value is a channel receive
// (`case <-ch`, `case v := <-ch`, `case <-ctx.Done()`).
func receiveShaped(comm ast.Stmt) bool {
	switch c := comm.(type) {
	case *ast.ExprStmt:
		if recv, ok := c.X.(*ast.UnaryExpr); ok && recv.Op == token.ARROW {
			return true
		}
	case *ast.AssignStmt:
		for _, rhs := range c.Rhs {
			if recv, ok := rhs.(*ast.UnaryExpr); ok && recv.Op == token.ARROW {
				return true
			}
		}
	}
	return false
}

// flood returns the family plus every family reachable through
// package-local call edges (go targets included: a spawned read pump
// parks the opener).
func flood(f *family) map[*family]bool {
	seen := map[*family]bool{f: true}
	queue := []*family{f}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for next := range cur.callees {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return seen
}

// resolveCall maps a callee expression to a package-local declaration:
// a plain function by name, a method by its receiver's named type.
// Ambiguity stays unresolved, which is the quiet direction.
func resolveCall(pass *analysis.Pass, fun ast.Expr, funcs, methods map[string][]*ast.FuncDecl) *ast.FuncDecl {
	switch e := fun.(type) {
	case *ast.Ident:
		if decls := funcs[e.Name]; len(decls) == 1 {
			return decls[0]
		}
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		if !ok {
			return nil
		}
		use := pass.TypesInfo.Uses[id]
		if pn, isPkg := use.(*types.PkgName); isPkg {
			// Same-package qualified call (pkg.LocalFunc in the
			// package's own files): resolve by function name.
			if pn.Imported() == pass.Pkg {
				if decls := funcs[e.Sel.Name]; len(decls) == 1 {
					return decls[0]
				}
			}
			return nil
		}
		if base := receiverTypeName(pass, e.X); base != "" {
			if decls := methods[base+"."+e.Sel.Name]; len(decls) == 1 {
				return decls[0]
			}
		}
	}
	return nil
}

func collectLiterals(body *ast.BlockStmt, out *[]*ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			*out = append(*out, lit.Body)
			collectLiterals(lit.Body, out)
			return false
		}
		return true
	})
}

func recvBaseName(fd *ast.FuncDecl) string {
	var t ast.Expr
	switch r := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		t = r.X
	case *ast.Ident:
		t = r
	default:
		return ""
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func receiverTypeName(pass *analysis.Pass, x ast.Expr) string {
	tv, ok := pass.TypesInfo.Types[x]
	if !ok {
		return ""
	}
	t := tv.Type
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if named, ok := t.(*types.Named); ok && named.Obj() != nil && named.Obj().Pkg() == pass.Pkg {
		return named.Obj().Name()
	}
	return ""
}

func typeOf(pass *analysis.Pass, e ast.Expr) types.Type {
	if tv, ok := pass.TypesInfo.Types[e]; ok {
		return tv.Type
	}
	return nil
}

func underlyingChan(t types.Type) (*types.Chan, bool) {
	if t == nil {
		return nil, false
	}
	c, ok := t.Underlying().(*types.Chan)
	return c, ok
}

func isLen(call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "len"
}

// indexBase renders the base of an index expression (m or s.m of
// m[k]) for the counter-map name test.
func indexBase(e ast.Expr) string {
	switch v := unparen(e).(type) {
	case *ast.IndexExpr:
		return indexBase(v.X)
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

func unparen(e ast.Expr) ast.Expr {
	if p, ok := e.(*ast.ParenExpr); ok {
		return unparen(p.X)
	}
	return e
}

func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}
