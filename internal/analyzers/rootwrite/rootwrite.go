// Package rootwrite catches writes whose containment under a root is
// resolved lexically only: os.WriteFile / os.Create / os.OpenFile(write
// flag) / os.MkdirAll / os.Remove on a path built under a root —
// filepath.Join, or a flat `root + "/" + x` concatenation — where the
// root is a caller-supplied parameter or field — with no
// filepath.EvalSymlinks on that path's chain — plus the archive twin:
// zip.Writer entry names assembled from a parameter with no path.Clean
// on the entry-name chain.
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
// The 2026-09-04 red-probe round proved two blind spots in
// battery/storage local.go (probe TestLocalStorageSymlinkEscapeRefused)
// and both are now in the shape:
//
//   - the Join lives in a same-package helper that RETURNS the path
//     (fullPath: Join(ls.BaseDir, key)) and the caller acts on the
//     result. A path bound to such a helper's result — the helper
//     joins a root/base/dir-named parameter or receiver field, and its
//     body resolves no symlinks — counts as root-derived at the
//     caller;
//   - the write is not a plain create: os.Rename / os.Link /
//     os.Symlink DESTINATION arguments (the rename-into-place a
//     storage Save does), os.Remove (a Delete unlinks through a
//     symlinked directory just as Save writes through one), and
//     os.MkdirAll(filepath.Dir(<root-derived path>)) — creating the
//     parent chain of an escaped path creates it outside the root.
//
// The same round's reviewer mutation removed both EvalSymlinks calls
// from core/upload Save and this rule stayed SILENT on every one of
// its sinks — the joined component had passed through sanitizeKey,
// whose RESULT replaced it, and a result-replacing sanitizer used to
// shield. That shield is gone (the 2026-09-04 posture change): a
// sanitizer strips "..", it cannot see a symlinked directory, which is
// exactly how the probes escaped on the read side — rootread never
// carried the shield. Only RESOLUTION postures gate now, and os.Root
// is the strongest of them.
//
// Every gate is per write and on the write's own dataflow: resolution
// or cleaning on an unrelated path (or consulted for a boolean and
// leaving the path components untouched) gates nothing.
//
// Silent postures, deliberately — os.Root first, as the fix posture
// of first resort on Go 1.27:
//   - writes made through an *os.Root method (os.OpenRoot(root) +
//     root.Create / OpenFile / WriteFile / Mkdir / MkdirAll / Remove /
//     RemoveAll / Rename / Link / Symlink): containment is enforced by
//     the kernel — a symlink under the root cannot lead the write out,
//     and there is no TOCTOU window between check and use. Root
//     methods are no os.* sink, so the rule is quiet by construction
//     (the stashViaRoot fixture keeps it that way);
//   - the write's path (or a component of it, or the Join's root) is
//     bound to a filepath.EvalSymlinks result, or an EvalSymlinks ran
//     on this path expression or on its Dir — resolution on the chain
//     (the fix posture; core/upload Save resolves the storage root and
//     the destination's parent directory before creating anything);
//   - calls to symlink-named guards (EnsureNoSymlinkPath): resolution
//     by another name;
//   - O_NOFOLLOW in a write-open's flags, and an Lstat of the sink's
//     own target consulted with ModeSymlink: the leaf postures —
//     documented partial fixes; the directory components above the
//     leaf remain the writer's problem;
//   - a sanitizer or validator in ANY spelling — result-replacing or
//     boolean: neither can see a symlinked directory, so neither
//     gates. Until the 2026-09-04 posture change a result-replacing
//     one kept the write quiet (fixture d's sanitizerResultStillFires
//     and cleanHelperStillFires are positives now, exactly the
//     silence the mutation proof broke);
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
//   - reads (os.Open, os.ReadFile, O_RDONLY) by construction —
//     rootread owns that side;
//   - _test.go files.
package rootwrite

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name: "rootwrite",
	Doc:  "forbids writes under a root whose containment is lexical only: prefer os.OpenRoot (kernel-enforced containment), or resolve with filepath.EvalSymlinks; and path.Clean zip entry names",
	Run:  run,
}

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
	// Resolution gates are per WRITE, not per function: an
	// EvalSymlinks (or leaf Lstat, or Clean) on an unrelated path
	// resolves nothing for this one.
	evals := pathflow.EvalSymlinkCalls(pass, fn.Body)
	symlinkGuard := pathflow.CallsSymlinkNamed(pass, fn.Body)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Filesystem writes under a lexically-contained root. The
		// destination of Rename/Link/Symlink is the write; the source
		// of a Rename may live anywhere. leaf marks the sinks that
		// clobber a path an Lstat can see; MkdirAll creates its leaf,
		// so the leaf postures do not map to it.
		var pathArg ast.Expr
		leaf := false
		switch pathflow.QualifiedFunc(pass, call.Fun) {
		case "os.WriteFile", "os.Create", "os.Remove":
			pathArg = pathflow.ArgAt(call, 0)
			leaf = true
		case "os.MkdirAll":
			pathArg = pathflow.ArgAt(call, 0)
		case "os.Rename", "os.Link", "os.Symlink":
			pathArg = pathflow.ArgAt(call, 1)
			leaf = true
		case "os.OpenFile":
			flags := pathflow.ArgAt(call, 1)
			if pathflow.HasWriteFlag(flags) && !pathflow.FlagMentions(flags, "O_NOFOLLOW") {
				pathArg = pathflow.ArgAt(call, 0)
				leaf = true
			}
		}
		if pathArg != nil && !symlinkGuard &&
			!pathflow.SymlinkResolved(pass, pathArg, bound, evals) &&
			!(leaf && pathflow.LstatLeafCheck(pass, fn.Body, pathArg, bound)) &&
			pathflow.JoinsUnderRooty(pass, pathArg, bound, params, pkgFuncs) {
			pass.Reportf(call.Pos(),
				"write under a root with lexical containment only: a symlinked directory component escapes the root undetected; resolve with filepath.EvalSymlinks before writing")
		}
		// zip.Writer entries named from a parameter without a Clean on
		// the entry-name chain.
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if ok && (sel.Sel.Name == "Create" || sel.Sel.Name == "CreateHeader") && isZipWriter(pass, sel.X) {
			var name ast.Expr
			if sel.Sel.Name == "Create" {
				name = pathflow.ArgAt(call, 0)
			} else {
				name = headerName(pass, pathflow.ArgAt(call, 0), bound)
			}
			if name != nil && !cleanedEntryName(pass, name, bound) && composedFromParam(pass, name, bound, params) {
				pass.Reportf(call.Pos(),
					"zip entry name built from a parameter without path.Clean: an uncleaned name can place entries outside the target directory on extract; path.Clean and reject \"..\" segments")
			}
		}
		return true
	})
}

