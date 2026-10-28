// Package keytrim catches strings.TrimSpace rewriting an identity value
// that is then used as a key.
//
// The bug: an id, tenant id or user id is an exact key. Trimming it
// turns " x " into "x", which names a different record: the operation
// the caller asked for on one row runs on its twin. Found by the
// 2026-10-27 bug bash: framework/entityui bulk.go dedupeIDs trims each
// selected id before the scoped re-read and the delete, so selecting
// " x " deletes "x" (U9); battery/admin rbac_admin.go takes
// strings.TrimSpace(vals.Get("user_id")), so saving " u1 "'s roles
// rewrites "u1"'s (A42). The fix is to use the value byte-exact and
// refuse an id that is not valid instead of rewriting it.
//
// Shape: a strings.TrimSpace call whose operand is an identity value
// and whose result is used as a value, not only tested for emptiness.
// An identity value is:
//   - an identifier or field selector whose name is id or ids, ends in
//     ID, IDs, Id or Ids (userID, s.OwnerID, recordIds), or names a
//     tenant or actor (tenant, tenantKey, actor);
//   - a request or form read with a literal key of that shape:
//     url.Values Get, Request FormValue / PostFormValue, Query().Get
//     ("user_id", "tenantID", "id").
//
// Silent postures, deliberately:
//   - a presence check: the trimmed value only compared with "" or
//     measured with len, never kept;
//   - operands that are not identity-named: file contents
//     (strings.TrimSpace(string(b)) drops a trailing newline on
//     purpose), and values people type, such as names, emails and
//     search queries, where trimming is the point;
//   - names that merely contain "id" (valid, idle, paid);
//   - a request read whose key is not a literal: the rule cannot tell
//     what it names;
//   - _test.go files.
//
// Out of reach, recorded so nobody re-derives it: the same bash's
// audit-trail scrub (A39–A41) deletes control bytes from tenant,
// record and actor ids through sanitizeAuditField, a display
// sanitizer that log sinks rightly require (see controlbytes), so a
// call-site rule cannot tell the identity-column use from the
// correct one; and the setup wizard (S40) trims every submitted field
// in one loop, password included, with no identity name in sight.
package keytrim

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strconv"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "keytrim",
	Doc:  "report strings.TrimSpace rewriting an identity value (an id, tenant or actor) that is then used as a key",
	Run:  run,
}

// identityName matches a Go identifier that names an identity value.
var identityName = regexp.MustCompile(`^(?:[iI][dD][sS]?|.*[a-z0-9_](?:ID|IDs|Id|Ids)|(?i:tenant|actor).*|.*(?i:tenant|actor))$`)

// identityKey matches a literal request or form key naming one.
var identityKey = regexp.MustCompile(`^(?:(?i:id|ids)|.*(?:_id|_ids|ID|IDs|Id|Ids)|(?i:.*tenant.*|.*actor.*))$`)

// requestReads are the accessor methods whose literal argument names
// the value read.
var requestReads = map[string]bool{"Get": true, "FormValue": true, "PostFormValue": true}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		parents := map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 || !isTrimSpace(pass, call) {
				return true
			}
			arg := ast.Unparen(call.Args[0])
			if !isIdentity(pass, arg) || presenceOnly(call, parents) {
				return true
			}
			pass.Reportf(call.Pos(),
				"keytrim: strings.TrimSpace rewrites the identity value %s, so \" x \" becomes \"x\" and names a different record; use the value byte-exact and refuse an invalid id instead",
				types.ExprString(arg))
			return true
		})
	}
	return nil, nil
}

// isTrimSpace: call is strings.TrimSpace.
func isTrimSpace(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "TrimSpace" {
		return false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "strings"
}

// isIdentity reports whether e is an identity-named value or a request
// read with an identity-named literal key.
func isIdentity(pass *analysis.Pass, e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return identityName.MatchString(x.Name)
	case *ast.SelectorExpr:
		return identityName.MatchString(x.Sel.Name)
	case *ast.CallExpr:
		sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr)
		if !ok || !requestReads[sel.Sel.Name] || len(x.Args) != 1 {
			return false
		}
		lit, ok := ast.Unparen(x.Args[0]).(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return false
		}
		key, err := strconv.Unquote(lit.Value)
		if err != nil || !identityKey.MatchString(key) {
			return false
		}
		return isRequestReceiver(pass, sel.X)
	}
	return false
}

// isRequestReceiver: e is a url.Values or an *http.Request (FormValue),
// so a Get on some unrelated map-like type does not count.
func isRequestReceiver(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	switch n.Obj().Pkg().Path() + "." + n.Obj().Name() {
	case "net/url.Values", "net/http.Request":
		return true
	}
	return false
}

// presenceOnly reports whether the TrimSpace result is only tested for
// emptiness: compared with "" or measured with len.
func presenceOnly(call *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	parent := parents[call]
	for {
		p, ok := parent.(*ast.ParenExpr)
		if !ok {
			break
		}
		parent = parents[p]
	}
	switch p := parent.(type) {
	case *ast.BinaryExpr:
		if p.Op != token.EQL && p.Op != token.NEQ {
			return false
		}
		other := p.Y
		if ast.Unparen(p.Y) == call {
			other = p.X
		}
		lit, ok := ast.Unparen(other).(*ast.BasicLit)
		return ok && lit.Kind == token.STRING && (lit.Value == `""` || lit.Value == "``")
	case *ast.CallExpr:
		id, ok := ast.Unparen(p.Fun).(*ast.Ident)
		return ok && id.Name == "len"
	}
	return false
}
