// Package nostore catches a per-caller 2xx response written with no
// Cache-Control on its path.
//
// The bug class is a shared cache or the back/forward cache replaying
// one user's response at another's URL: RFC 9111's storage restriction
// covers Authorization-header requests, NOT cookie-authenticated ones —
// this framework's default session auth — so every per-caller 200 must
// suppress storage itself. The repo has pinned the posture surface by
// surface (magiclink's credential page, the auth battery's
// writeCredentialHeaders, the admin battery's gate stamping no-store at
// the one choke point, the uihost's partial/page arms, core/mcp's
// transports setting no-store before any byte) and the round-5 red
// probes found the members that missed it one at a time
// (battery/auth list_nostore, cmd/gofastr chatpage_nostore, core/a2a
// post_nostore, framework/crud cachectl, harness control rest
// nostore, kiln/chat nostore, framework/ui table_cachectl, uihost
// pagecache); this rule is the family enumeration.
//
// Shape: a function (or handler literal) whose path RESOLVES A
// PRINCIPAL — calls a current-user/session reader (GetCurrentUser,
// GetUser, SessionFrom, TokenScopes, RequireOwner, a verify-token /
// read-session spelling, auth.Verify), reads an Owner/User/Principal/
// Session/Caller-named identity field, invokes one through such a
// field (a2a's cfg.Owner), or receives the identity as a
// session-named parameter of a session-id type (the harness chat
// page) — and then WRITES a 2xx body: w.Write, WriteHeader(2xx),
// json.NewEncoder(w).Encode, a render.Respond* helper, or an
// fmt.Fprint/io.WriteString onto the writer. Both ends are computed
// over the package-local call flood (plus the callback wiring below),
// because neither has to sit in the handler itself: battery/auth's
// handlers resolve through requireSessionUserID/requireUserID helpers
// and crud's arms through requireScope, while the REST tree writes
// through its shared writeJSON and crud's cursor arm through the List
// closure that calls it. A resolver that REACHES the writer (the
// writer invoked by a principal-resolving chain) counts the same way:
// the rest handlers resolve in the handle() wrapper one frame up.
//
// Cache-Control credit, in the shapes the repo already uses: the
// function's own unconditional header set — a set nested in an if/for/
// switch is a branch, not a posture, and the uihost pagecache finding
// is exactly a no-store only the re-mint branch executes; a set that
// guards the SAME conditional branch as the write it precedes (llm.md
// sets no-cache inside the very if that writes the doc) DOES credit
// that write — a branch-local posture is real for its own branch; a
// call at the function's top level to a same-package helper that sets
// it unconditionally (writeCredentialHeaders, meHandler's spelling);
// or a package-local WRAPPER that sets it before invoking the function
// — computed through callback wiring, where a handler passed into a
// wrapper (rest's handle(inner,...), the admin battery's guard/gate
// chain) is credited with the wrapper's header no matter how many
// frames of parameter threading sit between the registration and the
// next(w, r) call.
//
// Three more postures, added in the follow-up round:
//
//   - ACKNOWLEDGEMENT BODIES: an Encode whose payload is a composite
//     literal built entirely from constants, booleans, and
//     request-derived scalars (r.PathValue/FormValue/PostFormValue/
//     Cookie, router.Param(r, ...), or a field of a struct decoded
//     from the request) carries nothing a cache could replay against
//     another caller — {"accepted": true}, {"unlinked": provider},
//     the uihost action acks. The moment a composite embeds anything
//     else (a store read, a task body, a row) it is data and fires.
//
//   - HEADER PASS-THROUGH: a function that copies an upstream or
//     stored response's headers onto the writer before writing
//     (maps.Copy(w.Header(), src), or a range loop of Set/Add from the
//     map) has its caching decided by that origin — the idempotency
//     replay and the module proxy.
//
//   - BODYLESS 2XX: a WriteHeader with no body write anywhere in the
//     function (a bare 202 Accepted) has no body to store.
//
// Postures it deliberately stays silent on, because they are not this
// bug: a write with no reachable resolver; a response that already
// carries an unconditional (or same-branch) Cache-Control on its own
// path (the SSE arms' no-cache, the admin gate, writeAuthError via
// writeCredentialHeaders); WriteHeader(204/205) — no body, nothing to
// store; an ERROR WRITER — a family whose every explicit status is a
// non-2xx constant (webbotauth's 403/503 problem bodies, the module
// proxy's 503) whose body writes are therefore never 2xx bodies; a
// variable-status WriteHeader whose every resolvable caller constant
// is non-2xx (the client web UI's writeJSONError answers 409); writes
// onto anything but the function's own http.ResponseWriter (a marshal
// into a bytes.Buffer is not a response); a bare .Session field read
// or l.Session()-shaped call — a live-session accessor is state, not a
// resolved identity (kiln's watcher and freeze printer), and the
// id-bearing spellings (UserID, SessionID) plus the reader names carry
// the real resolutions; a session-named PARAMETER that does not sit
// beside an *http.Request (the same kiln CLI shape); and everything in
// _test.go, where a JSON body is a fixture, not a disclosure.
package nostore

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "nostore",
	Doc:  "reports a per-caller (principal-resolving) 2xx response written with no Cache-Control on its path",
	Run:  run,
}

const nostoreMsg = "2xx response for a resolved principal written with no Cache-Control on the path: a shared cache or the back/forward cache can replay one caller's body at another's URL — set Cache-Control: no-store before the write (the writeCredentialHeaders / admin-gate / mcp-transport spellings), or pin it once in the wrapper this handler is mounted behind"

