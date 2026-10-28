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
// Bug bash 2026-10-27 I4 widened the sink two ways. A same-package
// helper whose parameter (or its filepath.Dir) reaches an unresolved
// write sink is itself a sink at its call sites: the static export's
// writeFile(dst, data) — MkdirAll(Dir(dst)) + os.WriteFile(dst) — took
// framework/static/builder.go's Join(b.OutDir, pathToFile(p)) out of
// sight one call deep, and the report names the helper. And the
// parameters of a function literal count as caller data for the writes
// in its body, the same as the declaration's own: the walk callback's
// route path p is what lands under OutDir. A helper that resolves the
// path itself is the fix posture and makes no sink parameter; a leaf
// Lstat check at the caller covers a helper only when every write that
// helper makes is a leaf write (writeFile's MkdirAll is not).
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
//   - a call to a VERIFIED guard on the write's own path, its Dir, or
//     a local bound to either: a function whose body runs EvalSymlinks,
//     an O_NOFOLLOW open, an Lstat consulted with ModeSymlink, or calls
//     another verified guard. Guards in other packages are judged by
//     an exported symlinkGuard fact. A function that resolves its
//     argument and then writes it (ApplyFixed) counts too: it did check
//     that path.
//     Guards in other packages are judged by an exported symlinkGuard
//     fact. Until bug bash 2026-10-27 I13 any call whose NAME contained
//     "symlink" gated every write in the function, whatever its body did
//     and whatever path it was handed; fixture g pins the name-only
//     guard, the guard on another path, and the root-only guard;
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
//     body resolves symlinks is the fix posture. A bool or numeric
//     parameter is no caller-controlled component either: it carries
//     no path text (phaseName(maintenance), a run counter);
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

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "rootwrite",
	Doc:       "forbids writes under a root whose containment is lexical only: prefer os.OpenRoot (kernel-enforced containment), or resolve with filepath.EvalSymlinks; and path.Clean zip entry names",
	Run:       run,
	FactTypes: []analysis.Fact{new(symlinkGuard)},
}

// symlinkGuard marks a function whose body resolves or refuses
// symlinks (pathflow.ResolvesSymlinks), so a caller in another package
// is judged on the guard's body, not its name.
type symlinkGuard struct{}

func (*symlinkGuard) AFact()         {}
func (*symlinkGuard) String() string { return "symlinkGuard" }

const writeMsg = "write under a root with lexical containment only: a symlinked directory component escapes the root undetected; resolve with filepath.EvalSymlinks before writing"

func run(pass *analysis.Pass) (any, error) {
	pkgFuncs := pathflow.CollectFuncs(pass)
	isGuard := pathflow.SymlinkGuards(pass, func() analysis.Fact { return new(symlinkGuard) })
	helpers := writeHelpers(pass, pkgFuncs, isGuard)
	for _, fn := range pathflow.FuncsWithBodies(pass) {
		checkFunc(pass, fn, pkgFuncs, isGuard, helpers)
	}
	return nil, nil
}

// sinkArg is one path argument a call writes: its expression, whether
// the write only clobbers the leaf (so the Lstat leaf posture maps to
// it), and the helper the write happens in ("" for a direct os.* call).
type sinkArg struct {
	path   ast.Expr
	leaf   bool
	helper string
}

// osSink recognizes the direct os.* write sinks. The destination of
// Rename/Link/Symlink is the write; the source of a Rename may live
// anywhere. leaf marks the sinks that clobber a path an Lstat can see;
// MkdirAll creates its leaf, so the leaf postures do not map to it.
func osSink(pass *analysis.Pass, call *ast.CallExpr) (ast.Expr, bool) {
	switch pathflow.QualifiedFunc(pass, call.Fun) {
	case "os.WriteFile", "os.Create", "os.Remove":
		return pathflow.ArgAt(call, 0), true
	case "os.MkdirAll":
		return pathflow.ArgAt(call, 0), false
	case "os.Rename", "os.Link", "os.Symlink":
		return pathflow.ArgAt(call, 1), true
	case "os.OpenFile":
		flags := pathflow.ArgAt(call, 1)
		if pathflow.HasWriteFlag(flags) && !pathflow.FlagMentions(flags, "O_NOFOLLOW") {
			return pathflow.ArgAt(call, 0), true
		}
	}
	return nil, false
}

// sinksOf lists the path arguments call writes: a direct os.* sink, or
// a same-package write helper's sink parameters (I4).
func sinksOf(pass *analysis.Pass, call *ast.CallExpr, pkgFuncs map[string][]*ast.FuncDecl, helpers map[*ast.FuncDecl]map[int]bool) []sinkArg {
	if p, leaf := osSink(pass, call); p != nil {
		return []sinkArg{{path: p, leaf: leaf}}
	}
	decl := pathflow.CalleeDecl(pass, call.Fun, pkgFuncs)
	if decl == nil || len(helpers[decl]) == 0 {
		return nil
	}
	var out []sinkArg
	for i := 0; i < len(call.Args); i++ {
		if leaf, ok := helpers[decl][i]; ok {
			out = append(out, sinkArg{path: call.Args[i], leaf: leaf, helper: decl.Name.Name})
		}
	}
	return out
}

