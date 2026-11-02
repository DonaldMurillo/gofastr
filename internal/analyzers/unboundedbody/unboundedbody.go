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
// A read is any whole-body consumer: io.ReadAll / ioutil.ReadAll,
// io.Copy / io.CopyBuffer, io.ReadFull, ReadFrom on any io.ReaderFrom, a
// json/xml/yaml decoder, httputil.DumpBody. The body may reach it as
// r.Body, through a local bound once to it (body := r.Body), or through
// bufio.NewReader / NewReaderSize / io.TeeReader, none of which caps
// anything (widened 2026-11-02: each of those spellings was silent).
//
// A cap counts only when it is on the path of the request being read:
//
//   - r.Body rebound from http.MaxBytesReader / io.LimitReader earlier in
//     the reading function, or a same-package cap-establishing helper
//     called with r first (limitBody(w, r) then io.ReadAll(r.Body));
//   - the mount credit: the FuncDecl around the read (the handler, or the
//     factory returning the handler literal) is referenced in the
//     package only from inside the arguments of a call to a middleware
//     that rebinds a request's Body from a capping constructor —
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
// A cap counts for the ParseForm posture when it is established on the
// same request object BEFORE the parse:
//
//   - r.Body is reassigned from http.MaxBytesReader / io.LimitReader in
//     the same function (the battery/auth spelling);
//   - a same-package helper that establishes the cap (its own body wraps
//     a request) is called first with this request (the crud spelling:
//     limitRequestBody then readRequestBody);
//   - every same-package caller of this function has already capped the
//     request it passes in, by either spelling above, before the call
//     (the parse-helper credit: framework/crud parseMultipartBody is
//     capped by its caller's limitRequestBody, and that pre-condition is
//     the function's own documented contract).
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
//   - function literals get no caller credit (they are mounted as values,
//     not called by name), which is the handler shape and stays reported;
//     a literal's read takes the mount credit of the FuncDecl around it;
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
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "unboundedbody",
	Doc:  "forbids reading or decoding an inbound *http.Request body with no size cap; wrap it in http.MaxBytesReader first",
	Run:  run,
}

// sinks are the calls that consume a whole body. The int is the argument
// position holding the reader.
var sinks = map[string]int{
	"io.ReadAll":        0,
	"ioutil.ReadAll":    0,
	"io.Copy":           1,
	"io.CopyBuffer":     1,
	"json.NewDecoder":   0,
	"xml.NewDecoder":    0,
	"yaml.NewDecoder":   0,
	"json.Unmarshal":    0,
	"httputil.DumpBody": 0,
	"io.ReadFull":       0,
}

// sinkArg returns the reader argument of a whole-body read: a sinks
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

// bodySource sees through what does not cap a read: parentheses, the
// pass-through wrappers bufio.NewReader / NewReaderSize / io.TeeReader,
// and a local bound exactly once (body := r.Body). A limiter is not
// unwrapped: a read through io.LimitReader is bounded.
func bodySource(pass *analysis.Pass, e ast.Expr, aliases map[types.Object]ast.Expr) ast.Expr {
	for range 8 {
		e = ast.Unparen(e)
		switch x := e.(type) {
		case *ast.Ident:
			b, ok := aliases[pass.TypesInfo.ObjectOf(x)]
			if !ok {
				return e
			}
			e = b
		case *ast.CallExpr:
			switch astx.PkgFuncName(pass, x.Fun) {
			case "bufio.NewReader", "bufio.NewReaderSize", "io.TeeReader":
				if len(x.Args) == 0 {
					return e
				}
				e = x.Args[0]
			default:
				return e
			}
		default:
			return e
		}
	}
	return e
}

