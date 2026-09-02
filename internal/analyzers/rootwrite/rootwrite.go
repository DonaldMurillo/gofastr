// Package rootwrite catches writes whose containment under a root is
// resolved lexically only: os.WriteFile / os.Create / os.OpenFile(write
// flag) / os.MkdirAll on a path built under a root — filepath.Join, or
// a flat `root + "/" + x` concatenation — where the root is a
// caller-supplied parameter or field — with no filepath.EvalSymlinks
// on that path's chain — plus the archive twin: zip.Writer entry names
// assembled from a parameter with no path.Clean on the entry-name
// chain.
//
// The bug class: lexical prefix checks cannot see symlinks. Probes
// TestApplyRefusesSymlinkEscape (framework/contracts report.go
// containedPath/Apply, fixed in 77fdbaf4: a diagnostic whose path
// crossed a symlinked directory was written outside the project root
// even though Join+HasPrefix said "contained") and
// TestPackZipPrefixCannotEscapeDir (framework/sdk zip.go PackZip, fixed
// in 1501a555: a "../" prefix placed archive entries above the target
// directory on extract).
//
// Every gate is per write and on the write's own dataflow: resolution,
// validation, and cleaning on an unrelated path (or consulted for a
// boolean and leaving the path components untouched) gate nothing.
//
// Silent postures, deliberately:
//   - the write's path (or a component of it, or the Join's root) is
//     bound to a filepath.EvalSymlinks result, or an EvalSymlinks ran
//     on this path expression — resolution on the chain (the fix
//     posture);
//   - calls to symlink-named guards (EnsureNoSymlinkPath): resolution
//     by another name;
//   - a sanitizer/validator whose RESULT replaces a joined component
//     (sanitize(name) feeding the Join): the component that reaches
//     the disk passed through it. A validator consulted for its
//     boolean cannot see symlinks and gates nothing;
//   - roots that are not parameters or root/base/dir-named fields (a
//     constant or computed root has no caller-controlled boundary to
//     defend), including a rooty local resolved through one;
//   - builds whose every non-root argument is a literal — nothing
//     caller-controlled is appended under the root. This holds at the
//     helper hop too: a same-package containment helper called with
//     literal-only non-root arguments stays quiet, and a helper whose
//     body resolves symlinks is the fix posture;
//   - temp roots: a local bound to os.MkdirTemp or t.TempDir is
//     throwaway by construction;
//   - zip entry names assembled only from literals or non-parameter
//     values (a wrapper forwarding its own name parameter composes
//     nothing), and entry names whose assembly is bound to a
//     path.Clean / filepath.Clean result;
//   - reads (os.Open, os.ReadFile, O_RDONLY) by construction;
//   - _test.go files.
package rootwrite

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrrootwrite",
	Doc:  "forbids writes under a root whose containment is lexical only: resolve with filepath.EvalSymlinks, and path.Clean zip entry names",
	Run:  run,
}

// validatorRe matches sanitizer/validator callee names: only their
// RESULT replacing a path component shields a write.
var validatorRe = regexp.MustCompile(`(?i)sanit|valid|clean|safe`)

