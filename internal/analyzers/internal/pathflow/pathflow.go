// Package pathflow holds the path-dataflow machinery the root-shaped
// analyzers (rootwrite, rootread) share: local-binding resolution, the
// root/base/dir-named root tests, the lexical Join/concat containment
// shapes — including the one-hop same-package helper that RETURNS a
// root-joined path — and the contained-resolution judgement (an
// EvalSymlinks result the sink uses, checked against the root before
// the sink; #665),
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
	"go/constant"
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

// Derived follows a path back through the spellings that keep it the
// same path's business: local bindings, filepath.Dir (the parent of a
// root-joined path is still under the root), filepath.EvalSymlinks
// (where it points), and Join(x, filepath.Base(y)) (y's leaf re-joined
// under its resolved parent). Until #665 the walk stopped at the
// EvalSymlinks result and at the re-join, so a write through a resolved
// path that pointed outside the root was never a candidate at all.
func Derived(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr) ast.Expr {
	budget := 6
	return derived(pass, e, bound, &budget)
}

// derived is Derived with one step budget shared by the loop and the
// Join arm's recursion: `p = Join(p, Base(name))` resolves p back to
// the same call, and a per-call budget recursed until the stack ran out.
func derived(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, budget *int) ast.Expr {
	e = Resolve(pass, e, bound, 0)
	for ; *budget > 0; *budget-- {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return e
		}
		switch QualifiedFunc(pass, call.Fun) {
		case "path/filepath.Dir", "path/filepath.EvalSymlinks":
			if len(call.Args) != 1 {
				return e
			}
			e = Resolve(pass, call.Args[0], bound, 0)
		case "path/filepath.Join":
			// Join(x, Base(y)) is y's business only when x is y's own
			// parent (resolved or not): Join(EvalSymlinks(Dir(p)),
			// Base(p)). Join(baseDir, Base(name)) on a raw parameter is
			// a fresh root join and stays one, or the root drops out
			// and the write is never a candidate.
			base := baseArg(pass, call, bound)
			if base == nil {
				return e
			}
			yd := Resolve(pass, base, bound, 0)
			*budget--
			xd := derived(pass, call.Args[0], bound, budget)
			if xd != yd && types.ExprString(xd) != types.ExprString(yd) {
				return e
			}
			e = yd
		default:
			return e
		}
	}
	return e
}

func unrollsSelf(fn string) bool {
	return fn == "path/filepath.Join" || fn == "path/filepath.EvalSymlinks"
}

// flattenJoin spreads a nested first argument: Join(Join(root, k), x)
// is Join(root, k, x), the shape `p = filepath.Join(p, x)` leaves once
// PathBindings unrolls it.
func flattenJoin(pass *analysis.Pass, call *ast.CallExpr, bound map[types.Object]ast.Expr) []ast.Expr {
	args := call.Args
	for depth := 0; depth < 8 && len(args) > 0; depth++ {
		inner, ok := Resolve(pass, args[0], bound, 0).(*ast.CallExpr)
		if !ok || QualifiedFunc(pass, inner.Fun) != "path/filepath.Join" || len(inner.Args) == 0 {
			break
		}
		args = append(append([]ast.Expr{}, inner.Args...), args[1:]...)
	}
	return args
}

