// Package fixedtmp catches a path under the shared temp root whose
// name is CONSTANT or pid-predictable reaching a create/mkdir/exec or
// build sink: os.Mkdir / os.MkdirAll / os.Create / os.WriteFile /
// os.OpenFile(write flags) on it, an exec.Command /
// exec.CommandContext argument (`go build -o <path>` writes through
// whatever is at the path; argv[0] runs it), or an exec.Cmd Dir
// assignment (the child's cwd).
//
// The bug class: 2026-09-03 adversarial pass round 4 (tests-only red
// probes, fixes still open) — TestDevServerRedUniqueBinaryPath
// (cmd/gofastr dev.go devServerBinaryPath: the rebuilt server is
// compiled to Join(os.TempDir(), "gofastr-dev-server-"+pid) with
// `go build -o` and exec'd, a fully deterministic name: a local
// co-user pre-plants a symlink there and the build writes through it
// (CWE-377 clobber) or the binary is swapped between build and exec)
// and TestKilnAdapterRedUniqueWorkDir (cmd/kiln adapters.go Dir
// constants /tmp/kiln-{omp,claude,pi,codex} consumed by
// agent_watcher.go's os.MkdirAll(Dir, 0o755) + cmd.Dir = Dir: MkdirAll
// no-ops on an existing dir regardless of mode, so a pre-created or
// symlinked name becomes the cwd of a bash-capable coding agent).
//
// A PID IS NOT ENTROPY: os.Getpid(), uid, hostname, timestamps,
// runtime.GOOS and unknown helper results are all guessable or known
// before the fact; that is the whole of CWE-377. The only silence is
// PROVEN entropy. Silent postures, deliberately:
//   - the path (or a component of it, or its Join root) is bound to an
//     os.MkdirTemp / os.CreateTemp / t.TempDir result: unique by
//     construction (kiln/db EphemeralSQLite);
//   - the tail components' assembly provably involves crypto/rand,
//     directly or through locals, same-package helper bodies, or the
//     package-wide provenance of the struct fields feeding the path
//     (processmodule's scratch dir derives from the InstanceID nonce
//     mintInstanceID mints with crypto/rand);
//   - paths not rooted at the shared temp root: os.TempDir() or a
//     "/tmp" literal as the Join/concat root, or a "/tmp/..." literal
//     — anywhere else is another filesystem's problem;
//   - os.Remove and reads: an unlink follows no symlink target and
//     leaks nothing, so dev.go's shutdown cleanup stays quiet;
//   - _test.go files.
package fixedtmp

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "fixedtmp",
	Doc:  "forbids constant/pid-named paths under the shared temp root reaching create/mkdir/exec/build sinks: mint the name with os.MkdirTemp/os.CreateTemp, or create it 0700 and refuse a name you do not own",
	Run:  run,
}

// prov is one expression a struct field was assigned in a composite
// literal, with the function it lives in (for that function's local
// bindings when chasing entropy through field provenance).
type prov struct {
	expr ast.Expr
	fn   *ast.FuncDecl
}

type pkgCtx struct {
	funcs     map[string]*ast.FuncDecl // package-level functions
	fieldProv map[string][]prov        // field name -> literal assignments
}

