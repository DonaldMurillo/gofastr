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
// A bound counts only when it DOMINATES the conversion: it must sit in
// the same block, or an enclosing block, before the statement holding
// the conversion. A check in a sibling case arm of the same type switch
// guards nothing — that is exactly how the uint arm shipped without the
// check its uint64 sibling had.
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
// when the operand is a case variable of a type switch over an
// any/empty-interface tag — the genuinely unbounded source. Every
// other hit in the repo was semantically bounded by its caller (Unix
// seconds, read-capped frame lengths, float exponents ≤ 324, counter
// values, ULID timestamps ≤ 2^48, generated hex ids), and the only
// unbounded one — core/i18n's JSON number coercion — was any-boxed,
// exactly the toInt64 shape.
package intwrap

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrintwrap",
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
			parents := recordParents(fn.Body)
			abs := isAbsFunc(fn)
			anyVars := anySwitchVars(pass, fn.Body)
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
		return // bounded source: narrowed 2026-09-02, see anySwitchVars
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
		return // narrowed 2026-09-02 to unbounded sources; see anySwitchVars
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
// obj against a bound: the comparison must sit in the same block as the
// node, or an enclosing block, in a statement before the one holding
// the node. Sibling branches (other case arms) guard nothing.
func dominatingBound(pass *analysis.Pass, node ast.Node, parents map[ast.Node]ast.Node, body *ast.BlockStmt, subj subject, family string) bool {
	for _, stmts := range dominatingPrefix(node, parents, body) {
		for _, st := range stmts {
			if stmtBounds(pass, st, subj, family) {
				return true
			}
		}
	}
	return false
}

// dominatingPrefix returns the statement lists that dominate node: for
// each enclosing block (BlockStmt / case or comm clause), the
// statements before the one on the path down to node.
func dominatingPrefix(node ast.Node, parents map[ast.Node]ast.Node, body *ast.BlockStmt) [][]ast.Stmt {
	var out [][]ast.Stmt
	cur := node
	for {
		p, ok := parents[cur]
		if !ok || p == nil {
			break
		}
		var stmts []ast.Stmt
		switch b := p.(type) {
		case *ast.BlockStmt:
			stmts = b.List
		case *ast.CaseClause:
			stmts = b.Body
		case *ast.CommClause:
			stmts = b.Body
		}
		if stmts != nil {
			for i, st := range stmts {
				if st == cur || containsPath(st, node, parents) {
					out = append(out, stmts[:i])
					break
				}
			}
		}
		if p == body {
			break
		}
		cur = p
	}
	return out
}

// containsPath reports whether node sits inside st per the parent map,
// for when the path child is nested below statement level.
func containsPath(st ast.Node, node ast.Node, parents map[ast.Node]ast.Node) bool {
	for c := node; c != nil; {
		p, ok := parents[c]
		if !ok || p == nil {
			return false
		}
		if p == st {
			return true
		}
		c = p
	}
	return false
}

// stmtBounds reports whether st compares obj against the bound family:
// math.MaxInt/MaxInt32/MaxInt64 or math.MinInt/MinInt64 (resolved by
// import path, not spelling), or an integer literal of bound size.
func stmtBounds(pass *analysis.Pass, st ast.Stmt, subj subject, family string) bool {
	found := false
	ast.Inspect(st, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || !isComparison(bin.Op) {
			return true
		}
		var other ast.Expr
		switch {
		case matchesSubject(pass, bin.X, subj):
			other = bin.Y
		case matchesSubject(pass, bin.Y, subj):
			other = bin.X
		default:
			return true
		}
		if isMathBound(pass, other, family) || isBoundLiteral(pass, other, family) {
			found = true
		}
		return true
	})
	return found
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

// recordParents maps every node in the body to its parent node.
func recordParents(body ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var visit func(n ast.Node)
	visit = func(n ast.Node) {
		if n == nil {
			return
		}
		ast.Inspect(n, func(c ast.Node) bool {
			if c == n {
				return true
			}
			if c != nil {
				parents[c] = n
				visit(c)
			}
			return false
		})
	}
	visit(body)
	return parents
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