func run(pass *analysis.Pass) (any, error) {
	pkgFuncs := map[string]*ast.FuncDecl{}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				pkgFuncs[fn.Name.Name] = fn
			}
		}
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkFunc(pass, fn, pkgFuncs)
		}
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl, pkgFuncs map[string]*ast.FuncDecl) {
	bound := bindings(pass, fn.Body)
	params := paramObjects(pass, fn)
	// Resolution and validation gates are per WRITE, not per function:
	// an EvalSymlinks (or validator, or Clean) on an unrelated path
	// resolves nothing for this one.
	evals := evalSymlinkCalls(pass, fn.Body)
	symlinkGuard := callsSymlinkNamed(pass, fn.Body)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Filesystem writes under a lexically-contained root.
		var pathArg ast.Expr
		switch qualifiedFunc(pass, call.Fun) {
		case "os.WriteFile", "os.Create", "os.MkdirAll":
			pathArg = argAt(call, 0)
		case "os.OpenFile":
			if hasWriteFlag(argAt(call, 1)) {
				pathArg = argAt(call, 0)
			}
		}
		if pathArg != nil && !symlinkGuard &&
			!symlinkResolved(pass, pathArg, bound, evals) &&
			!validatorShields(pass, pathArg, bound) &&
			joinsUnderRooty(pass, pathArg, bound, params, pkgFuncs) {
			pass.Reportf(call.Pos(),
				"write under a root with lexical containment only: a symlinked directory component escapes the root undetected; resolve with filepath.EvalSymlinks before writing")
		}
		// zip.Writer entries named from a parameter without a Clean on
		// the entry-name chain.
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (sel.Sel.Name == "Create" || sel.Sel.Name == "CreateHeader") && isZipWriter(pass, sel.X) {
			var name ast.Expr
			if sel.Sel.Name == "Create" {
				name = argAt(call, 0)
			} else {
				name = headerName(pass, argAt(call, 0), bound)
			}
			if name != nil && !cleanedEntryName(pass, name, bound) && composedFromParam(pass, name, bound, params) {
				pass.Reportf(call.Pos(),
					"zip entry name built from a parameter without path.Clean: an uncleaned name can place entries outside the target directory on extract; path.Clean and reject \"..\" segments")
			}
		}
		return true
	})
}

// evalSymlinkCalls collects the filepath.EvalSymlinks calls in body.
func evalSymlinkCalls(pass *analysis.Pass, body *ast.BlockStmt) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok &&
			qualifiedFunc(pass, call.Fun) == "path/filepath.EvalSymlinks" {
			out = append(out, call)
		}
		return true
	})
	return out
}

// symlinkResolved reports whether the write's path is resolved against
// symlinks ON THE CHAIN: the path itself is an EvalSymlinks result, a
// resolved local feeds it, or an EvalSymlinks ran on this path
// expression (directly or through a local). An EvalSymlinks on an
// unrelated path resolves nothing.
func symlinkResolved(pass *analysis.Pass, pathArg ast.Expr, bound map[types.Object]ast.Expr, evals []*ast.CallExpr) bool {
	isEval := func(c *ast.CallExpr) bool {
		return qualifiedFunc(pass, c.Fun) == "path/filepath.EvalSymlinks"
	}
	r := resolve(pass, pathArg, bound, 0)
	if call, ok := r.(*ast.CallExpr); ok && isEval(call) {
		return true
	}
	if mentionsCall(pass, pathArg, bound, isEval, 0) {
		return true
	}
	for _, ev := range evals {
		for _, a := range ev.Args {
			if argTouchesPath(pass, a, bound, r) {
				return true
			}
		}
	}
	return false
}

// argTouchesPath reports whether an EvalSymlinks argument mentions a
// local bound to the same expression as the write path — the resolved
// leaf, its Dir, or the path itself.
func argTouchesPath(pass *analysis.Pass, arg ast.Expr, bound map[types.Object]ast.Expr, r ast.Expr) bool {
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

// validatorShields reports whether a sanitizer/validator's RESULT
// replaces a component of the built path: that is the only validator
// posture that touches what reaches the disk. A validator consulted
// for its boolean (validName(name) in a guard) cannot see symlinks
// and shields nothing.
func validatorShields(pass *analysis.Pass, pathArg ast.Expr, bound map[types.Object]ast.Expr) bool {
	isValidator := func(c *ast.CallExpr) bool {
		var name string
		switch fun := c.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		return name != "" && validatorRe.MatchString(name)
	}
	r := resolve(pass, pathArg, bound, 0)
	for _, comp := range pathComponents(r) {
		if mentionsCall(pass, comp, bound, isValidator, 0) {
			return true
		}
	}
	return false
}

// pathComponents returns the caller-facing components of a built path:
// the arguments of the building call, or everything concatenated after
// the leading operand.
func pathComponents(r ast.Expr) []ast.Expr {
	switch x := r.(type) {
	case *ast.CallExpr:
		return x.Args
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			if ops := concatOperands(x, nil); len(ops) >= 2 {
				return ops[1:]
			}
		}
	}
	return nil
}

