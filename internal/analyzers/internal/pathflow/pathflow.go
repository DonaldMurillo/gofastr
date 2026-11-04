// Package pathflow holds the path-dataflow machinery the root-shaped
// analyzers (rootwrite, rootread) share: local-binding resolution, the
// root/base/dir-named root tests, the lexical Join/concat containment
// shapes — including the one-hop same-package helper that RETURNS a
// root-joined path — and the EvalSymlinks-on-the-chain judgement that
// separates the fix posture from a resolution on an unrelated path,
// plus the verified symlink guards (SymlinkGuards, GuardsPath): a
// function counts as a guard by what its body does, never by its name,
// and a guard call gates only the path it was handed. Every resolution
// posture is ORDERED since the 2026-11 round: an EvalSymlinks, a
// guard, or an Lstat leaf check counts only when it dominates the
// sink (dominates, on dominance.Prefix/Spine) — a check that runs
// after the sink, or in a branch the sink never shares, resolved
// nothing for it.
//
// Nothing here decides anything: the analyzers pick their sinks and
// their silent postures; this package only answers "is this expression
// a path built under a caller-supplied root" and "did resolution touch
// this path's chain".
package pathflow

import (
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
)

// CollectFuncs indexes every function and method declaration with a
// body in the analyzed package, by name. Methods share the namespace:
// a package func and a method of the same name both appear, and
// CalleeDecl picks between them by receiver.
func CollectFuncs(pass *analysis.Pass) map[string][]*ast.FuncDecl {
	pkgFuncs := map[string][]*ast.FuncDecl{}
	for _, f := range pass.Files {
		if IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				pkgFuncs[fn.Name.Name] = append(pkgFuncs[fn.Name.Name], fn)
			}
		}
	}
	return pkgFuncs
}

// IsTestFile reports whether f is a _test.go file.
func IsTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}

// FuncsWithBodies returns every non-test FuncDecl with a body, for the
// per-function walk.
func FuncsWithBodies(pass *analysis.Pass) []*ast.FuncDecl {
	var out []*ast.FuncDecl
	for _, f := range pass.Files {
		if IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				out = append(out, fn)
			}
		}
	}
	return out
}

// JoinsUnderRooty reports whether e resolves — directly, through
// locals, through a filepath.Dir wrapper, or through one same-package
// helper call — to a path built under a root/base/dir-named parameter
// or field: a filepath.Join, or a flat `root + "/" + x` concatenation,
// with at least one caller-controlled component after the root.
//
// filepath.Dir unwrapping: MkdirAll(Dir(dst)) creates the parent chain
// of dst, which is still under the root when dst is.
//
// The helper hop covers two spellings: a plain function that joins its
// own rooty parameter (containedPath), and a method that joins a
// root/base/dir-named field of its receiver and RETURNS the joined
// path for the caller to act on (battery/storage fullPath — the
// 2026-09-04 rootwrite blind spot: the Join lives in the helper and
// the caller only sees its result).
func JoinsUnderRooty(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool, pkgFuncs map[string][]*ast.FuncDecl, isGuard GuardFunc) bool {
	e = Resolve(pass, e, bound, 0)
	// A Dir of a root-joined path is that path's parent: still under
	// the root.
	if call, ok := e.(*ast.CallExpr); ok && QualifiedFunc(pass, call.Fun) == "path/filepath.Dir" && len(call.Args) == 1 {
		e = Resolve(pass, call.Args[0], bound, 0)
	}
	if be, ok := e.(*ast.BinaryExpr); ok && be.Op == token.ADD {
		return concatUnderRooty(pass, be, bound, params)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	if QualifiedFunc(pass, call.Fun) == "path/filepath.Join" {
		if len(call.Args) < 2 {
			return false
		}
		root := RootyRoot(pass, call.Args[0], bound, params)
		if root == nil {
			return false
		}
		if IsTempRoot(pass, root, bound) {
			return false
		}
		// Narrowed 2026-09-02: a component the caller controls is what
		// makes the write an escape risk; formatted timestamps, counters,
		// and literal manifest names are not.
		for _, a := range call.Args[1:] {
			if MentionsParam(pass, a, bound, params, 0) {
				return true
			}
		}
		return false
	}
	// A helper that joins a root of its own (e.g. containedPath, or a
	// method joining a receiver field) and returns the joined path.
	// The non-root arguments at the call site must carry caller data:
	// a helper called with literal-only components appends nothing
	// caller-controlled under the root.
	decl := CalleeDecl(pass, call.Fun, pkgFuncs)
	if decl == nil {
		return false
	}
	rootIdx := -1
	if rootParam := RootyParam(decl); rootParam != "" && helperJoinsParam(pass, decl, rootParam) {
		rootIdx = RootyParamIndex(decl, rootParam)
	} else if decl.Recv != nil && helperJoinsRootyRoot(pass, decl) {
		// The receiver's field is the root; every call argument is a
		// data slot.
		rootIdx = -1
	} else {
		return false
	}
	if helperResolvesReturn(pass, decl, isGuard) {
		return false // the helper resolves the returned path's own chain
	}
	for i, a := range call.Args {
		if i == rootIdx {
			continue
		}
		if MentionsParam(pass, a, bound, params, 0) {
			return true
		}
	}
	return false
}

// helperJoinsRootyRoot reports whether decl's body joins a
// root/base/dir-named parameter or receiver field with anything else
// via filepath.Join — the method form of the helper hop, where the
// root never appears as a call argument.
func helperJoinsRootyRoot(pass *analysis.Pass, decl *ast.FuncDecl) bool {
	helperParams := ParamObjects(pass, decl)
	found := false
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || QualifiedFunc(pass, call.Fun) != "path/filepath.Join" || len(call.Args) < 2 {
			return true
		}
		if RootyParamOrField(pass, call.Args[0], nil, helperParams) != nil {
			found = true
		}
		return true
	})
	return found
}

