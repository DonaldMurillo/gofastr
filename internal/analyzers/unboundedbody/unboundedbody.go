// Package unboundedbody catches an inbound HTTP request body that is read
// or decoded without a size cap, so a single request can spend the
// server's memory.
//
// The class is a repeat offender here, fixed one sink at a time: multipart
// bodies capped in memory but not on the wire, an unbounded IN-list split,
// retained rate-limit key bytes, SSE subscription state, attacker-named
// DNS lookups. Every fix was correct and none of them stopped the next
// one, because "did anyone cap this body" is not visible at the call site.
//
// The check is type-aware on purpose. The dangerous shape is
// io.ReadAll(r.Body) where r is an inbound *http.Request; the identical
// spelling on an *http.Response is an outbound call whose peer you chose,
// a different and much smaller risk. A grep cannot tell those apart, which
// is why this lives in the vet lane rather than the contracts one.
//
// Read sinks include ReadAll, Copy, CopyBuffer, ReadFrom, JSON/XML/YAML
// decoders, and fixed-buffer io.ReadFull. Binding r.Body to a local or
// wrapping it in bufio.NewReader, NewReaderSize, or io.TeeReader keeps
// the request body visible to the check.
//
// A cap counts only when it is on the path of the request being read:
//
//   - r.Body rebound from http.MaxBytesReader / io.LimitReader earlier in
//     the reading function, or a same-package cap-establishing helper
//     called with r first (limitBody(w, r) then io.ReadAll(r.Body));
//   - the mount credit: the FuncDecl around the read (the handler, or the
//     factory returning the handler literal) is referenced in the
//     package only from inside the arguments of a call to a middleware
//     that caps the request's Body before dispatch —
//     battery/semantic/routes.go mounts
//     limitBody(requireJSON(auth(indexHandler(idx)))), and a method value
//     limitBody(http.HandlerFunc(s.serve)) counts the same;
//   - the callers credit: every same-package caller of the reading
//     function capped the request it passes in, by any spelling above.
//
// Until bug bash 2026-10-27 I12 a cap counted when MaxBytesReader or
// LimitReader appeared anywhere in the same FILE (fileHasCap,
// unboundedbody.go:107-108,516-518): one io.LimitReader in one handler
// silenced an uncapped io.ReadAll(r.Body) in a separately mounted
// sibling, and the capped fixture pinned exactly that.
//
// The second posture is per-handler form parity, born from the 2026-09-03
// probe audit (entity_bodycap_red_test.go TestEntitySaveRedCapsBody on
// battery/admin entitySave, body_limit_red_test.go TestSetupFormRedCapsBody
// on battery/setup handleSubmit, formcaps_red_test.go
// TestSiteDemoRedCapsFormBodies on examples/site servePaletteSearch and
// WizardDemoHandler — all still open): r.ParseForm, r.ParseMultipartForm,
// r.FormValue, r.PostFormValue and r.FormFile read the WHOLE body, and the
// Go stdlib only refuses a urlencoded body at its own 10 MiB floor, ten
// times the 1 MiB the auth battery caps the same shape at
// (battery/auth/form_decode.go). The trigger is per handler function, not
// per file: a cap a sibling handler established is not this handler's cap,
// which is exactly how the probed sites sat in files whose other routes
// were capped.
//
// Form parsing requires a local or caller-side cap.
//
// Silent postures, deliberately:
//   - a bare r.Form / r.PostForm / r.MultipartForm read is not a trigger:
//     the selector reads an already-parsed map, and the body read happened
//     at the parse call, which is where this rule speaks (reporting both
//     would double-report one surface);
//   - only the FIRST trigger per request object per function reports
//     (entitySave's ParseForm plus its three r.PostForm scans are one
//     finding);
//   - method routing is invisible to a per-function rule: a FormValue in
//     a handler the router mounts GET-only never reads a body, but the
//     mount is not visible here, so such a site still reports and carries
//     an explicit cap (or a query-only accessor, r.URL.Query().Get)
//     instead. In-tree no such site exists today;
//   - a route wrapped in http.MaxBytesHandler at the mount site would cap
//     the handler invisibly to this rule; nothing in this repo uses
//     MaxBytesHandler today, and a site that gains one should say so next
//     to the parse or carry its own wrap;
//   - for other reads, function literals take mount credit from their
//     enclosing declaration and caller credit through named helpers;
//   - for the read posture, a handler the package never references (an
//     exported handler a host mounts) has no visible cap and reports,
//     and a "middleware" whose LimitReader wraps something other than a
//     request's Body caps nothing it is handed;
//   - _test.go files.
package unboundedbody

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/readsrc"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "unboundedbody",
	Doc:  "forbids reading or decoding an inbound *http.Request body with no size cap; wrap it in http.MaxBytesReader first",
	Run:  run,
}

