// Package rootread catches reads whose containment under a root is
// resolved lexically only — the read twin of rootwrite. os.Open /
// os.ReadFile / os.ReadDir / os.Stat / os.Remove / read-only
// os.OpenFile on a path built under a root (filepath.Join or flat
// concatenation) — directly or one same-package read helper deep
// (readPage(src): os.ReadFile(src), the I4 hop ported 2026-11) — with
// no filepath.EvalSymlinks on the path's chain and no O_NOFOLLOW or
// Lstat+ModeSymlink leaf check — plus the fs.FS-mediated spelling:
// `.Open(name)` / `fs.ReadFile(fsys, name)` / `fs.Stat(fsys, name)`
// / `fs.ReadDir(fsys, name)` where the fs value is a CALLER-SUPPLIED
// io/fs.FS (parameter or field), the name is caller-controlled, and
// the scope serves HTTP (the declaration or any closure around the
// read).
//
// The bug class: a lexical check (Join + Clean + HasPrefix, or
// fs.ValidPath) cannot see a symlinked directory component, so a read
// "under the root" follows it out. Probes from the 2026-09-04 round:
// TestStaticSymlinkEscapeRefused (core/static serveFile opens the
// request path on a caller-supplied fs.FS — an os.DirFS-backed config
// serves whatever a symlink inside the tree points at, while the
// embed.FS the package documents cannot hold symlinks at all) and
// TestLocalStorageRefusesSymlinkEscape (core/upload local.go:
// Get/GetRange/Exists/Delete join baseDir and key, sanitize the key,
// prefix-check lexically, and then open/stat/remove through any
// symlinked directory planted under baseDir).
//
// NO validator shield, for reads or writes: a sanitizer's result
// replacing the joined component (sanitizeKey) does not stop a
// symlinked DIRECTORY component — the core/upload probes escaped
// through exactly such a sanitized join. rootwrite carried a
// sanitizer-result shield until the same round's reviewer mutation
// (core/upload Save with both EvalSymlinks calls removed stayed
// silent behind sanitizeKey) deleted it; only resolution postures
// gate in either twin, and os.Root is the strongest of them.
//
// The fs.FS sink carries an HTTP-serving gate (a net/http.Request or
// ResponseWriter parameter): the boundary the probes crossed is the
// request. A library helper whose caller owns BOTH the fs and the
// name (image.OpenFS, i18n.LoadJSONCatalog) is one trust domain; the
// site that matters is the caller that mixes a request into it, and
// firing inside every such helper would teach people to ignore the
// gate.
//
// Silent postures, deliberately — os.Root first, as the fix posture
// of first resort on Go 1.27:
//   - reads made through an *os.Root method (os.OpenRoot(root) +
//     root.Open / ReadFile / Stat / Lstat / Remove / RemoveAll / FS):
//     containment is enforced by the kernel — a symlink under the
//     root cannot lead the read out, and there is no TOCTOU window
//     between check and use. Root methods are no os.* sink and no
//     fs.FS value here, so the rule is quiet by construction (the
//     loadViaRoot fixture keeps it that way);
//   - the path (or a component, or the Join's root) is bound to an
//     EvalSymlinks result, or an EvalSymlinks ran on this path or on
//     its Dir — resolution on the chain (core/upload Save resolves
//     the storage root and the destination's parent before creating,
//     and its error-path os.Remove calls read as resolved too);
//   - a call to a VERIFIED guard on the read's own path, its Dir, or a
//     local bound to either (pathflow.SymlinkGuards: a body that runs
//     EvalSymlinks, an O_NOFOLLOW open, an Lstat consulted with
//     ModeSymlink, or another verified guard; cross-package guards
//     travel as a symlinkGuard fact). Until bug bash 2026-10-27 I13 any call whose NAME
//     contained "symlink" gated every read in the function, whatever
//     its body did and whatever path it got; fixture g and a.go's
//     guardNamedSymlink pin that;
//   - O_NOFOLLOW in a read-only OpenFile's flags, and an Lstat of the
//     Remove target consulted with ModeSymlink: the leaf-following
//     postures (directory components remain the caller's problem, but
//     these are the documented partial fixes this rule accepts);
//   - roots that are not parameters or root/base/dir-named fields
//     (constants name no caller-controlled boundary), literal-only
//     non-root components, and temp roots (os.MkdirTemp / t.TempDir);
//   - a same-package helper that resolves the RETURNED path's own
//     chain (an EvalSymlinks or a verified guard tied to what it
//     hands back, before the return) is the fix posture at the helper
//     hop — a resolution on any other value in its body gates
//     nothing; one called with literal-only non-root arguments
//     appends nothing caller-controlled;
//   - fs.FS values that are NOT caller-supplied here: concrete
//     embed.FS / fstest.MapFS values (an embed cannot hold symlinks),
//     and package-level fs variables whose construction this function
//     never sees;
//   - fs.FS reads in scopes with no HTTP parameter on the enclosing
//     chain — the declaration's or any function literal's (library
//     helpers, walkers, loaders) — and with literal-only names;
//   - write-flag OpenFile calls (rootwrite owns those);
//   - _test.go files.
package rootread

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "rootread",
	Doc:       "forbids reads under a root whose containment is lexical only: prefer os.OpenRoot (kernel-enforced containment), or resolve with filepath.EvalSymlinks, or an O_NOFOLLOW/Lstat leaf check",
	Run:       run,
	FactTypes: []analysis.Fact{new(symlinkGuard)},
}

