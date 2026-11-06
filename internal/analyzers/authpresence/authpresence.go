// Package authpresence catches an Authorization header's presence
// used as a scheme test, and a cookie-name fallback list that reads
// the unprefixed name before its __Host- / __Secure- twin.
//
// The bugs (2cfff500, the 2026-11-01 battery/auth bug bash), both in
// the CSRF path:
//
//   - core/middleware SkipBearerAuth, auth.CSRF's default Skip,
//     returned true for `r.Header.Get("Authorization") != ""`. Basic,
//     Digest, Negotiate and NTLM are credentials the browser attaches
//     by itself, to a cross-site form POST too, once a proxy or
//     intranet challenged for them, so behind an HTTP-auth staging gate
//     every cookie-authenticated form POST skipped CSRF. Only the
//     Bearer scheme is not cookie-borne. The fix parses the scheme:
//     strings.Cut(strings.TrimSpace(h), " ") then
//     strings.EqualFold(scheme, "bearer").
//   - battery/auth CSRFToken's cookie fallback ranged over
//     []string{CSRFCookieName, "__Host-" + CSRFCookieName}. On HTTPS
//     the __Host- cookie is the one the middleware set, and a sibling
//     subdomain can plant the bare name but never the prefixed one, so
//     reading the bare name first let a planted value win. The fix
//     lists the prefixed name first.
//
// Arm (a) fires on a presence test of the Authorization header — a
// Header.Get("Authorization") value (or a local bound to one, by
// assignment or var declaration, TrimSpace included, and a reference
// inside an invoked or escaped nested closure counts)
// compared with "" or measured with len — in a function that never
// otherwise uses the value: passes it to a call, slices or indexes it,
// or compares it with anything but "". A function that calls one that
// parses the header (a fact, so a helper in another package counts) is
// quiet too: `if h == "" { anonymous }` followed by parseBearer(r) is a
// presence check guarding a parse.
//
// Arm (b) fires on a []string literal that lists a cookie name before
// "__Host-"+name or "__Secure-"+name (the same expression, or two
// constants spelling the pair), when the literal is ranged over by a
// loop that reads (*http.Request).Cookie with the element.
//
// Silent postures, deliberately:
//   - presence tests of other headers (X-API-Key is not sent by the
//     browser on its own; its presence is a fine signal);
//   - a function that forwards the header (req.Header.Set(...,
//     h)): passing the value on is a use, and the receiver decides;
//   - a prefixed-first list, and a two-name list ranged by a loop
//     that only writes cookies (a logout clearing both names in any
//     order);
//   - _test.go files.
package authpresence

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "authpresence",
	Doc:       "report an Authorization header's presence used as a scheme test, and a cookie fallback that reads the unprefixed name before its __Host-/__Secure- twin",
	Run:       run,
	FactTypes: []analysis.Fact{(*parsesAuth)(nil)},
}

// parsesAuth marks a function that reads the Authorization header and
// uses its value beyond a presence test, itself or through a callee.
type parsesAuth struct{}

func (*parsesAuth) AFact()         {}
func (*parsesAuth) String() string { return "parsesAuth" }

type funcInfo struct {
	node     ast.Node
	obj      *types.Func // nil for a literal
	presence []ast.Expr  // presence tests
	uses     bool        // the value is used beyond presence
	forwards bool        // the value is written into another header
	callees  []*types.Func
}

func run(pass *analysis.Pass) (any, error) {
	var infos []*funcInfo
	byObj := map[*types.Func]*funcInfo{}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, fn := range astx.AllFuncs(f) {
			fi := &funcInfo{node: fn}
			if d, ok := fn.(*ast.FuncDecl); ok {
				fi.obj, _ = pass.TypesInfo.Defs[d.Name].(*types.Func)
			}
			scanAuth(pass, astx.BodyOf(fn), fi)
			infos = append(infos, fi)
			if fi.obj != nil {
				byObj[fi.obj] = fi
			}
		}
		checkCookieLists(pass, f)
	}

	parses := map[*types.Func]bool{}
	for _, fi := range infos {
		if fi.obj != nil && fi.uses {
			parses[fi.obj] = true
		}
	}
	calleeParses := func(c *types.Func) bool {
		if parses[c] {
			return true
		}
		return byObj[c] == nil && pass.ImportObjectFact(c, new(parsesAuth))
	}
	for changed := true; changed; {
		changed = false
		for _, fi := range infos {
			if fi.obj == nil || parses[fi.obj] {
				continue
			}
			for _, c := range fi.callees {
				if calleeParses(c) {
					parses[fi.obj] = true
					changed = true
					break
				}
			}
		}
	}
	for _, fi := range infos {
		if fi.obj != nil && parses[fi.obj] {
			pass.ExportObjectFact(fi.obj, &parsesAuth{})
		}
	}

	for _, fi := range infos {
		if len(fi.presence) == 0 || fi.uses || fi.forwards {
			continue
		}
		delegated := false
		for _, c := range fi.callees {
			if calleeParses(c) {
				delegated = true
				break
			}
		}
		if delegated {
			continue
		}
		for _, p := range fi.presence {
			pass.Reportf(p.Pos(),
				"authpresence: an Authorization header's presence is not its scheme: Basic, Digest, Negotiate and NTLM are attached by the browser on its own, to a cross-site form too; parse the scheme (strings.Cut, then strings.EqualFold(scheme, \"bearer\")) before treating the request as token-authenticated")
		}
	}
	return nil, nil
}