// resolverName matches the identity-reader call spellings by name.
var resolverName = regexp.MustCompile(`(?i)(currentuser|getuser|sessionfrom|tokenscopes|requireowner|requireauth|verifytoken|readsession|getsession|loadsession|resolvesession|currentsession|allowsession)`)

// resolverName2: a session-bearing verification spelling that contains
// none of the compact tokens above (verifySessionToken).
var resolverName2 = regexp.MustCompile(`(?i)session`)

// verifyish restricts resolverName2 to verification-ish verbs, so a
// bare "sessions" plural or "ParseSession" does not count.
var verifyish = regexp.MustCompile(`(?i)(verify|require|auth|read|get|load|resolve|current|allow)`)

// identityField: field reads that carry a principal. A bare ".Session"
// is deliberately absent — a field holding a live session object (kiln's
// watcher) is state, not a resolved identity; the id-bearing spellings
// (UserID, SessionID, Owner) are the identity reads.
var identityField = map[string]bool{
	"owner": true, "ownerid": true, "userid": true, "user": true,
	"principal": true, "principalid": true, "caller": true,
	"callerid": true, "sessionid": true,
}

// identitySelector: a call through an identity-named field (a2a's
// cfg.Owner stored as s.owner). "session" is deliberately absent: a
// Session-named selector is as often a live-session accessor (kiln's
// l.Session) as an identity, and the reader spellings (ReadSession,
// verifySessionToken) already carry the real resolutions.
var identitySelector = map[string]bool{
	"owner": true, "user": true, "principal": true, "caller": true,
}

// identityParam: a parameter that IS the identity, of a session or
// token-named type (the harness chat page's sess ids.SessionID).
var identityParam = map[string]bool{
	"sess": true, "session": true, "sessid": true, "sessionid": true,
	"userid": true, "user": true, "owner": true, "ownerid": true,
	"principal": true, "caller": true,
}

// identityType: the parameter's type says session/token/id.
var identityType = regexp.MustCompile(`(?i)(session|token)`)

// family is one analysis unit: a function declaration plus every
// function literal it owns.
// sinkSite is one 2xx write site; paramIdx >= 0 marks a WriteHeader
// whose status is the family's parameter at that index (judged by the
// callers' constants before it counts).
type sinkSite struct {
	node     ast.Node
	paramIdx int
}

type family struct {
	decl         *ast.FuncDecl
	bodies       []*ast.BlockStmt
	callees      map[*family]bool // named-call + wiring edges ("invokes")
	resolves     bool
	cc           bool
	sinks        []sinkSite
	topCalls     []*ast.CallExpr           // calls at the bodies' top level
	callSites    []*ast.CallExpr           // every call in the family
	errorOnly    bool                      // every explicit status is non-2xx
	passthrough  bool                      // upstream/stored headers copied onto w
	hasBodyWrite bool                      // any body write (not bare WriteHeader)
	bindings     map[types.Object]ast.Expr // single-value local assignments
	decoded      map[types.Object]bool     // locals decoded from the request
	requests     map[types.Object]bool     // the family's *http.Request params
	writers      map[types.Object]bool     // the family's ResponseWriter params
}

// forwardPair threads a callback parameter into a callee's parameter.
type forwardPair struct {
	from types.Object // caller's func-typed param
	to   types.Object // callee's corresponding param
}