// cleanedEntryName reports whether the zip entry name's assembly is
// bound to a path.Clean / filepath.Clean result. A Clean on an
// unrelated path cleans no entry name.
func cleanedEntryName(pass *analysis.Pass, name ast.Expr, bound map[types.Object]ast.Expr) bool {
	isClean := func(c *ast.CallExpr) bool {
		q := pathflow.QualifiedFunc(pass, c.Fun)
		return q == "path.Clean" || q == "path/filepath.Clean"
	}
	return pathflow.MentionsCall(pass, name, bound, isClean, 0)
}

// composedFromParam reports whether the zip entry name is ASSEMBLED
// from a function parameter — concatenated or formatted — rather than
// passed through verbatim. A wrapper that forwards its own name
// parameter to Create is composing nothing; its callers are.
func composedFromParam(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) bool {
	e = pathflow.Resolve(pass, e, bound, 0)
	switch x := e.(type) {
	case *ast.BinaryExpr:
		return pathflow.MentionsParam(pass, x, bound, params, 0)
	case *ast.CallExpr:
		return pathflow.MentionsParam(pass, x, bound, params, 0)
	default:
		return false
	}
}

// headerName resolves a CreateHeader argument — an &zip.FileHeader
// composite, possibly through a local — to its Name field expression.
func headerName(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr) ast.Expr {
	e = pathflow.Resolve(pass, e, bound, 0)
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

var _ = strings.HasSuffix // retained import: doc posture strings