func run(pass *analysis.Pass) (any, error) {
	ctx := &pkgCtx{
		funcs:     map[string]*ast.FuncDecl{},
		fieldProv: map[string][]prov{},
	}
	var files []*ast.File
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		files = append(files, f)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && fn.Body != nil && fn.Recv == nil {
				ctx.funcs[fn.Name.Name] = fn
			}
		}
	}
	// Field provenance: every composite-literal key/value pair, with
	// the enclosing function (nil for package-level vars like the
	// kiln adapter registry) for binding-aware entropy walks.
	for _, f := range files {
		for _, d := range f.Decls {
			var fn *ast.FuncDecl
			var root ast.Node
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Body == nil {
					continue
				}
				fn, root = decl, decl.Body
			case *ast.GenDecl:
				if decl.Tok != token.VAR {
					continue
				}
				root = decl
			default:
				continue
			}
			ast.Inspect(root, func(n ast.Node) bool {
				if lit, ok := n.(*ast.CompositeLit); ok {
					for _, elt := range lit.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						key, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						ctx.fieldProv[key.Name] = append(ctx.fieldProv[key.Name], prov{expr: kv.Value, fn: fn})
					}
				}
				return true
			})
		}
	}
	for _, f := range files {
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
	hist := bindingHistory(pass, fn.Body)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			checkCall(pass, x, bound, hist, fn, ctx)
		case *ast.AssignStmt:
			// exec.Cmd cwd: c.Dir = <tainted path>.
			if len(x.Lhs) == 1 && len(x.Rhs) == 1 {
				if sel, ok := x.Lhs[0].(*ast.SelectorExpr); ok && sel.Sel.Name == "Dir" {
					if tainted(pass, x.Rhs[0], bound, hist, fn, ctx, 0) {
						pass.Reportf(x.Pos(),
							"exec Dir is a fixed path under the shared temp root (%s): a local co-user can pre-create the directory or plant a symlink and own the child's working directory (CWE-377); os.MkdirTemp per invocation (unique and 0700), or 0700 plus an ownership check",
							types.ExprString(x.Rhs[0]))
					}
				}
			}
		}
		return true
	})
}

func checkCall(pass *analysis.Pass, call *ast.CallExpr, bound map[types.Object]ast.Expr, hist map[types.Object][]ast.Expr, fn *ast.FuncDecl, ctx *pkgCtx) {
	var pathExprs []ast.Expr
	kind := ""
	switch qualifiedFunc(pass, call.Fun) {
	case "os.Mkdir", "os.MkdirAll":
		pathExprs = []ast.Expr{argAt(call, 0)}
		kind = "mkdir"
	case "os.Create", "os.WriteFile":
		pathExprs = []ast.Expr{argAt(call, 0)}
		kind = "create"
	case "os.OpenFile":
		if hasWriteFlag(argAt(call, 1)) {
			pathExprs = []ast.Expr{argAt(call, 0)}
			kind = "create"
		}
	case "os/exec.Command", "os/exec.CommandContext":
		pathExprs = call.Args
		kind = "exec"
	}
	for _, p := range pathExprs {
		if p == nil {
			continue
		}
		if !tainted(pass, p, bound, hist, fn, ctx, 0) {
			continue
		}
		switch kind {
		case "mkdir":
			pass.Reportf(call.Pos(),
				"mkdir on a fixed path under the shared temp root (%s): a local co-user can pre-create the name or plant a symlink and own what lands there (CWE-377); mint the name with os.MkdirTemp, or create it 0700 and refuse a directory you do not own",
				types.ExprString(p))
		case "create":
			pass.Reportf(call.Pos(),
				"file created at a fixed path under the shared temp root (%s): a local co-user can pre-create the name or plant a symlink and own what lands there (CWE-377); use os.CreateTemp, or 0700 plus an ownership check",
				types.ExprString(p))
		case "exec":
			pass.Reportf(call.Pos(),
				"exec/build reaches a fixed path under the shared temp root (%s): the name is guessable (pid-only at best), so a pre-planted symlink turns the build into an arbitrary clobber and the exec into the attacker's binary (CWE-377); build and exec a path minted with os.MkdirTemp/os.CreateTemp per process",
				types.ExprString(p))
		}
	}
}