// sinks maps body-read calls to the argument position of their reader.
var sinks = map[string]int{
	"io.ReadAll":      0,
	"ioutil.ReadAll":  0,
	"io.Copy":         1,
	"io.CopyBuffer":   1,
	"json.NewDecoder": 0,
	"xml.NewDecoder":  0,
	"yaml.NewDecoder": 0,
	"json.Unmarshal":  0,
	"io.ReadFull":     0,
}

// sinkArg returns the reader argument of a body read: a sinks
// entry, or ReadFrom on any io.ReaderFrom (buf.ReadFrom(r.Body) reads to
// EOF). The 2026-11-02 guard bash found ioutil.ReadAll, io.CopyBuffer,
// ReadFrom, a bufio.NewReader wrap and a `body := r.Body` alias all
// silent.
func sinkArg(pass *analysis.Pass, call *ast.CallExpr) ast.Expr {
	if argIdx, ok := sinks[astx.PkgFuncName(pass, call.Fun)]; ok {
		if argIdx < len(call.Args) {
			return call.Args[argIdx]
		}
		return nil
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ReadFrom" && len(call.Args) == 1 {
		return call.Args[0]
	}
	return nil
}

func run(pass *analysis.Pass) (any, error) {
	ix := buildIndex(pass)
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		formReports := map[formRead]bool{}
		aliases := readsrc.SingleBindings(pass, f)
		walkCalls(f, func(call *ast.CallExpr, stack []ast.Node) {
			fn, encl := funcsOf(stack)
			if fn == nil {
				return
			}
			ix.checkFormParse(call, fn, encl, stack, formReports)
			arg := sinkArg(pass, call)
			if arg == nil {
				return
			}
			sel, ok := readsrc.Source(pass, arg, aliases).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Body" {
				return
			}
			if !isInboundRequest(pass, sel.X) {
				return
			}
			if ix.sinkCapped(fn, encl, requestObject(pass, sel.X), call.Pos(), true, map[types.Object]bool{}, 0) {
				return
			}
			pass.Reportf(call.Pos(),
				"reads an inbound request body with no size cap: wrap it first (r.Body = http.MaxBytesReader(w, r.Body, max)) so one request cannot spend the server's memory")
		})
	}
	return nil, nil
}

// pkgIndex is the package-wide knowledge the per-request sink posture
// consults: cap-establishing functions, middleware that rebinds a
// request's Body, every named call site with its innermost and
// enclosing functions, and every reference to a package function with
// whether a capping middleware wraps it.
type pkgIndex struct {
	pass  *analysis.Pass
	caps  map[types.Object]bool
	mount map[types.Object]bool
	calls map[types.Object][]siteIn
	refs  map[types.Object][]bool // reference → wrapped by a rebinder's argument
}

// siteIn is one same-package call of a named function: the innermost
// function holding the call, the FuncDecl around it, and the call.
type siteIn struct {
	fn   ast.Node
	encl *ast.FuncDecl
	call *ast.CallExpr
}

func buildIndex(pass *analysis.Pass) *pkgIndex {
	caps, mount := bodyRebinders(pass)
	ix := &pkgIndex{
		pass:  pass,
		caps:  caps,
		mount: mount,
		calls: map[types.Object][]siteIn{},
		refs:  map[types.Object][]bool{},
	}
	decls := astx.FuncDecls(pass)
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj, ok := pass.TypesInfo.Uses[id].(*types.Func)
			if !ok || decls[obj] == nil {
				return true
			}
			fn, encl := funcsOf(stack)
			if encl == nil || pass.TypesInfo.Defs[encl.Name] != obj {
				ix.refs[obj] = append(ix.refs[obj], ix.wrappedByRebinder(stack))
			}
			if call := calleeCall(stack); call != nil && fn != nil {
				ix.calls[obj] = append(ix.calls[obj], siteIn{fn: fn, encl: encl, call: call})
			}
			return true
		})
	}
	return ix
}

