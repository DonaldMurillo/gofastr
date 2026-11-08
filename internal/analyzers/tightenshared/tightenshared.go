// Package tightenshared catches owner-only tightening of a directory the
// caller named, applied whether or not this code created it.
//
// The bug: os.MkdirAll(dir, 0o700) creates a missing directory 0700 and
// leaves an existing one alone, so a following os.Chmod(dir, 0o700) or
// fileperm.Restrict(dir, true) rewrites a directory somebody else owns.
// Found by the 2026-10-27 bug bash: battery/log FileSink tightens the
// parent of its log path, and the documented /var/log/myapp.log either
// fails boot as non-root (chmod /var/log: operation not permitted) or,
// as root, strips /var/log for every other daemon (L20); ExportData
// does the same to a caller-named export directory (S43); and on
// Windows, where fileperm.Restrict replaces the DACL, the desktop
// Config.DataDir, the semantic Snapshot directory and the harness
// session and cost-ledger directories lose their groups and
// inheritance (B62). The fix is to tighten only a directory this call
// created, and refuse (or warn on) an existing one that is too open.
//
// Shape: in one function, a creating call for X — os.MkdirAll(X, ...),
// or os.Mkdir(X, ...) whose EEXIST is tolerated (an os.IsExist /
// errors.Is(err, fs.ErrExist) test on its error before the tighten;
// the battery/desktop/linux profile walk spelling, 2026-11) — and
// then a tightening of the same X, where X is caller-named: a
// parameter, filepath.Dir of one, or a local bound to either. A
// tightening is os.Chmod(X, m) with a constant owner-only m, or
// fileperm.Restrict(X, true). One report per function and directory,
// on the first tightening.
//
// Silent postures, deliberately:
//   - a tightening in the then-branch of `if err == nil` on the
//     tolerated os.Mkdir's own error (or a local bound to that test):
//     the Mkdir's success is the one proof this call made X. Until
//     #665 a consulted os.Stat of X before the creating call exempted
//     the tightening too, and it lost the race: another process's
//     mkdir lands between the Stat and the create, the tolerated
//     EEXIST continues, and the chmod rewrites their directory;
//   - a directory this code names itself: joined under a receiver
//     field, an environment value, os.UserConfigDir or a temp root
//     (the isolation runtime's .gofastr/isolation/<id>, the desktop
//     default data dir), which the package owns;
//   - os.Chmod with a mode that is not a constant (a zip extract
//     restoring the archive's own modes): the caller owns the policy;
//   - fileperm.Restrict(X, false), a file, and fileperm
//     RestrictDirectoryTree: the storage chain walk derives its
//     directories from the key under a configured root, which a
//     parameter test cannot follow (B62's storage and upload sites);
//   - _test.go files.
package tightenshared

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name: "tightenshared",
	Doc:  "report an owner-only chmod or fileperm.Restrict of a caller-named directory after os.MkdirAll, which rewrites a directory the call did not create",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && fn.Body != nil {
				checkFunc(pass, fn)
			}
		}
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl) {
	params := pathflow.ParamObjects(pass, fn)
	bound := pathflow.Bindings(pass, fn.Body)
	parents := dominance.Parents(fn.Body)
	key := func(e ast.Expr) string {
		return types.ExprString(pathflow.Resolve(pass, e, bound, 0))
	}
	var calls []*ast.CallExpr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			calls = append(calls, c)
		}
		return true
	})
	made := map[string]*ast.CallExpr{} // first MkdirAll per directory
	mkdirs := map[string]mkdirArm{}    // first Mkdir per directory, awaiting its ErrExist verdict
	reported := map[string]bool{}
	for _, c := range calls {
		if len(c.Args) == 0 {
			continue
		}
		k := key(c.Args[0])
		switch q := pathflow.QualifiedFunc(pass, c.Fun); {
		case q == "os.MkdirAll":
			if made[k] == nil {
				made[k] = c
			}
		case q == "os.Mkdir":
			if _, seen := mkdirs[k]; !seen && made[k] == nil {
				mkdirs[k] = mkdirArm{call: c, errObj: errVarOf(pass, c, parents)}
			}
		case isTighten(pass, c, q):
			// A tolerated-EEXIST Mkdir arms the same way MkdirAll
			// does (2026-11, AZ12-04): the EEXIST path continues past
			// an existing directory, so the tightening still reaches
			// one this call did not create.
			// A tightening inside `if err == nil` on that Mkdir's own
			// error is the fix: the Mkdir's success says this call made
			// the directory (#665).
			armed := made[k] != nil
			if !armed {
				if m, ok := mkdirs[k]; ok && errExistTolerated(pass, m, c.Pos(), fn.Body) && !createdGuard(pass, m, c, parents, bound, fn.Body) {
					armed = true
				}
			}
			if !armed || reported[k] || !callerNamed(pass, c.Args[0], bound, params, 0) {
				continue
			}
			reported[k] = true
			pass.Reportf(c.Pos(),
				"tightenshared: %s tightens the caller-named directory %s after os.MkdirAll, which leaves an existing directory in place: a shared parent (/var/log) loses its group access, or the call fails on a directory another user owns; tighten only a directory this call created (os.Mkdir, and tighten when its error is nil) and refuse an existing one that is too open",
				shortName(q), types.ExprString(c.Args[0]))
		}
	}
}