// singleBindings maps each local bound exactly once in f, by a
// single-value assignment or declaration, to its value.
func singleBindings(pass *analysis.Pass, f *ast.File) map[types.Object]ast.Expr {
	out := map[types.Object]ast.Expr{}
	count := map[types.Object]int{}
	bind := func(id *ast.Ident, v ast.Expr) {
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil && id.Name != "_" {
			count[obj]++
			out[obj] = v
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if len(st.Lhs) == len(st.Rhs) {
				for i, lhs := range st.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						bind(id, st.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			if len(st.Names) == len(st.Values) {
				for i, id := range st.Names {
					bind(id, st.Values[i])
				}
			}
		}
		return true
	})
	for obj, n := range count {
		if n > 1 {
			delete(out, obj)
		}
	}
	return out
}

func run(pass *analysis.Pass) (any, error) {
	ix := buildIndex(pass)
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		checkFormParse(pass, f)
		aliases := singleBindings(pass, f)
		walkWithFuncs(f, func(n ast.Node, fn ast.Node, encl *ast.FuncDecl) {
			call, ok := n.(*ast.CallExpr)
			if !ok || fn == nil {
				return
			}
			arg := sinkArg(pass, call)
			if arg == nil {
				return
			}
			sel, ok := bodySource(pass, arg, aliases).(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Body" {
				return
			}
			if !isInboundRequest(pass, sel.X) {
				return
			}
			if ix.sinkCapped(fn, encl, requestObject(pass, sel.X), call.Pos(), map[types.Object]bool{}, 0) {
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
	pass      *analysis.Pass
	caps      map[types.Object]bool
	rebinders map[types.Object]bool
	decls     map[types.Object]*ast.FuncDecl
	calls     map[types.Object][]siteIn
	refs      map[types.Object][]bool // reference → wrapped by a rebinder's argument
}

// siteIn is one same-package call of a named function: the innermost
// function holding the call, the FuncDecl around it, and the call.
type siteIn struct {
	fn   ast.Node
	encl *ast.FuncDecl
	call *ast.CallExpr
}

func buildIndex(pass *analysis.Pass) *pkgIndex {
	ix := &pkgIndex{
		pass:      pass,
		caps:      cappingObjects(pass),
		rebinders: bodyRebinders(pass),
		decls:     funcDecls(pass),
		calls:     map[types.Object][]siteIn{},
		refs:      map[types.Object][]bool{},
	}
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
			obj := pass.TypesInfo.Uses[id]
			if obj == nil || ix.decls[obj] == nil {
				return true
			}
			fn, encl := funcsOf(stack)
			if encl != nil && pass.TypesInfo.Defs[encl.Name] == obj {
				return true // recursion is no mount
			}
			ix.refs[obj] = append(ix.refs[obj], ix.wrappedByRebinder(stack))
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
// middleware, or every same-package caller of fn having capped the
// request it passes in (recursively, cycle-guarded).
func (ix *pkgIndex) sinkCapped(fn ast.Node, encl *ast.FuncDecl, obj types.Object, pos token.Pos, visiting map[types.Object]bool, depth int) bool {
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
	if ix.mountCapped(fo) {
		return true
	}
	// The callers credit needs the request to be fn's own parameter: a
	// literal's request never arrives through the enclosing function's
	// call sites.
	if fn != ast.Node(encl) || depth > 4 || visiting[fo] || len(ix.calls[fo]) == 0 {
		return false
	}
	idx := paramIndex(ix.pass, encl, obj)
	if idx < 0 {
		return false
	}
	visiting[fo] = true
	defer delete(visiting, fo)
	for _, site := range ix.calls[fo] {
		callerObj := requestObject(ix.pass, argAt(site.call, idx))
		if callerObj == nil {
			return false
		}
		if !ix.sinkCapped(site.fn, site.encl, callerObj, site.call.Pos(), visiting, depth+1) {
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
		if callee := calleeObject(ix.pass, call.Fun); callee != nil && ix.rebinders[callee] {
			return true
		}
	}
	return false
}

// calleeCall returns the call whose callee the innermost node of stack
// is (the ident itself, or the Sel of a selector callee), or nil.
func calleeCall(stack []ast.Node) *ast.CallExpr {
	n := len(stack)
	if n < 2 {
		return nil
	}
	child := stack[n-1]
	parent := stack[n-2]
	if sel, ok := parent.(*ast.SelectorExpr); ok && sel.Sel == child && n >= 3 {
		child, parent = sel, stack[n-3]
	}
	if call, ok := parent.(*ast.CallExpr); ok && call.Fun == child {
		return call
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

// walkWithFuncs visits every node of f with its innermost function and
// enclosing FuncDecl.
func walkWithFuncs(f *ast.File, visit func(n, fn ast.Node, encl *ast.FuncDecl)) {
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		fn, encl := funcsOf(stack)
		visit(n, fn, encl)
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

// bodyRebinders maps each package function whose body (literals
// included) assigns an *http.Request's Body from a capping constructor:
// the middleware spelling, r.Body = http.MaxBytesReader(w, r.Body, n).
// A function with a LimitReader over some other reader caps nothing it
// wraps.
func bodyRebinders(pass *analysis.Pass) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			decl, ok := d.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				continue
			}
			found := false
			ast.Inspect(decl.Body, func(n ast.Node) bool {
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
			if found {
				if obj := pass.TypesInfo.Defs[decl.Name]; obj != nil {
					out[obj] = true
				}
			}
		}
	}
	return out
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

// checkFormParse runs the per-handler form-parity posture over one file.
// Each function (declarations and literals alike, handlers are usually
// literals) is judged on its own request parameter: did anything cap that
// request before the first body-reading call?
func checkFormParse(pass *analysis.Pass, f *ast.File) {
	caps := cappingObjects(pass)
	callers := callerSites(pass)
	decls := funcDecls(pass)
	for _, fn := range astx.AllFuncs(f) {
		params := requestParams(pass, fn)
		if len(params) == 0 {
			continue
		}
		reported := map[types.Object]bool{}
		ast.Inspect(astx.BodyOf(fn), func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok || !parseMethods[sel.Sel.Name] {
				return true
			}
			if !isInboundRequest(pass, sel.X) {
				return true
			}
			obj := requestObject(pass, sel.X)
			if obj != nil && reported[obj] {
				return true
			}
			if parseCapped(pass, fn, obj, call.Pos(), caps, callers, decls, map[types.Object]bool{}, 0) {
				return true
			}
			pass.Reportf(call.Pos(),
				"parses the request form with no cap of its own: the stdlib floor is 10 MiB per urlencoded body, so one request can park that in memory — wrap r.Body in http.MaxBytesReader(w, r.Body, cap) before ParseForm and map *http.MaxBytesError to 413")
			if obj != nil {
				reported[obj] = true
			}
			return true
		})
	}
}

// parseCapped reports whether the request object obj (nil: any inbound
// request) is already capped at pos in fn: a rebind of this request's
// Body from a capping constructor earlier in fn, a call to a
// cap-establishing helper with this request earlier in fn, or every
// same-package caller of fn having capped the request it passes in
// before the call (one level of recursion, cycle-guarded).
func parseCapped(pass *analysis.Pass, fn ast.Node, obj types.Object, pos token.Pos,
	caps map[types.Object]bool, callers map[types.Object][]callSite, decls map[types.Object]*ast.FuncDecl,
	visiting map[types.Object]bool, depth int) bool {
	if rebindCapped(pass, fn, obj, pos) || helperCapped(pass, fn, obj, pos, caps) {
		return true
	}
	if depth > 4 {
		return false
	}
	fo := funcObject(pass, fn)
	if fo == nil || visiting[fo] || len(callers[fo]) == 0 {
		return false
	}
	visiting[fo] = true
	defer delete(visiting, fo)
	idx := paramIndex(pass, decls[fo], obj)
	if idx < 0 {
		return false
	}
	for _, site := range callers[fo] {
		arg := argAt(site.call, idx)
		callerObj := requestObject(pass, arg)
		if callerObj == nil {
			return false
		}
		if !parseCapped(pass, site.fn, callerObj, site.call.Pos(), caps, callers, decls, visiting, depth+1) {
			return false
		}
	}
	return true
}

// rebindCapped: an assignment in fn, before pos, rebinds this request's
// Body from a capping constructor (the battery/auth spelling).
func rebindCapped(pass *analysis.Pass, fn ast.Node, obj types.Object, pos token.Pos) bool {
	found := false
	ast.Inspect(fn, func(n ast.Node) bool {
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
	ast.Inspect(fn, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() >= pos {
			return !found
		}
		var callee types.Object
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.Ident:
			callee = pass.TypesInfo.ObjectOf(fun)
		case *ast.SelectorExpr:
			if sel, ok := pass.TypesInfo.Selections[fun]; ok {
				callee = sel.Obj()
			}
		}
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

// cappingObjects maps every package-level function or method whose body
// establishes a body cap to its object, for the helper-called-first
// credit. Methods included: crud's limitRequestBody shape is a plain
// function but nothing about the credit should depend on that.
func cappingObjects(pass *analysis.Pass) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, fn := range allFuncsAll(pass, false) {
		decl, ok := fn.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if hasCapCall(decl.Body) {
			if obj := pass.TypesInfo.Defs[decl.Name]; obj != nil {
				out[obj] = true
			}
		}
	}
	return out
}

// callSite is one same-package call of a named function: the enclosing
// function node and the call itself.
type callSite struct {
	fn   ast.Node
	call *ast.CallExpr
}

// callerSites indexes every same-package call of a named function or
// method by the callee's object. Handler literals are mounted as values,
// never called by name, so they keep zero callers and no credit. Test
// files are excluded: a unit test calling a helper directly proves
// nothing about the production surfaces that reach it.
func callerSites(pass *analysis.Pass) map[types.Object][]callSite {
	out := map[types.Object][]callSite{}
	for _, fn := range allFuncsAll(pass, false) {
		ast.Inspect(astx.BodyOf(fn), func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var callee types.Object
			switch fun := ast.Unparen(call.Fun).(type) {
			case *ast.Ident:
				callee = pass.TypesInfo.ObjectOf(fun)
			case *ast.SelectorExpr:
				if sel, ok := pass.TypesInfo.Selections[fun]; ok {
					callee = sel.Obj()
				}
			}
			if callee == nil {
				return true
			}
			out[callee] = append(out[callee], callSite{fn: fn, call: call})
			return true
		})
	}
	return out
}

// funcDecls maps each package-level declaration's object to its node.
func funcDecls(pass *analysis.Pass) map[types.Object]*ast.FuncDecl {
	out := map[types.Object]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if obj := pass.TypesInfo.Defs[fn.Name]; obj != nil {
				out[obj] = fn
			}
		}
	}
	return out
}

// allFuncsAll is allFuncs over every non-test file of the package
// (includeTests=false), or every file (true).
func allFuncsAll(pass *analysis.Pass, includeTests bool) []ast.Node {
	var out []ast.Node
	for _, f := range pass.Files {
		if !includeTests && pathflow.IsTestFile(pass, f) {
			continue
		}
		out = append(out, astx.AllFuncs(f)...)
	}
	return out
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

// requestParams collects fn's *http.Request parameter objects. Literals
// count: handlers are usually literals returned from factories.
func requestParams(pass *analysis.Pass, fn ast.Node) []types.Object {
	var params *ast.FieldList
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		params = fn.Type.Params
	case *ast.FuncLit:
		params = fn.Type.Params
	default:
		return nil
	}
	if params == nil {
		return nil
	}
	var out []types.Object
	for _, field := range params.List {
		for _, name := range field.Names {
			obj, ok := pass.TypesInfo.ObjectOf(name).(*types.Var)
			// ObjectOf, not Types: a DEFINING ident (the parameter name)
			// has no Types entry, its type lives on the object.
			if ok && isRequestType(obj.Type()) {
				out = append(out, obj)
			}
		}
	}
	return out
}

// paramIndex returns the positional index of obj among decl's
// parameters, or -1 (the parse-helper credit needs to know which call
// argument carries the request being judged).
func paramIndex(pass *analysis.Pass, decl *ast.FuncDecl, obj types.Object) int {
	if decl == nil || decl.Type.Params == nil {
		return -1
	}
	idx := 0
	for _, field := range decl.Type.Params.List {
		for _, name := range field.Names {
			if pass.TypesInfo.ObjectOf(name) == obj {
				return idx
			}
			idx++
		}
	}
	return -1
}

// funcObject resolves a function node to its object (literals have none).
func funcObject(pass *analysis.Pass, fn ast.Node) types.Object {
	if ft, ok := fn.(*ast.FuncDecl); ok {
		return pass.TypesInfo.Defs[ft.Name]
	}
	return nil
}

// argAt returns call's argument at index i, nil when out of range.
func argAt(call *ast.CallExpr, i int) ast.Expr {
	if i < 0 || i >= len(call.Args) {
		return nil
	}
	return call.Args[i]
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
	if ptr, isPtr := t.(*types.Pointer); isPtr {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Name() == "Request" &&
		obj.Pkg() != nil && obj.Pkg().Path() == "net/http"
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
