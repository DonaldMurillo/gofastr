package laxcoerce

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The nil-fold arm (2026-11, bug 8b19113f in battery/auth). A type switch
// on a scanned `any` that answers the same zero value for `case nil` and
// for a type it does not know (the default arm, or the return after the
// switch) folds "NULL" and "unreadable" into one. For a fact whose
// presence is what matters — a revocation, an expiry, a lock — that is
// fail-open: an operator's out-of-band `revoked_at = unixepoch()` scanned
// as a value the switch did not list, read as NULL, and the token stayed
// live.
//
// A coercer is a package function with an interface-typed parameter p
// whose body switches on p.(type) with a `case nil` arm and an unknown
// arm both returning one zero value (time.Time{}, nil, 0, "", false). A
// wrapper that hands its own interface parameter to a coercer and never
// tests that parameter against nil itself (timePtrFromRaw over
// coerceTime) is a coercer too. The arm fires where a coercer's result is
// stored into a field named Revoked*/Expires*/Disabled*/Deleted*/Locked*
// (the sink), and on the switch itself when the coercer's own name says
// it reads one of those facts. An Expires*/Expiry* sink counts only when
// it is a pointer: a value-typed ExpiresAt holds year 1 for the folded
// zero, which a plain now.After(exp) reads as long expired (fail-closed;
// battery/auth's Session.ExpiresAt is that shape).
//
// Silent: a wrapper that splits nil off first (`if src == nil { return
// nil }`, the timePtrFailClosed fix: a non-NULL value it cannot read is
// set, as the epoch); a coercer whose unknown arm returns something
// other than the nil arm's zero (an error, a sentinel); a coercer feeding
// any other field (LastUsedAt: absent and unreadable mean the same
// there); _test.go files.
var failOpenName = regexp.MustCompile(`^(Revoked|Revoke|Expires|Expiry|Expired|Disabled|Deleted|Locked)`)

const msgNilFold = "%s answers the same zero for NULL and for a value it cannot read, so an unreadable %s reads as unset (fail-open): split nil off first and treat a non-NULL value it cannot parse as set (timePtrFailClosed)"

func runNilFold(pass *analysis.Pass) {
	type fnDecl struct {
		obj  *types.Func
		decl *ast.FuncDecl
	}
	var decls []fnDecl
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if obj, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
				decls = append(decls, fnDecl{obj, fd})
			}
		}
	}

	coercers := map[*types.Func]token.Pos{} // value: the switch position
	for _, d := range decls {
		if pos, ok := nilFoldSwitch(pass, d.decl); ok {
			coercers[d.obj] = pos
		}
	}
	// Wrappers: an interface param handed straight to a coercer, never
	// itself compared with nil.
	for changed := true; changed; {
		changed = false
		for _, d := range decls {
			if _, ok := coercers[d.obj]; ok || hasErrorResult(pass, d.decl) {
				continue
			}
			if pos, ok := wrapsCoercer(pass, d.decl, coercers); ok {
				coercers[d.obj] = pos
				changed = true
			}
		}
	}
	if len(coercers) == 0 {
		return
	}

	for _, d := range decls {
		if pos, ok := coercers[d.obj]; ok && failOpenName.MatchString(capitalize(d.obj.Name())) {
			if _, direct := nilFoldSwitch(pass, d.decl); direct {
				pass.Reportf(pos, msgNilFold, d.obj.Name(), d.obj.Name())
			}
		}
		ast.Inspect(d.decl.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if len(x.Lhs) != len(x.Rhs) {
					return true
				}
				for i, l := range x.Lhs {
					sel, ok := l.(*ast.SelectorExpr)
					if !ok || !failOpenSink(pass, sel.Sel) {
						continue
					}
					if c := coercerCall(pass, x.Rhs[i], coercers); c != nil {
						pass.Reportf(x.Rhs[i].Pos(), msgNilFold, c.Name(), sel.Sel.Name)
					}
				}
			case *ast.CompositeLit:
				for _, el := range x.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || !failOpenSink(pass, key) {
						continue
					}
					if c := coercerCall(pass, kv.Value, coercers); c != nil {
						pass.Reportf(kv.Value.Pos(), msgNilFold, c.Name(), key.Name)
					}
				}
			}
			return true
		})
	}
}

// failOpenSink reports whether field id is a sink where the folded zero
// reads as "unset": a Revoked/Disabled/Deleted/Locked field of any type
// (its zero always means "not"), or an Expires/Expiry field that is a
// pointer (nil is "never"). A value-typed ExpiresAt is left alone: its
// zero is year 1, which a plain now.After(exp) check reads as long
// expired, so the fold there fails closed (battery/auth Session.Expired).
func failOpenSink(pass *analysis.Pass, id *ast.Ident) bool {
	if !failOpenName.MatchString(id.Name) {
		return false
	}
	if !strings.HasPrefix(id.Name, "Expir") {
		return true
	}
	obj := pass.TypesInfo.ObjectOf(id)
	if obj == nil {
		return false
	}
	_, isPtr := obj.Type().Underlying().(*types.Pointer)
	return isPtr
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func hasErrorResult(pass *analysis.Pass, fd *ast.FuncDecl) bool {
	if fd.Type.Results == nil {
		return false
	}
	for _, r := range fd.Type.Results.List {
		if t := pass.TypesInfo.TypeOf(r.Type); t != nil && types.Identical(t, errorIface) {
			return true
		}
	}
	return false
}

// coercerCall returns the coercer e calls (unwrapping & and a deref).
func coercerCall(pass *analysis.Pass, e ast.Expr, coercers map[*types.Func]token.Pos) *types.Func {
	e = ast.Unparen(e)
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		e = u.X
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil
	}
	fn := calleeFunc(pass, call)
	if fn == nil {
		return nil
	}
	if _, ok := coercers[fn]; ok {
		return fn
	}
	return nil
}

