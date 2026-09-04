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
//   - a guard that proves the divisor NONZERO on the reaching path
//
// (review 6): a diverging guard — an earlier if whose condition,
// when false, proves it (`limit == 0`, `limit < 1`, `limit <= 0`)
// and whose then-branch never completes normally (return, panic,
// continue, os.Exit), like `limit <= 0 { return }` — or the
// condition of an enclosing if whose then-branch holds the
// division, where the condition HELD (`limit != 0`, `limit > 0`,
// `limit >= 1`). The polarity matters: `if limit == 0 { return
// total / limit }` divides exactly when the divisor is zero, a
// branch that merely logs or returns on the safe values leaves
// the zero values on the continuing path, and a guard inside a
// nested conditional body of an earlier statement (a flag-gated
// check) executes only when the flag is set and dominates
// nothing;
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
	Name: "divlimit",
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
		if _, isLit := n.(*ast.FuncLit); isLit {
			// Closures are judged by their own checkFunc call from
			// run; descending here reports their divisions twice.
			return false
		}
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

// guardedBefore reports whether the divisor is proven nonzero on every
// path reaching the division (review 6 made the guard branch-aware):
//
//   - a diverging guard: an earlier if whose condition, when FALSE,
//
// proves the divisor nonzero — `limit == 0`, `limit < 1`,
// `limit <= 0` — and whose then-branch never completes normally
// (return, panic, continue, os.Exit): only in-range values
// continue past it;
//   - an enclosing condition: an if whose then-branch holds the
//
// division and whose condition, held TRUE there, proves the
// divisor nonzero — `limit != 0`, `limit > 0`, `limit >= 1`.
//
// Polarity is the whole point: `if limit == 0 { return total/limit }`
// reaches the division exactly when the divisor IS zero, a guard whose
// branch merely logs (or returns on the SAFE values) leaves the zero
// values on the continuing path, and a comparison in an unrelated
// earlier branch of the walk (a flag-gated check) executes only when
// that branch is taken and dominates nothing.
func guardedBefore(pass *analysis.Pass, body *ast.BlockStmt, parents map[ast.Node]ast.Node, divisor ast.Expr, div *ast.BinaryExpr) bool {
	for _, stmts := range dominance.Prefix(div, parents, body) {
		for _, st := range stmts {
			iff := dominance.IfStmtOf(st)
			if iff == nil || !dominance.Diverges(iff.Body) {
				continue
			}
			if condProvesNonzero(pass, iff.Cond, divisor, false) {
				return true
			}
		}
	}
	for _, cond := range dominance.EnclosingIfConds(div, parents, body) {
		if condProvesNonzero(pass, cond, divisor, true) {
			return true
		}
	}
	return false
}

// condProvesNonzero walks a condition's operator tree and reports
// whether it proves the divisor nonzero: when holds, the condition
// itself held on the reaching path; when !holds, it failed and the
// other side of the branch is what runs. For && held, any one operand
// suffices (A && B held means both held); failed, ¬(A && B) is ¬A ∨
// ¬B — which operand failed is unknown, so EVERY operand's failure
// must prove it (`if limit == 0 && strict { return }` with strict
// false still divides by zero). For || held, which operand is unknown,
// so every operand must prove it; failed, ¬(A ∨ B) is ¬A ∧ ¬B — both
// failed, so any one operand's failure proves it.
func condProvesNonzero(pass *analysis.Pass, cond ast.Expr, divisor ast.Expr, holds bool) bool {
	switch x := cond.(type) {
	case *ast.ParenExpr:
		return condProvesNonzero(pass, x.X, divisor, holds)
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return condProvesNonzero(pass, x.X, divisor, !holds)
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND:
			if holds {
				return condProvesNonzero(pass, x.X, divisor, true) || condProvesNonzero(pass, x.Y, divisor, true)
			}
			return condProvesNonzero(pass, x.X, divisor, false) && condProvesNonzero(pass, x.Y, divisor, false)
		case token.LOR:
			if holds {
				return condProvesNonzero(pass, x.X, divisor, true) && condProvesNonzero(pass, x.Y, divisor, true)
			}
			return condProvesNonzero(pass, x.X, divisor, false) || condProvesNonzero(pass, x.Y, divisor, false)
		}
		return comparisonProvesNonzero(pass, x, divisor, holds)
	}
	return false
}

// comparisonProvesNonzero reports whether `divisor op constant` with
// c ∈ {0, 1} proves the divisor nonzero on the given truth value.
func comparisonProvesNonzero(pass *analysis.Pass, bin *ast.BinaryExpr, divisor ast.Expr, holds bool) bool {
	if !isComparison(bin.Op) {
		return false
	}
	var other ast.Expr
	divisorOnLeft := isDivisorExpr(pass, bin.X, divisor)
	switch {
	case divisorOnLeft:
		other = bin.Y
	case isDivisorExpr(pass, bin.Y, divisor):
		other = bin.X
	default:
		return false
	}
	if !isZeroOrOne(pass, other) {
		return false
	}
	c := constValue(pass, other)
	op := bin.Op
	if !divisorOnLeft {
		op = flipComparison(op)
	}
	whenTrue, whenFalse := nonzeroSide(op, c)
	if holds {
		return whenTrue
	}
	return whenFalse
}