// scanAuth records fi's Authorization reads, presence tests, value
// uses and callees. Nested literals are their own funcInfo.
func scanAuth(pass *analysis.Pass, body *ast.BlockStmt, fi *funcInfo) {
	if body == nil {
		return
	}
	parents := dominance.Parents(body)
	runnable := runnableLiterals(pass, body, parents)
	// Locals bound to an Authorization read (or TrimSpace of one), by
	// assignment or var declaration.
	locals := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if literal, ok := n.(*ast.FuncLit); ok {
			return runnable[literal]
		}
		var lhs []ast.Expr
		var rhs []ast.Expr
		switch x := n.(type) {
		case *ast.AssignStmt:
			lhs, rhs = x.Lhs, x.Rhs
		case *ast.ValueSpec:
			for _, id := range x.Names {
				lhs = append(lhs, id)
			}
			rhs = x.Values
		}
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || i >= len(rhs) || !isAuthValue(pass, rhs[i], locals) {
				continue
			}
			if id.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					locals[obj] = true
				}
			}
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			// A nested literal is its own funcInfo for its reads and
			// callees, but a reference there to THIS function's bound
			// local is still this function's use when the closure can
			// run (a deferred or goroutine literal guarding on auth).
			if runnable[x] {
				litUses(pass, x.Body, parents, locals, fi, runnable)
			}
			return false
		case *ast.CallExpr:
			if fn, ok := astx.CalleeFunc(pass, ast.Unparen(x.Fun)); ok {
				fi.callees = append(fi.callees, fn.Origin())
			}
			if isAuthRead(pass, x) {
				classify(pass, x, parents, locals, fi)
			}
		case *ast.Ident:
			if obj := pass.TypesInfo.Uses[x]; obj != nil && locals[obj] {
				classify(pass, x, parents, locals, fi)
			}
		}
		return true
	})
}

// litUses classifies this function's Authorization locals inside a
// nested literal's body, recursively; the literal's own reads and
// callees belong to its own funcInfo.
func litUses(pass *analysis.Pass, body *ast.BlockStmt, parents map[ast.Node]ast.Node, locals map[types.Object]bool, fi *funcInfo, runnable map[*ast.FuncLit]bool) {
	if body == nil {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			if runnable[x] {
				litUses(pass, x.Body, parents, locals, fi, runnable)
			}
			return false
		case *ast.Ident:
			if obj := pass.TypesInfo.Uses[x]; obj != nil && locals[obj] {
				classify(pass, x, parents, locals, fi)
			}
		}
		return true
	})
}