// helperResolvesReturn reports whether decl — a helper that joins a
// rooty parameter or receiver field and hands the joined path back —
// resolves THAT path's chain before returning it: an EvalSymlinks or a
// verified guard tied to a returned expression (the path itself, a
// local bound to it, or its Dir), ordered before the return. Until the
// 2026-11 round any EvalSymlinks or guard call ANYWHERE in the helper
// body made every caller quiet, even one that resolved a different
// value — rootwrite's per-path rule, applied at the hop.
func helperResolvesReturn(pass *analysis.Pass, decl *ast.FuncDecl, isGuard GuardFunc) bool {
	helperBound := Bindings(pass, decl.Body)
	evals := EvalSymlinkCalls(pass, decl.Body)
	found := false
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || found {
			return !found
		}
		// Only the first result is the path slot; the error result of
		// an EvalSymlinks call is not a resolved path.
		if len(ret.Results) == 0 {
			return true
		}
		res := ret.Results[0]
		sr := SymlinkResolved(pass, res, helperBound, evals, decl.Body, ret)
		gp := GuardsPath(pass, decl.Body, res, helperBound, isGuard, ret)
		if sr || gp {
			found = true
			return false
		}
		return true
	})
	return found
}

// RootyRoot resolves the Join's first argument to the root expression:
// a rooty parameter or field directly, or a local whose binding is a
// rooty param/field selector — including one passed through a
// root-returning helper (root, err := resolveRoot(v.root)).
func RootyRoot(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) ast.Expr {
	if r := RootyParamOrField(pass, e, bound, params); r != nil {
		return r
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil
	}
	b, ok := bound[pass.TypesInfo.ObjectOf(id)]
	if !ok {
		return nil
	}
	if r := RootyParamOrField(pass, b, bound, params); r != nil {
		return r
	}
	if c, ok := b.(*ast.CallExpr); ok {
		for _, a := range c.Args {
			if r := RootyParamOrField(pass, a, bound, params); r != nil {
				return r
			}
		}
	}
	return nil
}

// concatUnderRooty reports whether a flat ADD chain is rooted at a
// rooty param/field with caller data concatenated after it — the same
// lexical-only containment a Join spells.
func concatUnderRooty(pass *analysis.Pass, be *ast.BinaryExpr, bound map[types.Object]ast.Expr, params map[types.Object]bool) bool {
	ops := ConcatOperands(be, nil)
	if len(ops) < 2 {
		return false
	}
	root := RootyRoot(pass, ops[0], bound, params)
	if root == nil {
		return false
	}
	if IsTempRoot(pass, root, bound) {
		return false
	}
	for _, o := range ops[1:] {
		if MentionsParam(pass, o, bound, params, 0) {
			return true
		}
	}
	return false
}

// ConcatOperands flattens a left-associated ADD chain, leftmost
// first. Exported for worldreadable and fixedtmp, which carried
// identical private copies.
func ConcatOperands(be *ast.BinaryExpr, out []ast.Expr) []ast.Expr {
	if inner, ok := be.X.(*ast.BinaryExpr); ok && inner.Op == token.ADD {
		out = ConcatOperands(inner, out)
	} else {
		out = append(out, be.X)
	}
	return append(out, be.Y)
}

// RootyParamIndex returns the positional index of decl's parameter
// named rootParam, or -1.
func RootyParamIndex(decl *ast.FuncDecl, rootParam string) int {
	if decl.Type.Params == nil {
		return -1
	}
	idx := 0
	for _, f := range decl.Type.Params.List {
		if len(f.Names) == 0 {
			idx++
			continue
		}
		for _, name := range f.Names {
			if name.Name == rootParam {
				return idx
			}
			idx++
		}
	}
	return -1
}