// writeHelpers summarizes, for every same-package function, which
// parameters reach a write sink inside it unresolved — the bare
// parameter, or its filepath.Dir, as a sink's path (writeFile(dst,
// data): MkdirAll(Dir(dst)) + os.WriteFile(dst)), directly or through
// another such helper. The bool says every sink the parameter reaches
// is a leaf sink. A parameter the helper itself resolves (EvalSymlinks
// on its chain, a verified guard on it, a leaf check before a leaf
// sink) is not a sink parameter: the helper is the fix posture.
//
// Bug bash 2026-10-27 I4: framework/static/builder.go:180-184 joins
// b.OutDir with a route path and writes it through writeFile
// (:1782-1786); pathflow stopped at writeFile's plain parameter, so
// the sink one helper deep was invisible.
func writeHelpers(pass *analysis.Pass, pkgFuncs map[string][]*ast.FuncDecl, isGuard pathflow.GuardFunc) map[*ast.FuncDecl]map[int]bool {
	helpers := map[*ast.FuncDecl]map[int]bool{}
	decls := pathflow.FuncsWithBodies(pass)
	for changed := true; changed; {
		changed = false
		for _, decl := range decls {
			idx := paramIndexes(pass, decl)
			if len(idx) == 0 {
				continue
			}
			bound := pathflow.Bindings(pass, decl.Body)
			evals := pathflow.EvalSymlinkCalls(pass, decl.Body)
			ast.Inspect(decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, s := range sinksOf(pass, call, pkgFuncs, helpers) {
					i, ok := sinkParam(pass, s.path, bound, idx)
					if !ok || resolvedHere(pass, decl.Body, s, bound, evals, isGuard) {
						continue
					}
					m := helpers[decl]
					if m == nil {
						m = map[int]bool{}
						helpers[decl] = m
					}
					prev, seen := m[i]
					next := s.leaf && (!seen || prev)
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

// sinkParam reports which parameter a helper's sink path IS: the bare
// parameter (through locals) or its filepath.Dir.
func sinkParam(pass *analysis.Pass, p ast.Expr, bound map[types.Object]ast.Expr, idx map[types.Object]int) (int, bool) {
	r := pathflow.Resolve(pass, p, bound, 0)
	if call, ok := r.(*ast.CallExpr); ok && pathflow.QualifiedFunc(pass, call.Fun) == "path/filepath.Dir" && len(call.Args) == 1 {
		r = pathflow.Resolve(pass, call.Args[0], bound, 0)
	}
	id, ok := r.(*ast.Ident)
	if !ok {
		return 0, false
	}
	i, ok := idx[pass.TypesInfo.ObjectOf(id)]
	return i, ok
}

// paramIndexes maps decl's parameter objects to their positions.
func paramIndexes(pass *analysis.Pass, decl *ast.FuncDecl) map[types.Object]int {
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

// resolvedHere reports whether body resolves the sink's path before
// writing it: EvalSymlinks on its chain, a verified guard on it, or —
// for a leaf sink — an Lstat+ModeSymlink check of it.
func resolvedHere(pass *analysis.Pass, body *ast.BlockStmt, s sinkArg, bound map[types.Object]ast.Expr, evals []*ast.CallExpr, isGuard pathflow.GuardFunc) bool {
	return pathflow.GuardsPath(pass, body, s.path, bound, isGuard) ||
		pathflow.SymlinkResolved(pass, s.path, bound, evals) ||
		(s.leaf && pathflow.LstatLeafCheck(pass, body, s.path, bound))
}

// callerParams is fn's parameters plus those of every function literal
// inside it: a callback's parameters (the static export's walk
// callback receives the route path) are caller data for the writes in
// its body, the same as the declaration's own.
func callerParams(pass *analysis.Pass, fn *ast.FuncDecl) map[types.Object]bool {
	params := pathflow.ParamObjects(pass, fn)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok || lit.Type.Params == nil {
			return true
		}
		for _, f := range lit.Type.Params.List {
			for _, name := range f.Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
					params[obj] = true
				}
			}
		}
		return true
	})
	return params
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl, pkgFuncs map[string][]*ast.FuncDecl, isGuard pathflow.GuardFunc, helpers map[*ast.FuncDecl]map[int]bool) {
	bound := pathflow.Bindings(pass, fn.Body)
	params := callerParams(pass, fn)
	declParams := pathflow.ParamObjects(pass, fn) // the zip arm keeps its own scope
	// Resolution gates are per WRITE, not per function: an
	// EvalSymlinks (or leaf Lstat, or guard) on an unrelated path
	// resolves nothing for this one.
	evals := pathflow.EvalSymlinkCalls(pass, fn.Body)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// Filesystem writes under a lexically-contained root, direct or
		// one or more same-package helpers deep. One report per call.
		for _, s := range sinksOf(pass, call, pkgFuncs, helpers) {
			if s.path == nil || resolvedHere(pass, fn.Body, s, bound, evals, isGuard) ||
				!pathflow.JoinsUnderRooty(pass, s.path, bound, params, pkgFuncs, isGuard) {
				continue
			}
			if s.helper != "" {
				pass.Reportf(call.Pos(), "%s (the write happens in %s)", writeMsg, s.helper)
			} else {
				pass.Reportf(call.Pos(), "%s", writeMsg)
			}
			break
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
			if name != nil && !cleanedEntryName(pass, name, bound) && composedFromParam(pass, name, bound, declParams) {
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
