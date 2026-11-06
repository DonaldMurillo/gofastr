// Package poisonedconn catches a session reset on a pinned *sql.Conn
// whose failure sends the connection back to the pool still holding the
// session state it was meant to clear.
//
// The bug: code pins a connection (db.Conn) to hold session state — a
// Postgres advisory lock, an explicit BEGIN, a SET — and clears it before
// conn.Close. But conn.Close does not end the session; it returns the
// connection to the pool. When the clearing statement fails, the next
// borrower inherits the lock, the open transaction, or the setting:
//   - a8ba13471: core/migrate's withLeasedLock dropped the error of
//     `SELECT pg_advisory_unlock($1)` on its pinned conn. A NoTransaction
//     migration that ran its own BEGIN and failed inside it leaves the
//     session in an aborted transaction, which refuses the unlock, so the
//     migration lock stayed held by a pooled connection for the life of
//     the process and every other replica waited on it;
//   - 4d8705a8b: framework/cron's PostgresAdvisoryLease dropped the same
//     unlock's error (its comment claimed the next statement closes the
//     session; it does not), so a cancelled or timed-out unlock left the
//     leader lock held and no other replica could lead.
//
// It fires on Exec/Query/QueryRow (and their Context forms) on a
// *sql.Conn whose SQL is a constant (or a fmt.Sprintf with a constant
// format) that resets session state — pg_advisory_unlock(_all),
// RESET, SET (not SET LOCAL or SET TRANSACTION), session-scoped
// set_config(…, false), DISCARD, UNLISTEN, ROLLBACK — when:
//   - no retire the reset's FAILURE can reach: no `conn.Raw(func(any)
//     error { return driver.ErrBadConn })` on the same conn — inline,
//     named package-local function, or through a package-local helper
//     the conn is handed to — on a path the reset's error reaches. A
//     retire the SUCCESS side of the reset's own error test guards out
//     (the else of `if err != nil`, the tail after an error branch
//     that returns) never sees the failure and does not count. That
//     covers the dropped error (`_, _ = conn.ExecContext(…)`) and the
//     error branch that only logs before conn.Close;
//   - or the statement is pg_advisory_unlock run through Exec: the
//     function returns false (not an error) when the session does not
//     hold the lock, and Exec discards it.
//
// The fix spelling (a8ba13471, 4d8705a8b):
//
//	var released bool
//	err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&released)
//	if err != nil || !released {
//		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
//	}
//
// Returning driver.ErrBadConn from Raw makes database/sql close the
// driver connection instead of pooling it; ending the session releases
// its locks and discards its transaction.
//
// Silent postures, deliberately:
//   - *sql.Tx and *sql.DB: a Tx's rollback ends with the Tx, and a DB
//     statement runs on whatever connection the pool picks — there is no
//     pinned session to poison;
//   - statements that set nothing up for the next borrower: SET LOCAL and
//     SET TRANSACTION (transaction-scoped), and every statement outside
//     the reset list (BEGIN, a SELECT, DDL);
//   - SQL the analyzer cannot read as a constant;
//   - _test.go files.
package poisonedconn

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var Analyzer = &analysis.Analyzer{
	Name: "poisonedconn",
	Doc:  "report a session reset on a pinned *sql.Conn whose failure returns the connection to the pool unretired",
	Run:  run,
}

const fix = "retire the connection when the reset fails: conn.Raw(func(any) error { return driver.ErrBadConn })"

var connMethods = map[string]bool{
	"Exec": true, "ExecContext": true, "Query": true, "QueryContext": true,
	"QueryRow": true, "QueryRowContext": true,
}

var (
	unlockRE    = regexp.MustCompile(`(?i)\bpg_advisory_unlock(_all|_shared)?\s*\(`)
	setConfigRE = regexp.MustCompile(`(?is)\bset_config\s*\(.*,\s*false\s*\)`)
	leadRE      = regexp.MustCompile(`(?i)^\s*(RESET|DISCARD|UNLISTEN|ROLLBACK)\b`)
	setRE       = regexp.MustCompile(`(?i)^\s*SET\s+(\w+)`)
)