func run(pass *analysis.Pass) (any, error) {
	funcs := map[string][]*ast.FuncDecl{}
	methods := map[string][]*ast.FuncDecl{}
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
			fam := &family{decl: fd, callees: map[*family]bool{}, writers: map[types.Object]bool{}}
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

	var forwards []forwardPair
	wiredInto := map[types.Object]map[*family]bool{}
	addWired := func(param types.Object, f *family) {
		if wiredInto[param] == nil {
			wiredInto[param] = map[*family]bool{}
		}
		wiredInto[param][f] = true
	}

	// Per-family facts, named-call edges, and callback wiring.
	for _, fam := range families {
		scanFamily(pass, fam)
		for _, body := range fam.bodies {
			ast.Inspect(body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				fam.callSites = append(fam.callSites, call)
				target := resolveCall(pass, call.Fun, funcs, methods)
				if target != nil {
					if t := byDecl[target]; t != nil && t != fam {
						fam.callees[t] = true
					}
				}
				collectWiring(pass, call, fam, byDecl, funcs, methods, wiredInto, addWired, &forwards)
				return true
			})
		}
	}

	// Helper arm of the CC credit: a top-level call in family F to a
	// package-local family that sets Cache-Control unconditionally
	// (meHandler -> writeCredentialHeaders).
	for _, fam := range families {
		if fam.cc {
			continue
		}
		for _, top := range fam.topCalls {
			if t := byDecl[resolveCall(pass, top.Fun, funcs, methods)]; t != nil && t.cc {
				fam.cc = true
				break
			}
		}
	}

	// Fixpoint: a param forwarded into another param inherits its wired
	// handlers, so the wrapper that finally invokes them is credited
	// (the guard/gate chain: guard(h) -> b.gate(h) -> next(w, r)).
	for changed := true; changed; {
		changed = false
		for _, fp := range forwards {
			for f := range wiredInto[fp.from] {
				if wiredInto[fp.to] == nil || !wiredInto[fp.to][f] {
					addWired(fp.to, f)
					changed = true
				}
			}
		}
	}
	for param, fs := range wiredInto {
		owner := familyOwningParam(families, param)
		if owner == nil {
			continue
		}
		for f := range fs {
			owner.callees[f] = true
		}
	}

	// Memoized floods.
	floods := map[*family]map[*family]bool{}
	floodOf := func(f *family) map[*family]bool {
		if floods[f] == nil {
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
			floods[f] = seen
		}
		return floods[f]
	}

	// resolverOK: a resolver in the family's flood, or a resolver that
	// reaches the family (the wrapper one frame up).
	resolverOK := func(h *family) bool {
		for r := range floodOf(h) {
			if r.resolves {
				return true
			}
		}
		for _, f := range families {
			if f.resolves {
				if _, ok := floodOf(f)[h]; ok {
					return true
				}
			}
		}
		return false
	}

	// A variable-status WriteHeader counts unless every resolvable
	// caller constant is non-2xx (writeJSONError answers 409/500, the
	// proxy's 503 writer): the write then never ships a 2xx body.
	keepSink := func(r *family, sink sinkSite) bool {
		if sink.paramIdx < 0 {
			return true
		}
		anyConst, any2xx := false, false
		for _, w := range families {
			for _, site := range w.callSites {
				if len(site.Args) <= sink.paramIdx {
					continue
				}
				target := resolveCall(pass, site.Fun, funcs, methods)
				if target != r.decl {
					continue
				}
				switch status2xx(pass, site.Args[sink.paramIdx]) {
				case statusYes:
					anyConst, any2xx = true, true
				case statusNo:
					anyConst = true
				}
			}
		}
		if anyConst {
			return any2xx
		}
		return true
	}

	// A family whose ONLY status sites are variable WriteHeaders that
	// every caller fills with a non-2xx constant never ships a 2xx
	// body; its body writes are not sinks either (the client web UI's
	// writeJSONError answers 409).
	bodyCounts := func(r *family) bool {
		var statuses []sinkSite
		bodies := false
		for _, sink := range r.sinks {
			if sink.paramIdx >= 0 {
				statuses = append(statuses, sink)
			} else {
				bodies = true
			}
		}
		if bodies || len(statuses) == 0 {
			return true
		}
		for _, st := range statuses {
			if keepSink(r, st) {
				return true
			}
		}
		return false
	}

	reported := map[ast.Node]bool{}
	for _, fam := range families {
		if !resolverOK(fam) || ccOK(fam, families) {
			continue
		}
		for r := range floodOf(fam) {
			if ccOK(r, families) || !bodyCounts(r) {
				continue // the write's own path is protected or non-2xx
			}
			for _, sink := range r.sinks {
				if !keepSink(r, sink) || reported[sink.node] {
					continue
				}
				reported[sink.node] = true
				pass.Reportf(sink.node.Pos(), "%s", nostoreMsg)
			}
		}
	}
	return nil, nil
}

// scanFamily computes the family's own facts: writer params, principal
// resolution, unconditional Cache-Control, 2xx write sinks.
func scanFamily(pass *analysis.Pass, fam *family) {
	// Writer params: http.ResponseWriter-typed parameters of the decl
	// and its owned literals.
	for _, body := range fam.bodies {
		_ = body
	}
	for _, obj := range paramObjects(pass, fam) {
		if isResponseWriter(obj.Type()) {
			fam.writers[obj] = true
		}
	}

	scanContext(pass, fam)

	if !fam.resolves {
		fam.resolves = identityParams(pass, fam)
	}
	for _, body := range fam.bodies {
		if !fam.resolves {
			fam.resolves = bodyResolves(pass, body)
		}
		if !fam.cc {
			fam.cc = bodySetsCC(pass, body)
		}
		for _, stmt := range body.List {
			if expr, ok := stmt.(*ast.ExprStmt); ok {
				if call, ok := expr.X.(*ast.CallExpr); ok {
					fam.topCalls = append(fam.topCalls, call)
				}
			}
		}
	}
	for _, body := range fam.bodies {
		fam.sinks = append(fam.sinks, bodySinks(pass, fam, body)...)
	}
}

// identityParams: a session-named parameter of a session/token-named
// type hands the function its principal (the harness chat page's sess
// ids.SessionID). Only handler-shaped functions count — the parameter
// must sit beside an *http.Request — so a CLI helper that happens to
// take a *journal.Session (kiln freeze's diff printer) does not make
// every route its main() also mounts principal-resolved.
func identityParams(pass *analysis.Pass, fam *family) bool {
	if fam.decl.Type.Params == nil {
		return false
	}
	hasRequest := false
	for _, field := range fam.decl.Type.Params.List {
		if isRequestType(exprType(pass, field.Type)) {
			hasRequest = true
			break
		}
	}
	if !hasRequest {
		return false
	}
	for _, field := range fam.decl.Type.Params.List {
		for _, id := range field.Names {
			if !identityParam[strings.ToLower(id.Name)] {
				continue
			}
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil && identityType.MatchString(obj.Type().String()) {
				return true
			}
		}
	}
	return false
}

// isRequestType: *http.Request — the handler shape's second half.
func isRequestType(t types.Type) bool {
	named := namedOf(t)
	return named != nil && named.Obj() != nil && named.Obj().Name() == "Request" &&
		named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http"
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

// bodyResolves: the principal-resolution spellings.
func bodyResolves(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch e := n.(type) {
		case *ast.CallExpr:
			found = resolverCall(pass, e)
		case *ast.SelectorExpr:
			name := strings.ToLower(e.Sel.Name)
			if _, ok := identityField[name]; ok {
				found = true
			}
		}
		return !found
	})
	return found
}

