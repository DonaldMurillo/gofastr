// Package intwrap catches the two integer-wrap postures that slip past
// range checks: unsigned→signed conversion without a bound, and unary
// negation of MinInt inside an abs.
//
// Shape A — `int64(x)` where x is a wide unsigned: the conversion wraps
// out-of-range values negative, so a validator that checks only
// `n <= Max` accepts uint64(MaxUint64) read as -1. Probe
// TestIntUintWrapBypassesMaxBound found it in core/schema validate.go
// toInt64's uint case (fixed in b79942f7): the uint64 arm had the
// overflow check, the uint arm — the same width on 64-bit — had none.
//
// Shape B — `-x` in a function whose name or doc says abs: |MinInt64|
// is MaxInt64+1, which does not fit, so -v wraps back to MinInt64 and
// abs returns a negative. Probe TestAbsNeverReturnsNegative found it in
// kiln/expr env.go builtinAbs (fixed in f06f4412), where the saturated
// fix also errs away from every threshold guard consuming abs().
//
// A bound counts only when it proves the subject inside the safe range
// on the REACHING path (review 6): a diverging guard — an earlier if
// whose condition, when false, leaves the subject in range (`u >
// math.MaxInt64`, `v == math.MinInt64`) and whose then-branch never
// completes normally (return, panic, continue, os.Exit) — or the
// condition of an enclosing if whose then-branch holds the node, where
// the condition HELD and must establish the range (`u <=
// math.MaxInt64`). A check in a sibling case arm of the same type
// switch guards nothing — that is exactly how the uint arm shipped
// without the check its uint64 sibling had — and neither does a check
// inside a flag-gated nested body of an earlier statement, a branch
// that merely flags the out-of-range values, or `if u <=
// math.MaxInt64 { return }`, which leaves exactly the wrapping values
// on the continuing path.
//
// Silent postures, deliberately:
//   - uint8/uint16 sources: they fit every signed target this rule
//     names, and uint32 → int64/int fits too (width compared, not
//     family);
//   - constants: the compiler rejects out-of-range constant conversions;
//   - any dominating comparison against math.MaxInt/MaxInt32/MaxInt64
//     (shape A) or math.MinInt/MinInt64 (shape B), or a bound-sized
//     integer literal;
//   - negation in functions that are not abs-shaped, and float
//     negation (no wrap);
//   - _test.go files.
//
// Narrowed 2026-09-02 after the whole-repo run: both shapes fire only
// when the operand is unboxed from an any/empty-interface — a case
// variable of a type switch over one, or the value variable of a
// comma-ok assertion from one (`if u, ok := box.(uint64); ok`) — the
// genuinely unbounded sources. Every other hit in the repo was
// semantically bounded by its caller (Unix seconds, read-capped frame
// lengths, float exponents ≤ 324, counter values, ULID timestamps ≤
// 2^48, generated hex ids), and the only unbounded one — core/i18n's
// JSON number coercion — was any-boxed, exactly the toInt64 shape.
package intwrap

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
)

var Analyzer = &analysis.Analyzer{
	Name: "intwrap",
	Doc:  "forbids unsigned→signed conversion and MinInt negation without a dominating bound check; wrapped values read negative and slip past Max-only guards",
	Run:  run,
}

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
			parents := dominance.Parents(fn.Body)
			abs := isAbsFunc(fn)
			anyVars := unboundedSources(pass, fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					checkUnsignedConv(pass, x, parents, fn.Body, anyVars)
				case *ast.UnaryExpr:
					if abs && x.Op == token.SUB {
						checkAbsNeg(pass, x, parents, fn.Body, anyVars)
					}
				}
				return true
			})
		}
	}
	return nil, nil
}

// checkUnsignedConv reports int/int32/int64(x) where x is an unsigned
// variable at least as wide as the target.
func checkUnsignedConv(pass *analysis.Pass, call *ast.CallExpr, parents map[ast.Node]ast.Node, body *ast.BlockStmt, anyVars map[types.Object]bool) {
	id, ok := call.Fun.(*ast.Ident)
	if !ok || len(call.Args) != 1 {
		return
	}
	tv, ok := pass.TypesInfo.Types[id]
	if !ok || !tv.IsType() {
		return // a function call, not a conversion
	}
	target, ok := tv.Type.(*types.Basic)
	if !ok {
		return
	}
	var tgtWidth int64
	switch target.Kind() {
	case types.Int:
		tgtWidth = 64 // this repo targets 64-bit platforms
	case types.Int32:
		tgtWidth = 32
	case types.Int64:
		tgtWidth = 64
	default:
		return
	}
	arg, ok := call.Args[0].(*ast.Ident)
	if !ok {
		return
	}
	obj := pass.TypesInfo.ObjectOf(arg)
	if obj == nil {
		return
	}
	if _, isConst := obj.(*types.Const); isConst {
		return // the compiler rejects out-of-range constant conversions
	}
	if !anyVars[obj] {
		return // bounded source: narrowed 2026-09-02, see unboundedSources
	}
	src, ok := pass.TypesInfo.TypeOf(arg).Underlying().(*types.Basic)
	if !ok || src.Info()&types.IsUnsigned == 0 {
		return
	}
	width := srcWidth(src)
	if width < 32 || tgtWidth > width {
		return // uint8/uint16 fit; so does uint32 → int64
	}
	if dominatingBound(pass, call, parents, body, subjectOf(pass, arg), boundMax) {
		return
	}
	pass.Reportf(call.Pos(),
		"conversion %s → %s without a dominating bound check: out-of-range values wrap negative and slip past Max-only guards; compare against math.Max%s first",
		src.Name(), target.Name(), maxSuffix(tgtWidth))
}