// symlinkGuard marks a function whose body resolves or refuses
// symlinks (pathflow.ResolvesSymlinks), so a caller in another package
// is judged on the guard's body, not its name.
type symlinkGuard struct{}

func (*symlinkGuard) AFact()         {}
func (*symlinkGuard) String() string { return "symlinkGuard" }

const readMsg = "read under a root with lexical containment only: a symlinked directory component escapes the root undetected; resolve with filepath.EvalSymlinks, or an O_NOFOLLOW/Lstat leaf check, before reading"

func run(pass *analysis.Pass) (any, error) {
	pkgFuncs := pathflow.CollectFuncs(pass)
	isGuard := pathflow.SymlinkGuards(pass, func() analysis.Fact { return new(symlinkGuard) })
	helpers := readHelpers(pass, pkgFuncs, isGuard)
	for _, fn := range pathflow.FuncsWithBodies(pass) {
		checkFunc(pass, fn, pkgFuncs, isGuard, helpers)
	}
	return nil, nil
}

// osReadSink recognizes the direct os.* read sinks. os.Remove is the
// leaf sink the Lstat posture maps to. The OpenFile flags resolve
// through the body's bindings first: write bits (or O_NOFOLLOW) held
// in a local are the same open as inline ones (the 2026-11-03 gap:
// a write-flag local read as a plain read here, and vice versa in
// rootwrite).
func osReadSink(pass *analysis.Pass, call *ast.CallExpr, bound map[types.Object]ast.Expr) (ast.Expr, bool) {
	switch pathflow.QualifiedFunc(pass, call.Fun) {
	case "os.Open", "os.ReadFile", "os.ReadDir", "os.Stat":
		return pathflow.ArgAt(call, 0), false
	case "os.Remove":
		return pathflow.ArgAt(call, 0), true
	case "os.OpenFile":
		flags := pathflow.Resolve(pass, pathflow.ArgAt(call, 1), bound, 0)
		if !pathflow.HasWriteFlag(flags) && !pathflow.FlagMentions(flags, "O_NOFOLLOW") {
			return pathflow.ArgAt(call, 0), false
		}
	}
	return nil, false
}

// readSinksOf lists the path arguments call reads: a direct os.* sink,
// or a same-package read helper's sink parameters (the write twin's
// I4 hop, ported 2026-11).
func readSinksOf(pass *analysis.Pass, call *ast.CallExpr, bound map[types.Object]ast.Expr, pkgFuncs map[string][]*ast.FuncDecl, helpers map[*ast.FuncDecl]map[int]bool) []pathflow.SinkArg {
	if p, leaf := osReadSink(pass, call, bound); p != nil {
		return []pathflow.SinkArg{{Path: p, Leaf: leaf}}
	}
	return pathflow.HelperSinkArgs(pass, call, pkgFuncs, helpers)
}

