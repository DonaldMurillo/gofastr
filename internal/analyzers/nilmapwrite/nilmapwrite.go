// Package nilmapwrite catches an exported function whose map parameter
// can be nil where something writes into it: an app callback it hands
// the map to, or the function's own working copy.
//
// The bug (round 12 S2-09, 0a5181f4b): crud's typed UpdateOne(ctx, id,
// body) passed body to doUpdate, which ran the entity's BeforeUpdate
// hooks with it. A caller with nothing to set passes nil, the zero
// value of the map type, and a hook that set a field panicked with
// "assignment to entry in nil map", while an empty body worked. Reading
// a nil map is fine in Go, so every function on the path looked
// correct; only the hook's write panics, and the hook is app code the
// framework cannot see. The fix starts doUpdate from an empty map.
//
// Two arms, both on an exported function or method with a parameter p
// whose type is a map, and both quiet once p is checked first (a
// comparison with nil, a comparison of len(p) with 0 or 1, or an
// assignment to p before the site in source order):
//   - callback: p is passed, as a map-typed parameter, to a callback (a
//     call through a func-typed value such as a field, a parameter, a
//     map entry or a local, or a method on an interface), or to a
//     package-local function or method that passes that parameter to
//     one, through any number of hops. The callback may write into the
//     map, and nothing on the path made it. A callback that takes the
//     map as any (a template's Execute) is taken to read it.
//   - working copy: p[k] is assigned (`p[k] = v`, `p[k] += v`, `p[k]++`)
//     and p is then returned or passed on as a call argument. The
//     function uses the map as its own value, so it can make one when it
//     is nil. (Button.Render setting attrs["type"] then handing attrs to
//     the element builder is this shape.)
//
// Silent postures, deliberately:
//   - a function that only fills p in place (writes, reads, len,
//     delete, range, and nothing that hands p on after the write):
//     nothing inside it can fix a nil map, because the caller would
//     never see the map it made, so the caller owns passing one
//     (InjectOwner, AttrNames, RecordLocalBindings);
//   - unexported functions are not reported, though the callback arm
//     follows calls through them: the exported entry above them is
//     where nil arrives;
//   - _test.go files.
//
// Not covered: a map decoded from a JSON `null` and then treated as an
// empty write body (S2-07, S2-11). Whether a nil map is wrong there
// depends on what the caller does with it (an optional object reads
// fine as nil), so the null-body refusal is held by crud's and mcp's
// own tests (TestToolsCallRefusesNullArguments and the S2-07 tests).
package nilmapwrite

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "nilmapwrite",
	Doc:  "report an exported function whose map parameter may be nil where a callback or the function itself writes into it: the write panics",
	Run:  run,
}

type checker struct {
	pass  *analysis.Pass
	decls map[*types.Func]*ast.FuncDecl
	// memo caches whether parameter i of a function reaches a callback
	// unguarded; seen breaks recursion.
	memo map[paramKey]*ast.CallExpr
	seen map[paramKey]bool
}

type paramKey struct {
	fn *types.Func
	i  int
}

func run(pass *analysis.Pass) (any, error) {
	c := &checker{pass: pass, decls: map[*types.Func]*ast.FuncDecl{}, memo: map[paramKey]*ast.CallExpr{}, seen: map[paramKey]bool{}}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					c.decls[fn] = fd
				}
			}
		}
	}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !fd.Name.IsExported() {
				continue
			}
			c.checkFunc(fd)
		}
	}
	return nil, nil
}

func (c *checker) checkFunc(fd *ast.FuncDecl) {
	fn, _ := c.pass.TypesInfo.Defs[fd.Name].(*types.Func)
	for i, obj := range c.params(fd) {
		if obj == nil {
			continue
		}
		if _, ok := obj.Type().Underlying().(*types.Map); !ok {
			continue
		}
		name := obj.Name()
		// The function's own write panics before any callback runs.
		if w := c.workingCopyWrite(fd.Body, obj); w != nil {
			c.pass.Reportf(w.Pos(), "%s writes into its map parameter %s and then hands it on, and a caller may pass nil: the write panics. Make %s if it is nil first", fd.Name.Name, name, name)
			continue
		}
		if call := c.reachesCallback(paramKey{fn, i}); call != nil {
			c.pass.Reportf(call.Pos(), "%s hands its map parameter %s to a callback that may write into it, and a caller may pass nil: the write panics. Make %s if it is nil first", fd.Name.Name, name, name)
		}
	}
}