func runnableLiterals(pass *analysis.Pass, body *ast.BlockStmt, parents map[ast.Node]ast.Node) map[*ast.FuncLit]bool {
	references := map[types.Object][]*ast.Ident{}
	var literals []*ast.FuncLit
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncLit:
			literals = append(literals, value)
		case *ast.Ident:
			if obj := pass.TypesInfo.Uses[value]; obj != nil {
				references[obj] = append(references[obj], value)
			}
		}
		return true
	})
	var canRun func(ast.Expr, map[ast.Expr]bool) bool
	canRun = func(expression ast.Expr, busy map[ast.Expr]bool) bool {
		if busy[expression] {
			return false
		}
		busy[expression] = true
		defer delete(busy, expression)
		parent := parents[expression]
		var bindings []ast.Expr
		switch target := parent.(type) {
		case *ast.ParenExpr:
			return canRun(target, busy)
		case *ast.AssignStmt:
			// A store into the callback's holder (callbacks[0] = nil,
			// holder.parse = nil) writes over it; it never runs it.
			for _, left := range target.Lhs {
				if left == expression {
					return false
				}
			}
			if len(target.Lhs) != len(target.Rhs) {
				return false
			}
			for index, value := range target.Rhs {
				if value == expression {
					bindings = append(bindings, target.Lhs[index])
				}
			}
		case *ast.ValueSpec:
			for index, value := range target.Values {
				if value == expression && index < len(target.Names) {
					bindings = append(bindings, target.Names[index])
				}
			}
		case *ast.RangeStmt:
			// Ranging over a holder hands each element to the value
			// variable; the callback runs if that variable does.
			if target.X != expression || target.Value == nil {
				return false
			}
			bindings = append(bindings, target.Value)
		case *ast.KeyValueExpr:
			container, ok := parents[target].(ast.Expr)
			return ok && canRun(container, busy)
		case *ast.CompositeLit:
			return canRun(target, busy)
		case *ast.UnaryExpr:
			return canRun(target, busy)
		case *ast.SelectorExpr:
			return canRun(target, busy)
		case *ast.IndexExpr:
			return canRun(target, busy)
		case *ast.CallExpr, *ast.ReturnStmt, *ast.SendStmt:
		default:
			return false
		}
		if len(bindings) > 0 {
			for _, binding := range bindings {
				if identifier, ok := binding.(*ast.Ident); ok {
					for _, reference := range references[pass.TypesInfo.ObjectOf(identifier)] {
						if canRun(reference, busy) {
							return true
						}
					}
				} else {
					return true
				}
			}
			return false
		}
		for ancestor := parents[parent]; ancestor != nil; ancestor = parents[ancestor] {
			if literal, ok := ancestor.(*ast.FuncLit); ok {
				return canRun(literal, busy)
			}
		}
		return true
	}
	runnable := make(map[*ast.FuncLit]bool, len(literals))
	for _, literal := range literals {
		runnable[literal] = canRun(literal, map[ast.Expr]bool{})
	}
	return runnable
}

// classify decides what one reference to the Authorization value does.
func classify(pass *analysis.Pass, ref ast.Expr, parents map[ast.Node]ast.Node, locals map[types.Object]bool, fi *funcInfo) {
	var cur ast.Node = ref
	for {
		p := parents[cur]
		switch x := p.(type) {
		case *ast.ParenExpr:
			cur = x
			continue
		case *ast.CallExpr:
			if astx.QualifiedCallee(pass, ast.Unparen(x.Fun)) == "strings.Cut" {
				if _, discarded := parents[x].(*ast.ExprStmt); discarded {
					return
				}
				if assignment, ok := parents[x].(*ast.AssignStmt); ok {
					allBlank := true
					for _, left := range assignment.Lhs {
						identifier, ok := ast.Unparen(left).(*ast.Ident)
						allBlank = allBlank && ok && identifier.Name == "_"
					}
					if allBlank {
						return
					}
				}
			}
			if isTrim(pass, x) {
				cur = x
				continue
			}
			if isLen(pass, x) {
				if b, ok := parents[x].(*ast.BinaryExpr); ok && isComparison(b.Op) {
					fi.presence = append(fi.presence, b)
					return
				}
			}
			if isHeaderWrite(pass, x) {
				// Forwarded: the receiver decides, but forwarding parses
				// nothing, so it earns no parsesAuth fact.
				fi.forwards = true
				return
			}
			fi.uses = true // passed to a call
			return
		case *ast.BinaryExpr:
			if isComparison(x.Op) && (isEmpty(pass, x.X) || isEmpty(pass, x.Y)) {
				fi.presence = append(fi.presence, x)
				return
			}
			fi.uses = true
			return
		case *ast.AssignStmt:
			// A binding to another local: classified at that local's
			// own references.
			for _, l := range x.Lhs {
				if id, ok := l.(*ast.Ident); ok && locals[pass.TypesInfo.ObjectOf(id)] {
					return
				}
			}
			fi.uses = true
			return
		case *ast.ValueSpec:
			// A var declaration binding to a local: classified at that
			// local's own references.
			for _, n := range x.Names {
				if locals[pass.TypesInfo.ObjectOf(n)] {
					return
				}
			}
			fi.uses = true
			return
		case *ast.ExprStmt:
			return
		default:
			fi.uses = true
			return
		}
	}
}

// isHeaderWrite reports whether call is an http.Header Set or Add.
func isHeaderWrite(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "Set" || sel.Sel.Name == "Add") &&
		astx.IsNamed(pass.TypesInfo.TypeOf(sel.X), "net/http", "Header")
}

func isComparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.GTR, token.GEQ, token.LSS, token.LEQ:
		return true
	}
	return false
}

func isEmpty(pass *analysis.Pass, e ast.Expr) bool {
	s, ok := astx.StringConstant(pass, e)
	return ok && s == ""
}

func isTrim(pass *analysis.Pass, call *ast.CallExpr) bool {
	return pathflow.QualifiedFunc(pass, call.Fun) == "strings.TrimSpace"
}