// RootyParamOrField returns the root expression when e is a parameter
// or struct field whose name says root/base/dir.
func RootyParamOrField(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) ast.Expr {
	switch x := e.(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		v, ok := obj.(*types.Var)
		if !ok {
			return nil
		}
		if !RootyName(v.Name()) {
			return nil
		}
		if v.IsField() || params[obj] {
			return x
		}
		return nil
	case *ast.SelectorExpr:
		obj := pass.TypesInfo.ObjectOf(x.Sel)
		v, ok := obj.(*types.Var)
		if ok && v.IsField() && RootyName(v.Name()) {
			return x
		}
		return nil
	default:
		return nil
	}
}

// RootyName matches the root/base/dir family: compound names like
// rootDir, baseDir, and workdir all carry the token.
func RootyName(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "root") || strings.Contains(l, "base") || strings.Contains(l, "dir")
}

// IsTempRoot reports whether the root expression is a local bound to a
// throwaway directory: os.MkdirTemp or t.TempDir.
func IsTempRoot(pass *analysis.Pass, root ast.Expr, bound map[types.Object]ast.Expr) bool {
	id, ok := root.(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := bound[pass.TypesInfo.ObjectOf(id)]
	if !ok {
		return false
	}
	call, ok := b.(*ast.CallExpr)
	if !ok {
		return false
	}
	if QualifiedFunc(pass, call.Fun) == "os.MkdirTemp" {
		return true
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "TempDir" {
		return true
	}
	return false
}

// helperJoinsParam reports whether decl's body joins its parameter
// named param with anything else via filepath.Join.
func helperJoinsParam(pass *analysis.Pass, decl *ast.FuncDecl, param string) bool {
	found := false
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || QualifiedFunc(pass, call.Fun) != "path/filepath.Join" {
			return true
		}
		// The count runs first: a zero-argument Join is legal Go ("")
		// and indexing Args[0] before the length check panicked the
		// walk (R8-AZ7-01).
		if len(call.Args) >= 2 {
			if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == param {
				found = true
			}
		}
		return true
	})
	return found
}

// RootyParam returns the name of decl's first parameter named in the
// root/base/dir family, or "".
func RootyParam(decl *ast.FuncDecl) string {
	if decl.Type.Params == nil {
		return ""
	}
	for _, f := range decl.Type.Params.List {
		for _, name := range f.Names {
			if RootyName(name.Name) {
				return name.Name
			}
		}
	}
	return ""
}

// CalleeDecl resolves a call to a package-level function or method
// declaration of this package, for the one helper hop. Plain calls and
// same-package qualified calls resolve to the function (never a
// method); a method call x.M resolves to the method whose receiver
// type is x's named type, when exactly one method of that name
// matches — ambiguity stays unresolved, which is the quiet direction.
func CalleeDecl(pass *analysis.Pass, fun ast.Expr, pkgFuncs map[string][]*ast.FuncDecl) *ast.FuncDecl {
	switch f := fun.(type) {
	case *ast.Ident:
		for _, d := range pkgFuncs[f.Name] {
			if d.Recv == nil {
				return d
			}
		}
		return nil
	case *ast.SelectorExpr:
		xid, ok := f.X.(*ast.Ident)
		if !ok {
			return nil
		}
		if pn, ok := pass.TypesInfo.ObjectOf(xid).(*types.PkgName); ok {
			if pn.Imported() != pass.Pkg {
				return nil
			}
			for _, d := range pkgFuncs[f.Sel.Name] {
				if d.Recv == nil {
					return d
				}
			}
			return nil
		}
		// A method call: match by receiver type name.
		recv := receiverTypeName(pass, xid)
		if recv == "" {
			return nil
		}
		var match *ast.FuncDecl
		for _, d := range pkgFuncs[f.Sel.Name] {
			if d.Recv == nil || astx.RecvBaseName(d) != recv {
				continue
			}
			if match != nil {
				return nil // ambiguous: two methods fit
			}
			match = d
		}
		return match
	default:
		return nil
	}
}

// receiverTypeName returns the named type behind x, or "".
func receiverTypeName(pass *analysis.Pass, x ast.Expr) string {
	t := pass.TypesInfo.TypeOf(x)
	if t == nil {
		return ""
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return ""
	}
	return named.Obj().Name()
}

// MentionsParam reports whether e's assembly involves one of the
// function's parameters (directly, or through locals bound in this
// function), which is what makes a path component caller-controlled.
func MentionsParam(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool, depth int) bool {
	if depth > 6 {
		return false
	}
	switch x := e.(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		if obj == nil {
			return false
		}
		if params[obj] && carriesText(obj) {
			return true
		}
		if b, ok := bound[obj]; ok {
			return MentionsParam(pass, b, bound, params, depth+1)
		}
		return false
	case *ast.BasicLit:
		return false
	default:
		hit := false
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				obj := pass.TypesInfo.ObjectOf(id)
				if obj != nil && params[obj] && carriesText(obj) {
					hit = true
					return false
				}
				if b, ok := bound[obj]; ok && MentionsParam(pass, b, bound, params, depth+1) {
					hit = true
					return false
				}
			}
			return !hit
		})
		return hit
	}
}