// tainted reports whether e resolves to a predictable path under the
// shared temp root: directly, through locals, through a one-hop
// same-package helper's return value, or through a struct field that
// the package initializes with such a path.
func tainted(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, hist map[types.Object][]ast.Expr, fn *ast.FuncDecl, ctx *pkgCtx, depth int) bool {
	if depth > 3 {
		return false
	}
	// Through locals first: tmpBin := devServerBinaryPath(rt).
	e = resolve(pass, e, bound, 0)
	// Field reads: the kiln adapter registry's Dir values.
	if sel, ok := e.(*ast.SelectorExpr); ok {
		for _, p := range ctx.fieldProv[sel.Sel.Name] {
			if tempRootedPredictable(pass, p.expr, p.fn, ctx, 0) {
				return true
			}
		}
	}
	// One same-package helper hop: devServerBinaryPath(rt).
	if call, ok := e.(*ast.CallExpr); ok {
		if id, ok := call.Fun.(*ast.Ident); ok {
			if decl, ok := ctx.funcs[id.Name]; ok && decl.Body != nil {
				hBound := bindings(pass, decl.Body)
				hHist := bindingHistory(pass, decl.Body)
				found := false
				ast.Inspect(decl.Body, func(n ast.Node) bool {
					if ret, ok := n.(*ast.ReturnStmt); ok {
						for _, r := range ret.Results {
							if tempRootedPredictableWith(pass, r, hBound, hHist, decl, ctx, 0) {
								found = true
								return false
							}
						}
					}
					return !found
				})
				if found {
					return true
				}
			}
		}
	}
	return tempRootedPredictable(pass, e, fn, ctx, depth)
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

// tempRootedPredictable resolves e through fn's locals and asks
// tempRootedPredictableWith.
func tempRootedPredictable(pass *analysis.Pass, e ast.Expr, fn *ast.FuncDecl, ctx *pkgCtx, depth int) bool {
	var bound map[types.Object]ast.Expr
	var hist map[types.Object][]ast.Expr
	if fn != nil && fn.Body != nil {
		bound = bindings(pass, fn.Body)
		hist = bindingHistory(pass, fn.Body)
	}
	return tempRootedPredictableWith(pass, e, bound, hist, fn, ctx, depth)
}

// tempRootedPredictableWith reports whether e is a path under the
// shared temp root whose tail after the root is guessable: no
// MkdirTemp/CreateTemp/t.TempDir/crypto/rand anywhere in the tail's
// assembly (through local binding history, same-package helper
// bodies, and field provenance).
func tempRootedPredictableWith(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, hist map[types.Object][]ast.Expr, fn *ast.FuncDecl, ctx *pkgCtx, depth int) bool {
	r := e
	for i := 0; r != nil && i < 8; i++ {
		if tempRootedShape(pass, r) {
			var tail []ast.Expr
			switch x := r.(type) {
			case *ast.CallExpr:
				tail = x.Args[1:]
			case *ast.BinaryExpr:
				ops := concatOperands(x, nil)
				tail = ops[1:]
			}
			if len(tail) == 0 {
				tail = []ast.Expr{r} // "/tmp/literal" whole-path literal
			}
			for _, t := range tail {
				if hasEntropy(pass, t, bound, hist, ctx, 0) {
					return false
				}
			}
			return true
		}
		id, ok := r.(*ast.Ident)
		if !ok {
			return false
		}
		b, ok := bound[pass.TypesInfo.ObjectOf(id)]
		if !ok {
			return false
		}
		r = b
	}
	return false
}

// tempRootedShape reports whether e IS the shared-root join shape:
// filepath.Join(os.TempDir(), ...), os.TempDir() + "/" + ...,
// filepath.Join("/tmp", ...), or a "/tmp/..." literal.
func tempRootedShape(pass *analysis.Pass, e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.CallExpr:
		if qualifiedFunc(pass, x.Fun) != "path/filepath.Join" || len(x.Args) < 1 {
			return false
		}
		return isTempRootExpr(pass, x.Args[0])
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return false
		}
		ops := concatOperands(x, nil)
		return len(ops) > 1 && isTempRootExpr(pass, ops[0])
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return false
		}
		v, err := strconv.Unquote(x.Value)
		return err == nil && strings.HasPrefix(v, "/tmp/")
	}
	return false
}

// isTempRootExpr matches os.TempDir() and the "/tmp" literal.
func isTempRootExpr(pass *analysis.Pass, e ast.Expr) bool {
	if call, ok := e.(*ast.CallExpr); ok && qualifiedFunc(pass, call.Fun) == "os.TempDir" {
		return true
	}
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if v, err := strconv.Unquote(lit.Value); err == nil && v == "/tmp" {
			return true
		}
	}
	return false
}