// checkAbsNeg reports -x on a signed integer in an abs-shaped function.
func checkAbsNeg(pass *analysis.Pass, un *ast.UnaryExpr, parents map[ast.Node]ast.Node, body *ast.BlockStmt, anyVars map[types.Object]bool) {
	if tv, ok := pass.TypesInfo.Types[un.X]; ok && tv.Value != nil {
		return // constant negation folds at compile time: no wrap possible
	}
	t := pass.TypesInfo.TypeOf(un.X)
	if t == nil {
		return
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok || b.Info()&types.IsInteger == 0 || b.Info()&types.IsUnsigned != 0 {
		return // float negation and unsigned math cannot wrap here
	}
	subj := subjectOf(pass, un.X)
	if subj.str == "" || subj.obj == nil || !anyVars[subj.obj] {
		return // narrowed 2026-09-02 to unbounded sources; see unboundedSources
	}
	if dominatingBound(pass, un, parents, body, subj, boundMin) {
		return
	}
	pass.Reportf(un.Pos(),
		"negation of %s in an abs without a MinInt check: -MinInt wraps back negative, so abs returns a negative; compare against math.MinInt64 first and saturate",
		subj.str)
}

// subject identifies the value a guard must name: the object for a
// plain identifier, the printed form for a selector (`d.v`), either of
// which is how a bound check spells its operand.
type subject struct {
	obj types.Object
	str string
}

func subjectOf(pass *analysis.Pass, e ast.Expr) subject {
	switch x := e.(type) {
	case *ast.Ident:
		return subject{obj: pass.TypesInfo.ObjectOf(x), str: types.ExprString(x)}
	case *ast.SelectorExpr:
		return subject{str: types.ExprString(x)}
	default:
		return subject{}
	}
}

func matchesSubject(pass *analysis.Pass, e ast.Expr, s subject) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return s.obj != nil && pass.TypesInfo.ObjectOf(x) == s.obj
	case *ast.SelectorExpr:
		return s.str != "" && types.ExprString(x) == s.str
	}
	return false
}

// boundMax/boundMin select which family of guards dominates.
const (
	boundMax = "max"
	boundMin = "min"
)

// dominatingBound reports whether node is dominated by a comparison of
// the subject against a bound (review 6 made the guard branch-aware):
//
//   - a diverging guard: an earlier if whose condition, when FALSE,
//
// leaves the subject inside the family's safe range —
// `if u > math.MaxInt64 { return }`, `if v == math.MinInt64 {
// return }` — and whose then-branch never completes normally
// (return, panic, continue, os.Exit): only in-range values
// continue past it;
//   - an enclosing condition: an if whose then-branch holds the node
//
// and whose condition, held TRUE there, establishes the safe
// range — `if u <= math.MaxInt64 { return int64(u), true }`.
//
// Polarity is the whole point: `if u <= math.MaxInt64 { return }`
// leaves exactly the out-of-range values on the continuing path, a
// branch that merely flags (or returns on the in-range values) leaves
// the wrapping values behind, and a comparison inside a nested
// conditional body of an earlier statement (a flag-gated check) or a
// sibling case arm guards nothing — that is exactly how the uint arm
// shipped without the check its uint64 sibling had.
func dominatingBound(pass *analysis.Pass, node ast.Node, parents map[ast.Node]ast.Node, body *ast.BlockStmt, subj subject, family string) bool {
	for _, stmts := range dominance.Prefix(node, parents, body) {
		for _, st := range stmts {
			iff := dominance.IfStmtOf(st)
			if iff == nil || !dominance.Diverges(iff.Body) {
				continue
			}
			if condBounds(pass, iff.Cond, subj, family, false) {
				return true
			}
		}
	}
	for _, cond := range dominance.EnclosingIfConds(node, parents, body) {
		if condBounds(pass, cond, subj, family, true) {
			return true
		}
	}
	return false
}