// resolverCall: a call whose name is an identity-reader spelling, a
// call through an identity-named field, or auth.Verify.
func resolverCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		name = fun.Sel.Name
		if identitySelector[strings.ToLower(name)] {
			return true // s.owner(r): the stored resolver invoked
		}
		if id, ok := fun.X.(*ast.Ident); ok {
			if pn, isPkg := pass.TypesInfo.Uses[id].(*types.PkgName); isPkg {
				if pn.Name() == "auth" && fun.Sel.Name == "Verify" {
					return true
				}
				// Cross-package otherwise: judged by the name arms below.
			}
		}
	default:
		return false
	}
	low := strings.ToLower(name)
	if resolverName.MatchString(low) {
		return true
	}
	return resolverName2.MatchString(low) && verifyish.MatchString(low)
}

// bodySetsCC: an unconditional Cache-Control set at the body's top
// level. Branch-nested sets are deliberately not credit.
func bodySetsCC(pass *analysis.Pass, body *ast.BlockStmt) bool {
	for _, stmt := range body.List {
		if expr, ok := stmt.(*ast.ExprStmt); ok {
			if call, ok := expr.X.(*ast.CallExpr); ok && isCacheControlSet(pass, call) {
				return true
			}
		}
	}
	return false
}

// isCacheControlSet: Header().Set/Add("Cache-Control", ...) on any
// writer value.
func isCacheControlSet(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add") {
		return false
	}
	if len(call.Args) < 2 {
		return false
	}
	return headerNameIs(call.Args[0], "cache-control")
}

func headerNameIs(e ast.Expr, want string) bool {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	return strings.EqualFold(strings.Trim(lit.Value, "\"'`"), want)
}

// scanContext collects the per-family facts the sink postures need:
// the request params, single-value local bindings, locals decoded from
// the request body, upstream-header pass-through, and whether any body
// write exists at all.
func scanContext(pass *analysis.Pass, fam *family) {
	fam.bindings = map[types.Object]ast.Expr{}
	fam.decoded = map[types.Object]bool{}
	fam.requests = map[types.Object]bool{}

	for _, obj := range paramObjects(pass, fam) {
		if v, ok := obj.(*types.Var); ok && isRequestType(v.Type()) {
			fam.requests[obj] = true
		}
	}

	for _, body := range fam.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.CallExpr:
				if headerPassthrough(pass, fam, e) {
					fam.passthrough = true
				}
				// A decode of the request into &local marks the local
				// as request payload (decodeBounded(w, r, &body),
				// handler.DecodeStrict(r.Body, &req)).
				for _, arg := range e.Args {
					un, ok := arg.(*ast.UnaryExpr)
					if !ok || un.Op != token.AND {
						continue
					}
					id, ok := un.X.(*ast.Ident)
					if !ok {
						continue
					}
					obj := pass.TypesInfo.ObjectOf(id)
					if obj == nil {
						continue
					}
					if exprMentionsRequest(pass, e, fam) {
						fam.decoded[obj] = true
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range e.Lhs {
					if len(e.Rhs) != len(e.Lhs) || i >= len(e.Rhs) {
						continue
					}
					id, ok := lhs.(*ast.Ident)
					if !ok {
						continue
					}
					obj := pass.TypesInfo.ObjectOf(id)
					if obj != nil {
						fam.bindings[obj] = e.Rhs[i]
					}
				}
			case *ast.RangeStmt:
				// A range whose body sets writer headers from the
				// iterated values is the loop spelling of the
				// pass-through (the module proxy's header copy).
				if rangeSetsWriterHeaders(pass, fam, e) {
					fam.passthrough = true
				}
			}
			return true
		})
	}

	for _, body := range fam.bodies {
		if fam.hasBodyWrite {
			break
		}
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sinkWrite(pass, fam, call) || sinkPrint(pass, fam, call) ||
				sinkRespond(pass, fam, call) {
				fam.hasBodyWrite = true
				return false
			}
			if sinkEncode(pass, fam, call) {
				// An ack encode is not a cacheable body: a 202 whose
				// only body is {"accepted": true} is still bodyless.
				if len(call.Args) == 1 {
					if lit, ok := unparen(call.Args[0]).(*ast.CompositeLit); ok && fam.ackLiteral(pass, lit) {
						return true
					}
				}
				fam.hasBodyWrite = true
				return false
			}
			return true
		})
	}
}

// headerPassthrough: maps.Copy(w.Header(), src) — the upstream or
// stored response's headers decide this response's caching.
func headerPassthrough(pass *analysis.Pass, fam *family, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Copy" || len(call.Args) < 2 {
		return false
	}
	return isWriterHeader(pass, fam, call.Args[0])
}

// rangeSetsWriterHeaders: a range loop whose body writes Set/Add onto
// the writer's header (the module proxy's `for k, v := range res.Headers`).
func rangeSetsWriterHeaders(pass *analysis.Pass, fam *family, rng *ast.RangeStmt) bool {
	found := false
	ast.Inspect(rng.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Set" && sel.Sel.Name != "Add") {
			return true
		}
		if isWriterHeader(pass, fam, sel.X) {
			found = true
			return false
		}
		return true
	})
	return found
}

// isWriterHeader: e is X.Header() with X the family's writer.
func isWriterHeader(pass *analysis.Pass, fam *family, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Header" && isWriterValue(pass, fam, sel.X)
}