// carriesText reports whether a parameter can carry path text: a bool
// or a number cannot (phaseName(maintenance), a run counter), so a
// component built from one alone is not caller-controlled, the
// 2026-09-02 narrowing extended to the parameter's type. Strings,
// byte slices, structs, interfaces and everything else can.
func carriesText(obj types.Object) bool {
	b, ok := obj.Type().Underlying().(*types.Basic)
	if !ok {
		return true
	}
	return b.Info()&(types.IsBoolean|types.IsNumeric) == 0
}

// MentionsCall reports whether e's assembly involves (directly, or
// through locals bound in this function) a call matching pred.
func MentionsCall(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, pred func(*ast.CallExpr) bool, depth int) bool {
	if depth > 6 {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok && pred(call) {
			found = true
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				if b, ok := bound[obj]; ok && MentionsCall(pass, b, bound, pred, depth+1) {
					found = true
					return false
				}
			}
		}
		return !found
	})
	return found
}

// Resolve follows single-value local bindings (`x := expr`), keeping the
// last binding in source order like mapwriter does.
func Resolve(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, depth int) ast.Expr {
	for depth < 8 {
		id, ok := e.(*ast.Ident)
		if !ok {
			return e
		}
		b, ok := bound[pass.TypesInfo.ObjectOf(id)]
		if !ok {
			return e
		}
		e = b
		depth++
	}
	return e
}

// Bindings maps each local defined by an assignment or a var-spec
// initializer (`var p = expr`, the 2026-11-03 gap: only `:=`/`=`
// resolved before) to the expression it was last bound to.
// Multi-value assignments map each left side to the single call on
// the right, which is what lets `dir, err := os.MkdirTemp(...)`
// register for the temp-root silence and `dst, err := s.fullPath(key)`
// register for the helper hop.
func Bindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
	bound := map[types.Object]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if len(st.Rhs) != 1 {
				return true
			}
			for _, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						bound[obj] = st.Rhs[0]
					}
				}
			}
		case *ast.ValueSpec:
			// `var p = expr` is the same binding as `p := expr`; a
			// zero-value spec binds nothing. Multi-value specs map
			// every name to the single call, like the assignment arm.
			if len(st.Values) != 1 {
				return true
			}
			for _, name := range st.Names {
				if name.Name != "_" {
					if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
						bound[obj] = st.Values[0]
					}
				}
			}
		}
		return true
	})
	return bound
}

// ParamObjects collects the function's parameter variables.
func ParamObjects(pass *analysis.Pass, fn *ast.FuncDecl) map[types.Object]bool {
	params := map[types.Object]bool{}
	if fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			for _, name := range f.Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
					params[obj] = true
				}
			}
		}
	}
	return params
}

// SinkParam reports which of decl's parameters a helper's sink path
// IS: the bare parameter (through locals) or its filepath.Dir. The
// helper-hop machinery rootwrite and rootread share (I4 and its 2026-11
// read-side port).
func SinkParam(pass *analysis.Pass, p ast.Expr, bound map[types.Object]ast.Expr, idx map[types.Object]int) (int, bool) {
	r := Resolve(pass, p, bound, 0)
	if call, ok := r.(*ast.CallExpr); ok && QualifiedFunc(pass, call.Fun) == "path/filepath.Dir" && len(call.Args) == 1 {
		r = Resolve(pass, call.Args[0], bound, 0)
	}
	id, ok := r.(*ast.Ident)
	if !ok {
		return 0, false
	}
	i, ok := idx[pass.TypesInfo.ObjectOf(id)]
	return i, ok
}

// ParamIndexes maps decl's parameter objects to their positions.
func ParamIndexes(pass *analysis.Pass, decl *ast.FuncDecl) map[types.Object]int {
	out := map[types.Object]int{}
	if decl.Type.Params == nil {
		return out
	}
	i := 0
	for _, f := range decl.Type.Params.List {
		if len(f.Names) == 0 {
			i++
			continue
		}
		for _, name := range f.Names {
			if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
				out[obj] = i
			}
			i++
		}
	}
	return out
}

// SinkArg is one path argument a call touches: its expression, whether
// the leaf posture maps to it, and the helper the sink happens in (""
// for a direct os.* call).
type SinkArg struct {
	Path   ast.Expr
	Leaf   bool
	Helper string
}