// condBounds walks a condition's operator tree and reports whether it
// proves the subject inside the safe range: when holds, the condition
// itself held on the reaching path; when !holds, it failed and the
// other side of the branch is what runs. For && held, any one operand
// suffices (A && B held means both held); failed, ¬(A && B) is ¬A ∨
// ¬B — which operand failed is unknown, so EVERY operand's failure
// must prove it (`if u > math.MaxInt64 && strict { return }` with
// strict false still converts the out-of-range u). For || held, which
// operand is unknown, so every operand must prove it; failed, ¬(A ∨ B)
// is ¬A ∧ ¬B — both failed, so any one operand's failure proves it.
func condBounds(pass *analysis.Pass, cond ast.Expr, subj subject, family string, holds bool) bool {
	switch x := cond.(type) {
	case *ast.ParenExpr:
		return condBounds(pass, x.X, subj, family, holds)
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return condBounds(pass, x.X, subj, family, !holds)
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND:
			if holds {
				return condBounds(pass, x.X, subj, family, true) || condBounds(pass, x.Y, subj, family, true)
			}
			return condBounds(pass, x.X, subj, family, false) && condBounds(pass, x.Y, subj, family, false)
		case token.LOR:
			if holds {
				return condBounds(pass, x.X, subj, family, true) && condBounds(pass, x.Y, subj, family, true)
			}
			return condBounds(pass, x.X, subj, family, false) || condBounds(pass, x.Y, subj, family, false)
		}
		return comparisonBounds(pass, x, subj, family, holds)
	}
	return false
}

// comparisonBounds reports whether `subj op bound` proves the subject
// inside the family's safe range on the given truth value. The bound
// operand is a math constant of the family or a bound-sized literal,
// exactly as before.
func comparisonBounds(pass *analysis.Pass, bin *ast.BinaryExpr, subj subject, family string, holds bool) bool {
	if !isComparison(bin.Op) {
		return false
	}
	var other ast.Expr
	subjectOnLeft := matchesSubject(pass, bin.X, subj)
	switch {
	case subjectOnLeft:
		other = bin.Y
	case matchesSubject(pass, bin.Y, subj):
		other = bin.X
	default:
		return false
	}
	if !isMathBound(pass, other, family) && !isBoundLiteral(pass, other, family) {
		return false
	}
	op := bin.Op
	if !subjectOnLeft {
		op = flipComparison(op)
	}
	whenTrue, whenFalse := boundSide(pass, op, other, family)
	if holds {
		return whenTrue
	}
	return whenFalse
}

// boundSide reports for `subj op bound` (subject on the left, op
// normalized) which truth value of the comparison proves the subject
// inside the safe range. For boundMax (conversion safety: subj ≤ Max,
// and every recognized bound is ≤ MaxInt64): holding, the subject is
// below or at the bound (<, <=, ==); failing, the subject is at most
// the bound (> , >= fail to ≤/<, != fails to ==). For boundMin
// (negation safety: subj > Min, a signed subject is always ≥ Min):
// holding, strictly above the bound (>); failing, above it (<=), or —
// only when the bound IS the exact minimum — not equal to it (==),
// since a signed value that is not MinInt64 is above it.
func boundSide(pass *analysis.Pass, op token.Token, bound ast.Expr, family string) (whenTrue, whenFalse bool) {
	if family == boundMax {
		switch op {
		case token.LSS, token.LEQ, token.EQL:
			return true, false
		case token.GTR, token.GEQ, token.NEQ:
			return false, true
		}
		return false, false
	}
	switch op {
	case token.GTR:
		return true, false
	case token.LEQ:
		return false, true
	case token.EQL:
		// ¬(v == MinInt64) proves v > MinInt64 only for the exact
		// minimum: a signed v is always ≥ it. A larger literal bound
		// (isBoundLiteral accepts any ≤ MinInt32) still admits MinInt64
		// on the continuing path.
		return false, isExactMinBound(pass, bound)
	}
	return false, false
}

// isExactMinBound reports whether e is exactly the 64-bit minimum:
// the math.MinInt/MinInt64 constants (on the 64-bit platforms this
// repo targets), or that value spelled as a literal.
func isExactMinBound(pass *analysis.Pass, e ast.Expr) bool {
	if isMathBound(pass, e, boundMin) {
		return true
	}
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return false
	}
	i, ok := constant.Int64Val(tv.Value)
	return ok && i == math.MinInt64
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

func isMathBound(pass *analysis.Pass, e ast.Expr, family string) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := pass.TypesInfo.ObjectOf(x).(*types.PkgName)
	if !ok || pn.Imported().Path() != "math" {
		return false
	}
	if family == boundMax {
		switch sel.Sel.Name {
		case "MaxInt", "MaxInt32", "MaxInt64":
			return true
		}
		return false
	}
	switch sel.Sel.Name {
	case "MinInt", "MinInt64":
		return true
	}
	return false
}

