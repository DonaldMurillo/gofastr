// Package laxcoerce catches a wrong type masquerading as absence: a
// comma-ok type assertion on a map[string]any entry whose failure branch
// returns zero values with a nil error — or continues — as though the key
// had never been sent.
//
// The bug class: MCP tool args / JSON payloads arrive as
// map[string]any, and `v, ok := m[k].(T)` collapses two distinct states —
// key absent, and key present with the wrong JSON type — into one !ok.
// Probe TestFilterTimestampTypeConfusionErr found it in battery/log
// mcp.go timeParam (fixed in 4b7a25d2): an agent filtering logs by a
// numeric `since_ts` got the !ok branch, which read as "no filter
// supplied", so the response quietly contained the unfiltered window
// while the agent believed it had narrowed the search.
//
// Silent postures, deliberately:
//   - a !ok branch that returns or assigns a value of error type: the
//     wrong type is surfaced, not swallowed (the fix posture);
//   - the function already separates presence from type with a comma-ok
//     map index (`v, present := m[k]`) on the same map;
//   - maps whose element type is not any/empty interface — a typed map
//     cannot hold a wrong type, so !ok genuinely means absent;
//   - zero returns in functions with no error result: no channel exists
//     to surface the problem, and the shape here is the nil error that
//     says "fine" when it is not fine;
//   - _test.go files.
package laxcoerce

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrlaxcoerce",
	Doc:  "forbids treating a failed type assertion on a map[string]any entry as absence; check presence separately or return an error",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				checkFunc(pass, fn.Type, fn.Body)
			case *ast.FuncLit:
				checkFunc(pass, fn.Type, fn.Body)
			}
			return true
		})
	}
	return nil, nil
}

// checkFunc examines one function body.
func checkFunc(pass *analysis.Pass, fnType *ast.FuncType, body *ast.BlockStmt) {
	if body == nil {
		return
	}
	hasErrorResult := false
	if fnType.Results != nil {
		for _, res := range fnType.Results.List {
			if t := pass.TypesInfo.TypeOf(res.Type); t != nil &&
				(types.Identical(t, errorIface) || types.Implements(t, errorIface)) {
				hasErrorResult = true
			}
		}
	}
	// Locals bound straight to a map index (`v := m[k]`), so an assert on
	// the local resolves back to the map access.
	bound := map[types.Object]ast.Expr{}
	// Maps that already get a comma-ok index (`v, present := m[k]`)
	// somewhere in this function: presence is separated from type there.
	present := map[string]bool{}

	var asserts []*ast.AssignStmt
	var assertOK []types.Object

	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		if len(st.Lhs) == 1 && len(st.Rhs) == 1 {
			if id, ok := st.Lhs[0].(*ast.Ident); ok {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					bound[obj] = st.Rhs[0]
				}
			}
			return true
		}
		if len(st.Lhs) != 2 || len(st.Rhs) != 1 {
			return true
		}
		switch rhs := st.Rhs[0].(type) {
		case *ast.IndexExpr:
			if m := mapOperand(pass, rhs.X); m != "" {
				present[m] = true
			}
		case *ast.TypeAssertExpr:
			asserts = append(asserts, st)
			assertOK = append(assertOK, okVar(pass, st.Lhs[1]))
		}
		return true
	})

	for i, st := range asserts {
		assert := st.Rhs[0].(*ast.TypeAssertExpr)
		operand := assert.X
		if id, ok := operand.(*ast.Ident); ok {
			if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok {
				operand = b
			}
		}
		idx, ok := operand.(*ast.IndexExpr)
		if !ok {
			continue
		}
		m := mapOperand(pass, idx.X)
		if m == "" || present[m] {
			continue
		}
		if assertOK[i] == nil {
			continue
		}
		for _, br := range notOKBranches(pass, body, assertOK[i]) {
			if branchIsLax(pass, br, hasErrorResult) {
				pass.Reportf(st.Pos(),
					"type assertion on %s treated as absence: a key present with the wrong type falls into the not-found branch and silently drops the caller's input; separate presence (v, present := m[k]) or return an error",
					m)
				break
			}
		}
	}
}

