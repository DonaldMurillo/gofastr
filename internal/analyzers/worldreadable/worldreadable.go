// Package worldreadable catches files and directories written for state
// or secrets with group/other permission bits: os.WriteFile /
// os.Create / os.OpenFile(write flags) / os.Mkdir / os.MkdirAll whose
// mode is a CONSTANT literal carrying group or other bits (0644, 0755,
// 0666, 0777; os.Create is umask-default 0666), while this repo's own
// discipline for that artifact class is owner-only (0600/0700 plus
// fileperm.Restrict, pinned by battery/log, upload storage, the
// session sqlite store and DEK, freeze's world.json, and the credstore).
//
// The bug class: 2026-09-03 adversarial pass round 4 (tests-only red
// probes, fixes still open) — TestExportDataRedRestrictiveModes
// (framework/export_data.go: a raw dump of every physical column,
// password hashes included, landed 0644 inside a 0755 dir),
// TestHarnessExportRedRestrictsZip (session export.go os.Create),
// TestCostLedgerRedRestrictiveDir (retention.go MkdirAll 0o755),
// TestIsolationRedRestrictiveState (isolation.go MkdirAll 0o755),
// TestJournalRedRestrictsSecretFile (journal.go OpenFile 0o644 three
// times: the journal embeds Auth.JWTSecret and Admin.SeedPassword
// verbatim while freeze writes the same data 0600 + fileperm.Restrict).
//
// The default is FIRE: a constant group/world mode on a write whose
// path does not prove itself public is the outlier posture here. The
// postures below are the carve-outs, and every one is judged from the
// write's own path expression (provenance and name), never by guessing
// at content.
//
// Silent postures, deliberately:
//   - mode expressions that are not constant literals (a variable, a
//     parameter, a struct field like battery/log's FileMode or the
//     codegen file.Mode branch): the caller owns the policy;
//   - a FILE whose constant mode carries execute bits (0755 scripts,
//     shims, binaries): the group/other bits are the deployment
//     posture of an executable, not a read leak;
//   - paths under a throwaway root: a local bound to os.MkdirTemp /
//     t.TempDir in the same function;
//   - a write to a path this function already read or opened
//     (os.ReadFile / os.Open on the same expression), or that a
//     same-package caller read before passing it here (cmd/mutate's
//     restore of pre-mutation source): the perm argument of these
//     calls applies only at creation, so a write to a known-existing
//     file leaves its mode alone;
//   - PUBLIC artifacts, judged by the path argument's provenance and
//     name, exactly one of:
//     (a) the final name component visible in the path assembly — a
//     literal Join/concat operand, a Sprintf format tail, or a
//     package-const resolved to its value — ends in a generated-source
//     or static-asset extension (.go .md .yml .yaml .sql .html .css
//     .js .ts .tsx .svg .png .jpg .jpeg .ico .webmanifest .mod .sum
//     .lock, and the committed VCS dotfiles .gitignore .gitkeep
//     .gitattributes .gitmodules .editorconfig): generated docs,
//     blueprints, migrations, assets, scaffold files;
//     (b) the path's provenance (the Join root, the whole expression,
//     or a bare parameter resolved through same-package callers) names
//     an output/build root: output, outdir, dist, dst, dest, build,
//     static, assets, public, migration, sarif — the dist/ build
//     output, the committed migrations/ dir, a CI-consumed SARIF
//     report, codegen's OutputRoot, the static builder's OutDir/dst;
//     (c) for DIRECTORIES (Mkdir/MkdirAll): every write reachable
//     into that dir — same function, an os.CreateTemp minted into it
//     (the atomic-write pattern, whose mode is set explicitly on the
//     handle), or one same-package helper hop called with that dir —
//     is itself public or owner-only (freeze's blueprint dir holds
//     gofastr.yml 0644 + world.json 0600; the migrations dir holds
//     .sql files; kiln's skill dir holds SKILL.md). A dir with no
//     reachable writer stays loud: it is created group-traversable and
//     whatever lands in it decides the exposure (isolation.go, the
//     cost ledger dir);
//   - MkdirAll(filepath.Dir(x)) in service of a write whose MODE this
//     function takes from its caller (the harness Write builtin): the
//     directory serves a caller-owned policy;
//   - writers named *Baseline*: the contracts baseline is a reviewed,
//     committed record by its own documentation ("it lives in the
//     repository... belongs in the diff");
//   - unexported writers with no non-test caller in their package
//     (cmd/gofastr's mkdirAll/writeFile test scaffolding): a helper
//     that only tests reach carries no production policy;
//   - _test.go files.
package worldreadable

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "worldreadable",
	Doc:  "forbids state/secret artifacts written with group/other permission bits: this repo's discipline is 0600/0700 + fileperm.Restrict, so a constant 0644/0755 mode (or os.Create) must name why the artifact is public",
	Run:  run,
}