// resets reports whether sql clears or sets session state a pooled
// connection would carry to its next borrower, and whether it is an
// advisory unlock.
func resets(sql string) (reset, unlock bool) {
	if unlockRE.MatchString(sql) {
		return true, true
	}
	if setConfigRE.MatchString(sql) || leadRE.MatchString(sql) {
		return true, false
	}
	if m := setRE.FindStringSubmatch(sql); m != nil {
		switch strings.ToUpper(m[1]) {
		case "LOCAL", "TRANSACTION", "CONSTRAINTS":
			return false, false
		}
		return true, false
	}
	return false, false
}

func run(pass *analysis.Pass) (any, error) {
	retirers := retireHelpers(pass)
	named := namedBadConnFuncs(pass)
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, fn := range astx.AllFuncs(f) {
			if body := astx.BodyOf(fn); body != nil {
				check(pass, body, retirers, named)
			}
		}
	}
	return nil, nil
}

// check reports the reset calls body makes itself (nested literals are
// separate units in astx.AllFuncs).
func check(pass *analysis.Pass, body *ast.BlockStmt, retirers map[*types.Func]int, named map[*types.Func]bool) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || !connMethods[sel.Sel.Name] || !isConn(pass.TypesInfo.TypeOf(sel.X)) {
			return true
		}
		sql, ok := sqlArg(pass, call)
		if !ok {
			return true
		}
		reset, unlock := resets(sql)
		if !reset {
			return true
		}
		conn := types.ExprString(sel.X)
		if unlock && strings.HasPrefix(sel.Sel.Name, "Exec") {
			pass.Reportf(call.Pos(), "poisonedconn: pg_advisory_unlock returns false, not an error, when this session does not hold the lock, and %s discards it: QueryRow(…).Scan(&released) and %s when the unlock errs or reports false",
				sel.Sel.Name, fix)
			return true
		}
		if retiresIn(pass, body, conn, retirers, named, call) {
			return true
		}
		pass.Reportf(call.Pos(), "poisonedconn: %q on the pinned %s can fail, and nothing here retires the connection when it does; conn.Close returns it to the pool still holding the session state this statement was meant to clear: %s",
			firstLine(sql), conn, fix)
		return true
	})
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return s
}

// sqlArg is the statement text: the first string-typed argument after
// an optional context, as a constant or a fmt.Sprintf constant format.
func sqlArg(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	for _, arg := range call.Args {
		t := pass.TypesInfo.TypeOf(arg)
		if t == nil {
			continue
		}
		if b, ok := t.Underlying().(*types.Basic); !ok || b.Info()&types.IsString == 0 {
			continue
		}
		if s, ok := astx.StringConstant(pass, arg); ok {
			return s, true
		}
		if inner, ok := ast.Unparen(arg).(*ast.CallExpr); ok && astx.QualifiedCallee(pass, inner.Fun) == "fmt.Sprintf" && len(inner.Args) > 0 {
			return astx.StringConstant(pass, inner.Args[0])
		}
		return "", false
	}
	return "", false
}

func isConn(t types.Type) bool {
	p, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	return astx.IsNamed(p.Elem(), "database/sql", "Conn")
}