// mkdirArm is an os.Mkdir call and the error variable its result was
// bound to, for the tolerated-EEXIST test.
type mkdirArm struct {
	call   *ast.CallExpr
	errObj types.Object
}

// createdGuard reports whether the tightening c sits in the then-branch
// of an if whose condition is `err == nil` on the Mkdir's own error
// variable, directly or through a local bound to that comparison
// (`created := err == nil`). The Mkdir succeeding is the only proof
// that this call made the directory: an os.Stat that said "missing"
// before it loses the race to another process's Mkdir, whose EEXIST
// the tolerated arm then waves through (#665).
func createdGuard(pass *analysis.Pass, m mkdirArm, c *ast.CallExpr, parents map[ast.Node]ast.Node, bound map[types.Object]ast.Expr, body *ast.BlockStmt) bool {
	if m.errObj == nil {
		return false
	}
	for cur := ast.Node(c); cur != nil; cur = parents[cur] {
		ifs, ok := parents[cur].(*ast.IfStmt)
		if !ok || cur != ifs.Body || ifs.Pos() < m.call.End() {
			continue
		}
		// The error must still be the Mkdir's: `_, err = os.Stat(dir)`
		// in between, or in the if's own initializer, makes err nil on
		// the EEXIST path too.
		if isErrNil(pass, ifs.Cond, m.errObj, bound, 0) && !assignedBetween(pass, body, m.errObj, m.call.End(), ifs.Cond.Pos()) {
			return true
		}
	}
	return false
}

// isErrNil reports whether e is `obj == nil` (either side), or a local
// bound to one.
func isErrNil(pass *analysis.Pass, e ast.Expr, obj types.Object, bound map[types.Object]ast.Expr, depth int) bool {
	if depth > 4 {
		return false
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if b, ok := bound[pass.TypesInfo.ObjectOf(x)]; ok {
			return isErrNil(pass, b, obj, bound, depth+1)
		}
	case *ast.BinaryExpr:
		if x.Op != token.EQL {
			return false
		}
		isObj := func(e ast.Expr) bool {
			id, ok := ast.Unparen(e).(*ast.Ident)
			return ok && pass.TypesInfo.ObjectOf(id) == obj
		}
		isNil := func(e ast.Expr) bool {
			id, ok := ast.Unparen(e).(*ast.Ident)
			return ok && id.Name == "nil"
		}
		return (isObj(x.X) && isNil(x.Y)) || (isNil(x.X) && isObj(x.Y))
	}
	return false
}

