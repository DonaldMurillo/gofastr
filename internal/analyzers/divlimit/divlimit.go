// Package divlimit catches integer division and remainder by a
// caller-supplied pagination-sized value that the function never guards
// against 0 or 1.
//
// The bug class: an exported in-process entrypoint taking a raw limit
// panics with "integer divide by zero" the moment a caller passes 0
// (or, for page math, 1). Probe TestStreamingListZeroLimitNoPanic
// found it in framework/crud crud_stream.go ServeStreamingList (fixed
// in a24928c1): `total / limit` with an unguarded limit panicked the
// stream before the first row; the fix guards `limit > 0` right where
// OffsetForPage already guarded its own division.
//
// Silent postures, deliberately:
//   - any comparison of the divisor against 0 or 1 earlier in the
//     function (the fix posture, including `limit <= 0 { return }`);
//   - divisors not named in the limit/perPage/pageSize/size/count/n
//     family: a divisor called `rows` or `stride` is not recognizably
//     caller pagination, and this rule refuses to guess;
//   - constants and len(...) results: they cannot become 0 from
//     outside;
//   - float division: no panic, no rule;
//   - _test.go files.
package divlimit

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrdivlimit",
	Doc:  "forbids integer division by a caller-supplied limit/pageSize without a zero-or-one guard; it panics on 0",
	Run:  run,
}

// limitName matches the pagination-family divisor names exactly
// (case-insensitively): compound names like `limitPerPage` would need a
// call site to judge, and substring matching on "n" or "size" matches
// half the integers in any codebase.
var limitName = regexp.MustCompile(`^(?i:limit|perPage|pageSize|size|count|n)$`)

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkFunc(pass, fn)
		}
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl) {
	body := fn.Body
	bound := bindings(pass, body)
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

	ast.Inspect(body, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		if bin.Op != token.QUO && bin.Op != token.REM {
			return true
		}
		divisor := divisorCandidate(pass, bin.Y, bound, params)
		if divisor == nil || !isInteger(pass, bin.X) || !isInteger(pass, bin.Y) {
			return true
		}
		if guardedBefore(pass, body, divisor, bin) {
			return true
		}
		pass.Reportf(bin.Pos(),
			"integer division by %s, a caller-supplied value, with no zero-or-one guard: it panics when 0; compare it against 0 (or 1) before dividing",
			divisor.Name())
		return true
	})
}

// guardedBefore reports whether the divisor was compared against 0 or 1
// anywhere in the function before the division.
func guardedBefore(pass *analysis.Pass, body *ast.BlockStmt, divisor types.Object, div *ast.BinaryExpr) bool {
	guarded := false
	ast.Inspect(body, func(n ast.Node) bool {
		if guarded {
			return false
		}
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		switch bin.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			return true
		}
		if bin.Pos() >= div.Pos() {
			return true
		}
		var other ast.Expr
		switch {
		case isIdentObj(pass, bin.X, divisor):
			other = bin.Y
		case isIdentObj(pass, bin.Y, divisor):
			other = bin.X
		default:
			return true
		}
		if isZeroOrOne(pass, other) {
			guarded = true
		}
		return true
	})
	return guarded
}

// divisorCandidate resolves the divisor to a limit-named function
// parameter, or a local of that name bound to a query/param accessor.
// Constants and len(...) locals are not caller-supplied.
func divisorCandidate(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) types.Object {
	switch x := e.(type) {
	case *ast.BasicLit:
		return nil // constants cannot become 0 from outside
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		if obj == nil {
			return nil
		}
		if v, ok := obj.(*types.Var); !ok || v.IsField() {
			return nil
		}
		if params[obj] {
			if limitName.MatchString(x.Name) {
				return obj
			}
			return nil
		}
		// A local: it counts only when it was parsed straight out of a
		// request (strconv on a param accessor), not computed or measured.
		b, ok := bound[obj]
		if !ok || !limitName.MatchString(x.Name) {
			return nil
		}
		call, ok := b.(*ast.CallExpr)
		if !ok {
			return nil
		}
		switch qualifiedFunc(pass, call.Fun) {
		case "strconv.Atoi", "strconv.ParseInt", "strconv.ParseUint":
			return obj
		}
		return nil
	case *ast.CallExpr:
		return nil // len(...) and every other call result is not caller-named
	default:
		return nil
	}
}

func isInteger(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

func isZeroOrOne(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return false
	}
	i, ok := constant.Int64Val(tv.Value)
	return ok && (i == 0 || i == 1)
}

func isIdentObj(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == obj
}

// bindings maps each local to the expression it was last bound to,
// including each side of a multi-value assignment (`limit, _ :=
// strconv.Atoi(...)`).
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