// cleanedEntryName reports whether the zip entry name's assembly is
// bound to a path.Clean / filepath.Clean result. A Clean on an
// unrelated path cleans no entry name.
func cleanedEntryName(pass *analysis.Pass, name ast.Expr, bound map[types.Object]ast.Expr) bool {
	isClean := func(c *ast.CallExpr) bool {
		q := qualifiedFunc(pass, c.Fun)
		return q == "path.Clean" || q == "path/filepath.Clean"
	}
	return mentionsCall(pass, name, bound, isClean, 0)
}

// mentionsCall reports whether e's assembly involves (directly, or
// through locals bound in this function) a call matching pred.
func mentionsCall(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, pred func(*ast.CallExpr) bool, depth int) bool {
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
				if b, ok := bound[obj]; ok && mentionsCall(pass, b, bound, pred, depth+1) {
					found = true
					return false
				}
			}
		}
		return !found
	})
	return found
}

// callsSymlinkNamed reports whether the body calls any function whose
// name says symlink (EnsureNoSymlinkPath, EnsureNoSymlinkLeaf): the
// codegen fileset posture, which refuses symlinked components without
// spelling filepath.EvalSymlinks. filepath.EvalSymlinks itself is
// excluded — its resolution is judged on the write's chain, not by
// name presence.
func callsSymlinkNamed(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if qualifiedFunc(pass, call.Fun) == "path/filepath.EvalSymlinks" {
			return true
		}
		var name string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		if name != "" && strings.Contains(strings.ToLower(name), "symlink") {
			found = true
		}
		return !found
	})
	return found
}

// composedFromParam reports whether the zip entry name is ASSEMBLED
// from a function parameter — concatenated or formatted — rather than
// passed through verbatim. A wrapper that forwards its own name
// parameter to Create is composing nothing; its callers are.
func composedFromParam(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) bool {
	e = resolve(pass, e, bound, 0)
	switch x := e.(type) {
	case *ast.BinaryExpr:
		return mentionsParam(pass, x, bound, params, 0)
	case *ast.CallExpr:
		return mentionsParam(pass, x, bound, params, 0)
	default:
		return false
	}
}