// sinkCapped reports whether the request obj read at pos in fn (whose
// enclosing FuncDecl is encl) is capped ON ITS PATH: a rebind of its
// Body earlier in fn, a cap-establishing helper called with it earlier
// in fn, the enclosing function mounted only under a Body-rebinding
// middleware (when allowed), or every same-package caller of fn having capped the
// request it passes in (recursively, cycle-guarded).
func (ix *pkgIndex) sinkCapped(fn ast.Node, encl *ast.FuncDecl, obj types.Object, pos token.Pos, allowMount bool, visiting map[types.Object]bool, depth int) bool {
	if rebindCapped(ix.pass, fn, obj, pos) || helperCapped(ix.pass, fn, obj, pos, ix.caps) {
		return true
	}
	if encl == nil {
		return false
	}
	fo := ix.pass.TypesInfo.Defs[encl.Name]
	if fo == nil {
		return false
	}
	if allowMount && ix.mountCapped(fo) {
		return true
	}
	// The callers credit needs the request to be fn's own parameter: a
	// literal's request never arrives through the enclosing function's
	// call sites.
	if fn != ast.Node(encl) || depth > 4 || visiting[fo] || len(ix.calls[fo]) == 0 {
		return false
	}
	idx, ok := pathflow.ParamIndexes(ix.pass, encl)[obj]
	if !ok {
		return false
	}
	visiting[fo] = true
	defer delete(visiting, fo)
	for _, site := range ix.calls[fo] {
		callerObj := requestObject(ix.pass, pathflow.ArgAt(site.call, idx))
		if callerObj == nil {
			return false
		}
		if !ix.sinkCapped(site.fn, site.encl, callerObj, site.call.Pos(), allowMount, visiting, depth+1) {
			return false
		}
	}
	return true
}

// mountCapped reports whether every same-package reference to fo sits
// inside an argument of a call to a middleware that rebinds a request's
// Body from a capping constructor (battery/semantic routes.go:
// limitBody(requireJSON(auth(indexHandler(idx))))). A function the
// package never references (an exported handler mounted elsewhere) has
// no visible cap.
func (ix *pkgIndex) mountCapped(fo types.Object) bool {
	refs := ix.refs[fo]
	if len(refs) == 0 {
		return false
	}
	for _, wrapped := range refs {
		if !wrapped {
			return false
		}
	}
	return true
}

// wrappedByRebinder reports whether the innermost node of stack lies
// inside an ARGUMENT (not the callee) of a call to a Body-rebinding
// middleware.
func (ix *pkgIndex) wrappedByRebinder(stack []ast.Node) bool {
	for i := len(stack) - 2; i >= 0; i-- {
		call, ok := stack[i].(*ast.CallExpr)
		if !ok {
			continue
		}
		if stack[i+1] == call.Fun {
			continue
		}
		if callee := calleeObject(ix.pass, call.Fun); ix.mount[callee] {
			return true
		}
	}
	return false
}

// calleeCall returns the call whose callee the innermost node of stack
// is (the ident itself, or the Sel of a selector callee), or nil.
func calleeCall(stack []ast.Node) *ast.CallExpr {
	child := stack[len(stack)-1]
	for i := len(stack) - 2; i >= 0; i-- {
		switch parent := stack[i].(type) {
		case *ast.SelectorExpr:
			if parent.Sel != child {
				return nil
			}
		case *ast.ParenExpr:
		case *ast.CallExpr:
			if parent.Fun == child {
				return parent
			}
			return nil
		default:
			return nil
		}
		child = stack[i]
	}
	return nil
}

// funcsOf returns the innermost function node on stack and the
// FuncDecl around it.
func funcsOf(stack []ast.Node) (ast.Node, *ast.FuncDecl) {
	var fn ast.Node
	var encl *ast.FuncDecl
	for i := len(stack) - 1; i >= 0; i-- {
		switch v := stack[i].(type) {
		case *ast.FuncLit:
			if fn == nil {
				fn = v
			}
		case *ast.FuncDecl:
			if fn == nil {
				fn = v
			}
			encl = v
			return fn, encl
		}
	}
	return fn, encl
}

// walkCalls visits each call and its enclosing AST nodes.
func walkCalls(f *ast.File, visit func(*ast.CallExpr, []ast.Node)) {
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		if call, ok := n.(*ast.CallExpr); ok {
			visit(call, stack)
		}
		return true
	})
}

// calleeObject resolves a callee expression to its object.
func calleeObject(pass *analysis.Pass, fun ast.Expr) types.Object {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return pass.TypesInfo.ObjectOf(f)
	case *ast.SelectorExpr:
		if sel, ok := pass.TypesInfo.Selections[f]; ok {
			return sel.Obj()
		}
		return pass.TypesInfo.ObjectOf(f.Sel)
	}
	return nil
}