// exprMentionsRequest: any identifier in e is one of the family's
// *http.Request params.
func exprMentionsRequest(pass *analysis.Pass, e ast.Expr, fam *family) bool {
	mentions := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if obj := pass.TypesInfo.Uses[id]; obj != nil && fam.requests[obj] {
				mentions = true
				return false
			}
		}
		return true
	})
	return mentions
}

// ackLiteral: every element of the encoded composite is a constant, a
// boolean, or a request-derived scalar (a path/query/form accessor's
// result, or a field of a struct decoded from the request) — an
// acknowledgement body, carrying nothing a cache could replay against
// another caller. A composite embedding anything else (a store read, a
// task body, a row) is data and still fires.
func (fam *family) ackLiteral(pass *analysis.Pass, lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		e := elt
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			e = kv.Value
		}
		if !fam.ackValue(pass, e, 0) {
			return false
		}
	}
	return true
}

func (fam *family) ackValue(pass *analysis.Pass, e ast.Expr, depth int) bool {
	if depth > 4 {
		return false
	}
	switch v := unparen(e).(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		switch v.Name {
		case "true", "false", "nil":
			return true
		}
		obj := pass.TypesInfo.Uses[v]
		if obj == nil {
			return false
		}
		bound, ok := fam.bindings[obj]
		if !ok {
			return false
		}
		return fam.ackValue(pass, bound, depth+1)
	case *ast.SelectorExpr:
		// A field off a struct decoded from the request body is a
		// request-derived scalar (the uihost action ack).
		if id, ok := v.X.(*ast.Ident); ok {
			if obj := pass.TypesInfo.Uses[id]; obj != nil && fam.decoded[obj] {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		return requestAccessor(pass, v, fam)
	case *ast.CompositeLit:
		return fam.ackLiteral(pass, v)
	}
	return false
}

// requestAccessor: a path/query/form/cookie accessor rooted at the
// request (router.Param(r, "id"), r.PathValue(...), r.FormValue(...)).
func requestAccessor(pass *analysis.Pass, call *ast.CallExpr, fam *family) bool {
	var name string
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		name = fun.Name
	case *ast.SelectorExpr:
		name = fun.Sel.Name
	default:
		return false
	}
	switch name {
	case "Param", "PathValue", "FormValue", "PostFormValue", "Cookie":
	default:
		return false
	}
	return exprMentionsRequest(pass, call, fam)
}

// bodySinks: the 2xx write sites onto the family's own writer. A
// family whose every explicit WriteHeader is a non-2xx constant is an
// error writer (webbotauth's 403/503 problem bodies, the proxy's 503):
// its body writes are not 2xx bodies, whatever the implicit-200 rule
// would say.
func bodySinks(pass *analysis.Pass, fam *family, body *ast.BlockStmt) []sinkSite {
	var consts []int64
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "WriteHeader" || len(call.Args) != 1 {
			return true
		}
		if tv, ok := pass.TypesInfo.Types[call.Args[0]]; ok && tv.Value != nil {
			if i, err := strconv.ParseInt(tv.Value.String(), 10, 64); err == nil {
				consts = append(consts, i)
			}
		}
		return true
	})
	allNon2xx := len(consts) > 0
	for _, c := range consts {
		if c >= 200 && c <= 299 {
			allNon2xx = false
			break
		}
	}
	if allNon2xx {
		fam.errorOnly = true
	}

	var sinks []sinkSite
	if allNon2xx {
		return sinks
	}
	parent := parentMap(body)
	ccSets := []*ast.CallExpr{}
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isCacheControlSet(pass, call) {
			ccSets = append(ccSets, call)
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if fam.passthrough {
			return true // the origin's headers decide this response
		}
		idx := -1
		if sinkHeader(pass, fam, call) {
			if !fam.hasBodyWrite {
				return true // bodyless 2xx: no body, nothing to store
			}
			idx = statusParamIdx(pass, fam, call)
			sinks = append(sinks, sinkSite{node: call, paramIdx: idx})
		} else if sinkEncode(pass, fam, call) {
			if len(call.Args) == 1 {
				if lit, ok := unparen(call.Args[0]).(*ast.CompositeLit); ok && fam.ackLiteral(pass, lit) {
					return true // acknowledgement body: nothing to replay
				}
			}
			sinks = append(sinks, sinkSite{node: call, paramIdx: -1})
		} else if sinkWrite(pass, fam, call) ||
			sinkPrint(pass, fam, call) || sinkRespond(pass, fam, call) {
			sinks = append(sinks, sinkSite{node: call, paramIdx: -1})
		} else {
			return true
		}
		// A Cache-Control set in the SAME conditional branch as this
		// write guards exactly this write (llm.md sets no-cache inside
		// the if that writes the doc) — a set in a DIFFERENT branch
		// does not (the uihost re-mint branch's no-store says nothing
		// about the live-session write).
		for _, set := range ccSets {
			if sameBranch(parent, set, call, body) {
				sinks = sinks[:len(sinks)-1]
				break
			}
		}
		return true
	})
	return sinks
}

// parentMap maps every node in body to its parent.
func parentMap(body *ast.BlockStmt) map[ast.Node]ast.Node {
	m := map[ast.Node]ast.Node{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch p := n.(type) {
		case *ast.IfStmt:
			for _, c := range subNodes(p) {
				if _, seen := m[c]; !seen {
					m[c] = p
				}
			}
		}
		return true
	})
	// The generic walk: record parent for every container node.
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		children := containerChildren(n)
		for _, c := range children {
			if _, seen := m[c]; !seen {
				m[c] = n
			}
			walk(c)
		}
	}
	walk(body)
	return m
}