// joinsUnderRooty reports whether e resolves — directly, through
// locals, or through one same-package helper call — to a path built
// under a root/base/dir-named parameter or field: a filepath.Join, or
// a flat `root + "/" + x` concatenation, with at least one
// caller-controlled component after the root.
func joinsUnderRooty(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool, pkgFuncs map[string]*ast.FuncDecl) bool {
	e = resolve(pass, e, bound, 0)
	if be, ok := e.(*ast.BinaryExpr); ok && be.Op == token.ADD {
		return concatUnderRooty(pass, be, bound, params)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	if qualifiedFunc(pass, call.Fun) == "path/filepath.Join" {
		if len(call.Args) < 2 {
			return false
		}
		root := rootyRoot(pass, call.Args[0], bound, params)
		if root == nil {
			return false
		}
		if isTempRoot(pass, root, bound) {
			return false
		}
		// Narrowed 2026-09-02: a component the caller controls is what
		// makes the write an escape risk; formatted timestamps, counters,
		// and literal manifest names are not.
		for _, a := range call.Args[1:] {
			if mentionsParam(pass, a, bound, params, 0) {
				return true
			}
		}
		return false
	}
	// A helper that joins its own root parameter (e.g. containedPath).
	// The non-root arguments at the call site must carry caller data:
	// a helper called with literal-only components appends nothing
	// caller-controlled under the root.
	decl := calleeDecl(pass, call.Fun, pkgFuncs)
	if decl == nil {
		return false
	}
	rootParam := rootyParam(decl)
	if rootParam == "" || !helperJoinsParam(pass, decl, rootParam) {
		return false
	}
	if callsQualified(decl.Body, pass, "path/filepath.EvalSymlinks") || callsSymlinkNamed(pass, decl.Body) {
		return false // the helper resolves symlinks: the fix posture
	}
	rootIdx := rootyParamIndex(decl, rootParam)
	for i, a := range call.Args {
		if i == rootIdx {
			continue
		}
		if mentionsParam(pass, a, bound, params, 0) {
			return true
		}
	}
	return false
}

// rootyRoot resolves the Join's first argument to the root expression:
// a rooty parameter or field directly, or a local whose binding is a
// rooty param/field selector — including one passed through a
// root-returning helper (root, err := resolveRoot(v.root)).
func rootyRoot(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) ast.Expr {
	if r := rootyParamOrField(pass, e, bound, params); r != nil {
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
	if r := rootyParamOrField(pass, b, bound, params); r != nil {
		return r
	}
	if c, ok := b.(*ast.CallExpr); ok {
		for _, a := range c.Args {
			if r := rootyParamOrField(pass, a, bound, params); r != nil {
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
	ops := concatOperands(be, nil)
	if len(ops) < 2 {
		return false
	}
	root := rootyRoot(pass, ops[0], bound, params)
	if root == nil {
		return false
	}
	if isTempRoot(pass, root, bound) {
		return false
	}
	for _, o := range ops[1:] {
		if mentionsParam(pass, o, bound, params, 0) {
			return true
		}
	}
	return false
}

// concatOperands flattens a left-associated ADD chain, leftmost first.
func concatOperands(be *ast.BinaryExpr, out []ast.Expr) []ast.Expr {
	if inner, ok := be.X.(*ast.BinaryExpr); ok && inner.Op == token.ADD {
		out = concatOperands(inner, out)
	} else {
		out = append(out, be.X)
	}
	return append(out, be.Y)
}

// rootyParamIndex returns the positional index of decl's parameter
// named rootParam, or -1.
func rootyParamIndex(decl *ast.FuncDecl, rootParam string) int {
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

// rootyParamOrField returns the root expression when e is a parameter
// or struct field whose name says root/base/dir.
func rootyParamOrField(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) ast.Expr {
	switch x := e.(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		v, ok := obj.(*types.Var)
		if !ok {
			return nil
		}
		if !rootyName(v.Name()) {
			return nil
		}
		if v.IsField() || params[obj] {
			return x
		}
		return nil
	case *ast.SelectorExpr:
		obj := pass.TypesInfo.ObjectOf(x.Sel)
		v, ok := obj.(*types.Var)
		if ok && v.IsField() && rootyName(v.Name()) {
			return x
		}
		return nil
	default:
		return nil
	}
}

// rootyName matches the root/base/dir family: compound names like
// rootDir, baseDir, and workdir all carry the token.
func rootyName(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "root") || strings.Contains(l, "base") || strings.Contains(l, "dir")
}

// isTempRoot reports whether the root expression is a local bound to a
// throwaway directory: os.MkdirTemp or t.TempDir.
func isTempRoot(pass *analysis.Pass, root ast.Expr, bound map[types.Object]ast.Expr) bool {
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
	if qualifiedFunc(pass, call.Fun) == "os.MkdirTemp" {
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
		if !ok || qualifiedFunc(pass, call.Fun) != "path/filepath.Join" {
			return true
		}
		if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == param && len(call.Args) >= 2 {
			found = true
		}
		return true
	})
	return found
}

// rootyParam returns the name of decl's first parameter named in the
// root/base/dir family, or "".
func rootyParam(decl *ast.FuncDecl) string {
	if decl.Type.Params == nil {
		return ""
	}
	for _, f := range decl.Type.Params.List {
		for _, name := range f.Names {
			if rootyName(name.Name) {
				return name.Name
			}
		}
	}
	return ""
}

// calleeDecl resolves a call to a package-level function declaration,
// for the one helper hop.
func calleeDecl(pass *analysis.Pass, fun ast.Expr, pkgFuncs map[string]*ast.FuncDecl) *ast.FuncDecl {
	var name string
	switch f := fun.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		if _, ok := f.X.(*ast.Ident); !ok {
			return nil
		}
		if pn, ok := pass.TypesInfo.ObjectOf(f.X.(*ast.Ident)).(*types.PkgName); !ok || pn.Imported() != pass.Pkg {
			return nil
		}
		name = f.Sel.Name
	default:
		return nil
	}
	return pkgFuncs[name]
}

// headerName resolves a CreateHeader argument — an &zip.FileHeader
// composite, possibly through a local — to its Name field expression.
func headerName(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr) ast.Expr {
	e = resolve(pass, e, bound, 0)
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Name" {
			return kv.Value
		}
	}
	return nil
}

// mentionsParam reports whether e's assembly involves one of the
// function's parameters (directly, or through locals bound in this
// function), which is what makes a zip entry name caller-controlled.
func mentionsParam(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool, depth int) bool {
	if depth > 6 {
		return false
	}
	switch x := e.(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		if obj == nil {
			return false
		}
		if params[obj] {
			return true
		}
		if b, ok := bound[obj]; ok {
			return mentionsParam(pass, b, bound, params, depth+1)
		}
		return false
	case *ast.BasicLit:
		return false
	default:
		hit := false
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				obj := pass.TypesInfo.ObjectOf(id)
				if obj != nil && params[obj] {
					hit = true
					return false
				}
				if b, ok := bound[obj]; ok && mentionsParam(pass, b, bound, params, depth+1) {
					hit = true
					return false
				}
			}
			return !hit
		})
		return hit
	}
}