// rebindsRequestBody reports whether body (literals included) assigns
// an *http.Request's Body from a capping constructor: the middleware
// spelling, r.Body = http.MaxBytesReader(w, r.Body, n). A function
// with a LimitReader over some other reader caps nothing it wraps.
func rebindsRequestBody(pass *analysis.Pass, body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || found {
			return !found
		}
		for i, lhs := range assign.Lhs {
			sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Body" || i >= len(assign.Rhs) {
				continue
			}
			if isInboundRequest(pass, sel.X) && hasCapCall(assign.Rhs[i]) {
				found = true
			}
		}
		return !found
	})
	return found
}

func bodyRebinders(pass *analysis.Pass) (helpers, middleware map[types.Object]bool) {
	helpers, middleware = map[types.Object]bool{}, map[types.Object]bool{}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			decl, ok := d.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				continue
			}
			if obj, ok := pass.TypesInfo.Defs[decl.Name].(*types.Func); ok {
				if rebindsRequestBody(pass, decl.Body) {
					helpers[obj] = true
				}
				if capsBeforeDispatch(pass, decl, obj.Signature()) {
					middleware[obj] = true
				}
			}
		}
	}
	return helpers, middleware
}

func capsBeforeDispatch(pass *analysis.Pass, decl *ast.FuncDecl, sig *types.Signature) bool {
	if sig.Params().Len() != 1 || !astx.IsNamed(types.Unalias(sig.Params().At(0).Type()), "net/http", "Handler") ||
		len(decl.Body.List) != 1 {
		return false
	}
	ret, ok := decl.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	wrapper, ok := ast.Unparen(ret.Results[0]).(*ast.CallExpr)
	if !ok || len(wrapper.Args) != 1 || !astx.IsNamed(types.Unalias(pass.TypesInfo.TypeOf(wrapper.Fun)), "net/http", "HandlerFunc") {
		return false
	}
	handler, ok := ast.Unparen(wrapper.Args[0]).(*ast.FuncLit)
	if !ok || len(handler.Body.List) < 2 {
		return false
	}
	stmts := handler.Body.List
	dispatch, ok := stmts[len(stmts)-1].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := ast.Unparen(dispatch.X).(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	next := sig.Params().At(0)
	if !ok || sel.Sel.Name != "ServeHTTP" || calleeObject(pass, sel.X) != next {
		return false
	}
	request := requestObject(pass, call.Args[1])
	handlerSignature := pass.TypesInfo.TypeOf(handler).(*types.Signature)
	writer := calleeObject(pass, call.Args[0])
	if request == nil || request != handlerSignature.Params().At(1) || writer != handlerSignature.Params().At(0) {
		return false
	}
	for n := range ast.Preorder(handler.Body) {
		if id, ok := n.(*ast.Ident); ok && pass.TypesInfo.Uses[id] == next && id != ast.Unparen(sel.X) {
			return false
		}
	}
	assign, ok := stmts[len(stmts)-2].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}
	target, ok := ast.Unparen(assign.Lhs[0]).(*ast.SelectorExpr)
	if !ok || target.Sel.Name != "Body" || !sameRequest(pass, target.X, request) {
		return false
	}
	limit, ok := ast.Unparen(assign.Rhs[0]).(*ast.CallExpr)
	if !ok || len(limit.Args) != 3 || calleeObject(pass, limit.Args[0]) != writer ||
		pass.TypesInfo.Types[limit.Args[2]].Value == nil {
		return false
	}
	fn, ok := calleeObject(pass, limit.Fun).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "net/http" || fn.Name() != "MaxBytesReader" {
		return false
	}
	body, ok := ast.Unparen(limit.Args[1]).(*ast.SelectorExpr)
	return ok && body.Sel.Name == "Body" && sameRequest(pass, body.X, request)
}

// parseMethods are the *http.Request methods that read the whole body.
// ParseForm and ParseMultipartForm read it outright; FormValue,
// PostFormValue and FormFile parse on first use. A bare r.Form or
// r.PostForm read is deliberately absent: the map is already parsed by
// then, and the parse call is the site this rule reports.
var parseMethods = map[string]bool{
	"ParseForm":          true,
	"ParseMultipartForm": true,
	"FormValue":          true,
	"PostFormValue":      true,
	"FormFile":           true,
}

type formRead struct {
	fn  ast.Node
	req types.Object
}