func subNodes(n ast.Node) []ast.Node { return containerChildren(n) }

func containerChildren(n ast.Node) []ast.Node {
	var out []ast.Node
	switch v := n.(type) {
	case *ast.BlockStmt:
		for _, st := range v.List {
			out = append(out, st)
		}
	case *ast.IfStmt:
		out = append(out, v.Cond, v.Body)
		if v.Else != nil {
			out = append(out, v.Else)
		}
	case *ast.ForStmt:
		out = append(out, v.Body)
	case *ast.RangeStmt:
		out = append(out, v.Body)
	case *ast.SwitchStmt:
		out = append(out, v.Body)
	case *ast.TypeSwitchStmt:
		out = append(out, v.Body)
	case *ast.SelectStmt:
		out = append(out, v.Body)
	case *ast.CaseClause:
		for _, st := range v.Body {
			out = append(out, st)
		}
	case *ast.CommClause:
		for _, st := range v.Body {
			out = append(out, st)
		}
	case *ast.AssignStmt:
		for _, e := range v.Rhs {
			out = append(out, e)
		}
	case *ast.ExprStmt:
		out = append(out, v.X)
	case *ast.ReturnStmt:
		for _, e := range v.Results {
			out = append(out, e)
		}
	case *ast.DeferStmt:
		out = append(out, v.Call)
	case *ast.GoStmt:
		out = append(out, v.Call)
	case *ast.CallExpr:
		out = append(out, v.Fun)
		for _, a := range v.Args {
			out = append(out, a)
		}
	case *ast.SelectorExpr:
		out = append(out, v.X)
	}
	return out
}

// sameBranch: set and sink share a conditional ancestor (an If/For/
// Case block) below the function's top-level body — a set in one
// branch guards only the writes of that branch.
func sameBranch(parent map[ast.Node]ast.Node, set, sink ast.Node, root *ast.BlockStmt) bool {
	ancestors := map[ast.Node]bool{}
	for n := parent[set]; n != nil; n = parent[n] {
		ancestors[n] = true
	}
	for n := parent[sink]; n != nil; n = parent[n] {
		if n == root {
			return false
		}
		if ancestors[n] {
			switch n.(type) {
			case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
				return true
			}
		}
	}
	return false
}

// statusParamIdx: the WriteHeader argument is one of the family's own
// parameters; its position lets the callers' constants decide 2xx-ness.
func statusParamIdx(pass *analysis.Pass, fam *family, call *ast.CallExpr) int {
	if len(call.Args) != 1 {
		return -1
	}
	id, ok := unparen(call.Args[0]).(*ast.Ident)
	if !ok {
		return -1
	}
	obj := pass.TypesInfo.Uses[id]
	if obj == nil {
		return -1
	}
	if fam.decl.Type.Params == nil {
		return -1
	}
	idx := 0
	for _, field := range fam.decl.Type.Params.List {
		for _, pid := range field.Names {
			if pass.TypesInfo.ObjectOf(pid) == obj {
				return idx
			}
			idx++
		}
	}
	return -1
}

// sinkWrite: w.Write(...) on the family's writer.
func sinkWrite(pass *analysis.Pass, fam *family, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Write" {
		return false
	}
	return isWriterValue(pass, fam, sel.X)
}

// sinkEncode: json.NewEncoder(w).Encode(...).
func sinkEncode(pass *analysis.Pass, fam *family, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Encode" {
		return false
	}
	newEnc, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	if id, ok := newEnc.Fun.(*ast.SelectorExpr); !ok || id.Sel.Name != "NewEncoder" {
		return false
	}
	if len(newEnc.Args) != 1 {
		return false
	}
	return isWriterValue(pass, fam, newEnc.Args[0])
}

// sinkHeader: w.WriteHeader(N) with N 2xx (204/205 excluded: no body),
// a non-constant N whose callers include a 2xx, or an unknown N.
func sinkHeader(pass *analysis.Pass, fam *family, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WriteHeader" {
		return false
	}
	if !isWriterValue(pass, fam, sel.X) || len(call.Args) != 1 {
		return false
	}
	switch status2xx(pass, call.Args[0]) {
	case statusYes:
		return true
	case statusNo:
		return false
	}
	// Unknown constant: the implicit-200 rule — a body write without
	// an explicit status is a 200; a variable status is judged by its
	// callers when possible, else treated as 2xx (the fire direction).
	return true
}

// sinkPrint: fmt.Fprint*/io.WriteString with the writer first.
func sinkPrint(pass *analysis.Pass, fam *family, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "Fprint", "Fprintf", "Fprintln", "WriteString":
	default:
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	return isWriterValue(pass, fam, call.Args[0])
}

// sinkRespond: render.Respond*/RespondJSON-style helpers with the
// writer first.
func sinkRespond(pass *analysis.Pass, fam *family, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if !strings.HasPrefix(sel.Sel.Name, "Respond") {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	return isWriterValue(pass, fam, call.Args[0])
}

const (
	statusYes = iota
	statusNo
	statusVar
)

// status2xx classifies a WriteHeader argument.
func status2xx(pass *analysis.Pass, e ast.Expr) int {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.INT {
			return statusVar
		}
		return litStatus(v.Value)
	case *ast.Ident, *ast.SelectorExpr:
		if tv, ok := pass.TypesInfo.Types[e]; ok {
			if val := tv.Value; val != nil {
				if i, err := strconv.ParseInt(val.String(), 10, 64); err == nil {
					return litStatusInt(i)
				}
			}
		}
		return statusVar
	}
	return statusVar
}

