// Package bareduration catches a bare integer handed to a
// time.Duration: `Every(5)` is five nanoseconds, not five seconds.
//
// The bug: an untyped integer constant converts to time.Duration
// implicitly, so `s.Every(5)`, `Config{Timeout: 30}` and `return 10`
// from a func returning time.Duration all compile, and every one of
// them is a count of NANOSECONDS. Found by the 2026-11 bug bash round 2
// (7615b2bd2): the in-memory queue.Scheduler took Every(5), its
// tickInterval became 5ns, and Start's timer loop enqueued a job per
// iteration, over 12,000 in 20ms into whatever queue it fed. The fix
// there refused a sub-millisecond interval at Register; this rule
// catches the CALLER that wrote the bare count. staticcheck's SA1004
// covers time.Sleep only.
//
// It fires on a constant expression of type time.Duration whose value
// is a positive integer and whose every leaf is an integer literal or a
// named UNTYPED constant (`const pollSeconds = 5`), when that
// expression is the whole of a call argument, a composite-literal
// element or field value, an assignment's right-hand side, a return
// result, a var declaration's value, or a channel send.
//
// Silent postures, deliberately:
//   - 0: zero nanoseconds and zero seconds are the same value, and 0 is
//     how this repo spells "unset, take the default";
//   - negative values: -1 is a sentinel ("disabled", "no timeout"), not
//     a unit mistake;
//   - a product or sum with a typed operand (`5 * time.Second`,
//     `d + 5`) and compound assignment (`d *= 2`, core/backoff's
//     doubling): the untyped operand is a multiplier or an offset, not
//     the value;
//   - a named Duration constant (`const tick time.Duration = 5`, or
//     `const tick = 5 * time.Millisecond`) at its use, and the const
//     declaration itself: an explicit time.Duration type on a named
//     constant is a choice the declaration states;
//   - an explicit `time.Duration(n)` conversion: the reader sees the
//     unit is the type's own, nanoseconds;
//   - math.Max*/Min* constants: `math.MaxInt64` is "forever", a
//     sentinel, not a count;
//   - _test.go files: fixtures pass tiny durations on purpose.
//
// The fix spelling multiplies by the unit the author meant:
// `Every(5 * time.Second)`.
package bareduration

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "bareduration",
	Doc:  "forbids a bare positive integer constant implicitly converted to time.Duration (Every(5) is five nanoseconds); multiply by the unit meant (5 * time.Second) or convert explicitly",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if tv, ok := pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() {
					return true // explicit conversion: the reader sees the unit
				}
				for _, a := range x.Args {
					check(pass, a)
				}
			case *ast.CompositeLit:
				for _, e := range x.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						check(pass, kv.Value)
						continue
					}
					check(pass, e)
				}
			case *ast.AssignStmt:
				// `d *= 2` and `d += 5` are arithmetic on a typed value: the
				// constant is a multiplier or an offset, never the value.
				if x.Tok == token.ASSIGN && len(x.Lhs) == len(x.Rhs) {
					for _, r := range x.Rhs {
						check(pass, r)
					}
				}
			case *ast.ReturnStmt:
				for _, r := range x.Results {
					check(pass, r)
				}
			case *ast.SendStmt:
				check(pass, x.Value)
			case *ast.GenDecl:
				if x.Tok == token.CONST {
					return false // a typed const declaration names its type
				}
			case *ast.ValueSpec:
				for _, v := range x.Values {
					check(pass, v)
				}
			}
			return true
		})
	}
	return nil, nil
}

// check reports e when it is a bare positive integer constant whose
// recorded type is time.Duration.
func check(pass *analysis.Pass, e ast.Expr) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || !astx.IsNamed(tv.Type, "time", "Duration") {
		return
	}
	if tv.Value.Kind() != constant.Int || constant.Sign(tv.Value) <= 0 {
		return // 0 is "unset"; a negative is a sentinel
	}
	if !bareLeaves(pass, e) {
		return
	}
	pass.Reportf(e.Pos(),
		"bare integer %s converted to time.Duration is %s nanoseconds: multiply by the unit meant (%s * time.Second) or convert explicitly with time.Duration(n)",
		types.ExprString(e), tv.Value.ExactString(), types.ExprString(e))
}

// bareLeaves reports whether every leaf of e is an integer literal or a
// named untyped constant from outside package math: no typed operand
// (a time unit, a Duration constant) and no explicit conversion.
func bareLeaves(pass *analysis.Pass, e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.BasicLit:
		return x.Kind == token.INT
	case *ast.UnaryExpr:
		return bareLeaves(pass, x.X)
	case *ast.BinaryExpr:
		return bareLeaves(pass, x.X) && bareLeaves(pass, x.Y)
	case *ast.Ident:
		return untypedConst(pass.TypesInfo.Uses[x])
	case *ast.SelectorExpr:
		obj := pass.TypesInfo.Uses[x.Sel]
		if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "math" {
			return false // math.MaxInt64 is a "forever" sentinel
		}
		return untypedConst(obj)
	}
	return false
}

func untypedConst(obj types.Object) bool {
	c, ok := obj.(*types.Const)
	if !ok {
		return false
	}
	b, ok := c.Type().(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}