func isLen(pass *analysis.Pass, call *ast.CallExpr) bool {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok || id.Name != "len" {
		return false
	}
	_, ok = pass.TypesInfo.Uses[id].(*types.Builtin)
	return ok
}

// isAuthValue reports whether e is an Authorization read, a local bound
// to one, or TrimSpace of either.
func isAuthValue(pass *analysis.Pass, e ast.Expr, locals map[types.Object]bool) bool {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.CallExpr:
		if isAuthRead(pass, x) {
			return true
		}
		if isTrim(pass, x) && len(x.Args) == 1 {
			return isAuthValue(pass, x.Args[0], locals)
		}
	case *ast.Ident:
		return locals[pass.TypesInfo.ObjectOf(x)]
	}
	return false
}

// isAuthRead reports whether call is http.Header.Get("Authorization").
func isAuthRead(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Get" || len(call.Args) != 1 {
		return false
	}
	if !astx.IsNamed(pass.TypesInfo.TypeOf(sel.X), "net/http", "Header") {
		return false
	}
	k, ok := astx.StringConstant(pass, call.Args[0])
	return ok && strings.EqualFold(k, "Authorization")
}

// checkCookieLists is arm (b).
func checkCookieLists(pass *analysis.Pass, f *ast.File) {
	ast.Inspect(f, func(n ast.Node) bool {
		rs, ok := n.(*ast.RangeStmt)
		if !ok || rs.Value == nil {
			return true
		}
		lit, ok := ast.Unparen(rs.X).(*ast.CompositeLit)
		if !ok || !isStringSlice(pass.TypesInfo.TypeOf(lit)) {
			return true
		}
		bare, prefixed, found := bareBeforePrefixed(pass, lit)
		if !found {
			return true
		}
		elem, ok := rs.Value.(*ast.Ident)
		if !ok || !readsCookieWith(pass, rs.Body, pass.TypesInfo.ObjectOf(elem)) {
			return true
		}
		pass.Reportf(lit.Pos(),
			"authpresence: this cookie fallback reads %s before %s: a sibling subdomain can plant the bare name but never the prefixed one, so the planted value wins; list the prefixed name first",
			types.ExprString(bare), types.ExprString(prefixed))
		return true
	})
}

func isStringSlice(t types.Type) bool {
	s, ok := t.(*types.Slice)
	if !ok {
		return false
	}
	b, ok := s.Elem().Underlying().(*types.Basic)
	return ok && b.Kind() == types.String
}

var cookiePrefixes = []string{"__Host-", "__Secure-"}

// bareBeforePrefixed finds elements i < j where j is a cookie prefix
// plus i.
func bareBeforePrefixed(pass *analysis.Pass, lit *ast.CompositeLit) (bare, prefixed ast.Expr, found bool) {
	for i, a := range lit.Elts {
		for _, b := range lit.Elts[i+1:] {
			if isPrefixedOf(pass, b, a) {
				return a, b, true
			}
		}
	}
	return nil, nil, false
}

// isPrefixedOf reports whether b is a cookie prefix followed by a.
func isPrefixedOf(pass *analysis.Pass, b, a ast.Expr) bool {
	if av, ok := astx.StringConstant(pass, a); ok {
		if bv, ok := astx.StringConstant(pass, b); ok {
			for _, p := range cookiePrefixes {
				if av != "" && bv == p+av {
					return true
				}
			}
			return false
		}
	}
	be, ok := ast.Unparen(b).(*ast.BinaryExpr)
	if !ok || be.Op != token.ADD {
		return false
	}
	pv, ok := astx.StringConstant(pass, be.X)
	if !ok {
		return false
	}
	isPrefix := false
	for _, p := range cookiePrefixes {
		if pv == p {
			isPrefix = true
		}
	}
	return isPrefix && types.ExprString(ast.Unparen(be.Y)) == types.ExprString(ast.Unparen(a))
}

// readsCookieWith reports whether body calls (*http.Request).Cookie
// with elem.
func readsCookieWith(pass *analysis.Pass, body *ast.BlockStmt, elem types.Object) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found || len(call.Args) != 1 {
			return !found
		}
		fn, ok := astx.CalleeFunc(pass, ast.Unparen(call.Fun))
		if !ok || fn.Name() != "Cookie" {
			return true
		}
		sig, _ := fn.Type().(*types.Signature)
		if sig == nil || sig.Recv() == nil || !astx.IsNamed(astx.Deref(sig.Recv().Type()), "net/http", "Request") {
			return true
		}
		if id, ok := ast.Unparen(call.Args[0]).(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == elem {
			found = true
		}
		return !found
	})
	return found
}