// HelperSinkParams summarizes, for every same-package function, which
// parameters reach an unresolved sink inside it — the bare parameter
// or its filepath.Dir (writeFile(dst, data): MkdirAll(Dir(dst)) +
// os.WriteFile(dst)) — to a fixpoint: sinkOf lists the path arguments
// a call touches (a direct os.* sink, or another helper's sink
// parameters one or more hops deep; it receives the enclosing body's
// Bindings so an os.OpenFile flag argument held in a local resolves,
// the 2026-11-03 gap), and resolvedHere says whether the
// enclosing body resolves a sink's path before it happens (the helper
// is then the fix posture and makes no sink parameter). The bool per
// parameter says every sink it reaches is a leaf sink. rootwrite owns
// the write spelling (bug bash 2026-10-27 I4) and rootread the 2026-11
// read-side port; the fixpoint driver is shared.
func HelperSinkParams(
	pass *analysis.Pass,
	pkgFuncs map[string][]*ast.FuncDecl,
	sinkOf func(*analysis.Pass, *ast.CallExpr, map[types.Object]ast.Expr, map[string][]*ast.FuncDecl, map[*ast.FuncDecl]map[int]bool) []SinkArg,
	resolvedHere func(*analysis.Pass, *ast.BlockStmt, SinkArg, map[types.Object]ast.Expr, []*ast.CallExpr, GuardFunc, ast.Node) bool,
	isGuard GuardFunc,
) map[*ast.FuncDecl]map[int]bool {
	helpers := map[*ast.FuncDecl]map[int]bool{}
	decls := FuncsWithBodies(pass)
	for changed := true; changed; {
		changed = false
		for _, decl := range decls {
			idx := ParamIndexes(pass, decl)
			if len(idx) == 0 {
				continue
			}
			bound := Bindings(pass, decl.Body)
			evals := EvalSymlinkCalls(pass, decl.Body)
			ast.Inspect(decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, s := range sinkOf(pass, call, bound, pkgFuncs, helpers) {
					i, ok := SinkParam(pass, s.Path, bound, idx)
					if !ok || resolvedHere(pass, decl.Body, s, bound, evals, isGuard, call) {
						continue
					}
					m := helpers[decl]
					if m == nil {
						m = map[int]bool{}
						helpers[decl] = m
					}
					prev, seen := m[i]
					next := s.Leaf && (!seen || prev)
					if !seen || prev != next {
						m[i] = next
						changed = true
					}
				}
				return true
			})
		}
	}
	return helpers
}

// HelperSinkArgs lists the sink parameters a call passes to a
// same-package helper: one SinkArg per argument position the helper's
// body sinks, carrying the call-site argument.
func HelperSinkArgs(pass *analysis.Pass, call *ast.CallExpr, pkgFuncs map[string][]*ast.FuncDecl, helpers map[*ast.FuncDecl]map[int]bool) []SinkArg {
	decl := CalleeDecl(pass, call.Fun, pkgFuncs)
	if decl == nil || len(helpers[decl]) == 0 {
		return nil
	}
	var out []SinkArg
	for i := range call.Args {
		if leaf, ok := helpers[decl][i]; ok {
			out = append(out, SinkArg{Path: call.Args[i], Leaf: leaf, Helper: decl.Name.Name})
		}
	}
	return out
}

// EvalSymlinkCalls collects the filepath.EvalSymlinks calls in body.
func EvalSymlinkCalls(pass *analysis.Pass, body *ast.BlockStmt) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok &&
			QualifiedFunc(pass, call.Fun) == "path/filepath.EvalSymlinks" {
			out = append(out, call)
		}
		return true
	})
	return out
}

// SymlinkResolved reports whether the path is resolved against
// symlinks ON THE CHAIN, by a resolution that DOMINATES the sink: the
// path itself is an EvalSymlinks result, a resolved local feeds it, or
// an EvalSymlinks ran on this path expression before the sink —
// directly, through a local, or on its Dir, which resolves the
// directory chain above the leaf (the core/upload Save posture). An
// EvalSymlinks on an unrelated path resolves nothing, and one that
// runs after the sink (a verification read-back) resolves nothing for
// it either; sink may be nil for callers without a position, which
// keeps the unordered posture.
func SymlinkResolved(pass *analysis.Pass, pathArg ast.Expr, bound map[types.Object]ast.Expr, evals []*ast.CallExpr, body *ast.BlockStmt, sink ast.Node) bool {
	isEval := func(c *ast.CallExpr) bool {
		return QualifiedFunc(pass, c.Fun) == "path/filepath.EvalSymlinks"
	}
	r := Resolve(pass, pathArg, bound, 0)
	if call, ok := r.(*ast.CallExpr); ok && isEval(call) {
		return true
	}
	if MentionsCall(pass, pathArg, bound, isEval, 0) {
		return true
	}
	for _, ev := range evals {
		if !dominates(ev, sink, body) {
			continue
		}
		for _, a := range ev.Args {
			if ArgTouchesPath(pass, a, bound, r) {
				return true
			}
		}
	}
	return false
}

