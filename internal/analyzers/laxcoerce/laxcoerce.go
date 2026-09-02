// Package laxcoerce catches a wrong type masquerading as absence: a
// comma-ok type assertion on a map[string]any entry whose failure path
// returns zero values with a nil error — or continues — as though the
// key had never been sent.
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
// The failure path is collected in every spelling: the then-branch of
// `if !ok`, the else of a bare `if ok`, the statements after a bare
// `if ok` with no else (the early-return fall-through), the body of a
// `case !ok:` clause, and the !ok branch that rebinds the asserted
// variable to its zero value and falls through. Each function literal
// is judged by its own pass with its own error-result flag.
//
// Silent postures, deliberately:
//   - a !ok branch that returns or assigns a value of error type: the
//     wrong type is surfaced, not swallowed (the fix posture);
//   - the function already separates presence from type with a comma-ok
//     map index (`v, present := m[k]`) — for a literal key, on the
//     same (map, key); checking one key's presence says nothing about
//     another's, so the silence is per key. For a non-literal key the
//     whole map is taken, the pre-2026-09-02 posture: the function has
//     demonstrated it knows the distinction on this map;
//   - maps whose element type is not any/empty interface — a typed map
//     cannot hold a wrong type, so !ok genuinely means absent;
//   - zero returns in functions with no error result (including
//     closures inside one that has): no channel exists to surface the
//     problem, and the shape here is the nil error that says "fine"
//     when it is not fine;
//   - _test.go files.
package laxcoerce

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
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

// checkFunc examines one function body. Nested function literals are
// cut here and judged by their own invocation from run, with their own
// error-result flag: descending with the outer flag misattributes
// reports (or silences) to a function that has no such channel.
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
	// somewhere in this function: presence is separated from type
	// there. With a literal key the separation is recorded per
	// (map, key) — checking "fmt" says nothing about "region"; with a
	// non-literal key the map itself is recorded, the pre-2026-09-02
	// posture.
	presentByKey := map[presenceKey]bool{}
	presentByMap := map[string]bool{}

	var asserts []*ast.AssignStmt
	var assertOK []types.Object

	ast.Inspect(body, func(n ast.Node) bool {
		if _, isLit := n.(*ast.FuncLit); isLit {
			return false // nested closures get their own checkFunc pass
		}
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
				if key, ok := literalKey(pass, rhs.Index); ok {
					presentByKey[presenceKey{m: m, key: key}] = true
				} else {
					presentByMap[m] = true
				}
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
		if m == "" {
			continue
		}
		if key, isLit := literalKey(pass, idx.Index); isLit {
			if presentByKey[presenceKey{m: m, key: key}] {
				continue
			}
		} else if presentByMap[m] {
			continue
		}
		if assertOK[i] == nil {
			continue
		}
		var asserted types.Object
		if id, ok := st.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			asserted = pass.TypesInfo.ObjectOf(id)
		}
		for _, br := range notOKBranches(pass, body, assertOK[i]) {
			if branchIsLax(pass, br, hasErrorResult, asserted) {
				pass.Reportf(st.Pos(),
					"type assertion on %s treated as absence: a key present with the wrong type falls into the not-found branch and silently drops the caller's input; separate presence (v, present := m[k]) or return an error",
					m)
				break
			}
		}
	}
}

// literalKey returns the value of a string basic literal, or false for
// every other key expression.
func literalKey(pass *analysis.Pass, e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	if tv, ok := pass.TypesInfo.Types[lit]; ok && tv.Value != nil {
		return tv.Value.ExactString(), true
	}
	return lit.Value, true
}

// presenceKey identifies a (map, string-literal key) pair whose
// presence was separated from type.
type presenceKey struct{ m, key string }

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
// combination), the else-branch of a bare `if ok`, the fall-through
// statements after a bare `if ok` with no else, and the body of a
// `case !ok:` / `switch ok { case false: }` clause.
func notOKBranches(pass *analysis.Pass, body *ast.BlockStmt, okObj types.Object) []*ast.BlockStmt {
	parents := dominance.Parents(body)
	var out []*ast.BlockStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.FuncLit:
			return false // closures carry their own ok variables, or none
		case *ast.IfStmt:
			if mentionsNotOK(pass, st.Cond, okObj) {
				out = append(out, st.Body)
			}
			if st.Else != nil && isIdentObj(pass, st.Cond, okObj) {
				if els, ok := st.Else.(*ast.BlockStmt); ok {
					out = append(out, els)
				}
			}
			// A bare `if ok` (or `ok && …`) with no else: the statements
			// after it in its enclosing block are the not-ok path — the
			// early-return layout's other half.
			if st.Else == nil && mentionsOK(pass, st.Cond, okObj) {
				if ft := fallThroughRegion(parents, st); ft != nil {
					out = append(out, ft)
				}
			}
		case *ast.SwitchStmt:
			for _, cc := range st.Body.List {
				clause, ok := cc.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, e := range clause.List {
					if mentionsNotOK(pass, e, okObj) ||
						(st.Tag != nil && isIdentObj(pass, st.Tag, okObj) && isFalse(e)) {
						out = append(out, &ast.BlockStmt{List: clause.Body})
						break
					}
				}
			}
		}
		return true
	})
	return out
}