func litStatus(s string) int {
	if i, err := strconv.ParseInt(s, 10, 64); err != nil {
		return statusVar
	} else {
		return litStatusInt(i)
	}
}

func litStatusInt(n int64) int {
	if n >= 200 && n <= 299 && n != 204 && n != 205 {
		return statusYes
	}
	return statusNo
}

// isWriterValue: e is one of the family's ResponseWriter params or an
// alias local bound directly to one.
func isWriterValue(pass *analysis.Pass, fam *family, e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.Uses[id]
	if obj == nil {
		return false
	}
	if fam.writers[obj] {
		return true
	}
	// One-hop alias: w2 := w.
	if def, ok := obj.(*types.Var); ok && def.Parent() == nil {
		return false
	}
	return false
}

// ccOK: Cache-Control credit — the family's own unconditional set
// (fam.cc, which run extends with the helper arm: a top-level call to
// a same-package helper that sets it unconditionally), or a wrapper
// family that sets it and invokes (or wires) this one.
func ccOK(fam *family, families []*family) bool {
	if fam.cc || fam.passthrough {
		return true
	}
	for _, other := range families {
		if other.cc && other != fam {
			if _, ok := other.callees[fam]; ok {
				return true
			}
		}
	}
	return false
}

// collectWiring records, for a call to a package-local function or a
// same-family literal, which package-local handlers are handed to it:
// a method value or handler-producing call becomes a wired handler; a
// forwarded func-typed parameter becomes a threading edge.
func collectWiring(pass *analysis.Pass, call *ast.CallExpr, fam *family, byDecl map[*ast.FuncDecl]*family,
	funcs, methods map[string][]*ast.FuncDecl, wiredInto map[types.Object]map[*family]bool,
	addWired func(types.Object, *family), forwards *[]forwardPair) {

	// The callee's parameter objects, in order, plus the family that
	// owns the callee (nil for cross-package calls).
	var calleeParams []types.Object
	var calleeFamily *family
	if target := resolveCall(pass, call.Fun, funcs, methods); target != nil {
		calleeFamily = byDecl[target]
		calleeParams = declParams(pass, target)
	} else if id, ok := call.Fun.(*ast.Ident); ok {
		// A call to a local function literal of this same family: its
		// params are threading targets.
		if lit := boundLiteral(pass, fam, id); lit != nil {
			calleeParams = litParams(pass, lit)
		}
	}
	if len(calleeParams) == 0 || len(call.Args) == 0 {
		return
	}

	for i, arg := range call.Args {
		if i >= len(calleeParams) {
			break
		}
		switch a := arg.(type) {
		case *ast.SelectorExpr, *ast.Ident:
			if target := resolveCall(pass, a, funcs, methods); false {
				_ = target
			}
			if hf := handlerFamilyOf(pass, arg, funcs, methods, byDecl); hf != nil {
				if calleeFamily != nil {
					calleeFamily.callees[hf] = true
				}
				addWired(calleeParams[i], hf)
			} else if obj := funcTypedParam(pass, fam, arg); obj != nil {
				*forwards = append(*forwards, forwardPair{from: obj, to: calleeParams[i]})
			}
		case *ast.CallExpr:
			// A handler-producing call: b.entityRows(ent).
			if hf := handlerFamilyOf(pass, arg, funcs, methods, byDecl); hf != nil {
				if calleeFamily != nil {
					calleeFamily.callees[hf] = true
				}
				addWired(calleeParams[i], hf)
			}
		}
	}
}

// handlerFamilyOf: the expression names or produces a package-local
// handler — a method/function VALUE with a handler-shaped signature
// (rest's s.handleSessions), or a CALL whose result is handler-shaped
// (the admin battery's b.entityRows(ent) returning the closure).
func handlerFamilyOf(pass *analysis.Pass, e ast.Expr, funcs, methods map[string][]*ast.FuncDecl, byDecl map[*ast.FuncDecl]*family) *family {
	if !exprHandlerShaped(pass, e) {
		return nil
	}
	var target *ast.FuncDecl
	switch v := unparen(e).(type) {
	case *ast.CallExpr:
		// A handler-producing call (b.entityRows(ent)): the producer's
		// family owns the returned closure.
		switch fun := v.Fun.(type) {
		case *ast.Ident:
			if decls := funcs[fun.Name]; len(decls) == 1 {
				target = decls[0]
			}
		case *ast.SelectorExpr:
			if base := receiverTypeName(pass, fun.X); base != "" {
				if decls := methods[base+"."+fun.Sel.Name]; len(decls) == 1 {
					target = decls[0]
				}
			}
		}
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			if _, isPkg := pass.TypesInfo.Uses[id].(*types.PkgName); isPkg {
				return nil
			}
		}
		if base := receiverTypeName(pass, v.X); base != "" {
			if decls := methods[base+"."+v.Sel.Name]; len(decls) == 1 {
				target = decls[0]
			}
		}
	case *ast.Ident:
		if decls := funcs[v.Name]; len(decls) == 1 {
			target = decls[0]
		}
	}
	if target == nil {
		return nil
	}
	return byDecl[target]
}