// baseArg returns y when call is filepath.Join(x, filepath.Base(y)).
func baseArg(pass *analysis.Pass, call *ast.CallExpr, bound map[types.Object]ast.Expr) ast.Expr {
	if len(call.Args) != 2 {
		return nil
	}
	b, ok := Resolve(pass, call.Args[1], bound, 0).(*ast.CallExpr)
	if !ok || QualifiedFunc(pass, b.Fun) != "path/filepath.Base" || len(b.Args) != 1 {
		return nil
	}
	return b.Args[0]
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
	e = Derived(pass, e, bound)
	if be, ok := e.(*ast.BinaryExpr); ok && be.Op == token.ADD {
		return concatUnderRooty(pass, be, bound, params)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	if QualifiedFunc(pass, call.Fun) == "path/filepath.Join" {
		args := flattenJoin(pass, call, bound)
		if len(args) < 2 {
			return false
		}
		root := RootyRoot(pass, args[0], bound, params)
		if root == nil {
			return false
		}
		if IsTempRoot(pass, root, bound) {
			return false
		}
		// Narrowed 2026-09-02: a component the caller controls is what
		// makes the write an escape risk; formatted timestamps, counters,
		// and literal manifest names are not.
		for _, a := range args[1:] {
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
	helperBound := PathBindings(pass, decl.Body)
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

// PathBindings is Bindings with self-rebinding Join and EvalSymlinks
// calls unrolled: after `p := Join(root, k)` (or `var p = ...`) and
// `p, err = EvalSymlinks(p)`, p resolves to EvalSymlinks(Join(root, k))
// rather than to a call whose argument is p itself, a loop that hid the
// Join and kept the most common spelling of #665's class (a resolved
// path nobody compared with the root) out of the candidates.
// `p = Join(p, x)` unrolls the same way. The unrolled call is
// synthetic: it keeps the original Fun node, so QualifiedFunc still
// names it.
func PathBindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
	bound := Bindings(pass, body)
	if body == nil {
		return bound
	}
	cur := map[types.Object]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		// `var p = Join(root, k)` seeds p's prior value the way
		// `p := Join(root, k)` does.
		if vs, ok := n.(*ast.ValueSpec); ok && len(vs.Values) == 1 {
			for _, name := range vs.Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil && name.Name != "_" {
					cur[obj] = vs.Values[0]
				}
			}
			return true
		}
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Rhs) != 1 {
			return true
		}
		rhs := st.Rhs[0]
		for _, lhs := range st.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(id)
			if obj == nil {
				continue
			}
			next := rhs
			prior, seeded := cur[obj]
			// Join and EvalSymlinks only: `dir = filepath.Dir(dir)` is an
			// upward walk that leaves the directory on purpose, with no
			// root to contain.
			if call, ok := rhs.(*ast.CallExpr); ok && seeded && unrollsSelf(QualifiedFunc(pass, call.Fun)) {
				args := make([]ast.Expr, len(call.Args))
				self := false
				for i, a := range call.Args {
					args[i] = a
					if id, ok := ast.Unparen(a).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
						args[i] = prior
						self = true
					}
				}
				if self {
					next = &ast.CallExpr{Fun: call.Fun, Lparen: call.Lparen, Args: args, Rparen: call.Rparen}
					if bound[obj] == rhs {
						bound[obj] = next
					}
				}
			}
			cur[obj] = next
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
	r := Derived(pass, p, bound)
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
			bound := PathBindings(pass, decl.Body)
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

// SymlinkResolved reports whether the sink's path is a CONTAINED
// resolution: the path is a local bound to a filepath.EvalSymlinks
// result, and a containment check of that local (filepath.Rel,
// strings.HasPrefix or strings.CutPrefix in the condition of an if that
// returns or holds the sink,
// directly or through a local bound to one) dominates the sink. The
// write or read then goes through the resolved path, and the resolved
// path is known to sit under the root.
//
// Issue #665 (2026-11) narrowed this from "an EvalSymlinks touched the
// chain". EvalSymlinks says where a path points; it confines nothing.
// Replays of the old quiet fixtures escaped three ways: the resolved
// path pointed outside the root and nothing compared it with the root;
// the root alone was resolved and a symlinked component of the key
// joined under it still led out; and the check resolved the path while
// the sink used the original spelling, so a link swapped in between
// (TOCTOU) won. Kernel confinement (os.Root) is the fix of first
// resort; this posture is the fallback for a path that already exists.
// sink may be nil for callers without a position, which keeps the
// unordered posture.
func SymlinkResolved(pass *analysis.Pass, pathArg ast.Expr, bound map[types.Object]ast.Expr, evals []*ast.CallExpr, body *ast.BlockStmt, sink ast.Node) bool {
	return resolvedContained(pass, pathArg, bound, body, sink)
}

// ContainedParent reports whether the sink's path is
// filepath.Join(dir, filepath.Base(x)) where dir is a contained
// resolution (SymlinkResolved's posture). It is a fix only for
// os.Mkdir, which refuses an existing leaf, symlink or not. os.MkdirAll
// stats the leaf, follows a link to a directory and returns nil, so a
// later write through it lands outside. A sink that opens or clobbers
// an existing leaf follows a symlinked one, and os.CreateTemp's
// argument is a directory it enters,
// so the leaf postures (O_NOFOLLOW, an Lstat check) or os.Root are
// their fixes.
func ContainedParent(pass *analysis.Pass, pathArg ast.Expr, bound map[types.Object]ast.Expr, body *ast.BlockStmt, sink ast.Node) bool {
	call, ok := Resolve(pass, pathArg, bound, 0).(*ast.CallExpr)
	if !ok || QualifiedFunc(pass, call.Fun) != "path/filepath.Join" || baseArg(pass, call, bound) == nil {
		return false
	}
	return resolvedContained(pass, call.Args[0], bound, body, sink)
}

func resolvedContained(pass *analysis.Pass, pathArg ast.Expr, bound map[types.Object]ast.Expr, body *ast.BlockStmt, sink ast.Node) bool {
	id, ok := ast.Unparen(pathArg).(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(id)
	call, ok := Resolve(pass, id, bound, 0).(*ast.CallExpr)
	if obj == nil || !ok || QualifiedFunc(pass, call.Fun) != "path/filepath.EvalSymlinks" {
		return false
	}
	// The resolution must run on every path to the sink: after
	// `if resolve { p, err = EvalSymlinks(p) }` the other branch writes
	// the unresolved p, and the check it passed was lexical.
	if !dominates(call, sink, body) {
		return false
	}
	// A local holds this resolution when it is bound to the call
	// itself (`real, err := EvalSymlinks(p)`, or the `p, err =
	// EvalSymlinks(p)` rebind), or is a copy of one assigned once,
	// after the call. bound holds each local's last binding, so a copy
	// made earlier, `out := real` before `real, err =
	// EvalSymlinks(other)`, resolves to the second call while it holds
	// the first. The sink's own path is held to the same rule.
	assigns := assignCounts(pass, body)
	holdsCall := func(o types.Object) bool {
		b, ok := bound[o]
		if !ok {
			return false
		}
		if ast.Unparen(b) == call {
			return true
		}
		return assigns[o] == 1 && b.Pos() >= call.End() && Resolve(pass, b, bound, 0) == call
	}
	if !holdsCall(obj) {
		return false
	}
	resolved := map[types.Object]bool{obj: true}
	for o := range bound {
		if holdsCall(o) {
			resolved[o] = true
		}
	}
	return ContainmentChecked(pass, body, bound, resolved, joinRoots(pass, call, bound), call.End(), sink)
}

// rootKey spells the root e names, looking through locals and the
// calls that keep a path's identity (EvalSymlinks, Abs, Clean), so
// `realRoot := EvalSymlinks(baseDir)` and baseDir compare equal.
func rootKey(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr) string {
	for i := 0; i < 8; i++ {
		e = ast.Unparen(Resolve(pass, e, bound, 0))
		c, ok := e.(*ast.CallExpr)
		if !ok || len(c.Args) != 1 {
			break
		}
		q := QualifiedFunc(pass, c.Fun)
		if q != "path/filepath.EvalSymlinks" && q != "path/filepath.Abs" && q != "path/filepath.Clean" {
			break
		}
		e = c.Args[0]
	}
	return types.ExprString(e)
}

// joinRoots collects the roots the path resolved by call was joined
// under: the first argument of every Join on its chain that appends a
// non-constant element, and the leftmost operand of a concatenation
// (`root + sep + name`) (`dir := Join(root, id)` then `Join(dir, name)`
// names both dir and root). A Join of constants only, such as
// Join(home, ".app"), is the root itself and the walk stops there. A
// containment test against any other root, such as the root's
// filepath.Dir, says nothing about this one.
func joinRoots(pass *analysis.Pass, call *ast.CallExpr, bound map[types.Object]ast.Expr) map[string]bool {
	roots := map[string]bool{}
	if len(call.Args) != 1 {
		return roots
	}
	isConst := func(a ast.Expr) bool {
		tv, ok := pass.TypesInfo.Types[a]
		return ok && tv.Value != nil
	}
	e := call.Args[0]
	for i := 0; i < 8; i++ {
		e = ast.Unparen(Resolve(pass, e, bound, 0))
		// root + sep + name: the leftmost operand is the root. (Only a
		// concatenation that appends caller data reaches here: one of
		// constants is no root-joined path.)
		if add, ok := e.(*ast.BinaryExpr); ok && add.Op == token.ADD {
			first := ast.Expr(add)
			for {
				b, ok := ast.Unparen(first).(*ast.BinaryExpr)
				if !ok || b.Op != token.ADD {
					break
				}
				first = b.X
			}
			e = first
			roots[rootKey(pass, e, bound)] = true
			continue
		}
		c, ok := e.(*ast.CallExpr)
		if !ok {
			break
		}
		q := QualifiedFunc(pass, c.Fun)
		if len(c.Args) == 1 && (q == "path/filepath.Dir" || q == "path/filepath.EvalSymlinks" || q == "path/filepath.Abs" || q == "path/filepath.Clean") {
			e = c.Args[0]
			continue
		}
		if q != "path/filepath.Join" || len(c.Args) < 2 {
			break
		}
		appends := false
		for _, a := range c.Args[1:] {
			appends = appends || !isConst(a)
		}
		if !appends {
			break
		}
		e = c.Args[0]
		roots[rootKey(pass, e, bound)] = true
	}
	return roots
}

// ContainmentChecked reports whether body tests one of objs against a
// root before sink so that an outside path cannot reach it. The test
// runs after the resolution at `after` and is filepath.Rel(root, real)
// or strings.HasPrefix/CutPrefix(real, root+sep), where real is one of
// objs itself (not an expression built from it), root is the root the
// path was joined under (roots, keyed by rootKey) and holds one value
// (each local on its chain assigned once, a parameter never
// reassigned), and sep is the target's separator. It
// decides an if that dominates sink in the right direction. Three
// shapes count:
//
//   - the then-branch refuses (returns, panics or branches away) and an
//     outside path makes the condition true: `if !HasPrefix(real, rootSep)`,
//     `if err != nil || HasPrefix(rel, "..")`;
//   - the sink sits in the then-branch and an outside path makes the
//     condition false: `if HasPrefix(real, rootSep) { write }`;
//   - the else-branch refuses and an outside path makes the condition
//     false: `if HasPrefix(real, rootSep) {} else { return }`.
//
// A ".." test of a Rel result also needs Rel's error refused
// (relErrRefused): a failed Rel returns "". IsLocal(rel) does not, as
// IsLocal("") is false.
//
// A Rel whose result nobody reads, a prefix test whose answer only
// logs, and `if HasPrefix(real, root) { return nil }` before the write
// (the inside path refuses, the outside one writes) confine nothing.
func ContainmentChecked(pass *analysis.Pass, body *ast.BlockStmt, bound map[types.Object]ast.Expr, objs map[types.Object]bool, roots map[string]bool, after token.Pos, sink ast.Node) bool {
	if body == nil {
		return false
	}
	sep := targetSeparator(pass)
	// rootOperand reports whether e names the root the path was joined
	// under (roots, keyed by rootKey) and holds one value. The join-root
	// match also refuses a constant (HasPrefix(real, "/") holds for every
	// absolute path, and a constant root joins no candidate path), a root
	// built from the resolved path itself (HasPrefix(real,
	// filepath.Dir(real)+sep) holds for nearly every path) and some other
	// directory such as the root's parent: none of them keys as the join
	// root.
	// stable reports whether e holds one root on every path: each local
	// on its chain, through EvalSymlinks/Abs/Clean, is assigned once (a
	// parameter, never), so `if widen { root = "" } else { root =
	// baseDir }` cannot hide its "" behind the last binding.
	assigns := assignCounts(pass, body)
	stable := func(e ast.Expr) bool {
		for i := 0; i < 8; i++ {
			if id, ok := e.(*ast.Ident); ok {
				obj := pass.TypesInfo.ObjectOf(id)
				values := assigns[obj]
				if obj != nil && (obj.Pos() < body.Pos() || obj.Pos() > body.End()) {
					values++ // a parameter or outer variable has its own value too
				}
				if values > 1 {
					return false
				}
				if b, ok := bound[obj]; ok {
					e = b
					continue
				}
				return true
			}
			c, ok := e.(*ast.CallExpr)
			if !ok || len(c.Args) != 1 {
				return true
			}
			q := QualifiedFunc(pass, c.Fun)
			if q != "path/filepath.EvalSymlinks" && q != "path/filepath.Abs" && q != "path/filepath.Clean" {
				return true
			}
			e = c.Args[0]
		}
		return true
	}
	rootOperand := func(e ast.Expr) bool {
		return stable(e) && roots[rootKey(pass, e, bound)]
	}
	// target reports whether e is the resolved path itself: one of objs,
	// not an expression built from it. HasPrefix(Join(root,
	// Base(real)), root+sep) holds while real points anywhere.
	target := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && objs[pass.TypesInfo.ObjectOf(id)]
	}
	// isContain reports whether c is a containment test of the
	// resolved path, made after it was resolved: a check of p before
	// `p, err = EvalSymlinks(p)` tested the unresolved spelling.
	//   - filepath.Rel(root, real);
	//   - strings.HasPrefix/CutPrefix(real, root + sep): the separator
	//     is the boundary, without it /srv/app-backup passes a test
	//     against /srv/app.
	isContain := func(c *ast.CallExpr) bool {
		if c.Pos() < after || len(c.Args) != 2 {
			return false
		}
		switch QualifiedFunc(pass, c.Fun) {
		case "path/filepath.Rel":
			return rootOperand(c.Args[0]) && target(c.Args[1])
		case "strings.HasPrefix", "strings.CutPrefix":
			// The operand holds one value too: after `rootSep := ""` and
			// `if strict { rootSep = root + sep }`, HasPrefix(real,
			// rootSep) is true for every path when strict is false.
			if !target(c.Args[0]) || !stable(c.Args[1]) {
				return false
			}
			add, ok := ast.Unparen(Resolve(pass, c.Args[1], bound, 0)).(*ast.BinaryExpr)
			if !ok || add.Op != token.ADD || !rootOperand(add.X) {
				return false
			}
			tv, ok := pass.TypesInfo.Types[add.Y]
			return ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) == sep
		}
		return false
	}
	isPrefix := func(c *ast.CallExpr) bool {
		fn := QualifiedFunc(pass, c.Fun)
		return fn == "strings.HasPrefix" || fn == "strings.CutPrefix"
	}
	// rel := filepath.Rel(root, real) on a contained root: an outside
	// real gives a rel that is exactly ".." or starts with ".." plus
	// the target's separator. relOf returns the Rel call, so parts
	// checked on two different Rel results never combine.
	//
	// Resolve follows each local's last binding, so a local assigned
	// twice breaks the chain: after `alias := rel` and
	// `rel, _ = filepath.Rel(root, real)`, alias still holds the first
	// Rel's answer but resolves to the second. No credit through one.
	relOf := func(e ast.Expr) *ast.CallExpr {
		for depth := 0; depth < 8; depth++ {
			id, ok := ast.Unparen(e).(*ast.Ident)
			if !ok {
				break
			}
			obj := pass.TypesInfo.ObjectOf(id)
			b, ok := bound[obj]
			if !ok || assigns[obj] > 1 {
				return nil
			}
			e = b
		}
		r, ok := ast.Unparen(e).(*ast.CallExpr)
		if ok && QualifiedFunc(pass, r.Fun) == "path/filepath.Rel" && isContain(r) {
			return r
		}
		return nil
	}
	strConst := func(e ast.Expr) (string, bool) {
		tv, ok := pass.TypesInfo.Types[e]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return "", false
		}
		return constant.StringVal(tv.Value), true
	}
	parentPrefix := ".." + sep
	// cover is what an outside path makes a condition: forced true,
	// forced false, neither, or, for tests of a Rel result, true on
	// only part of the outside spellings, kept per Rel call: dotdot
	// (rel == "..") or parent (rel under ".." plus separator). A
	// disjunction covering both parts of one Rel is forced true; a part
	// alone is not: HasPrefix(rel, "../") misses rel == "..", the
	// root's parent itself.
	const (
		forceFalse = -1
		forceTrue  = 1
		dotdot     = 1
		parent     = 2
	)
	type cover struct {
		force int
		parts map[*ast.CallExpr]int
	}
	part := func(r *ast.CallExpr, bit int) cover {
		return cover{parts: map[*ast.CallExpr]int{r: bit}}
	}
	// settle turns a cover into its force: Rel parts covering both
	// ".." and ".." plus separator force the condition true, but only
	// when Rel's own error is refused (relErrRefused). A failed Rel
	// leaves rel == "", which no ".." test catches.
	settle := func(c cover) int {
		if c.force != 0 {
			return c.force
		}
		for call, bits := range c.parts {
			if bits == dotdot|parent && relErrRefused(pass, body, call, sink) {
				return forceTrue
			}
		}
		return 0
	}
	var outsideForces func(e ast.Expr, depth int) cover
	outsideForces = func(e ast.Expr, depth int) cover {
		if depth > 6 {
			return cover{}
		}
		switch x := ast.Unparen(e).(type) {
		case *ast.UnaryExpr:
			if x.Op == token.NOT {
				return cover{force: -settle(outsideForces(x.X, depth+1))}
			}
		case *ast.BinaryExpr:
			if x.Op == token.EQL {
				// rel == ".."
				for _, pair := range [][2]ast.Expr{{x.X, x.Y}, {x.Y, x.X}} {
					if c, ok := strConst(pair[1]); ok && c == ".." && dominates(x, sink, body) {
						if r := relOf(pair[0]); r != nil {
							return part(r, dotdot)
						}
					}
				}
				return cover{}
			}
			l, r := outsideForces(x.X, depth+1), outsideForces(x.Y, depth+1)
			switch x.Op {
			case token.LOR:
				// true if either side is forced true; false only
				// when both are forced false.
				if l.force == forceTrue || r.force == forceTrue {
					return cover{force: forceTrue}
				}
				parts := map[*ast.CallExpr]int{}
				for _, side := range []cover{l, r} {
					for call, bits := range side.parts {
						parts[call] |= bits
					}
				}
				if len(parts) > 0 {
					return cover{parts: parts}
				}
				if l.force == forceFalse && r.force == forceFalse {
					return cover{force: forceFalse}
				}
			case token.LAND:
				lf, rf := settle(l), settle(r)
				if lf == forceFalse || rf == forceFalse {
					return cover{force: forceFalse}
				}
				if lf == forceTrue && rf == forceTrue {
					return cover{force: forceTrue}
				}
			}
		case *ast.Ident:
			// `inside := HasPrefix(real, root)`, or CutPrefix's found
			// result. Only a bool counts: the err beside Rel's result
			// says nothing about containment. The read must dominate:
			// `if check { if !inside { return } }` refuses only when
			// check is set.
			obj := pass.TypesInfo.ObjectOf(x)
			if obj == nil || !types.Identical(obj.Type(), types.Typ[types.Bool]) || !dominates(x, sink, body) {
				return cover{}
			}
			if r, ok := Resolve(pass, x, bound, 0).(*ast.CallExpr); ok && isPrefix(r) && isContain(r) &&
				dominates(r, sink, body) {
				return cover{force: forceFalse}
			}
		case *ast.CallExpr:
			if !dominates(x, sink, body) {
				return cover{}
			}
			if isPrefix(x) && isContain(x) {
				return cover{force: forceFalse} // HasPrefix(real, root): false outside
			}
			// filepath.IsLocal(rel) is false for ".." and "../x".
			if QualifiedFunc(pass, x.Fun) == "path/filepath.IsLocal" && len(x.Args) == 1 && relOf(x.Args[0]) != nil {
				return cover{force: forceFalse}
			}
			// HasPrefix(rel, ...) on rel := Rel(root, real): ".."
			// covers every outside rel, ".." plus the target's
			// separator only those below the parent, and anything
			// else ("..blocked", the other platform's separator)
			// neither.
			if isPrefix(x) && len(x.Args) == 2 {
				if r := relOf(x.Args[0]); r != nil {
					switch c, _ := strConst(x.Args[1]); c {
					case "..":
						return part(r, dotdot|parent)
					case parentPrefix:
						return part(r, parent)
					}
				}
			}
		}
		return cover{}
	}
	holds := func(b ast.Node) bool {
		return b != nil && sink != nil && b.Pos() <= sink.Pos() && sink.End() <= b.End()
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || found {
			return !found
		}
		f := settle(outsideForces(ifs.Cond, 0))
		switch {
		case f == forceTrue && refuses(ifs.Body) && !holds(ifs.Body):
			found = true
		case f == forceFalse && holds(ifs.Body):
			found = true
		case f == forceFalse && !holds(ifs.Body) && !holds(ifs.Else):
			if eb, ok := ifs.Else.(*ast.BlockStmt); ok && refuses(eb) {
				found = true
			}
		}
		return !found
	})
	return found
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
// posture: an O_NOFOLLOW flag, an os.Lstat whose FileInfo result a
// ModeSymlink test READS, or a call to a verified guard. A bare
// filepath.EvalSymlinks is no guard since #665: it says where a path
// points, and the guard's caller goes on to use the unresolved one. It judges a guard's body; whether the guard touched
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
		var first ast.Expr
		var values []ast.Expr
		switch declaration := n.(type) {
		case *ast.AssignStmt:
			if len(declaration.Lhs) > 0 {
				first = declaration.Lhs[0]
			}
			values = declaration.Rhs
		case *ast.ValueSpec:
			if len(declaration.Names) > 0 {
				first = declaration.Names[0]
			}
			values = declaration.Values
		}
		if first == nil || len(values) != 1 {
			return true
		}
		call, ok := values[0].(*ast.CallExpr)
		if !ok || QualifiedFunc(pass, call.Fun) != "os.Lstat" {
			return true
		}
		if keep != nil && !keep(call) {
			return true
		}
		if id, ok := first.(*ast.Ident); ok && id.Name != "_" {
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

// AddRangeVars marks the key and value variables of every range loop in
// body whose range expression carries caller data as caller data too:
// `for _, n := range names` hands each element of the names parameter
// to the loop body. Issue #665: resolveBeforeLoop joined each n under a
// resolved root and wrote it, and n was not caller data, so the write
// stayed quiet once the root's EvalSymlinks stopped counting. Only a
// range DIRECTLY over a parameter (or a field of one) counts: a range
// over a directory listing, a search path or a config table built from
// a parameter yields values this code computed, and counting them made
// every such loop variable both a root and caller data.
func AddRangeVars(pass *analysis.Pass, body *ast.BlockStmt, bound map[types.Object]ast.Expr, params map[types.Object]bool) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok || !rangesParam(pass, rs.X, params) {
			return true
		}
		for _, e := range []ast.Expr{rs.Key, rs.Value} {
			if id, ok := e.(*ast.Ident); ok && id.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					params[obj] = true
				}
			}
		}
		return true
	})
}