func calleeFunc(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch f := call.Fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil
	}
	fn, _ := pass.TypesInfo.Uses[id].(*types.Func)
	return fn
}

// interfaceParams returns fd's interface-typed parameters.
func interfaceParams(pass *analysis.Pass, fd *ast.FuncDecl) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, fl := range fd.Type.Params.List {
		for _, n := range fl.Names {
			if obj := pass.TypesInfo.Defs[n]; obj != nil && types.IsInterface(obj.Type()) {
				out[obj] = true
			}
		}
	}
	return out
}

// nilFoldSwitch finds `switch p.(type)` on an interface parameter whose
// `case nil` returns the same zero value as its unknown arm: the default
// clause, or (with no default) the return right after the switch.
func nilFoldSwitch(pass *analysis.Pass, fd *ast.FuncDecl) (token.Pos, bool) {
	params := interfaceParams(pass, fd)
	if len(params) == 0 {
		return 0, false
	}
	var found token.Pos
	var visit func(stmts []ast.Stmt)
	visit = func(stmts []ast.Stmt) {
		for i, st := range stmts {
			if found.IsValid() {
				return
			}
			ts, ok := st.(*ast.TypeSwitchStmt)
			if !ok {
				continue
			}
			if !switchesOnParam(pass, ts, params) {
				continue
			}
			var nilRet, defRet ast.Expr
			hasDefault := false
			for _, s := range ts.Body.List {
				cc := s.(*ast.CaseClause)
				if cc.List == nil {
					hasDefault = true
					defRet = soleReturn(cc.Body)
					continue
				}
				for _, t := range cc.List {
					if id, ok := t.(*ast.Ident); ok && id.Name == "nil" && len(cc.List) == 1 {
						nilRet = soleReturn(cc.Body)
					}
				}
			}
			if !hasDefault && i+1 < len(stmts) {
				if r, ok := stmts[i+1].(*ast.ReturnStmt); ok && len(r.Results) == 1 {
					defRet = r.Results[0]
				}
			}
			if nilRet != nil && defRet != nil && isZeroExpr(pass, nilRet) && types.ExprString(nilRet) == types.ExprString(defRet) {
				found = ts.Pos()
			}
		}
	}
	visit(fd.Body.List)
	return found, found.IsValid()
}

func switchesOnParam(pass *analysis.Pass, ts *ast.TypeSwitchStmt, params map[types.Object]bool) bool {
	var x ast.Expr
	switch a := ts.Assign.(type) {
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			x = a.Rhs[0]
		}
	case *ast.ExprStmt:
		x = a.X
	}
	ta, ok := x.(*ast.TypeAssertExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(ta.X).(*ast.Ident)
	return ok && params[pass.TypesInfo.Uses[id]]
}

// soleReturn returns the single result of a clause body that is exactly
// one return statement.
func soleReturn(body []ast.Stmt) ast.Expr {
	if len(body) != 1 {
		return nil
	}
	r, ok := body[0].(*ast.ReturnStmt)
	if !ok || len(r.Results) != 1 {
		return nil
	}
	return r.Results[0]
}

// isZeroExpr reports whether e is a zero value: nil, a zero constant, an
// empty string, false, or an element-less composite literal.
func isZeroExpr(pass *analysis.Pass, e ast.Expr) bool {
	e = ast.Unparen(e)
	if cl, ok := e.(*ast.CompositeLit); ok {
		return len(cl.Elts) == 0
	}
	tv, ok := pass.TypesInfo.Types[e]
	if !ok {
		return false
	}
	if tv.IsNil() {
		return true
	}
	if tv.Value == nil {
		return false
	}
	switch tv.Value.String() {
	case "0", `""`, "false":
		return true
	}
	return false
}

// wrapsCoercer reports whether fd passes one of its interface parameters
// straight to a coercer and never compares that parameter with nil (nor
// switches on its type) itself.
func wrapsCoercer(pass *analysis.Pass, fd *ast.FuncDecl, coercers map[*types.Func]token.Pos) (token.Pos, bool) {
	params := interfaceParams(pass, fd)
	if len(params) == 0 {
		return 0, false
	}
	var pos token.Pos
	handed := map[types.Object]bool{}
	split := map[types.Object]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			fn := calleeFunc(pass, x)
			if fn == nil {
				return true
			}
			cpos, ok := coercers[fn]
			if !ok {
				return true
			}
			for _, a := range x.Args {
				if id, ok := ast.Unparen(a).(*ast.Ident); ok && params[pass.TypesInfo.Uses[id]] {
					handed[pass.TypesInfo.Uses[id]] = true
					pos = cpos
				}
			}
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ {
				return true
			}
			for _, pair := range [][2]ast.Expr{{x.X, x.Y}, {x.Y, x.X}} {
				id, ok := ast.Unparen(pair[0]).(*ast.Ident)
				if !ok || !params[pass.TypesInfo.Uses[id]] {
					continue
				}
				if tv, ok := pass.TypesInfo.Types[pair[1]]; ok && tv.IsNil() {
					split[pass.TypesInfo.Uses[id]] = true
				}
			}
		case *ast.TypeSwitchStmt:
			for p := range params {
				if switchesOnParam(pass, x, map[types.Object]bool{p: true}) {
					split[p] = true
				}
			}
		}
		return true
	})
	for p := range handed {
		if !split[p] {
			return pos, true
		}
	}
	return 0, false
}