// exprHandlerShaped: e's type is a handler-shaped signature, or a call
// whose RESULT is one (or a named handler type like http.HandlerFunc).
func exprHandlerShaped(pass *analysis.Pass, e ast.Expr) bool {
	t := exprType(pass, e)
	if t == nil {
		return false
	}
	return handlerTypeShaped(t)
}

// exprType resolves an expression's type: Types for selector/method
// values and calls, Uses for plain identifiers (a func ident's type
// lives on its object).
func exprType(pass *analysis.Pass, e ast.Expr) types.Type {
	if id, ok := e.(*ast.Ident); ok {
		if obj := pass.TypesInfo.Uses[id]; obj != nil {
			return obj.Type()
		}
	}
	if tv, ok := pass.TypesInfo.Types[e]; ok {
		return tv.Type
	}
	return nil
}

func handlerTypeShaped(t types.Type) bool {
	if sig, ok := t.Underlying().(*types.Signature); ok {
		return handlerShaped(sig)
	}
	return false
}

// handlerShaped: func(http.ResponseWriter, *http.Request)-ish.
func handlerShaped(sig *types.Signature) bool {
	params := sig.Params()
	if params == nil || params.Len() < 2 {
		return false
	}
	if !isResponseWriter(params.At(0).Type()) {
		return false
	}
	second := params.At(1).Type()
	named, ok := second.(*types.Named)
	if !ok {
		ptr, isPtr := second.(*types.Pointer)
		if !isPtr {
			return false
		}
		named, ok = ptr.Elem().(*types.Named)
	}
	if !ok || named == nil || named.Obj() == nil {
		return false
	}
	return named.Obj().Name() == "Request" && named.Obj().Pkg().Path() == "net/http"
}

// isResponseWriter: net/http's ResponseWriter, an alias of it, or an
// interface with its method trio.
func isResponseWriter(t types.Type) bool {
	if named, ok := t.(*types.Named); ok && named.Obj() != nil &&
		named.Obj().Name() == "ResponseWriter" && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "net/http" {
		return true
	}
	iface, ok := t.Underlying().(*types.Interface)
	if !ok {
		return false
	}
	var header, write, writeHeader bool
	for i := 0; i < iface.NumMethods(); i++ {
		switch iface.Method(i).Name() {
		case "Header":
			header = true
		case "Write":
			write = true
		case "WriteHeader":
			writeHeader = true
		}
	}
	return header && write && writeHeader
}

// funcTypedParam: e is an identifier naming one of the family's own
// func-typed parameters (a callback being forwarded).
func funcTypedParam(pass *analysis.Pass, fam *family, e ast.Expr) types.Object {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.Uses[id]
	if obj == nil {
		return nil
	}
	if _, ok := obj.Type().Underlying().(*types.Signature); !ok {
		return nil
	}
	return obj
}

// boundLiteral: the function literal bound to a local identifier in
// the family (guard := func(...){...}).
func boundLiteral(pass *analysis.Pass, fam *family, id *ast.Ident) *ast.FuncLit {
	// The call site's identifier is a USE of the variable; the
	// definition side (guard := func...) carries the object in Defs.
	obj := pass.TypesInfo.Uses[id]
	if obj == nil {
		return nil
	}
	for _, body := range fam.bodies {
		var found *ast.FuncLit
		ast.Inspect(body, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != len(assign.Rhs) {
				return true
			}
			for i, lhs := range assign.Lhs {
				lid, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if pass.TypesInfo.Defs[lid] != obj {
					continue
				}
				if lit, ok := assign.Rhs[i].(*ast.FuncLit); ok {
					found = lit
					return false
				}
			}
			return true
		})
		if found != nil {
			return found
		}
	}
	return nil
}

// familyOwningParam: which family's decl or literals declares param.
func familyOwningParam(families []*family, param types.Object) *family {
	for _, fam := range families {
		if fam.decl.Pos() <= param.Pos() && param.Pos() < fam.decl.End() {
			return fam
		}
	}
	return nil
}

// paramObjects: the decl's and its literals' parameter variables.
func paramObjects(pass *analysis.Pass, fam *family) []types.Object {
	var out []types.Object
	if fam.decl.Type.Params != nil {
		for _, field := range fam.decl.Type.Params.List {
			for _, id := range field.Names {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					out = append(out, obj)
				}
			}
		}
	}
	for _, body := range fam.bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				if lit.Type.Params != nil {
					for _, field := range lit.Type.Params.List {
						for _, id := range field.Names {
							if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
								out = append(out, obj)
							}
						}
					}
				}
			}
			return true
		})
	}
	return out
}

func declParams(pass *analysis.Pass, fd *ast.FuncDecl) []types.Object {
	var out []types.Object
	if fd.Type.Params == nil {
		return nil
	}
	for _, field := range fd.Type.Params.List {
		for _, id := range field.Names {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				out = append(out, obj)
			}
		}
	}
	return out
}

func litParams(pass *analysis.Pass, lit *ast.FuncLit) []types.Object {
	var out []types.Object
	if lit.Type.Params == nil {
		return nil
	}
	for _, field := range lit.Type.Params.List {
		for _, id := range field.Names {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				out = append(out, obj)
			}
		}
	}
	return out
}

// resolveCall maps a callee expression to a package-local declaration.
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

func unparen(e ast.Expr) ast.Expr {
	if p, ok := e.(*ast.ParenExpr); ok {
		return unparen(p.X)
	}
	return e
}

func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}