// site is one sink call: a file or directory creation whose mode (or
// os.Create's implicit 0666) may carry group/other bits.
type site struct {
	call  *ast.CallExpr
	path  ast.Expr
	mode  *ast.BasicLit // nil for os.Create
	perm  uint64        // effective constant perm; os.Create reports 0o666
	isDir bool
}

// publicExts are the generated-source / static-asset extensions: files
// whose destination is a commit or a build output, not app state. The
// VCS dotfiles (.gitignore, .gitkeep, ...) are committed project
// scaffolding.
var publicExts = map[string]bool{
	".go": true, ".md": true, ".yml": true, ".yaml": true, ".sql": true,
	".html": true, ".css": true, ".js": true, ".ts": true, ".tsx": true,
	".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".ico": true,
	".webmanifest": true, ".mod": true, ".sum": true, ".lock": true,
	".gitignore": true, ".gitkeep": true, ".gitattributes": true,
	".gitmodules": true, ".editorconfig": true,
}

// outMarkers name the output/build-root family on identifiers in a
// path's provenance (codegen OutputRoot, static OutDir, copy dst, the
// committed migrations/ dir, a CI-consumed SARIF report path).
var outMarkers = []string{
	"output", "outdir", "dist", "dst", "dest", "build", "static",
	"assets", "public", "migration", "sarif",
}

// pkgCtx is the package-level context: package-level funcs, the
// literal values of package constants (name evidence), and the callee
// names that non-test code actually calls (test-scaffolding posture).
type pkgCtx struct {
	funcs       map[string]*ast.FuncDecl
	constValues map[string]string
	calledNames map[string]bool
}

func run(pass *analysis.Pass) (any, error) {
	ctx := &pkgCtx{
		funcs:       map[string]*ast.FuncDecl{},
		constValues: map[string]string{},
		calledNames: map[string]bool{},
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Body != nil {
					ctx.funcs[decl.Name.Name] = decl
				}
			case *ast.GenDecl:
				if decl.Tok != token.CONST {
					continue
				}
				for _, spec := range decl.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								ctx.constValues[name.Name] = unquote(lit.Value)
							}
						}
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				switch fun := call.Fun.(type) {
				case *ast.Ident:
					ctx.calledNames[fun.Name] = true
				case *ast.SelectorExpr:
					ctx.calledNames[fun.Sel.Name] = true
				}
			}
			return true
		})
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
			checkFunc(pass, fn, ctx)
		}
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl, ctx *pkgCtx) {
	bound := bindings(pass, fn.Body)
	sites := collectSites(pass, fn.Body)
	readPaths := readFirstPaths(pass, fn.Body, bound)

	for _, s := range sites {
		if s.perm&0o077 == 0 {
			continue // owner-only or narrower: the discipline itself
		}
		if !s.isDir && s.perm&0o111 != 0 {
			// A FILE whose constant mode carries execute bits is an
			// executable (scripts, shims, binaries): the group/other
			// bits are the deployment posture, not a read leak.
			continue
		}
		if silent(pass, s, fn, bound, readPaths, ctx, sites, 0) {
			continue
		}
		kind := "file"
		if s.isDir {
			kind = "directory"
		}
		mode := "0o666 (os.Create, umask default)"
		if s.mode != nil {
			mode = "0o" + strconv.FormatUint(s.perm, 8)
		}
		pass.Reportf(s.call.Pos(),
			"%s created with mode %s under %s: state and secret artifacts are owner-only in this repo (0600/0700, fileperm.Restrict); pass 0o600/0o700 or name why this artifact is public",
			kind, mode, types.ExprString(s.path))
	}
}