// retiresIn reports a retire of conn in body that a failure of the
// reset call can reach: conn.Raw handed a literal or package-local
// named function that returns driver.ErrBadConn, or conn passed to a
// retire helper. A retire the SUCCESS side of the reset's own error
// test guards out (the else of `if err != nil`, the tail after an
// error branch that returns) never sees the failure, so it does not
// count. call is nil when there is no one reset to path-check (the
// retire-helper scan), and every retire counts.
func retiresIn(pass *analysis.Pass, body *ast.BlockStmt, conn string, retirers map[*types.Func]int, named map[*types.Func]bool, call *ast.CallExpr) bool {
	spans := successOnlySpans(pass, body, call, errIDsOf(pass, body, call))
	inSpan := func(pos token.Pos) bool {
		for _, s := range spans {
			if s.from <= pos && pos < s.to {
				return true
			}
		}
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Raw" &&
			types.ExprString(sel.X) == conn && isConn(pass.TypesInfo.TypeOf(sel.X)) && len(call.Args) == 1 && returnsBadConn(pass, call.Args[0], named) {
			if !inSpan(call.Pos()) {
				found = true
				return false
			}
		}
		if fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func); ok {
			if i, ok := retirers[fn]; ok && i < len(call.Args) && types.ExprString(call.Args[i]) == conn {
				if !inSpan(call.Pos()) {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

// returnsBadConn: a func literal whose body returns driver.ErrBadConn,
// or the name of a package-local function that does (a working retire:
// recover-style indirection aside, Raw runs exactly what it is handed).
func returnsBadConn(pass *analysis.Pass, e ast.Expr, named map[*types.Func]bool) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.FuncLit:
		return bodyReturnsBadConn(pass, e.Body)
	case *ast.Ident:
		if fn, ok := pass.TypesInfo.Uses[e].(*types.Func); ok {
			return named[fn]
		}
	}
	return false
}

// bodyReturnsBadConn: some return statement returns driver.ErrBadConn.
func bodyReturnsBadConn(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, r := range ret.Results {
			if sel, ok := ast.Unparen(r).(*ast.SelectorExpr); ok && sel.Sel.Name == "ErrBadConn" {
				if v, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Var); ok && v.Pkg() != nil && v.Pkg().Path() == "database/sql/driver" {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// namedBadConnFuncs maps the package's named functions whose body
// returns driver.ErrBadConn, so conn.Raw(badConn) counts as a retire.
func namedBadConnFuncs(pass *analysis.Pass) map[*types.Func]bool {
	out := map[*types.Func]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if obj, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok && bodyReturnsBadConn(pass, fd.Body) {
				out[obj] = true
			}
		}
	}
	return out
}

// errIDsOf returns the error variables the enclosing assignment binds
// from the call: `_, err := conn.ExecContext(…)`, or the Scan that
// consumes a QueryRow reset. A call whose result is discarded (a bare
// statement, `_ =`) has none, and every retire counts.
func errIDsOf(pass *analysis.Pass, body *ast.BlockStmt, call *ast.CallExpr) map[types.Object]bool {
	out := map[types.Object]bool{}
	if call == nil {
		return out
	}
	var best *ast.AssignStmt
	ast.Inspect(body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && as.Pos() <= call.Pos() && call.End() <= as.End() {
			if best == nil || as.Pos() > best.Pos() {
				best = as
			}
		}
		return true
	})
	if best == nil {
		return out
	}
	lhs := best.Lhs
	for i, r := range best.Rhs {
		if r.Pos() > call.Pos() || call.End() > r.End() {
			continue
		}
		if len(best.Lhs) == len(best.Rhs) {
			lhs = best.Lhs[i : i+1]
		}
	}
	for _, l := range lhs {
		if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
			if v, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok && isErrNamed(v) {
				out[v] = true
			}
		}
	}
	return out
}

// isErrNamed: a variable of interface type named like an error.
func isErrNamed(v *types.Var) bool {
	if _, ok := v.Type().Underlying().(*types.Interface); !ok {
		return false
	}
	return v.Name() == "err" || v.Name() == "e" || strings.HasSuffix(v.Name(), "Err") || strings.Contains(v.Name(), "err")
}

// span is a source range a failure of the reset call cannot reach.
type span struct{ from, to token.Pos }

// successOnlySpans returns the regions of body a failure of the reset
// call cannot reach: the success side of an if that tests the reset's
// own error (the else of `if err != nil`, the then of `if err == nil`)
// and the tail of a block whose error-reaching side cannot fall
// through — where a goto into that tail counts as falling through: the
// failure lands there. Only if statements are analyzed; a switch or
// select guard stays conservative (no span).
func successOnlySpans(pass *analysis.Pass, body *ast.BlockStmt, call *ast.CallExpr, errIDs map[types.Object]bool) []span {
	if call == nil || len(errIDs) == 0 {
		return nil
	}
	var out []span
	var walk func(stmts []ast.Stmt, after bool)
	tailOnly := func(st *ast.IfStmt, thenOnly, elseOnly bool) bool {
		thenOK := thenOnly || diverges(pass, st.Body)
		elseOK := elseOnly || st.Else != nil && diverges(pass, st.Else)
		return thenOK && elseOK
	}
	walk = func(stmts []ast.Stmt, after bool) {
		for i, st := range stmts {
			if containsNode(st, call) && st.Pos() <= call.Pos() && call.End() <= st.End() {
				after = true
			}
			switch st := st.(type) {
			case *ast.IfStmt:
				applies := after || containsNode(st.Init, call) || containsNode(st.Cond, call)
				var thenOnly, elseOnly bool
				if applies && mentionsErr(pass, st.Cond, errIDs) {
					thenOnly = impliesNil(pass, st.Cond, errIDs)
					elseOnly = forcedByErr(pass, st.Cond, errIDs)
				}
				if thenOnly {
					out = append(out, span{st.Body.Pos(), st.Body.End()})
				}
				walk(st.Body.List, applies)
				if st.Else != nil {
					if elseOnly {
						out = append(out, span{st.Else.Pos(), st.Else.End()})
					}
					switch e := st.Else.(type) {
					case *ast.BlockStmt:
						walk(e.List, applies)
					case *ast.IfStmt:
						walk([]ast.Stmt{e}, applies)
					}
				}
				if i+1 < len(stmts) && tailOnly(st, thenOnly, elseOnly) &&
					!gotoInto(pass, st, stmts[i+1].Pos(), stmts[len(stmts)-1].End()) {
					out = append(out, span{stmts[i+1].Pos(), stmts[len(stmts)-1].End()})
				}
			case *ast.ForStmt:
				walk(st.Body.List, after)
			case *ast.RangeStmt:
				walk(st.Body.List, after)
			case *ast.SwitchStmt:
				for _, cc := range st.Body.List {
					if cc, ok := cc.(*ast.CaseClause); ok {
						walk(cc.Body, after)
					}
				}
			case *ast.TypeSwitchStmt:
				for _, cc := range st.Body.List {
					if cc, ok := cc.(*ast.CaseClause); ok {
						walk(cc.Body, after)
					}
				}
			case *ast.SelectStmt:
				for _, cc := range st.Body.List {
					if cc, ok := cc.(*ast.CommClause); ok {
						walk(cc.Body, after)
					}
				}
			case *ast.BlockStmt:
				walk(st.List, after)
			case *ast.LabeledStmt:
				walk([]ast.Stmt{st.Stmt}, after)
			}
		}
	}
	walk(body.List, false)
	return out
}

// mentionsErr: cond names one of errIDs.
func mentionsErr(pass *analysis.Pass, cond ast.Expr, errIDs map[types.Object]bool) bool {
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if obj := pass.TypesInfo.Uses[id]; obj != nil && errIDs[obj] {
				found = true
			}
		}
		return !found
	})
	return found
}