// errVarOf returns the variable a single-result call's error was
// assigned to, or nil.
func errVarOf(pass *analysis.Pass, call *ast.CallExpr, parents map[ast.Node]ast.Node) types.Object {
	assign, ok := parents[call].(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || assign.Rhs[0] != call {
		return nil
	}
	id, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(id)
	if _, isNil := obj.(*types.Nil); obj == nil || isNil {
		return nil
	}
	return obj
}

// errExistTolerated reports whether an os.IsExist(err) or
// errors.Is(err, fs.ErrExist) on the Mkdir's error variable runs
// between the Mkdir and pos: the walk-then-create spelling, where an
// EEXIST from another process is a concurrent win and anything else
// refuses (battery/desktop/linux profile walk).
func errExistTolerated(pass *analysis.Pass, m mkdirArm, pos token.Pos, body *ast.BlockStmt) bool {
	if m.errObj == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() <= m.call.Pos() || call.Pos() >= pos || len(call.Args) == 0 {
			return true
		}
		q := pathflow.QualifiedFunc(pass, call.Fun)
		if q != "os.IsExist" && q != "errors.Is" {
			return true
		}
		if q == "errors.Is" && !mentionsErrExist(call) {
			return true
		}
		if mentionsObject(pass, call.Args[0], m.errObj) {
			found = true
			return false
		}
		return true
	})
	return found
}

// mentionsErrExist reports whether the errors.Is call's second
// argument names an ErrExist sentinel.
func mentionsErrExist(call *ast.CallExpr) bool {
	if len(call.Args) < 2 {
		return false
	}
	found := false
	ast.Inspect(call.Args[1], func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "ErrExist" {
			found = true
		}
		return !found
	})
	return found
}

// mentionsObject reports whether e mentions the variable obj.
func mentionsObject(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || found {
			return !found
		}
		if pass.TypesInfo.ObjectOf(id) == obj {
			found = true
		}
		return !found
	})
	return found
}

// isTighten: os.Chmod with a constant owner-only mode, or
// fileperm.Restrict with a constant true (a directory).
func isTighten(pass *analysis.Pass, c *ast.CallExpr, q string) bool {
	if len(c.Args) != 2 {
		return false
	}
	tv, ok := pass.TypesInfo.Types[c.Args[1]]
	if !ok || tv.Value == nil {
		return false
	}
	switch {
	case q == "os.Chmod":
		m, ok := constant.Uint64Val(constant.ToInt(tv.Value))
		return ok && m&0o077 == 0
	case strings.HasSuffix(q, "fileperm.Restrict"):
		return tv.Value.Kind() == constant.Bool && constant.BoolVal(tv.Value)
	}
	return false
}

// callerNamed: e is a parameter, filepath.Dir of a caller-named path,
// or a local bound to either.
func callerNamed(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool, depth int) bool {
	if depth > 8 {
		return false
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		if params[obj] {
			return true
		}
		if b, ok := bound[obj]; ok {
			return callerNamed(pass, b, bound, params, depth+1)
		}
	case *ast.CallExpr:
		if pathflow.QualifiedFunc(pass, x.Fun) == "path/filepath.Dir" && len(x.Args) == 1 {
			return callerNamed(pass, x.Args[0], bound, params, depth+1)
		}
	}
	return false
}

func shortName(q string) string {
	if i := strings.LastIndex(q, "/"); i >= 0 {
		return q[i+1:]
	}
	return q
}

// assignedBetween reports whether obj is assigned anywhere in body
// between the positions from and to.
func assignedBetween(pass *analysis.Pass, body *ast.BlockStmt, obj types.Object, from, to token.Pos) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || found || st.Pos() < from || st.Pos() >= to {
			return !found
		}
		for _, lhs := range st.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
				found = true
			}
		}
		return !found
	})
	return found
}
