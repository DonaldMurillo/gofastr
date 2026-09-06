// Package rootread catches reads whose containment under a root is
// resolved lexically only — the read twin of rootwrite. os.Open /
// os.ReadFile / os.Stat / os.Remove / read-only os.OpenFile on a path
// built under a root (filepath.Join or flat concatenation) with no
// filepath.EvalSymlinks on the path's chain and no O_NOFOLLOW or
// Lstat+ModeSymlink leaf check — plus the fs.FS-mediated spelling:
// `.Open(name)` / `fs.ReadFile(fsys, name)` / `fs.Stat(fsys, name)`
// where the fs value is a CALLER-SUPPLIED io/fs.FS (parameter or
// field), the name is caller-controlled, and the function serves HTTP.
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
//   - calls to symlink-named guards (EnsureNoSymlinkPath): resolution
//     by another name;
//   - O_NOFOLLOW in a read-only OpenFile's flags, and an Lstat of the
//     Remove target consulted with ModeSymlink: the leaf-following
//     postures (directory components remain the caller's problem, but
//     these are the documented partial fixes this rule accepts);
//   - roots that are not parameters or root/base/dir-named fields
//     (constants name no caller-controlled boundary), literal-only
//     non-root components, and temp roots (os.MkdirTemp / t.TempDir);
//   - a same-package helper whose body resolves symlinks is the fix
//     posture at the helper hop; one called with literal-only
//     non-root arguments appends nothing caller-controlled;
//   - fs.FS values that are NOT caller-supplied here: concrete
//     embed.FS / fstest.MapFS values (an embed cannot hold symlinks),
//     and package-level fs variables whose construction this function
//     never sees;
//   - fs.FS reads in functions with no HTTP parameter (library
//     helpers, walkers, loaders) and with literal-only names;
//   - write-flag OpenFile calls (rootwrite owns those);
//   - _test.go files.
package rootread

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name: "rootread",
	Doc:  "forbids reads under a root whose containment is lexical only: prefer os.OpenRoot (kernel-enforced containment), or resolve with filepath.EvalSymlinks, or an O_NOFOLLOW/Lstat leaf check",
	Run:  run,
}

const readMsg = "read under a root with lexical containment only: a symlinked directory component escapes the root undetected; resolve with filepath.EvalSymlinks, or an O_NOFOLLOW/Lstat leaf check, before reading"

func run(pass *analysis.Pass) (any, error) {
	pkgFuncs := pathflow.CollectFuncs(pass)
	for _, fn := range pathflow.FuncsWithBodies(pass) {
		checkFunc(pass, fn, pkgFuncs)
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl, pkgFuncs map[string][]*ast.FuncDecl) {
	bound := pathflow.Bindings(pass, fn.Body)
	params := pathflow.ParamObjects(pass, fn)
	evals := pathflow.EvalSymlinkCalls(pass, fn.Body)
	symlinkGuard := pathflow.CallsSymlinkNamed(pass, fn.Body)
	httpServing := hasHTTPParam(pass, fn)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// String-path reads: os.* on a disk path built under a root.
		var pathArg ast.Expr
		switch pathflow.QualifiedFunc(pass, call.Fun) {
		case "os.Open", "os.ReadFile", "os.Stat":
			pathArg = pathflow.ArgAt(call, 0)
		case "os.Remove":
			pathArg = pathflow.ArgAt(call, 0)
			if pathArg != nil && pathflow.LstatLeafCheck(pass, fn.Body, pathArg, bound) {
				pathArg = nil
			}
		case "os.OpenFile":
			flags := pathflow.ArgAt(call, 1)
			if !pathflow.HasWriteFlag(flags) && !pathflow.FlagMentions(flags, "O_NOFOLLOW") {
				pathArg = pathflow.ArgAt(call, 0)
			}
		}
		if pathArg != nil && !symlinkGuard &&
			!pathflow.SymlinkResolved(pass, pathArg, bound, evals) &&
			pathflow.JoinsUnderRooty(pass, pathArg, bound, params, pkgFuncs) {
			pass.Reportf(call.Pos(), "%s", readMsg)
		}

		// fs.FS-mediated reads: the fs value is the root. Only in
		// HTTP-serving functions, only on caller-supplied fs values,
		// only with a caller-controlled name.
		if recv, name := fsReadSink(pass, call); recv != nil && httpServing &&
			callerSuppliedFS(pass, recv, bound, params) &&
			!symlinkGuard &&
			pathflow.MentionsParam(pass, name, bound, params, 0) {
			pass.Reportf(call.Pos(), "%s", readMsg)
		}
		return true
	})
}

// fsReadSink recognizes the fs.FS read spellings — a method .Open on
// an io/fs.FS or http.Dir value, and the fs.ReadFile / fs.Stat
// package forms — returning the fs value and the name argument.
func fsReadSink(pass *analysis.Pass, call *ast.CallExpr) (recv, name ast.Expr) {
	if q := pathflow.QualifiedFunc(pass, call.Fun); q == "io/fs.ReadFile" || q == "io/fs.Stat" {
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