// silent reports whether site s is one of the deliberate postures.
// depth guards the directory-content recursion (one level only).
func silent(pass *analysis.Pass, s site, fn *ast.FuncDecl, bound map[types.Object]ast.Expr, readPaths map[string]bool, ctx *pkgCtx, all []site, depth int) bool {
	// Test scaffolding: an unexported writer no non-test code calls.
	if fn.Recv == nil && !ast.IsExported(fn.Name.Name) && !ctx.calledNames[fn.Name.Name] {
		return true
	}
	// The committed-record writer (contracts baseline).
	if strings.Contains(strings.ToLower(fn.Name.Name), "baseline") {
		return true
	}
	// Throwaway roots.
	if underTempRoot(pass, s.path, bound) {
		return true
	}
	// Mode applies only at creation: a path this function already read
	// or opened exists, so the write cannot loosen anything. The same
	// holds one hop up: a helper rewriting bytes its caller read from
	// that path (cmd/mutate's restore).
	if readPaths[exprString(resolve(pass, s.path, bound, 0))] ||
		callerReadsPath(pass, s, fn, bound, ctx) {
		return true
	}
	// Public by name: generated-source / static-asset extension.
	if publicNameEvidence(pass, s.path, bound, ctx) {
		return true
	}
	// Public by provenance: an output/build-root name.
	if outputRooted(pass, s.path, bound, fn, ctx, 0) {
		return true
	}
	// The parent of a write whose MODE the caller owns (the harness
	// Write builtin): the directory serves a caller-owned policy.
	if s.isDir && dirOfCallerOwnedWrite(pass, s, fn, bound) {
		return true
	}
	// Directories: public when every reachable write into them is
	// itself public or owner-only.
	if s.isDir && depth == 0 && dirHoldsOnlyPublic(pass, s, fn, bound, readPaths, ctx, all) {
		return true
	}
	return false
}

// collectSites gathers the write sinks in body with their constant
// modes. Non-constant modes are dropped here: the caller owns the
// policy.
func collectSites(pass *analysis.Pass, body *ast.BlockStmt) []site {
	var out []site
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch qualifiedFunc(pass, call.Fun) {
		case "os.WriteFile":
			if mode, perm, ok := constPerm(argAt(call, 2)); ok {
				out = append(out, site{call: call, path: argAt(call, 0), mode: mode, perm: perm})
			}
		case "os.OpenFile":
			if hasWriteFlag(argAt(call, 1)) {
				if mode, perm, ok := constPerm(argAt(call, 2)); ok {
					out = append(out, site{call: call, path: argAt(call, 0), mode: mode, perm: perm})
				}
			}
		case "os.Create":
			out = append(out, site{call: call, path: argAt(call, 0), perm: 0o666})
		case "os.Mkdir", "os.MkdirAll":
			if mode, perm, ok := constPerm(argAt(call, 1)); ok {
				out = append(out, site{call: call, path: argAt(call, 0), mode: mode, perm: perm, isDir: true})
			}
		}
		return true
	})
	return out
}

// readFirstPaths collects the resolved path expressions the same
// function reads or opens before writing them.
func readFirstPaths(pass *analysis.Pass, body *ast.BlockStmt, bound map[types.Object]ast.Expr) map[string]bool {
	reads := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch qualifiedFunc(pass, call.Fun) {
		case "os.ReadFile", "os.Open":
			if p := argAt(call, 0); p != nil {
				reads[exprString(resolve(pass, p, bound, 0))] = true
			}
		}
		return true
	})
	return reads
}