// ArgTouchesPath reports whether an EvalSymlinks argument mentions a
// local bound to the same expression as the sink path — the resolved
// leaf, its Dir, or the path itself.
func ArgTouchesPath(pass *analysis.Pass, arg ast.Expr, bound map[types.Object]ast.Expr, r ast.Expr) bool {
	// EvalSymlinks(Dir(p)): resolving the parent chain covers every
	// symlinked component above the leaf the sink is about to touch.
	ra := Resolve(pass, arg, bound, 0)
	// The same expression written inline, not through a local:
	// EvalSymlinks(Join(root, k)) touching the sink's Join(root, k).
	if ra == r || types.ExprString(ra) == types.ExprString(r) {
		return true
	}
	if call, ok := ra.(*ast.CallExpr); ok && QualifiedFunc(pass, call.Fun) == "path/filepath.Dir" && len(call.Args) == 1 {
		inner := Resolve(pass, call.Args[0], bound, 0)
		if inner == r || types.ExprString(inner) == types.ExprString(r) {
			return true
		}
	}
	found := false
	ast.Inspect(arg, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok {
			if b == r || types.ExprString(b) == types.ExprString(r) {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// GuardFunc reports whether a function is a verified symlink guard:
// its body resolves or refuses symlinks (see ResolvesSymlinks). Nil
// means no function is one.
type GuardFunc func(*types.Func) bool

// SymlinkGuards finds the analyzed package's symlink guards — every
// function or method whose body ResolvesSymlinks, to a fixpoint so a
// wrapper around a guard is one — exports a fact (from newFact) on
// each, and returns the GuardFunc that answers for this package's
// functions directly and for imported ones through their facts. Each
// analyzer passes its own fact type: go/analysis lets one analyzer
// own a fact type.
//
// Bug bash 2026-10-27 I13 replaced the name test (CallsSymlinkNamed:
// any call whose name contained "symlink" gated every sink in the
// function, so `checkSymlink(other)` with a `return nil` body silenced
// the access). The codegen fileset guards (EnsureNoSymlinkPath,
// EnsureNoSymlinkLeaf) still count, by their bodies.
func SymlinkGuards(pass *analysis.Pass, newFact func() analysis.Fact) GuardFunc {
	local := map[*types.Func]bool{}
	var decls []*ast.FuncDecl
	for _, f := range pass.Files {
		// The standard library is no source of guards: vet computes
		// facts over every dependency, and os / syscall carry
		// O_NOFOLLOW and Lstat+ModeSymlink internally, which would make
		// os.Chmod or filepath.EvalSymlinks itself a "guard" whose call
		// on the sink path gates it. EvalSymlinks is credited by name.
		if IsTestFile(pass, f) || inGOROOT(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
			}
		}
	}
	var isGuard GuardFunc
	isGuard = func(fn *types.Func) bool {
		if fn == nil {
			return false
		}
		if fn.Pkg() == pass.Pkg {
			return local[fn]
		}
		return pass.ImportObjectFact(fn.Origin(), newFact())
	}
	for changed := true; changed; {
		changed = false
		for _, fd := range decls {
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok || local[fn] {
				continue
			}
			if ResolvesSymlinks(pass, fd.Body, isGuard) {
				local[fn] = true
				changed = true
			}
		}
	}
	// Only exported names can be called from another package; keeping
	// the facts to them keeps the fact set (and the fixtures' fact
	// expectations) to the cross-package surface.
	for fn := range local {
		if fn.Exported() {
			pass.ExportObjectFact(fn, newFact())
		}
	}
	return isGuard
}

// ResolvesSymlinks reports whether body performs a real resolution
// posture: filepath.EvalSymlinks, an O_NOFOLLOW flag, an os.Lstat
// whose FileInfo result a ModeSymlink test READS, or a call to a
// verified guard. It judges a guard's body; whether the guard touched
// the right path is GuardsPath's question. (os.Root needs no guard:
// its methods are no os.* sink, so a write or read through one is
// quiet by construction.) Until the 2026-11 round the Lstat arm read
// co-presence — any Lstat plus any ModeSymlink selector in the body —
// which credited a body that Lstat'd and threw the FileInfo away
// (consultsModeSymlink carries the tie).
func ResolvesSymlinks(pass *analysis.Pass, body *ast.BlockStmt, isGuard GuardFunc) bool {
	lstat, found := false, false
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			switch QualifiedFunc(pass, v.Fun) {
			case "path/filepath.EvalSymlinks":
				found = true
			case "os.Lstat":
				lstat = true
			}
			if isGuard != nil && isGuard(staticCallee(pass, v.Fun)) {
				found = true
			}
		case *ast.SelectorExpr:
			if v.Sel.Name == "O_NOFOLLOW" {
				found = true
			}
		}
		return !found
	})
	if found || !lstat {
		return found
	}
	return consultsModeSymlink(pass, body, Bindings(pass, body), lstatInfoObjs(pass, body, nil))
}