// params lists the declared parameters in order, nil for an unnamed one.
func (c *checker) params(fd *ast.FuncDecl) []types.Object {
	var out []types.Object
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			out = append(out, nil)
			continue
		}
		for _, name := range field.Names {
			out = append(out, c.pass.TypesInfo.Defs[name])
		}
	}
	return out
}

// reachesCallback returns the call in fn's body through which parameter
// i reaches a callback with no guard before it, or nil.
func (c *checker) reachesCallback(k paramKey) *ast.CallExpr {
	if call, ok := c.memo[k]; ok {
		return call
	}
	if c.seen[k] {
		return nil
	}
	c.seen[k] = true
	fd := c.decls[k.fn]
	var found *ast.CallExpr
	if fd != nil {
		params := c.params(fd)
		if k.i < len(params) && params[k.i] != nil {
			found = c.callbackCall(fd.Body, params[k.i])
		}
	}
	c.memo[k] = found
	return found
}

func (c *checker) callbackCall(body *ast.BlockStmt, obj types.Object) *ast.CallExpr {
	var found *ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for j, arg := range call.Args {
			if !c.is(arg, obj) || c.guardedBefore(body, obj, call.Pos()) {
				continue
			}
			if c.isCallbackCall(call) && c.takesMap(call, j) {
				found = call
				return false
			}
			if callee := c.localCallee(call); callee != nil && c.reachesCallback(paramKey{callee, j}) != nil {
				found = call
				return false
			}
		}
		return true
	})
	return found
}

// isCallbackCall reports a call through a func-typed value rather than a
// declared function, method, builtin or conversion.
func (c *checker) isCallbackCall(call *ast.CallExpr) bool {
	fun := ast.Unparen(call.Fun)
	if tv, ok := c.pass.TypesInfo.Types[fun]; !ok || tv.IsType() || tv.IsBuiltin() {
		return false
	}
	if _, ok := c.pass.TypesInfo.TypeOf(fun).Underlying().(*types.Signature); !ok {
		return false
	}
	switch f := fun.(type) {
	case *ast.Ident:
		_, declared := c.pass.TypesInfo.Uses[f].(*types.Func)
		return !declared
	case *ast.SelectorExpr:
		if sel, ok := c.pass.TypesInfo.Selections[f]; ok {
			if sel.Kind() == types.FieldVal {
				return true
			}
			// A method through an interface is whatever the host
			// installed behind it.
			_, iface := sel.Recv().Underlying().(*types.Interface)
			return sel.Kind() == types.MethodVal && iface
		}
		_, declared := c.pass.TypesInfo.Uses[f.Sel].(*types.Func)
		return !declared
	case *ast.FuncLit:
		return false
	}
	// A map entry, an index into a slice of funcs, a call result.
	return true
}

// takesMap reports whether the callee's parameter j is itself a map: a
// callback taking any (a template's Execute) reads its data.
func (c *checker) takesMap(call *ast.CallExpr, j int) bool {
	sig, ok := c.pass.TypesInfo.TypeOf(call.Fun).Underlying().(*types.Signature)
	if !ok || sig.Params().Len() == 0 {
		return false
	}
	if j >= sig.Params().Len() {
		j = sig.Params().Len() - 1
	}
	t := sig.Params().At(j).Type()
	if sig.Variadic() && j == sig.Params().Len()-1 {
		if sl, ok := t.(*types.Slice); ok {
			t = sl.Elem()
		}
	}
	_, isMap := t.Underlying().(*types.Map)
	return isMap
}