// mentionsOK reports whether the condition asserts the comma-ok
// variable positively: the bare `ok` ident, or a conjunction
// (`ok && …`) whose fall-through includes !ok.
func mentionsOK(pass *analysis.Pass, cond ast.Expr, okObj types.Object) bool {
	switch c := cond.(type) {
	case *ast.Ident:
		return isIdentObj(pass, c, okObj)
	case *ast.ParenExpr:
		return mentionsOK(pass, c.X, okObj)
	case *ast.BinaryExpr:
		return c.Op == token.LAND &&
			(mentionsOK(pass, c.X, okObj) || mentionsOK(pass, c.Y, okObj))
	default:
		return false
	}
}

// fallThroughRegion returns the statements after st in its enclosing
// block, up to and including the first one that diverges: the prefix
// an !ok execution falls into.
func fallThroughRegion(parents map[ast.Node]ast.Node, st *ast.IfStmt) *ast.BlockStmt {
	blk, ok := parents[st].(*ast.BlockStmt)
	if !ok {
		return nil
	}
	idx := -1
	for i, s := range blk.List {
		if s == st {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	var list []ast.Stmt
	for _, s := range blk.List[idx+1:] {
		list = append(list, s)
		if diverges(s) {
			break
		}
	}
	return &ast.BlockStmt{List: list}
}

func diverges(s ast.Stmt) bool {
	switch x := s.(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		if call, ok := x.X.(*ast.CallExpr); ok {
			if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
				return true
			}
		}
	}
	return false
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
// through) returns without any operand carrying an error — counting
// only statements at the branch's own top level, since a lax statement
// nested under another condition executes only when that condition
// holds; or neither diverges nor records an error but rebinds the
// asserted variable to its zero value and falls through.
func branchIsLax(pass *analysis.Pass, br *ast.BlockStmt, hasErrorResult bool, asserted types.Object) bool {
	// An error surfaced or recorded anywhere in the branch — nested or
	// not — precludes laxness.
	surfaced := false
	ast.Inspect(br, func(n ast.Node) bool {
		if surfaced {
			return false
		}
		switch st := n.(type) {
		case *ast.FuncLit:
			return false // the closure's returns are not this branch's
		case *ast.ReturnStmt:
			for _, res := range st.Results {
				if carriesError(pass, res) {
					surfaced = true
					return false
				}
			}
		case *ast.AssignStmt:
			for _, rhs := range st.Rhs {
				if carriesError(pass, rhs) {
					surfaced = true
					return false
				}
			}
		}
		return true
	})
	if surfaced {
		return false
	}
	diverges := false
	for _, st := range br.List {
		switch x := st.(type) {
		case *ast.ReturnStmt:
			diverges = true
			if !hasErrorResult {
				continue
			}
			if len(x.Results) == 0 {
				// Bare return: the named error result holds whatever the
				// branch left it, which is nil unless an error was
				// assigned before the return.
				return true
			}
			for _, res := range x.Results {
				if carriesError(pass, res) {
					return false // error surfaced: not lax
				}
			}
			return true
		case *ast.BranchStmt:
			diverges = true
			if x.Tok == token.CONTINUE {
				return true
			}
		case *ast.ExprStmt:
			if call, ok := x.X.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "panic" {
					diverges = true
				}
			}
		}
	}
	// A branch that neither diverges nor records an error, but rebinds
	// the asserted variable to its zero value, falls through with the
	// wrong type already erased — the zero default reads as "not
	// supplied" while the nil error says fine.
	return !diverges && hasErrorResult && zeroesAsserted(pass, br, asserted)
}

// zeroesAsserted reports whether the branch assigns the asserted
// variable a zero literal (0, "", false).
func zeroesAsserted(pass *analysis.Pass, br *ast.BlockStmt, asserted types.Object) bool {
	if asserted == nil {
		return false
	}
	found := false
	ast.Inspect(br, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range st.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || pass.TypesInfo.ObjectOf(id) != asserted {
				continue
			}
			if i < len(st.Rhs) && isZeroLiteral(pass, st.Rhs[i]) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// isZeroLiteral reports whether e is the constant zero of its type.
func isZeroLiteral(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return false
	}
	switch tv.Value.Kind() {
	case constant.Int:
		return tv.Value.ExactString() == "0"
	case constant.String:
		return tv.Value.ExactString() == `""`
	case constant.Bool:
		return !constant.BoolVal(tv.Value)
	}
	return false
}

// carriesError reports whether e is a non-nil value of error type: the
// wrong type is being surfaced rather than swallowed. A multi-value
// call return carries an error when any result does (`return
// t.Render(...)`, where the call's second result is the error).
func carriesError(pass *analysis.Pass, e ast.Expr) bool {
	if id, ok := e.(*ast.Ident); ok && id.Name == "nil" {
		return false
	}
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	if tup, ok := t.(*types.Tuple); ok {
		for i := range tup.Len() {
			et := tup.At(i).Type()
			if types.Implements(et, errorIface) || types.Identical(et, errorIface) {
				return true
			}
		}
		return false
	}
	return types.Implements(t, errorIface) || types.Identical(t, errorIface)
}

var errorIface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