// underTempRoot reports whether the path (or the root it is built
// under) is a local bound to os.MkdirTemp / t.TempDir: throwaway by
// construction.
func underTempRoot(pass *analysis.Pass, path ast.Expr, bound map[types.Object]ast.Expr) bool {
	r := resolve(pass, path, bound, 0)
	if call, ok := r.(*ast.CallExpr); ok && isTempMint(pass, call) {
		return true
	}
	// The Join/concat root: filepath.Join(tmp, x), tmp + "/" + x.
	root := assemblyRoot(r)
	if root == nil {
		return false
	}
	if call, ok := root.(*ast.CallExpr); ok && isTempMint(pass, call) {
		return true // t.TempDir() inline in the Join
	}
	if id, ok := root.(*ast.Ident); ok {
		if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok {
			if call, ok := b.(*ast.CallExpr); ok && isTempMint(pass, call) {
				return true
			}
		}
	}
	return false
}

// isTempMint matches the throwaway-mint calls (os.TempDir itself is
// NOT one: it is the shared root, not a private one).
func isTempMint(pass *analysis.Pass, call *ast.CallExpr) bool {
	q := qualifiedFunc(pass, call.Fun)
	if q == "os.MkdirTemp" || q == "os.CreateTemp" {
		return true
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "TempDir" {
		return true // t.TempDir()
	}
	return false
}

// assemblyRoot returns the root operand of a built path: the first
// Join argument or the leftmost operand of a flat ADD chain.
func assemblyRoot(e ast.Expr) ast.Expr {
	switch x := e.(type) {
	case *ast.CallExpr:
		if len(x.Args) > 0 {
			return x.Args[0]
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			ops := concatOperands(x, nil)
			if len(ops) > 0 {
				return ops[0]
			}
		}
	}
	return nil
}

// publicNameEvidence reports whether the path's final name component
// (through the local's whole binding history: a flag-parsed default
// may carry the evidence the last reassignment hides) ends in a public
// extension.
func publicNameEvidence(pass *analysis.Pass, path ast.Expr, bound map[types.Object]ast.Expr, ctx *pkgCtx) bool {
	candidates := []ast.Expr{resolve(pass, path, bound, 0)}
	if id, ok := path.(*ast.Ident); ok {
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			for _, b := range bindingHistory(pass, obj, id.Pos()) {
				candidates = append(candidates, resolve(pass, b, bound, 0))
			}
		}
	}
	for _, cand := range candidates {
		if name, ok := finalNameEvidence(pass, cand, bound, ctx); ok && publicExt(name) {
			return true
		}
	}
	return false
}

// finalNameEvidence extracts the visible final-name evidence of a path
// expression: a literal Join/concat operand, a Sprintf format tail, or
// a package-const resolved to its literal value. Missing evidence
// returns ok=false.
func finalNameEvidence(pass *analysis.Pass, path ast.Expr, bound map[types.Object]ast.Expr, ctx *pkgCtx) (string, bool) {
	r := resolve(pass, path, bound, 0)
	var last ast.Expr
	switch x := r.(type) {
	case *ast.CallExpr:
		if qualifiedFunc(pass, x.Fun) == "path/filepath.Join" && len(x.Args) > 0 {
			last = x.Args[len(x.Args)-1]
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			ops := concatOperands(x, nil)
			if len(ops) > 0 {
				last = ops[len(ops)-1]
			}
		}
	default:
		last = r
	}
	if last == nil {
		return "", false
	}
	return nameFromExpr(pass, resolve(pass, last, bound, 0), bound, ctx), true
}

// nameFromExpr reduces one component to its name evidence: the literal
// itself, a const's value, a Sprintf format, or the rightmost operand
// of a concat chain.
func nameFromExpr(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, ctx *pkgCtx) string {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return unquote(x.Value)
		}
	case *ast.Ident:
		if v, ok := ctx.constValues[x.Name]; ok {
			return v
		}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			ops := concatOperands(x, nil)
			if len(ops) > 0 {
				return nameFromExpr(pass, ops[len(ops)-1], bound, ctx)
			}
		}
	case *ast.CallExpr:
		if qualifiedFunc(pass, x.Fun) == "fmt.Sprintf" {
			if len(x.Args) > 0 {
				if lit, ok := x.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					return sprintfTail(unquote(lit.Value))
				}
			}
		}
	}
	return ""
}