// nonzeroSide reports for `divisor op c` (divisor on the left, c ∈
// {0, 1}) which truth value of the comparison proves the divisor
// nonzero: whenTrue for `limit != 0`, `limit > 0`, `limit > 1`,
// `limit >= 1`, `limit == 1`, `limit < 0`; whenFalse for `limit == 0`,
// `limit < 1`, `limit <= 0`, `limit <= 1`, `limit != 1`, `limit >= 0`.
// Every other combination proves neither side (e.g. `limit >= 0` held
// still contains 0, and its failure proves < 0).
func nonzeroSide(op token.Token, c int64) (whenTrue, whenFalse bool) {
	switch op {
	case token.EQL:
		return c != 0, c == 0
	case token.NEQ:
		return c == 0, c != 0
	case token.LSS:
		return c == 0, c == 1 // D < 0 / ¬(D < 1) ⟹ D ≥ 1
	case token.LEQ:
		return false, true // ¬(D ≤ c) ⟹ D ≥ 1 for c ∈ {0, 1}
	case token.GTR:
		return true, false // D > c ⟹ D ≥ 1; ¬(D > c) ⟹ D ≤ c ∋ 0
	case token.GEQ:
		return c == 1, c == 0 // D ≥ 1 / ¬(D ≥ 0) ⟹ D < 0
	}
	return false, false
}

// flipComparison mirrors an operator so the subject can be treated as
// the left operand.
func flipComparison(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.LEQ:
		return token.GEQ
	case token.GTR:
		return token.LSS
	case token.GEQ:
		return token.LEQ
	}
	return op
}

// constValue returns the int64 constant value of e, or ok=false —
// isZeroOrOne's value, split out for nonzeroSide.
func constValue(pass *analysis.Pass, e ast.Expr) int64 {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return -1
	}
	i, ok := constant.Int64Val(tv.Value)
	if !ok {
		return -1
	}
	return i
}

// isDivisorExpr reports whether e is the divisor expression: the same
// identifier object, or a selector with the same receiver ROOT BINDING
// and the same selected field object. Printed-spelling comparison
// (types.ExprString) let a block-shadowed `q.Limit` match an outer
// `q.Limit` comparison and suppress the finding (review 6); binding
// identity cannot.
func isDivisorExpr(pass *analysis.Pass, e ast.Expr, divisor ast.Expr) bool {
	switch d := divisor.(type) {
	case *ast.Ident:
		return isIdentObj(pass, e, pass.TypesInfo.ObjectOf(d))
	case *ast.SelectorExpr:
		ds, ok := e.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		dObj, dOk := selectorFieldObj(pass, d)
		eObj, eOk := selectorFieldObj(pass, ds)
		if !dOk || !eOk || dObj != eObj {
			return false
		}
		dRoot, dHasRoot := selectorRootObj(pass, d)
		eRoot, eHasRoot := selectorRootObj(pass, ds)
		if dHasRoot != eHasRoot {
			return false
		}
		if !dHasRoot {
			// Neither receiver roots in a plain identifier (a call
			// chain): fall back to the printed receiver, which cannot
			// shadow.
			return types.ExprString(ds.X) == types.ExprString(d.X)
		}
		return dRoot == eRoot
	}
	return false
}

// isComparison reports whether op is a comparison operator.
func isComparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

// selectorFieldObj resolves the selected field of a struct selector to
// its *types.Var. Two selectors over the same struct type share the
// field object, so the receiver binding below is what distinguishes
// them.
func selectorFieldObj(pass *analysis.Pass, sel *ast.SelectorExpr) (types.Object, bool) {
	obj := pass.TypesInfo.ObjectOf(sel.Sel)
	if obj == nil {
		return nil, false
	}
	return obj, true
}

// selectorRootObj resolves the receiver's root binding: the object of
// the identifier the member chain starts from (`q` of q.Limit,
// `cfg` of cfg.Q.Limit). A shadowing declaration in an inner scope is
// a different object, so the same printed selector over it is a
// different value. ok=false when the chain does not root in an
// identifier.
func selectorRootObj(pass *analysis.Pass, sel *ast.SelectorExpr) (types.Object, bool) {
	x := sel.X
	for {
		switch v := x.(type) {
		case *ast.Ident:
			obj := pass.TypesInfo.ObjectOf(v)
			return obj, obj != nil
		case *ast.SelectorExpr:
			x = v.X
		case *ast.IndexExpr:
			x = v.X
		default:
			return nil, false
		}
	}
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