// rangesParam reports whether e is a text-carrying parameter, or a
// selector chain rooted at one (req.Names).
func rangesParam(pass *analysis.Pass, e ast.Expr, params map[types.Object]bool) bool {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			obj := pass.TypesInfo.ObjectOf(x)
			return obj != nil && params[obj]
		case *ast.SelectorExpr:
			e = x.X
		default:
			return false
		}
	}
}

// relErrRefused reports whether the error beside the Rel result r is
// refused before sink: an if that dominates sink, whose condition has
// `err != nil` as a disjunct and whose then-branch refuses, with err
// still holding r's error there. `rel, _ := filepath.Rel(...)` has no
// such if: a failed Rel gives rel == "", which passes every ".." test.
func relErrRefused(pass *analysis.Pass, body *ast.BlockStmt, r *ast.CallExpr, sink ast.Node) bool {
	var errObj types.Object
	var at token.Pos
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Rhs) != 1 || len(st.Lhs) != 2 || ast.Unparen(st.Rhs[0]) != r {
			return errObj == nil
		}
		if id, ok := st.Lhs[1].(*ast.Ident); ok && id.Name != "_" {
			errObj, at = pass.TypesInfo.ObjectOf(id), st.End()
		}
		return false
	})
	if errObj == nil {
		return false
	}
	// reassigned reports whether errObj is assigned in (at, before).
	reassigned := func(before token.Pos) bool {
		found := false
		ast.Inspect(body, func(n ast.Node) bool {
			st, ok := n.(*ast.AssignStmt)
			if found || !ok {
				return !found
			}
			for _, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == errObj && st.Pos() >= at && st.Pos() < before {
					found = true
				}
			}
			return !found
		})
		return found
	}
	var hasErrNil func(e ast.Expr) bool
	hasErrNil = func(e ast.Expr) bool {
		x, ok := ast.Unparen(e).(*ast.BinaryExpr)
		if !ok {
			return false
		}
		if x.Op == token.LOR {
			return hasErrNil(x.X) || hasErrNil(x.Y)
		}
		if x.Op != token.NEQ {
			return false
		}
		isErr := func(e ast.Expr) bool {
			id, ok := ast.Unparen(e).(*ast.Ident)
			return ok && pass.TypesInfo.ObjectOf(id) == errObj
		}
		isNil := func(e ast.Expr) bool {
			id, ok := ast.Unparen(e).(*ast.Ident)
			return ok && id.Name == "nil"
		}
		return (isErr(x.X) && isNil(x.Y)) || (isNil(x.X) && isErr(x.Y))
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if found || !ok {
			return !found
		}
		if ifs.Cond.Pos() > at && hasErrNil(ifs.Cond) && refuses(ifs.Body) &&
			!(ifs.Body.Pos() <= sink.Pos() && sink.End() <= ifs.Body.End()) &&
			dominates(ifs.Cond, sink, body) && !reassigned(ifs.Cond.Pos()) {
			found = true
		}
		return !found
	})
	return found
}