// publicExt reports whether the name evidence ends in a
// generated-source or static-asset extension.
func publicExt(name string) bool {
	if name == "" {
		return false
	}
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return false
	}
	return publicExts[strings.ToLower(name[dot:])]
}

// outputRooted reports whether the path's provenance names an
// output/build root: directly, through local bindings, or through a
// bare parameter resolved via same-package callers.
func outputRooted(pass *analysis.Pass, path ast.Expr, bound map[types.Object]ast.Expr, fn *ast.FuncDecl, ctx *pkgCtx, depth int) bool {
	if depth > 2 {
		return false
	}
	r := resolve(pass, path, bound, 0)
	cands := []ast.Expr{r}
	if call, ok := r.(*ast.CallExpr); ok && qualifiedFunc(pass, call.Fun) == "path/filepath.Join" {
		cands = call.Args
	}
	for _, c := range cands {
		if exprNamesOutputRoot(pass, c, bound) {
			return true
		}
	}
	// A bare parameter: hop to same-package callers and judge the
	// arguments they pass at that position.
	for _, c := range cands {
		id, ok := c.(*ast.Ident)
		if !ok {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(id)
		v, ok := obj.(*types.Var)
		if !ok || !isParamOf(pass, fn, v) {
			continue
		}
		idx := paramIndex(fn, v.Name())
		if idx < 0 {
			continue
		}
		if callerArgNamesOutputRoot(pass, fn, idx, ctx) {
			return true
		}
	}
	return false
}

// callerArgNamesOutputRoot reports whether any same-package caller of
// fn passes an output-rooted expression at parameter index idx.
func callerArgNamesOutputRoot(pass *analysis.Pass, fn *ast.FuncDecl, idx int, ctx *pkgCtx) bool {
	for _, caller := range ctx.funcs {
		if caller == fn || caller.Body == nil || !callsName(caller.Body, fn.Name.Name) {
			continue
		}
		callerBound := bindings(pass, caller.Body)
		found := false
		ast.Inspect(caller.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || calleeName(call.Fun) != fn.Name.Name {
				return true
			}
			if arg := argAt(call, idx); arg != nil &&
				exprNamesOutputRoot(pass, resolve(pass, arg, callerBound, 0), callerBound) {
				found = true
				return false
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// exprNamesOutputRoot reports whether e's assembly (or any name or
// path-literal component in it, through local bindings) carries an
// output/build-root marker.
func exprNamesOutputRoot(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr) bool {
	found := false
	var walk func(ast.Expr, int)
	walk = func(x ast.Expr, depth int) {
		if found || x == nil || depth > 5 {
			return
		}
		switch n := x.(type) {
		case *ast.Ident:
			if namesOutputRoot(n.Name) {
				found = true
				return
			}
			if n.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(n); obj != nil {
					if b, ok := bound[obj]; ok {
						walk(b, depth+1)
					}
				}
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING && namesOutputRoot(unquote(n.Value)) {
				found = true
			}
		case *ast.SelectorExpr:
			if namesOutputRoot(n.Sel.Name) {
				found = true
				return
			}
			walk(n.X, depth)
		case *ast.CallExpr:
			if fn := calleeName(n.Fun); fn != "" && namesOutputRoot(fn) {
				found = true
				return
			}
			for _, a := range n.Args {
				walk(a, depth+1)
			}
		default:
			ast.Inspect(x, func(k ast.Node) bool {
				if found {
					return false
				}
				switch v := k.(type) {
				case *ast.Ident:
					walk(v, depth+1)
				case *ast.SelectorExpr:
					walk(v, depth+1)
				}
				return !found
			})
		}
	}
	walk(e, 0)
	return found
}

// namesOutputRoot matches the output/build-root family on the
// lowercased identifier (camelCase compounds included: OutDir →
// "outdir", OutputRoot → "outputroot", MigrationsDir →
// "migrationsdir").
func namesOutputRoot(name string) bool {
	l := strings.ToLower(name)
	for _, m := range outMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// callerReadsPath reports whether a same-package caller of fn read or
// opened the expression it passes as this write's path: the helper
// rewrites bytes that already exist on disk (cmd/mutate's restore of
// pre-mutation source).
func callerReadsPath(pass *analysis.Pass, s site, fn *ast.FuncDecl, bound map[types.Object]ast.Expr, ctx *pkgCtx) bool {
	id, ok := resolve(pass, s.path, bound, 0).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !isParamOf(pass, fn, v) {
		return false
	}
	idx := paramIndex(fn, v.Name())
	if idx < 0 {
		return false
	}
	for _, caller := range ctx.funcs {
		if caller == fn || caller.Body == nil || !callsName(caller.Body, fn.Name.Name) {
			continue
		}
		callerBound := bindings(pass, caller.Body)
		if callerReadsArg(pass, caller, idx, fn.Name.Name, callerBound) {
			return true
		}
	}
	return false
}

// callerReadsArg reports whether caller reads (os.ReadFile / os.Open)
// the expression it passes at index idx to callee name.
func callerReadsArg(pass *analysis.Pass, caller *ast.FuncDecl, idx int, name string, callerBound map[types.Object]ast.Expr) bool {
	found := false
	ast.Inspect(caller.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch qualifiedFunc(pass, call.Fun) {
		case "os.ReadFile", "os.Open":
			if p := argAt(call, 0); p != nil {
				target := exprString(resolve(pass, p, callerBound, 0))
				if readFeedsArg(pass, caller, idx, name, target, callerBound) {
					found = true
					return false
				}
			}
		}
		return !found
	})
	return found
}

// readFeedsArg reports whether the read target expression is also
// passed at index idx in a call to name inside caller.
func readFeedsArg(pass *analysis.Pass, caller *ast.FuncDecl, idx int, name, target string, callerBound map[types.Object]ast.Expr) bool {
	found := false
	ast.Inspect(caller.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || calleeName(call.Fun) != name {
			return true
		}
		if arg := argAt(call, idx); arg != nil &&
			exprString(resolve(pass, arg, callerBound, 0)) == target {
			found = true
			return false
		}
		return !found
	})
	return found
}

// dirOfCallerOwnedWrite reports whether a MkdirAll(filepath.Dir(x)) is
// the parent-creation of a write whose MODE this function takes from
// its caller (the harness Write builtin): the directory serves a
// caller-owned policy.
func dirOfCallerOwnedWrite(pass *analysis.Pass, s site, fn *ast.FuncDecl, bound map[types.Object]ast.Expr) bool {
	dRes := resolve(pass, s.path, bound, 0)
	call, ok := dRes.(*ast.CallExpr)
	if !ok || qualifiedFunc(pass, call.Fun) != "path/filepath.Dir" || len(call.Args) == 0 {
		return false
	}
	x := exprString(resolve(pass, call.Args[0], bound, 0))
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		w, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var modeArg ast.Expr
		switch qualifiedFunc(pass, w.Fun) {
		case "os.WriteFile":
			modeArg = argAt(w, 2)
		case "os.OpenFile":
			if hasWriteFlag(argAt(w, 1)) {
				modeArg = argAt(w, 2)
			}
		}
		if modeArg == nil {
			return true
		}
		if _, _, isConst := constPerm(modeArg); !isConst {
			if p := argAt(w, 0); p != nil && exprString(resolve(pass, p, bound, 0)) == x {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// dirHoldsOnlyPublic reports whether every write reachable into dir
// site d — same-function writes under the same root, an os.CreateTemp
// minted into it (the atomic-write pattern: the mode is set explicitly
// on the handle), or writes in a same-package helper called with that
// dir — is itself public or owner-only. A dir with no reachable
// writer stays loud.
func dirHoldsOnlyPublic(pass *analysis.Pass, d site, fn *ast.FuncDecl, bound map[types.Object]ast.Expr, readPaths map[string]bool, ctx *pkgCtx, all []site) bool {
	dRes := resolve(pass, d.path, bound, 0)
	dExpr := exprString(dRes)
	dRoot := exprString(resolveRoot(pass, dRes, bound))
	linked := 0
	for _, w := range all {
		if w.call == d.call {
			continue
		}
		wRes := resolve(pass, w.path, bound, 0)
		wExpr := exprString(wRes)
		wRoot := exprString(resolveRoot(pass, wRes, bound))
		under := wExpr == dExpr ||
			wRoot == dExpr ||
			(dRoot != "" && wRoot != "" && wRoot == dRoot) ||
			isDirOf(pass, wRes, dRes, bound)
		if !under {
			continue
		}
		linked++
		if !dirContentNeutral(pass, w, fn, bound, readPaths, ctx) {
			return false
		}
	}
	// The atomic-write pattern: files minted into this dir by
	// os.CreateTemp carry the write's own explicit handle-chmod policy.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || qualifiedFunc(pass, call.Fun) != "os.CreateTemp" {
			return true
		}
		if dir := argAt(call, 0); dir != nil &&
			exprString(resolve(pass, dir, bound, 0)) == dExpr {
			linked++
			return false
		}
		return true
	})
	// One helper hop: a package function called with this dir whose
	// writes under that parameter are all neutral.
	for _, h := range ctx.funcs {
		if h == fn || h.Body == nil {
			continue
		}
		consumed := helperConsumesDir(pass, fn.Body, h, dExpr, bound)
		if consumed == "" {
			continue
		}
		hSites := collectSites(pass, h.Body)
		hBound := bindings(pass, h.Body)
		for _, w := range hSites {
			hRes := resolve(pass, w.path, hBound, 0)
			if exprString(resolveRoot(pass, hRes, hBound)) != consumed && exprString(hRes) != consumed {
				continue
			}
			linked++
			if !dirContentNeutral(pass, w, h, hBound, readPathsFor(pass, h, hBound), ctx) {
				return false
			}
		}
	}
	return linked > 0
}

// resolveRoot returns the assembly root of a resolved path expression,
// itself resolved through locals (target := Join(dir, "agents")).
func resolveRoot(pass *analysis.Pass, resolved ast.Expr, bound map[types.Object]ast.Expr) ast.Expr {
	root := assemblyRoot(resolved)
	if root == nil {
		return nil
	}
	return resolve(pass, root, bound, 0)
}

// dirContentNeutral reports whether a write into a directory is public
// or owner-only (anything but a loud group-bits state write).
func dirContentNeutral(pass *analysis.Pass, w site, fn *ast.FuncDecl, bound map[types.Object]ast.Expr, readPaths map[string]bool, ctx *pkgCtx) bool {
	if w.perm&0o077 == 0 {
		return true // owner-only: the discipline itself
	}
	return silent(pass, w, fn, bound, readPaths, ctx, nil, 1)
}

// readPathsFor recomputes the read-first set for a helper body.
func readPathsFor(pass *analysis.Pass, fn *ast.FuncDecl, bound map[types.Object]ast.Expr) map[string]bool {
	if fn.Body == nil {
		return map[string]bool{}
	}
	return readFirstPaths(pass, fn.Body, bound)
}

// helperConsumesDir reports the parameter name of helper h that the
// function under check passes this dir expression to, or "".
func helperConsumesDir(pass *analysis.Pass, body *ast.BlockStmt, h *ast.FuncDecl, dExpr string, bound map[types.Object]ast.Expr) string {
	found := ""
	ast.Inspect(body, func(n ast.Node) bool {
		if found != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || calleeName(call.Fun) != h.Name.Name {
			return true
		}
		for i, a := range call.Args {
			if exprString(resolve(pass, a, bound, 0)) != dExpr {
				continue
			}
			if p := paramAt(h, i); p != "" {
				found = p
				return false
			}
		}
		return true
	})
	return found
}

// isDirOf reports whether fileExpr's parent is dirExpr: the
// filepath.Dir(x) ≡ d linkage (journal's MkdirAll over Dir(path) with
// the journal file written at path).
func isDirOf(pass *analysis.Pass, fileExpr, dirExpr ast.Expr, bound map[types.Object]ast.Expr) bool {
	call, ok := dirExpr.(*ast.CallExpr)
	if !ok || qualifiedFunc(pass, call.Fun) != "path/filepath.Dir" {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	return exprString(resolve(pass, call.Args[0], bound, 0)) == exprString(fileExpr)
}

// constPerm evaluates e as a constant permission literal.
func constPerm(e ast.Expr) (*ast.BasicLit, uint64, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return nil, 0, false
	}
	v, err := strconv.ParseUint(lit.Value, 0, 32)
	if err != nil {
		return nil, 0, false
	}
	return lit, v, true
}

// sprintfTail returns everything after the last format verb of a
// Sprintf format string: the literal name evidence ("%04d_%s.sql" →
// ".sql").
func sprintfTail(format string) string {
	i := strings.LastIndexByte(format, '%')
	if i < 0 {
		return format
	}
	rest := format[i+1:]
	j := 0
	for j < len(rest) && (rest[j] == '.' || (rest[j] >= '0' && rest[j] <= '9')) {
		j++
	}
	if j < len(rest) {
		j++ // the verb character
	}
	return rest[j:]
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

// resolve follows single-value local bindings, keeping the last
// binding in source order.
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

// bindings maps each local defined by an assignment to the expression
// it was last bound to. Multi-value assignments map each left side to
// the single call on the right, and range-loop value variables map to
// the range expression (the scaffold dir list carries the path names).
func bindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
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
		case *ast.RangeStmt:
			if id, ok := st.Value.(*ast.Ident); ok && id.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					bound[obj] = st.X
				}
			}
		}
		return true
	})
	return bound
}