// readHelpers summarizes which parameters of every same-package
// function reach an unresolved READ sink inside it (the fixpoint and
// its semantics live in pathflow.HelperSinkParams). Ported from
// rootwrite's write spelling (bug bash 2026-10-27 I4) in the 2026-11
// round: a root-joined path handed to readPage used to be invisible on
// the read side.
func readHelpers(pass *analysis.Pass, pkgFuncs map[string][]*ast.FuncDecl, isGuard pathflow.GuardFunc) map[*ast.FuncDecl]map[int]bool {
	return pathflow.HelperSinkParams(pass, pkgFuncs, readSinksOf, resolvedReadHere, isGuard)
}

// resolvedReadHere reports whether body resolves the sink's path
// before reading it: a verified guard on it, EvalSymlinks on its
// chain, or — for the Remove leaf — an Lstat+ModeSymlink check.
func resolvedReadHere(pass *analysis.Pass, body *ast.BlockStmt, s pathflow.SinkArg, bound map[types.Object]ast.Expr, evals []*ast.CallExpr, isGuard pathflow.GuardFunc, sink ast.Node) bool {
	return pathflow.GuardsPath(pass, body, s.Path, bound, isGuard, sink) ||
		pathflow.SymlinkResolved(pass, s.Path, bound, evals, body, sink) ||
		(s.Leaf && pathflow.LstatLeafCheck(pass, body, s.Path, bound, sink))
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl, pkgFuncs map[string][]*ast.FuncDecl, isGuard pathflow.GuardFunc, helpers map[*ast.FuncDecl]map[int]bool) {
	bound := pathflow.Bindings(pass, fn.Body)
	evals := pathflow.EvalSymlinkCalls(pass, fn.Body)
	parents := dominance.Parents(fn.Body)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Each call is judged in its own scope: the parameters and
		// HTTP-serving gate of every function literal enclosing it,
		// plus the declaration's own. A handler factory's closure
		// (func h(assets fs.FS) http.HandlerFunc) sees the factory's
		// fs and its own request alike.
		params, httpServing := scopeOf(pass, call, parents, fn)

		// String-path reads: os.* on a disk path built under a root,
		// direct or one same-package read helper deep. One report per
		// call.
		for _, s := range readSinksOf(pass, call, bound, pkgFuncs, helpers) {
			if s.Path == nil || resolvedReadHere(pass, fn.Body, s, bound, evals, isGuard, call) ||
				!pathflow.JoinsUnderRooty(pass, s.Path, bound, params, pkgFuncs, isGuard) {
				continue
			}
			if s.Helper != "" {
				pass.Reportf(call.Pos(), "%s (the read happens in %s)", readMsg, s.Helper)
			} else {
				pass.Reportf(call.Pos(), "%s", readMsg)
			}
			break
		}

		// fs.FS-mediated reads: the fs value is the root. Only in
		// HTTP-serving scopes, only on caller-supplied fs values,
		// only with a caller-controlled name.
		if recv, name := fsReadSink(pass, call); recv != nil && httpServing &&
			callerSuppliedFS(pass, recv, bound, params) &&
			!pathflow.GuardsPath(pass, fn.Body, name, bound, isGuard, call) &&
			pathflow.MentionsParam(pass, name, bound, params, 0) {
			pass.Reportf(call.Pos(), "%s", readMsg)
		}
		return true
	})
}

// scopeOf returns the caller-data parameters and the HTTP-serving
// verdict for a call inside fn: every function literal enclosing it,
// innermost out, plus the declaration itself — the 2026-11 closure
// fix; until then the fs.FS arm saw only the declaring function's own
// parameters, and a handler factory returning a closure was invisible.
func scopeOf(pass *analysis.Pass, call ast.Node, parents map[ast.Node]ast.Node, fn *ast.FuncDecl) (map[types.Object]bool, bool) {
	params := map[types.Object]bool{}
	serving := false
	for cur := ast.Node(call); cur != nil; {
		p, ok := parents[cur]
		if !ok || p == nil {
			break
		}
		if lit, ok := p.(*ast.FuncLit); ok && lit.Type != nil && lit.Type.Params != nil {
			serving = serving || fieldListServesHTTP(pass, lit.Type.Params)
			collectParams(pass, lit.Type.Params, params)
		}
		if p == fn.Body {
			break
		}
		cur = p
	}
	collectParams(pass, fn.Type.Params, params)
	if hasHTTPParam(pass, fn) {
		serving = true
	}
	return params, serving
}