// assignCounts counts the assignments and var-spec initializers that
// bind each local in body.
func assignCounts(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]int {
	counts := map[types.Object]int{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						counts[obj]++
					}
				}
			}
		case *ast.ValueSpec:
			for _, name := range st.Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil && name.Name != "_" {
					counts[obj]++
				}
			}
		case *ast.IncDecStmt:
			if id, ok := st.X.(*ast.Ident); ok {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					counts[obj]++
				}
			}
		}
		return true
	})
	return counts
}

// targetSeparator is filepath.Separator as the package was type-checked
// for: the vettool runs on the host, and `make analyze` vets each
// package for linux, windows and darwin.
func targetSeparator(pass *analysis.Pass) string {
	for _, imp := range pass.Pkg.Imports() {
		if imp.Path() != "path/filepath" {
			continue
		}
		if c, ok := imp.Scope().Lookup("Separator").(*types.Const); ok {
			if v, ok := constant.Int64Val(c.Val()); ok {
				return string(rune(v))
			}
		}
	}
	return "/"
}

// refuses reports whether a block's last statement keeps control from
// the sink: a return, a panic call, or an unlabelled break or continue.
// A goto or a labelled branch can land on the sink itself. An
// unlabelled break or continue needs no target check here: the if it
// ends dominates the sink, so it sits in a statement list ahead of the
// sink, and the loop or switch it leaves encloses the sink too.
func refuses(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	switch st := b.List[len(b.List)-1].(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return st.Label == nil && (st.Tok == token.BREAK || st.Tok == token.CONTINUE)
	case *ast.ExprStmt:
		if c, ok := st.X.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	}
	return false
}