// checkFormParse reports the first uncapped parse for each request in fn.
func (ix *pkgIndex) checkFormParse(call *ast.CallExpr, fn ast.Node, encl *ast.FuncDecl, stack []ast.Node, reported map[formRead]bool) {
	pass := ix.pass
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || !parseMethods[sel.Sel.Name] || !isInboundRequest(pass, sel.X) || !hasRequestParam(pass, stack) {
		return
	}
	read := formRead{fn: fn, req: requestObject(pass, sel.X)}
	if reported[read] || ix.sinkCapped(fn, encl, read.req, call.Pos(), false, map[types.Object]bool{}, 0) {
		return
	}
	pass.Reportf(call.Pos(),
		"parses the request form with no cap of its own: the stdlib floor is 10 MiB per urlencoded body, so one request can park that in memory — wrap r.Body in http.MaxBytesReader(w, r.Body, cap) before ParseForm and map *http.MaxBytesError to 413")
	if read.req != nil {
		reported[read] = true
	}
}

// rebindCapped: an assignment in fn, before pos, rebinds this request's
// Body from a capping constructor (the battery/auth spelling).
func rebindCapped(pass *analysis.Pass, fn ast.Node, obj types.Object, pos token.Pos) bool {
	found := false
	ast.Inspect(astx.BodyOf(fn), func(n ast.Node) bool {
		if _, nested := n.(*ast.FuncLit); nested {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Pos() >= pos {
			return !found
		}
		for i, lhs := range assign.Lhs {
			sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Body" || i >= len(assign.Rhs) {
				continue
			}
			if !sameRequest(pass, sel.X, obj) {
				continue
			}
			if hasCapCall(assign.Rhs[i]) {
				found = true
			}
		}
		return !found
	})
	return found
}

// helperCapped: a call in fn, before pos, hands this request to a
// same-package helper whose own body establishes a cap (the crud
// spelling: limitRequestBody(w, r) then readRequestBody(r)).
func helperCapped(pass *analysis.Pass, fn ast.Node, obj types.Object, pos token.Pos, caps map[types.Object]bool) bool {
	found := false
	ast.Inspect(astx.BodyOf(fn), func(n ast.Node) bool {
		if _, nested := n.(*ast.FuncLit); nested {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= pos {
			return !found
		}
		callee := calleeObject(pass, call.Fun)
		if callee == nil || !caps[callee] {
			return !found
		}
		for _, arg := range call.Args {
			if sameRequest(pass, arg, obj) {
				found = true
			}
		}
		return !found
	})
	return found
}

// requestObject resolves e to its *http.Request variable object, nil
// when e is not a plain request identifier.
func requestObject(pass *analysis.Pass, e ast.Expr) types.Object {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(id)
	if obj == nil {
		return nil
	}
	if _, ok := obj.(*types.Var); !ok {
		return nil
	}
	return obj
}

// sameRequest reports whether e is the request variable obj (or, when
// obj is nil, any inbound request — a trigger whose receiver is not a
// plain identifier still deserves the function-level credits).
func sameRequest(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	if obj == nil {
		return isInboundRequest(pass, e)
	}
	return requestObject(pass, e) == obj
}

// hasRequestParam reports whether a containing function names an inbound request parameter.
func hasRequestParam(pass *analysis.Pass, stack []ast.Node) bool {
	for _, n := range stack {
		var params *ast.FieldList
		switch fn := n.(type) {
		case *ast.FuncDecl:
			params = fn.Type.Params
		case *ast.FuncLit:
			params = fn.Type.Params
		}
		if params == nil {
			continue
		}
		for _, field := range params.List {
			for _, name := range field.Names {
				if obj := pass.TypesInfo.Defs[name]; obj != nil && isRequestType(obj.Type()) {
					return true
				}
			}
		}
	}
	return false
}

// isInboundRequest reports whether expr is a *net/http.Request — the
// inbound side. An *http.Response body is a different risk class and is
// deliberately not flagged.
func isInboundRequest(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok {
		return false
	}
	return isRequestType(tv.Type)
}

// isRequestType: *net/http.Request (or an http.Request value).
func isRequestType(t types.Type) bool {
	named := astx.NamedOf(t)
	return named != nil && astx.IsNamed(named, "net/http", "Request")
}

// hasCapCall reports whether n mentions one of the standard capping
// constructors.
func hasCapCall(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "MaxBytesReader", "LimitReader", "LimitedReader":
			found = true
		}
		return !found
	})
	return found
}
