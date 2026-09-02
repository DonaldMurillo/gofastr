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
//   - a comparison of the divisor against 0 or 1 that DOMINATES the
//     division: it sits in the same block before it, in an enclosing
//     block before the statement holding it, or is the condition of an
//     enclosing if whose then-branch holds it (the fix posture,
//     including `limit <= 0 { return }`). A guard inside a nested
//     conditional body of an earlier statement (a flag-gated check)
//     executes only when the flag is set and dominates nothing;
//   - divisors not named in the limit/perPage/pageSize/size/count/n
//     family (parameters, or struct fields of that family — a limit
//     decoded into a request struct is the standard handler spelling):
//     a divisor called `rows` or `stride` is not recognizably caller
//     pagination, and this rule refuses to guess;
//   - constants and len(...) results: they cannot become 0 from
//     outside;
//   - float division: no panic, no rule;
//   - _test.go files.
//
// Every function body is examined, including divisions inside
// package-level function literals (the handler-table spelling).
package divlimit

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
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
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					checkFunc(pass, fn.Type, fn.Body)
				}
			case *ast.FuncLit:
				checkFunc(pass, fn.Type, fn.Body)
			}
			return true
		})
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, fnType *ast.FuncType, body *ast.BlockStmt) {
	bound := bindings(pass, body)
	params := map[types.Object]bool{}
	if fnType.Params != nil {
		for _, f := range fnType.Params.List {
			for _, name := range f.Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
					params[obj] = true
				}
			}
		}
	}
	parents := dominance.Parents(body)

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
		if guardedBefore(pass, body, parents, divisor, bin) {
			return true
		}
		pass.Reportf(bin.Pos(),
			"integer division by %s, a caller-supplied value, with no zero-or-one guard: it panics when 0; compare it against 0 (or 1) before dividing",
			types.ExprString(divisor))
		return true
	})
}

// guardedBefore reports whether the divisor was compared against 0 or
// 1 in a statement or condition that dominates the division: the same
// block before it, an enclosing block before it, or the condition of
// an enclosing if whose then-branch holds it. A comparison inside the
// body of a nested conditional of an earlier statement runs only when
// that branch is taken and is not a guard.
func guardedBefore(pass *analysis.Pass, body *ast.BlockStmt, parents map[ast.Node]ast.Node, divisor ast.Expr, div *ast.BinaryExpr) bool {
	if spineComparesDivisor(pass, dominance.Spine(divisor), divisor) {
		return true
	}
	for _, stmts := range dominance.Prefix(div, parents, body) {
		for _, st := range stmts {
			if spineComparesDivisor(pass, dominance.Spine(st), divisor) {
				return true
			}
		}
	}
	for _, cond := range dominance.EnclosingIfConds(div, parents, body) {
		if spineComparesDivisor(pass, dominance.Spine(cond), divisor) {
			return true
		}
	}
	return false
}

// spineComparesDivisor reports whether any comparison in nodes matches
// the divisor expression against a constant 0 or 1.
func spineComparesDivisor(pass *analysis.Pass, nodes []ast.Node, divisor ast.Expr) bool {
	for _, n := range nodes {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			continue
		}
		switch bin.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			continue
		}
		var other ast.Expr
		switch {
		case isDivisorExpr(pass, bin.X, divisor):
			other = bin.Y
		case isDivisorExpr(pass, bin.Y, divisor):
			other = bin.X
		default:
			continue
		}
		if isZeroOrOne(pass, other) {
			return true
		}
	}
	return false
}

// isDivisorExpr reports whether e spells the divisor expression: the
// same identifier object, or the same printed selector (`q.Limit`).
func isDivisorExpr(pass *analysis.Pass, e ast.Expr, divisor ast.Expr) bool {
	switch d := divisor.(type) {
	case *ast.Ident:
		return isIdentObj(pass, e, pass.TypesInfo.ObjectOf(d))
	case *ast.SelectorExpr:
		ds, ok := e.(*ast.SelectorExpr)
		return ok && types.ExprString(ds) == types.ExprString(d)
	}
	return false
}

// divisorCandidate resolves the divisor to a limit-named function
// parameter, a limit-named struct field, or a local of that name bound
// to a query/param accessor. Constants and len(...) locals are not
// caller-supplied.
func divisorCandidate(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, params map[types.Object]bool) ast.Expr {
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
				return x
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
			return x
		}
		return nil
	case *ast.SelectorExpr:
		// A limit-named field of anything the caller filled in: the
		// decoded-request spelling (`q.Total / q.Limit`).
		obj, ok := pass.TypesInfo.ObjectOf(x.Sel).(*types.Var)
		if ok && obj.IsField() && limitName.MatchString(x.Sel.Name) {
			return x
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
