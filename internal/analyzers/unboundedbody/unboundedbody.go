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
// A cap counts when http.MaxBytesReader or io.LimitReader appears anywhere
// in the same file: middleware that wraps a body is conventionally written
// beside the handlers it wraps (battery/semantic/routes.go is the model),
// and demanding the wrap be in the same function would flag correct code.
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
//   - _test.go files.
package unboundedbody

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

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
	"io.Copy":           1,
	"json.NewDecoder":   0,
	"xml.NewDecoder":    0,
	"json.Unmarshal":    0,
	"httputil.DumpBody": 0,
	"io.ReadFull":       0,
}

func run(pass *analysis.Pass) (any, error) {
	cappers := cappingHelpers(pass)
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		checkFormParse(pass, f)
		if fileHasCap(f, cappers) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			argIdx, ok := sinks[qualifiedName(pass, call.Fun)]
			if !ok || argIdx >= len(call.Args) {
				return true
			}
			sel, ok := call.Args[argIdx].(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Body" {
				return true
			}
			if !isInboundRequest(pass, sel.X) {
				return true
			}
			pass.Reportf(call.Pos(),
				"reads an inbound request body with no size cap: wrap it first (r.Body = http.MaxBytesReader(w, r.Body, max)) so one request cannot spend the server's memory")
			return true
		})
	}
	return nil, nil
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
	for _, fn := range allFuncs(f) {
		params := requestParams(pass, fn)
		if len(params) == 0 {
			continue
		}
		reported := map[types.Object]bool{}
		ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
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
			sel, ok := unparen(lhs).(*ast.SelectorExpr)
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
		switch fun := unparen(call.Fun).(type) {
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
		ast.Inspect(bodyOf(fn), func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var callee types.Object
			switch fun := unparen(call.Fun).(type) {
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

// allFuncs yields every function body in f: declarations plus literals
// (handlers are usually literals returned from factories).
func allFuncs(f *ast.File) []ast.Node {
	var out []ast.Node
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
			out = append(out, fn)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok && lit.Body != nil {
			out = append(out, lit)
		}
		return true
	})
	return out
}

// allFuncsAll is allFuncs over every non-test file of the package
// (includeTests=false), or every file (true).
func allFuncsAll(pass *analysis.Pass, includeTests bool) []ast.Node {
	var out []ast.Node
	for _, f := range pass.Files {
		if !includeTests && isTestFile(pass, f) {
			continue
		}
		out = append(out, allFuncs(f)...)
	}
	return out
}

// isTestFile: _test.go, the posture both halves of this rule skip.
func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}

// requestObject resolves e to its *http.Request variable object, nil
// when e is not a plain request identifier.
func requestObject(pass *analysis.Pass, e ast.Expr) types.Object {
	id, ok := unparen(e).(*ast.Ident)
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

// bodyOf returns the function node's body, nil when it has none.
func bodyOf(fn ast.Node) *ast.BlockStmt {
	switch fn := fn.(type) {
	case *ast.FuncDecl:
		return fn.Body
	case *ast.FuncLit:
		return fn.Body
	}
	return nil
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

// unparen strips parentheses around an expression.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
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

// cappingHelpers collects the package's own functions that establish a body
// cap, so a package that factors the wrap into a named helper
// (`limitJSONBody(w, r)`) is read the same as one that inlines
// http.MaxBytesReader. Without this the analyzer sees only the file it is
// looking at, and factoring out the wrap — the tidier way to write it —
// would fail the check.
func cappingHelpers(pass *analysis.Pass) map[string]bool {
	out := map[string]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			if hasCapCall(fn.Body) {
				out[fn.Name.Name] = true
			}
		}
	}
	return out
}

// fileHasCap reports whether this file establishes a body cap anywhere,
// either directly or by calling one of the package's capping helpers.
// Middleware that wraps a body lives beside its handlers by convention.
func fileHasCap(f *ast.File, cappers map[string]bool) bool {
	if hasCapCall(f) {
		return true
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && cappers[id.Name] {
			found = true
		}
		return !found
	})
	return found
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

// qualifiedName renders a call target as "pkg.Func", resolving the import
// through the type checker so an aliased import is still the real package.
func qualifiedName(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkgName, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkgName.Imported().Name() + "." + sel.Sel.Name
}