// lstatInfoObjs returns the variables bound to the FIRST result (the
// fs.FileInfo) of each os.Lstat call in body that keep accepts (nil
// accepts every Lstat). An Lstat whose FileInfo is discarded —
// `_, err := os.Lstat(p)` or the call as a statement — binds nothing.
func lstatInfoObjs(pass *analysis.Pass, body *ast.BlockStmt, keep func(*ast.CallExpr) bool) map[types.Object]bool {
	objs := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Rhs) != 1 || len(st.Lhs) < 1 {
			return true
		}
		call, ok := st.Rhs[0].(*ast.CallExpr)
		if !ok || QualifiedFunc(pass, call.Fun) != "os.Lstat" {
			return true
		}
		if keep != nil && !keep(call) {
			return true
		}
		if id, ok := st.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				objs[obj] = true
			}
		}
		return true
	})
	return objs
}

// consultsModeSymlink reports whether the body's ModeSymlink test
// READS the FileInfo result of one of the Lstat calls whose first
// result landed in infos: the innermost statement enclosing a
// ModeSymlink selector also mentions one of those variables (or a
// local bound to one, `mode := fi.Mode()`). A selector that reads no
// Lstat result consults nothing, whatever else the body holds.
func consultsModeSymlink(pass *analysis.Pass, body *ast.BlockStmt, bound map[types.Object]ast.Expr, infos map[types.Object]bool) bool {
	if len(infos) == 0 {
		return false
	}
	var stmts []ast.Stmt
	ast.Inspect(body, func(n ast.Node) bool {
		if s, ok := n.(ast.Stmt); ok {
			stmts = append(stmts, s)
		}
		return true
	})
	consulted := false
	ast.Inspect(body, func(n ast.Node) bool {
		if consulted {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ModeSymlink" {
			return true
		}
		if st := innermostStmt(stmts, sel); st != nil && mentionsObj(pass, st, bound, infos, 0) {
			consulted = true
			return false
		}
		return true
	})
	return consulted
}

// innermostStmt returns the smallest statement fully containing n.
func innermostStmt(stmts []ast.Stmt, n ast.Node) ast.Stmt {
	var best ast.Stmt
	for _, s := range stmts {
		if n.Pos() >= s.Pos() && n.End() <= s.End() {
			if best == nil || s.End()-s.Pos() < best.End()-best.Pos() {
				best = s
			}
		}
	}
	return best
}

// mentionsObj reports whether e's tree uses a variable in objs, or an
// ident whose binding does (one hop per binding, depth-capped).
func mentionsObj(pass *analysis.Pass, e ast.Node, bound map[types.Object]ast.Expr, objs map[types.Object]bool, depth int) bool {
	if depth > 2 {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := pass.TypesInfo.ObjectOf(id)
		if obj == nil {
			return true
		}
		if objs[obj] {
			found = true
			return false
		}
		if b, ok := bound[obj]; ok && mentionsObj(pass, b, bound, objs, depth+1) {
			found = true
			return false
		}
		return true
	})
	return found
}

// inGOROOT reports whether f is a standard-library source file.
func inGOROOT(pass *analysis.Pass, f *ast.File) bool {
	root := build.Default.GOROOT
	if root == "" {
		return false
	}
	name := pass.Fset.Position(f.Pos()).Filename
	return strings.HasPrefix(name, filepath.Join(root, "src")+string(filepath.Separator))
}