// hasWriteFlag reports whether the os.OpenFile flag expression
// contains any of the write/create bits.
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

// qualifiedFunc renders a selector callee as "importpath.Func",
// resolving the package through the type checker so an aliased import
// still matches.
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

// calleeName renders a callee's base name for marker checks.
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// isParamOf reports whether v is one of fn's parameters.
func isParamOf(pass *analysis.Pass, fn *ast.FuncDecl, v *types.Var) bool {
	if fn.Type.Params == nil {
		return false
	}
	for _, f := range fn.Type.Params.List {
		for _, name := range f.Names {
			if obj, ok := pass.TypesInfo.ObjectOf(name).(*types.Var); ok && obj == v {
				return true
			}
		}
	}
	return false
}

// bindingHistory returns every expression the object was bound to
func bindingHistory(pass *analysis.Pass, obj types.Object, _ token.Pos) []ast.Expr {
	var out []ast.Expr
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.AssignStmt)
			if !ok || len(st.Rhs) != 1 {
				return true
			}
			for _, lhs := range st.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" {
					continue
				}
				if o := pass.TypesInfo.ObjectOf(id); o != nil && o == obj {
					out = append(out, st.Rhs[0])
				}
			}
			return true
		})
	}
	return out
}

// -1.
func paramIndex(fn *ast.FuncDecl, name string) int {
	if fn.Type.Params == nil {
		return -1
	}
	idx := 0
	for _, f := range fn.Type.Params.List {
		if len(f.Names) == 0 {
			idx++
			continue
		}
		for _, n := range f.Names {
			if n.Name == name {
				return idx
			}
			idx++
		}
	}
	return -1
}

// paramAt returns helper's parameter name at positional index i.
func paramAt(fn *ast.FuncDecl, i int) string {
	if fn.Type.Params == nil {
		return ""
	}
	idx := 0
	for _, f := range fn.Type.Params.List {
		if len(f.Names) == 0 {
			idx++
			continue
		}
		for _, n := range f.Names {
			if idx == i {
				return n.Name
			}
			idx++
		}
	}
	return ""
}

// callsName reports whether body calls a function of that base name.
func callsName(body *ast.BlockStmt, name string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if calleeName(call.Fun) == name {
			found = true
		}
		return !found
	})
	return found
}

func argAt(call *ast.CallExpr, i int) ast.Expr {
	if len(call.Args) > i {
		return call.Args[i]
	}
	return nil
}

// exprString is a nil-safe types.ExprString.
func exprString(e ast.Expr) string {
	if e == nil {
		return ""
	}
	return types.ExprString(e)
}

func unquote(s string) string {
	if v, err := strconv.Unquote(s); err == nil {
		return v
	}
	return s
}
