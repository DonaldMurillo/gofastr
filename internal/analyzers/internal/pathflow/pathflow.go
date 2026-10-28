// Package pathflow holds the path-dataflow machinery the root-shaped
// analyzers (rootwrite, rootread) share: local-binding resolution, the
// root/base/dir-named root tests, the lexical Join/concat containment
// shapes — including the one-hop same-package helper that RETURNS a
// root-joined path — and the EvalSymlinks-on-the-chain judgement that
// separates the fix posture from a resolution on an unrelated path,
// plus the verified symlink guards (SymlinkGuards, GuardsPath): a
// function counts as a guard by what its body does, never by its name,
// and a guard call gates only the path it was handed.
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

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"golang.org/x/tools/go/analysis"
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
	if CallsQualified(decl.Body, pass, "path/filepath.EvalSymlinks") || CallsGuard(pass, decl.Body, isGuard) {
		return false // the helper resolves symlinks: the fix posture
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
		if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == param && len(call.Args) >= 2 {
			found = true
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

// Bindings maps each local defined by an assignment to the expression it
// was last bound to. Multi-value assignments map each left side to the
// single call on the right, which is what lets `dir, err :=
// os.MkdirTemp(...)` register for the temp-root silence and `dst, err :=
// s.fullPath(key)` register for the helper hop.
func Bindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
	bound := map[types.Object]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Rhs) != 1 {
			return true
		}
		for _, lhs := range st.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					bound[obj] = st.Rhs[0]
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

// CallsQualified reports whether body contains a call to the
// import-resolved pkg.Func.
func CallsQualified(body *ast.BlockStmt, pass *analysis.Pass, want string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && QualifiedFunc(pass, call.Fun) == want {
			found = true
		}
		return !found
	})
	return found
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
// symlinks ON THE CHAIN: the path itself is an EvalSymlinks result, a
// resolved local feeds it, or an EvalSymlinks ran on this path
// expression — directly, through a local, or on its Dir, which
// resolves the directory chain above the leaf (the core/upload Save
// posture). An EvalSymlinks on an unrelated path resolves nothing.
func SymlinkResolved(pass *analysis.Pass, pathArg ast.Expr, bound map[types.Object]ast.Expr, evals []*ast.CallExpr) bool {
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
// consulted with ModeSymlink, or a call to a verified guard. It judges
// a guard's body; whether the guard touched the right path is
// GuardsPath's question. (os.Root needs no guard: its methods are no
// os.* sink, so a write or read through one is quiet by construction.)
func ResolvesSymlinks(pass *analysis.Pass, body *ast.BlockStmt, isGuard GuardFunc) bool {
	lstat, modeSymlink, found := false, false, false
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
			switch v.Sel.Name {
			case "ModeSymlink":
				modeSymlink = true
			case "O_NOFOLLOW":
				found = true
			}
		}
		return !found
	})
	return found || (lstat && modeSymlink)
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
// sink's own path: the path expression itself, its Dir, or a local
// bound to either (ArgTouchesPath). A guard on the root alone, or on
// an unrelated path, leaves the sink's components unchecked.
func GuardsPath(pass *analysis.Pass, body *ast.BlockStmt, pathArg ast.Expr, bound map[types.Object]ast.Expr, isGuard GuardFunc) bool {
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

// CallsGuard reports whether body calls any verified guard: the helper
// hop's posture, where the helper that RETURNS a root-joined path
// refuses symlinks on it before returning.
func CallsGuard(pass *analysis.Pass, body *ast.BlockStmt, isGuard GuardFunc) bool {
	if isGuard == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && isGuard(staticCallee(pass, call.Fun)) {
			found = true
		}
		return !found
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
// and consults ModeSymlink — the leaf posture that makes an unlink or
// an overwrite refuse to travel through a symlinked leaf. The
// directory components above the leaf stay unresolved; both root
// analyzers accept it as the documented partial fix (rootread for
// os.Remove, rootwrite for the leaf-clobbering write sinks).
func LstatLeafCheck(pass *analysis.Pass, body *ast.BlockStmt, pathArg ast.Expr, bound map[types.Object]ast.Expr) bool {
	r := Resolve(pass, pathArg, bound, 0)
	touched := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || QualifiedFunc(pass, call.Fun) != "os.Lstat" {
			return true
		}
		for _, a := range call.Args {
			if ArgTouchesPath(pass, a, bound, r) {
				touched = true
				return false
			}
		}
		return true
	})
	if !touched {
		return false
	}
	modeCheck := false
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "ModeSymlink" {
			modeCheck = true
		}
		return !modeCheck
	})
	return modeCheck
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