// impliesNil: e true implies every errID in it is nil (`err == nil`,
// `err == nil && released`).
func impliesNil(pass *analysis.Pass, e ast.Expr, errIDs map[types.Object]bool) bool {
	return errForces(pass, e, errIDs, true)
}

// forcedByErr: a non-nil errID forces e true (`err != nil`, the
// disjunction `err != nil || !released`): the else of e cannot hold
// the failure.
func forcedByErr(pass *analysis.Pass, e ast.Expr, errIDs map[types.Object]bool) bool {
	return errForces(pass, e, errIDs, false)
}

// errForces is the shared walk: with nilSide, e true implies err == nil;
// without it, err != nil forces e true. The two are duals, so a negation
// swaps sides and the connectives and comparison trade places.
func errForces(pass *analysis.Pass, e ast.Expr, errIDs map[types.Object]bool, nilSide bool) bool {
	either, both, cmp := token.LAND, token.LOR, token.EQL
	if !nilSide {
		either, both, cmp = token.LOR, token.LAND, token.NEQ
	}
	switch t := ast.Unparen(e).(type) {
	case *ast.UnaryExpr:
		if t.Op == token.NOT {
			return errForces(pass, t.X, errIDs, !nilSide)
		}
	case *ast.BinaryExpr:
		switch t.Op {
		case either:
			return errForces(pass, t.X, errIDs, nilSide) || errForces(pass, t.Y, errIDs, nilSide)
		case both:
			return errForces(pass, t.X, errIDs, nilSide) && errForces(pass, t.Y, errIDs, nilSide)
		case cmp:
			return isErrID(pass, t.X, errIDs) && isNil(t.Y) || isNil(t.X) && isErrID(pass, t.Y, errIDs)
		}
	}
	return false
}

