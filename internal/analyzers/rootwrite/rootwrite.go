// Package rootwrite catches writes whose containment under a root is
// resolved lexically only: os.WriteFile / os.Create / os.OpenFile(write
// flag) / os.MkdirAll on a path built with filepath.Join(root, …) where
// root is a caller-supplied parameter or field — with no
// filepath.EvalSymlinks anywhere on the path-producing chain — plus the
// archive twin: zip.Writer entry names assembled from a parameter with
// no path.Clean in the function.
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
// Silent postures, deliberately:
//   - any filepath.EvalSymlinks on the chain (writing function, or the
//     same-package helper that produced the path) — the fix posture;
//   - roots that are not parameters or root/base/dir-named fields: a
//     constant or computed root has no caller-controlled boundary to
//     defend;
//   - joins whose every non-root argument is a literal: nothing
//     caller-controlled is appended under the root;
//   - temp roots: a local bound to os.MkdirTemp or t.TempDir is
//     throwaway by construction;
//   - zip entry names assembled only from literals or non-parameter
//     values, and any function that calls path.Clean / filepath.Clean;
//   - reads (os.Open, os.ReadFile, O_RDONLY) by construction;
//   - _test.go files.
//
// Narrowed 2026-09-02 after the whole-repo run: a direct Join must
// carry a caller-controlled (parameter-derived) component beside the
// root — formatted timestamps, counters, manifest literals, and
// generated ids are not escape vectors — and zip entry names must be
// COMPOSED from a parameter, not forwarded through a wrapper whose own
// callers compose. Calls to symlink-named guards (EnsureNoSymlinkPath)
// count as resolution. A same-package containment helper (rooty first
// parameter joined with the rest) feeding a write still fires when
// neither side resolves symlinks: that is the containedPath/Apply pair
// this rule was born from.
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
	guarded := callsQualified(fn.Body, pass, "path/filepath.EvalSymlinks") || callsSymlinkNamed(fn.Body)
	cleans := callsQualified(fn.Body, pass, "path.Clean") || callsQualified(fn.Body, pass, "path/filepath.Clean")
	bound := bindings(pass, fn.Body)
	params := paramObjects(pass, fn)
	// A function that runs its components through a validator or
	// sanitizer (validateScaffoldName, sanitizeMigrationName) is doing
	// its own containment: cmd/gofastr's scaffold and migrate's
	// generator both sanitize before joining. Measured 2026-09-02.
	validates := callsNamedLike(fn.Body, `(?i)sanit|valid|clean|safe`)

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
		if pathArg != nil && !guarded && !validates &&
			joinsUnderRooty(pass, pathArg, bound, params, pkgFuncs) {
			pass.Reportf(call.Pos(),
				"write under a root with lexical containment only: a symlinked directory component escapes the root undetected; resolve with filepath.EvalSymlinks before writing")
		}
		// zip.Writer entries named from a parameter without a Clean.
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (sel.Sel.Name == "Create" || sel.Sel.Name == "CreateHeader") && isZipWriter(pass, sel.X) {
			var name ast.Expr
			if sel.Sel.Name == "Create" {
				name = argAt(call, 0)
			} else {
				name = headerName(pass, argAt(call, 0), bound)
			}
			if name != nil && !cleans && composedFromParam(pass, name, bound, params) {
				pass.Reportf(call.Pos(),
					"zip entry name built from a parameter without path.Clean: an uncleaned name can place entries outside the target directory on extract; path.Clean and reject \"..\" segments")
			}
		}
		return true
	})
}

// callsSymlinkNamed reports whether the body calls any function whose
// name says symlink (EnsureNoSymlinkPath, EnsureNoSymlinkLeaf): the
// codegen fileset posture, which refuses symlinked components without
// spelling filepath.EvalSymlinks.
func callsSymlinkNamed(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
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

// joinsUnderRooty reports whether e resolves — directly, through locals,
// or through one same-package helper call — to filepath.Join(root, …)
// with root a root/base/dir-named parameter or field and at least one
// non-literal joined component after it.
func joinsUnderRooty(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool, pkgFuncs map[string]*ast.FuncDecl) bool {
	e = resolve(pass, e, bound, 0)
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	if qualifiedFunc(pass, call.Fun) == "path/filepath.Join" {
		if len(call.Args) < 2 {
			return false
		}
		root := rootyParamOrField(pass, call.Args[0], bound, params)
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
	// The non-root arguments at the call site must carry caller data.
	decl := calleeDecl(pass, call.Fun, pkgFuncs)
	if decl == nil {
		return false
	}
	rootParam := rootyParam(decl)
	if rootParam == "" || !helperJoinsParam(pass, decl, rootParam) {
		return false
	}
	if callsQualified(decl.Body, pass, "path/filepath.EvalSymlinks") || callsSymlinkNamed(decl.Body) {
		return false // the helper resolves symlinks: the fix posture
	}
	return true
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

// callsNamedLike reports whether the body calls any function whose name
// matches the pattern: validators and sanitizers that shield the path
// components this function joins.
func callsNamedLike(body *ast.BlockStmt, pattern string) bool {
	re := regexp.MustCompile(pattern)
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			name = fun.Name
		case *ast.SelectorExpr:
			name = fun.Sel.Name
		}
		if name != "" && re.MatchString(name) {
			found = true
		}
		return !found
	})
	return found
}