// mapOperand returns a stable identity for a map[string]any expression —
// its printed form — or "" when the expression is not such a map. Only
// maps whose element type is any/empty interface can hold a wrong type;
// for every other map !ok really does mean the key is absent.
func mapOperand(pass *analysis.Pass, e ast.Expr) string {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return ""
	}
	mt, ok := t.Underlying().(*types.Map)
	if !ok {
		return ""
	}
	if !types.Identical(mt.Key(), types.Typ[types.String]) {
		return ""
	}
	iface, ok := mt.Elem().Underlying().(*types.Interface)
	if !ok || iface.NumMethods() != 0 {
		return ""
	}
	return types.ExprString(e)
}

// okVar resolves the comma-ok variable of an assignment, if it is a
// named local.
func okVar(pass *analysis.Pass, e ast.Expr) types.Object {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(id)
	if obj == nil {
		return nil
	}
	if _, ok := obj.(*types.Var); !ok {
		return nil
	}
	return obj
}

// notOKBranches returns the branch bodies that execute when the comma-ok
// variable is false: the then-branch of `if !ok ...` (in any boolean
// combination), and the else-branch of a bare `if ok`.
func notOKBranches(pass *analysis.Pass, body *ast.BlockStmt, okObj types.Object) []*ast.BlockStmt {
	var out []*ast.BlockStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if st, ok := n.(*ast.IfStmt); ok {
			if mentionsNotOK(pass, st.Cond, okObj) {
				out = append(out, st.Body)
			}
			if st.Else != nil && isIdentObj(pass, st.Cond, okObj) {
				if els, ok := st.Else.(*ast.BlockStmt); ok {
					out = append(out, els)
				}
			}
		}
		return true
	})
	return out
}

// mentionsNotOK reports whether the condition is, or contains as a
// conjunct or disjunct, `!ok` or `ok == false`.
func mentionsNotOK(pass *analysis.Pass, cond ast.Expr, okObj types.Object) bool {
	switch c := cond.(type) {
	case *ast.UnaryExpr:
		return c.Op == token.NOT && isIdentObj(pass, c.X, okObj)
	case *ast.BinaryExpr:
		if mentionsNotOK(pass, c.X, okObj) || mentionsNotOK(pass, c.Y, okObj) {
			return true
		}
		if c.Op == token.EQL {
			if isIdentObj(pass, c.X, okObj) && isFalse(c.Y) {
				return true
			}
			if isIdentObj(pass, c.Y, okObj) && isFalse(c.X) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return mentionsNotOK(pass, c.X, okObj)
	default:
		return false
	}
}

func isIdentObj(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == obj
}

func isFalse(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "false"
}

// branchIsLax reports whether the !ok branch silently swallows the wrong
// type: it continues, or (in a function with an error result to speak
// through) returns without any operand carrying an error.
func branchIsLax(pass *analysis.Pass, br *ast.BlockStmt, hasErrorResult bool) bool {
	lax := false
	ast.Inspect(br, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.BranchStmt:
			if st.Tok == token.CONTINUE {
				lax = true
			}
		case *ast.ReturnStmt:
			if !hasErrorResult {
				return true
			}
			if len(st.Results) == 0 {
				// Bare return: the named error result holds whatever the
				// branch left it, which is nil unless an error was
				// assigned before the return.
				lax = true
			}
			for _, res := range st.Results {
				if carriesError(pass, res) {
					return false // error surfaced: not lax
				}
			}
			if len(st.Results) > 0 {
				lax = true
			}
		case *ast.AssignStmt:
			for _, rhs := range st.Rhs {
				if carriesError(pass, rhs) {
					return false // an error is recorded: not lax
				}
			}
		}
		return true
	})
	return lax
}

// carriesError reports whether e is a non-nil value of error type: the
// wrong type is being surfaced rather than swallowed.
func carriesError(pass *analysis.Pass, e ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok && id.Name == "nil" {
		return false
	}
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	return types.Implements(t, errorIface) || types.Identical(t, errorIface)
}

var errorIface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