func isErrID(pass *analysis.Pass, e ast.Expr, errIDs map[types.Object]bool) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && errIDs[pass.TypesInfo.Uses[id]]
}

func isNil(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "nil"
}

// containsNode reports whether root's subtree holds target.
func containsNode(root, target ast.Node) bool {
	if root == nil {
		return false
	}
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if n == target {
			found = true
		}
		return !found
	})
	return found
}

// diverges: control cannot fall through st (a return, a goto or a
// panic; an if whose both sides diverge). A goto that jumps into a
// range divergence was granted for still reaches it — gotoInto is the
// caller's correction for that.
func diverges(pass *analysis.Pass, st ast.Stmt) bool {
	switch st := st.(type) {
	case *ast.IfStmt:
		return diverges(pass, st.Body) && st.Else != nil && diverges(pass, st.Else)
	case *ast.BlockStmt:
		if len(st.List) == 0 {
			return false
		}
		switch last := st.List[len(st.List)-1].(type) {
		case *ast.ReturnStmt, *ast.BranchStmt:
			return true
		case *ast.ExprStmt:
			if call, ok := last.X.(*ast.CallExpr); ok {
				if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
					if b, ok := pass.TypesInfo.Uses[id].(*types.Builtin); ok && b.Name() == "panic" {
						return true
					}
				}
			}
		}
	}
	return false
}

// gotoInto: st's subtree holds a goto whose target label lies within
// [from, to]. A branch that only gotos away looks divergent, but when
// it jumps INTO the range that divergence was granted for, the failure
// path reaches it and the range is not success-only.
func gotoInto(pass *analysis.Pass, st ast.Stmt, from, to token.Pos) bool {
	found := false
	ast.Inspect(st, func(n ast.Node) bool {
		bs, ok := n.(*ast.BranchStmt)
		if !ok || bs.Tok != token.GOTO || bs.Label == nil {
			return true
		}
		if obj := pass.TypesInfo.Uses[bs.Label]; obj != nil && from <= obj.Pos() && obj.Pos() <= to {
			found = true
		}
		return !found
	})
	return found
}

// retireHelpers maps the package's functions that retire a *sql.Conn
// parameter (Raw returning driver.ErrBadConn on it) to that parameter's
// index.
func retireHelpers(pass *analysis.Pass) map[*types.Func]int {
	out := map[*types.Func]int{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			obj, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			i := 0
			for _, field := range fd.Type.Params.List {
				names := field.Names
				if len(names) == 0 {
					i++
					continue
				}
				for _, id := range names {
					if po := pass.TypesInfo.Defs[id]; po != nil && isConn(po.Type()) && retiresIn(pass, fd.Body, id.Name, nil, nil, nil) {
						out[obj] = i
					}
					i++
				}
			}
		}
	}
	return out
}
