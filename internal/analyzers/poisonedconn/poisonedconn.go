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
//   - nothing in the enclosing function retires the connection: no
//     `conn.Raw(func(any) error { return driver.ErrBadConn })` on the same
//     conn, directly or through a package-local helper the conn is handed
//     to. That covers the dropped error (`_, _ = conn.ExecContext(…)`) and
//     the error branch that only logs before conn.Close;
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
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			check(pass, fd.Body, retirers)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					check(pass, lit.Body, retirers)
				}
				return true
			})
		}
	}
	return nil, nil
}

// check reports the reset calls body makes itself (not inside nested
// literals, which are checked on their own).
func check(pass *analysis.Pass, body *ast.BlockStmt, retirers map[*types.Func]int) {
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
		if retiresIn(pass, body, conn, retirers) {
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

// retiresIn reports a retire of conn in body: conn.Raw handed a literal
// that returns driver.ErrBadConn, or conn passed to a retire helper.
func retiresIn(pass *analysis.Pass, body *ast.BlockStmt, conn string, retirers map[*types.Func]int) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Raw" &&
			types.ExprString(sel.X) == conn && isConn(pass.TypesInfo.TypeOf(sel.X)) && len(call.Args) == 1 && returnsBadConn(pass, call.Args[0]) {
			found = true
			return false
		}
		if fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func); ok {
			if i, ok := retirers[fn]; ok && i < len(call.Args) && types.ExprString(call.Args[i]) == conn {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// returnsBadConn: a func literal whose body returns driver.ErrBadConn.
func returnsBadConn(pass *analysis.Pass, e ast.Expr) bool {
	lit, ok := ast.Unparen(e).(*ast.FuncLit)
	if !ok {
		return false
	}
	found := false
	ast.Inspect(lit.Body, func(n ast.Node) bool {
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
					if po := pass.TypesInfo.Defs[id]; po != nil && isConn(po.Type()) && retiresIn(pass, fd.Body, id.Name, nil) {
						out[obj] = i
					}
					i++
				}
			}
		}
	}
	return out
}
