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
// Shape: in one function, os.MkdirAll(X, ...) and then a tightening of
// the same X, where X is caller-named: a parameter, filepath.Dir of
// one, or a local bound to either. A tightening is os.Chmod(X, m) with
// a constant owner-only m, or fileperm.Restrict(X, true). One report
// per function and directory, on the first tightening.
//
// Silent postures, deliberately:
//   - an os.Stat or os.Lstat of X before the MkdirAll: the existence
//     check the fix spelling needs to know whether it created X;
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
//
// A helper whose callers pass only directories the package namespaces
// itself (battery/service ensurePrivateDir) is the shape on purpose and
// carries the allow marker with that reason.
package tightenshared

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
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
	statted := map[string]bool{}       // Stat/Lstat seen before that MkdirAll
	reported := map[string]bool{}
	for _, c := range calls {
		if len(c.Args) == 0 {
			continue
		}
		k := key(c.Args[0])
		switch q := pathflow.QualifiedFunc(pass, c.Fun); {
		case q == "os.Stat" || q == "os.Lstat":
			if made[k] == nil {
				statted[k] = true
			}
		case q == "os.MkdirAll":
			if made[k] == nil {
				made[k] = c
			}
		case isTighten(pass, c, q):
			if made[k] == nil || statted[k] || reported[k] || !callerNamed(pass, c.Args[0], bound, params, 0) {
				continue
			}
			reported[k] = true
			pass.Reportf(c.Pos(),
				"tightenshared: %s tightens the caller-named directory %s after os.MkdirAll, which leaves an existing directory in place: a shared parent (/var/log) loses its group access, or the call fails on a directory another user owns; tighten only a directory this call created (os.Stat first) and refuse an existing one that is too open",
				shortName(q), types.ExprString(c.Args[0]))
		}
	}
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