// resolve follows single-value local bindings (`x := expr`), keeping the
// last binding in source order like mapwriter does.
func resolve(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, depth int) ast.Expr {
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

// bindings maps each local defined by an assignment to the expression it
// was last bound to. Multi-value assignments map each left side to the
// single call on the right, which is what lets `dir, err :=
// os.MkdirTemp(...)` register for the temp-root silence.
func bindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
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

// paramObjects collects the function's parameter variables.
func paramObjects(pass *analysis.Pass, fn *ast.FuncDecl) map[types.Object]bool {
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

// callsQualified reports whether body contains a call to the
// import-resolved pkg.Func.
func callsQualified(body *ast.BlockStmt, pass *analysis.Pass, want string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && qualifiedFunc(pass, call.Fun) == want {
			found = true
		}
		return !found
	})
	return found
}

// hasWriteFlag reports whether the os.OpenFile flag expression contains
// any of the write/create bits.
func hasWriteFlag(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			switch sel.Sel.Name {
			case "O_CREATE", "O_WRONLY", "O_RDWR", "O_APPEND", "O_TRUNC":
				found = true
			}
		}
		return !found
	})
	return found
}

// isZipWriter reports whether e's type is archive/zip.Writer or a
// pointer to it.
func isZipWriter(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "archive/zip" && obj.Name() == "Writer"
}

// qualifiedFunc renders a selector callee as "importpath.Func",
// resolving the package through the type checker so an aliased import
// still matches (and path/filepath reports its full path, not "filepath").
func qualifiedFunc(pass *analysis.Pass, fun ast.Expr) string {
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

func argAt(call *ast.CallExpr, i int) ast.Expr {
	if len(call.Args) > i {
		return call.Args[i]
	}
	return nil
}

func isLiteral(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.BinaryExpr:
		return isLiteral(x.X) && isLiteral(x.Y)
	default:
		return false
	}
}