// isBoundLiteral accepts any integer literal of bound size: at least
// MaxInt32 for upper bounds, at most MinInt32 for lower ones.
func isBoundLiteral(pass *analysis.Pass, e ast.Expr, family string) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return false
	}
	i, ok := constant.Int64Val(tv.Value)
	if !ok {
		return false
	}
	if family == boundMax {
		return i >= 1<<31
	}
	return i <= -(1 << 31)
}

// isAbsFunc reports whether the function's name or doc says abs.
func isAbsFunc(fn *ast.FuncDecl) bool {
	name := strings.ToLower(fn.Name.Name)
	if strings.Contains(name, "abs") {
		return true
	}
	return fn.Doc != nil && strings.Contains(strings.ToLower(fn.Doc.Text()), "abs")
}

func srcWidth(b *types.Basic) int64 {
	switch b.Kind() {
	case types.Uint8:
		return 8
	case types.Uint16:
		return 16
	case types.Uint32:
		return 32
	case types.Uint, types.Uint64:
		return 64 // uint is 64-bit on every platform this repo ships
	default:
		return 0
	}
}

func maxSuffix(width int64) string {
	if width == 32 {
		return "Int32"
	}
	return "Int64"
}

func isComparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

func isIdentObj(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == obj
}

// unboundedSources collects the objects of the genuinely unbounded
// operands: case variables of a type switch over an any/empty-interface
// tag (anySwitchVars), and the value variables of comma-ok assertions
// from one (anyAssertVars) — the same JSON-box value obtained with
// `if u, ok := box.(uint64); ok` instead of a switch arm. A conversion
// or negation of a plain int parameter is usually semantically bounded
// by its caller (Unix seconds, frame lengths under a read cap, float
// exponents ≤ 324), and the 2026-09-02 whole-repo run measured exactly
// that: every non-any-boxed hit was bounded, the only unbounded one
// (core/i18n's JSON number coercion) was any-boxed.
func unboundedSources(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := anySwitchVars(pass, body)
	for obj := range anyAssertVars(pass, body) {
		out[obj] = true
	}
	return out
}

// anyAssertVars collects the value variables of comma-ok type
// assertions whose operand has an empty-interface type.
func anyAssertVars(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Lhs) != 2 || len(st.Rhs) != 1 {
			return true
		}
		ta, ok := st.Rhs[0].(*ast.TypeAssertExpr)
		if !ok || ta.Type == nil {
			return true // `x.(type)` or a single-value assert
		}
		if t := pass.TypesInfo.TypeOf(ta.X); t == nil {
			return true
		} else if iface, ok := t.Underlying().(*types.Interface); !ok || iface.NumMethods() != 0 {
			return true
		}
		if id, ok := st.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				out[obj] = true
			}
		}
		return true
	})
	return out
}

// anySwitchVars collects the objects of case variables whose type
// switch ranges over an any/empty-interface tag — the unbounded
// sources this rule cares about. A conversion or negation of a plain
// int parameter is usually semantically bounded by its caller (Unix
// seconds, frame lengths under a read cap, float exponents ≤ 324), and
// the 2026-09-02 whole-repo run measured exactly that: every
// non-any-boxed hit was bounded, the only unbounded one (core/i18n's
// JSON number coercion) was any-boxed. Narrowed here deliberately.
func anySwitchVars(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSwitchStmt)
		if !ok {
			return true
		}
		assign, ok := ts.Assign.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 {
			return true
		}
		ta, ok := assign.Rhs[0].(*ast.TypeAssertExpr)
		if !ok || ta.Type != nil {
			return true // `switch v.(type)`, not `switch v.(T)`
		}
		if t := pass.TypesInfo.TypeOf(ta.X); t == nil {
			return true
		} else if iface, ok := t.Underlying().(*types.Interface); !ok || iface.NumMethods() != 0 {
			return true
		}
		switchName := ""
		if lhs := assign.Lhs; len(lhs) == 1 {
			if id, ok := lhs[0].(*ast.Ident); ok && id.Name != "_" {
				switchName = id.Name
			}
		}
		if switchName == "" {
			return true // `switch v.(type)` with no case variable
		}
		for _, cc := range ts.Body.List {
			clause, ok := cc.(*ast.CaseClause)
			if !ok || len(clause.List) != 1 {
				continue // multi-type arms keep the interface type
			}
			caseType := pass.TypesInfo.TypeOf(clause.List[0])
			if caseType == nil {
				continue
			}
			ast.Inspect(clause, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || id.Name != switchName {
					return true
				}
				obj := pass.TypesInfo.ObjectOf(id)
				if obj == nil {
					return true
				}
				if t := pass.TypesInfo.TypeOf(id); t != nil && types.Identical(t, caseType) {
					out[obj] = true
				}
				return true
			})
		}
		return true
	})
	return out
}