// hasEntropy reports whether e's assembly provably involves an
// entropy source: os.MkdirTemp / os.CreateTemp / t.TempDir /
// crypto/rand, through local binding history, same-package helper
// bodies, and the package-wide provenance of struct fields read in the
// assembly.
func hasEntropy(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, hist map[types.Object][]ast.Expr, ctx *pkgCtx, depth int) bool {
	if depth > 3 || e == nil {
		return false
	}
	found := false
	var walk func(ast.Expr)
	walk = func(x ast.Expr) {
		if found || x == nil {
			return
		}
		switch n := x.(type) {
		case *ast.CallExpr:
			q := qualifiedFunc(pass, n.Fun)
			if q == "os.MkdirTemp" || q == "os.CreateTemp" ||
				strings.HasPrefix(q, "crypto/rand.") {
				found = true
				return
			}
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "TempDir" &&
				qualifiedFunc(pass, n.Fun) != "os.TempDir" {
				found = true // t.TempDir()
				return
			}
			// Same-package helper: entropy inside its body counts.
			if id, ok := n.Fun.(*ast.Ident); ok {
				if decl, ok := ctx.funcs[id.Name]; ok && decl.Body != nil {
					if bodyHasEntropy(pass, decl.Body, ctx, depth+1) {
						found = true
						return
					}
				}
			}
			for _, a := range n.Args {
				walk(a)
			}
		case *ast.Ident:
			if n.Name == "_" {
				return
			}
			obj := pass.TypesInfo.ObjectOf(n)
			if obj == nil {
				return
			}
			for _, b := range hist[obj] {
				walk(b)
			}
		case *ast.SelectorExpr:
			// A struct field read: follow the package's assignments of
			// that field (processmodule's InstanceID nonce).
			for _, p := range ctx.fieldProv[n.Sel.Name] {
				var h map[types.Object][]ast.Expr
				if p.fn != nil && p.fn.Body != nil {
					h = bindingHistory(pass, p.fn.Body)
				}
				if hasEntropy(pass, p.expr, nil, h, ctx, depth+1) {
					found = true
					return
				}
			}
			walk(n.X)
		default:
			ast.Inspect(x, func(k ast.Node) bool {
				if found {
					return false
				}
				switch v := k.(type) {
				case *ast.CallExpr:
					walk(v)
				case *ast.Ident:
					walk(v)
				case *ast.SelectorExpr:
					walk(v)
				}
				return !found
			})
		}
	}
	walk(e)
	return found
}

// bodyHasEntropy scans a helper body once (depth-limited recursion
// through hasEntropy).
func bodyHasEntropy(pass *analysis.Pass, body *ast.BlockStmt, ctx *pkgCtx, depth int) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			q := qualifiedFunc(pass, call.Fun)
			if q == "os.MkdirTemp" || q == "os.CreateTemp" ||
				strings.HasPrefix(q, "crypto/rand.") {
				found = true
				return false
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "TempDir" &&
				qualifiedFunc(pass, call.Fun) != "os.TempDir" {
				found = true
				return false
			}
		}
		return !found
	})
	return found
}

// bindings maps each local to the expression it was last bound to.
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

// bindingHistory maps each local to EVERY expression it was ever
// bound to (including +=): entropy may enter at any assignment.
func bindingHistory(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object][]ast.Expr {
	hist := map[types.Object][]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Rhs) != 1 {
			return true
		}
		for _, lhs := range st.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					hist[obj] = append(hist[obj], st.Rhs[0])
				}
			}
		}
		return true
	})
	return hist
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

// qualifiedFunc renders a selector callee as "importpath.Func".
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

// concatOperands flattens a left-associated ADD chain, leftmost first.
func concatOperands(be *ast.BinaryExpr, out []ast.Expr) []ast.Expr {
	if inner, ok := be.X.(*ast.BinaryExpr); ok && inner.Op == token.ADD {
		out = concatOperands(inner, out)
	} else {
		out = append(out, be.X)
	}
	return append(out, be.Y)
}

func argAt(call *ast.CallExpr, i int) ast.Expr {
	if len(call.Args) > i {
		return call.Args[i]
	}
	return nil
}
