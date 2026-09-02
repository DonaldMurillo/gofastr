// Package discardederr catches a multi-value assignment that drops an
// error on the floor while keeping the values around it: `ch, cancel, _
// := m.subscribeImpl(id)`.
//
// The bug class: the discarded error is usually the only signal that
// the call REFUSED, and the zero values handed back in its place are
// usable enough to march on with — a nil channel that never delivers,
// a no-op cancel, an empty list that reads as "nothing found". Probe
// TestSubscribeCapRefusalObservable found it in core-ui/island
// manager.go Subscribe (fixed in e936f791): a stream-cap refusal was
// discarded, the documented `for upd := range ch` consume pattern then
// hung forever on a channel that would never deliver and never close.
//
// Silent postures, deliberately:
//   - `_ = f()` and `_, _ = f()`: discarding everything is a
//     statement, and an idiomatic one;
//   - assignments where no kept value is ever used: nothing marches on
//     with the refusal hidden;
//   - _test.go files.
//
// Narrowed 2026-09-02 after the whole-repo run measured 165 findings
// for the broad shape: fire only when the discarded error is the LAST
// result and the call is a method on a receiver — the subscribeImpl
// posture, where the error is a refusal back-channel beside usable
// results. Package-function drops (strconv.Atoi and friends) are not
// this rule's to police.
package discardederr

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrdiscardederr",
	Doc:  "forbids discarding an error in a multi-value assignment while keeping the other results; the refusal it signals is silently swallowed",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var body *ast.BlockStmt
			switch fn := n.(type) {
			case *ast.FuncDecl:
				body = fn.Body
			case *ast.FuncLit:
				body = fn.Body
			default:
				return true
			}
			if body == nil {
				return true
			}
			checkFunc(pass, body)
			return true
		})
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Lhs) < 2 || len(st.Rhs) != 1 {
			return true
		}
		call, ok := st.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		tuple, ok := pass.TypesInfo.TypeOf(call).(*types.Tuple)
		if !ok || tuple.Len() != len(st.Lhs) {
			return true
		}
		blankAt := -1
		for i, lhs := range st.Lhs {
			if id, ok := lhs.(*ast.Ident); ok && id.Name == "_" {
				if isError(tuple.At(i).Type()) {
					blankAt = i
				}
			}
		}
		if blankAt == -1 {
			return true
		}
		// Narrowed 2026-09-02 after the whole-repo run measured 165
		// findings for the broad shape: fire only when the discarded
		// error is the LAST result and the call is a method on a
		// receiver.
		if blankAt != len(st.Lhs)-1 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || isPkgName(pass, sel.X) {
			return true
		}
		// And the call must hand back a RESOURCE with its cleanup, not a
		// plain (value, error): 2-result drops degrade to an observable
		// zero value (stats panels, catalogs, counts), while dropping the
		// refusal beside a channel+cancel pair leaves a consumer waiting
		// on something that will never deliver. Measured 2026-09-02:
		// 165 broad, 33 at last+method (25 of them the RowsAffected
		// accessor after an already-checked Exec), 0 at ≥3 results.
		if tuple.Len() < 3 {
			return true
		}
		if !keepsUsedValue(pass, body, st) {
			return true
		}
		pass.Reportf(st.Pos(),
			"assignment discards the error from %s while keeping its other results: the refusal it signals is silently swallowed; handle it or surface it",
			calleeName(call))
		return true
	})
}

// keepsUsedValue reports whether at least one non-blank left side is
// used after the assignment — the posture where code marches on with
// the refusal hidden.
func keepsUsedValue(pass *analysis.Pass, body *ast.BlockStmt, st *ast.AssignStmt) bool {
	var kept []types.Object
	for _, lhs := range st.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				kept = append(kept, obj)
			}
		}
	}
	if len(kept) == 0 {
		return false
	}
	// `_ = v` is a discard, not a use: a value explicitly silenced is
	// not "marching on" with the refusal hidden.
	discarded := map[token.Pos]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if d, ok := n.(*ast.AssignStmt); ok && len(d.Lhs) == 1 && len(d.Rhs) == 1 {
			if id, ok := d.Lhs[0].(*ast.Ident); ok && id.Name == "_" {
				if v, ok := d.Rhs[0].(*ast.Ident); ok {
					discarded[v.Pos()] = true
				}
			}
		}
		return true
	})
	used := false
	ast.Inspect(body, func(n ast.Node) bool {
		if used {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && !discarded[id.Pos()] {
			for _, obj := range kept {
				if pass.TypesInfo.ObjectOf(id) == obj && id.Pos() != obj.Pos() {
					used = true
					return false
				}
			}
		}
		return true
	})
	return used
}

// isError reports whether t is or implements error.
func isError(t types.Type) bool {
	if t == nil {
		return false
	}
	return types.Identical(t, errorIface) || types.Implements(t, errorIface)
}

func calleeName(call *ast.CallExpr) string {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name + "()"
	}
	return "call"
}

var errorIface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

// isPkgName reports whether e names an imported package (a qualified
// function call rather than a method on a receiver).
func isPkgName(pass *analysis.Pass, e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	_, ok = pass.TypesInfo.ObjectOf(id).(*types.PkgName)
	return ok
}