// GuardsPath reports whether body calls a verified guard with the
// sink's own path — the path expression itself, its Dir, or a local
// bound to either (ArgTouchesPath) — BEFORE the sink: a guard after
// it (a post-hoc audit) protected nothing. A guard on the root alone,
// or on an unrelated path, leaves the sink's components unchecked.
// sink may be nil for callers without a position.
func GuardsPath(pass *analysis.Pass, body *ast.BlockStmt, pathArg ast.Expr, bound map[types.Object]ast.Expr, isGuard GuardFunc, sink ast.Node) bool {
	if isGuard == nil || pathArg == nil {
		return false
	}
	r := Resolve(pass, pathArg, bound, 0)
	rs := types.ExprString(r)
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		if !isGuard(staticCallee(pass, call.Fun)) {
			return true
		}
		if !dominates(call, sink, body) {
			return true
		}
		for _, a := range call.Args {
			ra := Resolve(pass, a, bound, 0)
			if ra == r || types.ExprString(ra) == rs || ArgTouchesPath(pass, a, bound, r) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// staticCallee resolves a call's callee to its *types.Func: a plain or
// package-qualified function, or a statically dispatched method.
func staticCallee(pass *analysis.Pass, fun ast.Expr) *types.Func {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		fn, _ := pass.TypesInfo.Uses[f].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		fn, _ := pass.TypesInfo.Uses[f.Sel].(*types.Func)
		return fn
	case *ast.IndexExpr:
		return staticCallee(pass, f.X)
	}
	return nil
}

// LstatLeafCheck reports whether the body Lstats the sink's own target
// and consults ModeSymlink on THAT Lstat's FileInfo, BEFORE the sink —
// the leaf posture that makes an unlink or an overwrite refuse to
// travel through a symlinked leaf. The directory components above the
// leaf stay unresolved; both root analyzers accept it as the
// documented partial fix (rootread for os.Remove, rootwrite for the
// leaf-clobbering write sinks). An Lstat after the sink audits a
// clobber that already happened, and a ModeSymlink test that reads no
// Lstat result consults nothing; sink may be nil for callers without
// a position.
func LstatLeafCheck(pass *analysis.Pass, body *ast.BlockStmt, pathArg ast.Expr, bound map[types.Object]ast.Expr, sink ast.Node) bool {
	r := Resolve(pass, pathArg, bound, 0)
	var touching *ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if touching != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || QualifiedFunc(pass, call.Fun) != "os.Lstat" {
			return true
		}
		if !dominates(call, sink, body) {
			return true
		}
		for _, a := range call.Args {
			if ArgTouchesPath(pass, a, bound, r) {
				touching = call
				return false
			}
		}
		return true
	})
	if touching == nil {
		return false
	}
	return consultsModeSymlink(pass, body, bound, lstatInfoObjs(pass, body, func(c *ast.CallExpr) bool {
		return c == touching
	}))
}

// dominates is the ordering half of every resolution posture: the
// check has to run before the sink it claims to gate. A nil sink (a
// caller with no position) keeps the unordered posture; a nil body
// does too. The lexical dominance walk: the check sits in a statement
// of one of sink's dominating statement lists (dominance.Prefix), or
// in the always-run parts — the Spine — of an enclosing statement on
// the path down to sink (an if's Init and Cond ran before either of
// its branches; a loop's controlling clause runs before each
// iteration's body). A check inside a branch the sink never shares,
// or after the sink, dominates nothing.
func dominates(check, sink ast.Node, body *ast.BlockStmt) bool {
	if sink == nil || body == nil {
		return true
	}
	if check == nil || check.Pos() > sink.Pos() {
		return false
	}
	parents := dominance.Parents(body)
	for _, list := range dominance.Prefix(sink, parents, body) {
		for _, st := range list {
			if spineHolds(st, check) {
				return true
			}
		}
	}
	cur := sink
	for {
		p, ok := parents[cur]
		if !ok || p == nil || p == body {
			return false
		}
		if st, ok := p.(ast.Stmt); ok && spineHolds(st, check) {
			return true
		}
		cur = p
	}
}

// spineHolds reports whether check sits inside one of n's always-run
// parts (dominance.Spine). Only the EXPRESSION parts count as
// containers: Spine also lists statement and block nodes themselves
// (an if, a block), whose source range covers the very branch bodies
// the spine excludes, so a positional containment test on those would
// credit a check buried in a skipped branch.
func spineHolds(n ast.Node, check ast.Node) bool {
	for _, part := range dominance.Spine(n) {
		if e, ok := part.(ast.Expr); ok && e.Pos() <= check.Pos() && check.End() <= e.End() {
			return true
		}
	}
	return false
}

// FlagMentions reports whether e's flag expression mentions any of the
// named os.OpenFile flag bits.
func FlagMentions(e ast.Expr, names ...string) bool {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && want[sel.Sel.Name] {
			found = true
		}
		return !found
	})
	return found
}

// HasWriteFlag reports whether the os.OpenFile flag expression contains
// any of the write/create bits.
func HasWriteFlag(e ast.Expr) bool {
	return FlagMentions(e, "O_CREATE", "O_WRONLY", "O_RDWR", "O_APPEND", "O_TRUNC")
}

// QualifiedFunc renders a selector callee as "importpath.Func",
// resolving the package through the type checker so an aliased import
// still matches (and path/filepath reports its full path, not "filepath").
func QualifiedFunc(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pn, ok := pass.TypesInfo.ObjectOf(x).(*types.PkgName)
	if !ok {
		return ""
	}
	return pn.Imported().Path() + "." + sel.Sel.Name
}

// ArgAt returns call's i-th argument, or nil.
func ArgAt(call *ast.CallExpr, i int) ast.Expr {
	if len(call.Args) > i {
		return call.Args[i]
	}
	return nil
}