// collectParams adds the parameter objects of one field list.
func collectParams(pass *analysis.Pass, list *ast.FieldList, out map[types.Object]bool) {
	if list == nil {
		return
	}
	for _, f := range list.List {
		for _, name := range f.Names {
			if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
				out[obj] = true
			}
		}
	}
}

// fieldListServesHTTP reports whether a parameter list carries a
// net/http.Request or ResponseWriter.
func fieldListServesHTTP(pass *analysis.Pass, list *ast.FieldList) bool {
	for _, f := range list.List {
		for _, name := range f.Names {
			obj, ok := pass.TypesInfo.ObjectOf(name).(*types.Var)
			if ok && isHTTPType(obj.Type()) {
				return true
			}
		}
	}
	return false
}

// fsReadSink recognizes the fs.FS read spellings — a method .Open on
// an io/fs.FS or http.Dir value, and the fs.ReadFile / fs.Stat /
// fs.ReadDir package forms — returning the fs value and the name
// argument.
func fsReadSink(pass *analysis.Pass, call *ast.CallExpr) (recv, name ast.Expr) {
	switch pathflow.QualifiedFunc(pass, call.Fun) {
	case "io/fs.ReadFile", "io/fs.Stat", "io/fs.ReadDir":
		if len(call.Args) >= 2 {
			return call.Args[0], call.Args[1]
		}
		return nil, nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Open" || len(call.Args) != 1 {
		return nil, nil
	}
	if isDiskFSType(pass, pass.TypesInfo.TypeOf(sel.X)) {
		return sel.X, call.Args[0]
	}
	return nil, nil
}

// isDiskFSType reports whether t is a filesystem value a caller can
// back with disk: the io/fs.FS interface itself, or net/http.Dir.
// Concrete types (embed.FS, fstest.MapFS) cannot hold symlinks and
// never count.
func isDiskFSType(pass *analysis.Pass, t types.Type) bool {
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
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	switch obj.Pkg().Path() + "." + obj.Name() {
	case "io/fs.FS", "net/http.Dir":
		return true
	}
	return false
}

// callerSuppliedFS reports whether the fs value expression comes from
// outside the function: a parameter, a struct field (a receiver's own
// fields included), or a local os.DirFS/http.Dir over a caller-named
// root — the disk-backed spellings. A package-level fs variable is
// construction this function never sees, and stays quiet.
func callerSuppliedFS(pass *analysis.Pass, recv ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) bool {
	switch x := recv.(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		if v, ok := obj.(*types.Var); ok && params[obj] && !v.IsField() {
			return true
		}
		b, ok := bound[obj]
		if !ok {
			return false
		}
		call, ok := b.(*ast.CallExpr)
		if !ok {
			return false
		}
		switch pathflow.QualifiedFunc(pass, call.Fun) {
		case "os.DirFS", "net/http.Dir":
			for _, a := range call.Args {
				if pathflow.MentionsParam(pass, a, bound, params, 0) {
					return true
				}
			}
		}
		return false
	case *ast.SelectorExpr:
		obj := pass.TypesInfo.ObjectOf(x.Sel)
		v, ok := obj.(*types.Var)
		return ok && v.IsField()
	default:
		return false
	}
}

// hasHTTPParam reports whether fn serves HTTP: a net/http.Request or
// net/http.ResponseWriter parameter. That is the trust boundary the
// probes crossed — a name an unauthenticated requester can type.
func hasHTTPParam(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	found := false
	if fn.Type.Params == nil {
		return false
	}
	for _, f := range fn.Type.Params.List {
		for _, name := range f.Names {
			obj, ok := pass.TypesInfo.ObjectOf(name).(*types.Var)
			if !ok {
				continue
			}
			if isHTTPType(obj.Type()) {
				found = true
			}
		}
	}
	return found
}

func isHTTPType(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == "net/http" && (obj.Name() == "Request" || obj.Name() == "ResponseWriter")
}

var _ = strings.Contains // strings retained for posture docs