// localCallee resolves a call to a function or method declared in this
// package with a body.
func (c *checker) localCallee(call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil
	}
	fn, ok := c.pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	fn = fn.Origin()
	if _, ok := c.decls[fn]; !ok {
		return nil
	}
	return fn
}

// workingCopyWrite returns the first write into obj that is followed by
// obj being returned or passed as a call argument, or nil.
func (c *checker) workingCopyWrite(body *ast.BlockStmt, obj types.Object) ast.Node {
	var first ast.Node
	consider := func(lhs ast.Expr) {
		ix, ok := ast.Unparen(lhs).(*ast.IndexExpr)
		if !ok || !c.is(ix.X, obj) {
			return
		}
		if first == nil || ix.Pos() < first.Pos() {
			first = ix
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok != token.DEFINE {
				for _, lhs := range x.Lhs {
					consider(lhs)
				}
			}
		case *ast.IncDecStmt:
			consider(x.X)
		}
		return true
	})
	if first == nil || c.guardedBefore(body, obj, first.Pos()) || !c.handedOnAfter(body, obj, first.End()) {
		return nil
	}
	return first
}

// handedOnAfter reports obj returned or passed to a non-builtin call
// after pos.
func (c *checker) handedOnAfter(body *ast.BlockStmt, obj types.Object, pos token.Pos) bool {
	handed := false
	ast.Inspect(body, func(n ast.Node) bool {
		if handed || n == nil {
			return false
		}
		switch x := n.(type) {
		case *ast.ReturnStmt:
			if x.Pos() >= pos {
				for _, r := range x.Results {
					if c.is(r, obj) {
						handed = true
					}
				}
			}
		case *ast.CallExpr:
			if x.Pos() >= pos {
				if tv, ok := c.pass.TypesInfo.Types[ast.Unparen(x.Fun)]; ok && tv.IsBuiltin() {
					return true
				}
				for _, a := range x.Args {
					if c.is(a, obj) {
						handed = true
					}
				}
			}
		}
		return true
	})
	return handed
}

func (c *checker) is(e ast.Expr, obj types.Object) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && c.pass.TypesInfo.Uses[id] == obj
}

// guardedBefore reports whether the body checks obj for nil, checks its
// length, or reassigns it before pos.
func (c *checker) guardedBefore(body *ast.BlockStmt, obj types.Object, pos token.Pos) bool {
	isLen := func(e ast.Expr) bool {
		call, ok := ast.Unparen(e).(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return false
		}
		fn, ok := ast.Unparen(call.Fun).(*ast.Ident)
		if !ok || fn.Name != "len" {
			return false
		}
		_, builtin := c.pass.TypesInfo.Uses[fn].(*types.Builtin)
		return builtin && c.is(call.Args[0], obj)
	}
	isNil := func(e ast.Expr) bool {
		tv, ok := c.pass.TypesInfo.Types[e]
		return ok && tv.IsNil()
	}
	isSmall := func(e ast.Expr) bool {
		tv, ok := c.pass.TypesInfo.Types[e]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
			return false
		}
		v, exact := constant.Int64Val(tv.Value)
		return exact && (v == 0 || v == 1)
	}
	guarded := false
	ast.Inspect(body, func(n ast.Node) bool {
		if guarded || n == nil || n.Pos() >= pos {
			return false
		}
		switch x := n.(type) {
		case *ast.BinaryExpr:
			switch x.Op {
			case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			default:
				return true
			}
			if (c.is(x.X, obj) && isNil(x.Y)) || (c.is(x.Y, obj) && isNil(x.X)) ||
				(isLen(x.X) && isSmall(x.Y)) || (isLen(x.Y) && isSmall(x.X)) {
				guarded = true
			}
		case *ast.AssignStmt:
			if x.Tok == token.ASSIGN {
				for _, lhs := range x.Lhs {
					if c.is(lhs, obj) {
						guarded = true
					}
				}
			}
		}
		return true
	})
	return guarded
}
