// Package ratelimitkey catches a per-IP rate limit keyed on a client's
// full address.
//
// The bug (be0acf07, the 2026-11-01 battery/auth bug bash): a
// subscriber is routinely handed a whole IPv6 /64, 2^64 addresses, and
// every per-IP limit keyed the full address, so one client sending each
// request from a fresh source address never met a limit: register,
// login, forgot-password, magic-link send, the 2FA challenge. The fix
// introduced ratelimit.IPKey, which collapses IPv6 to its /64 (IPv4,
// mapped included, stays itself), and (*Limiter).ClientKey, which
// applies it; Middleware, MiddlewareByKey(nil), the auth battery's
// per-IP limiters and framework.AgentRateLimitKey key on it. The fix
// spelling is a key that passes through a collapsing helper:
//
//	allowed, retry := rl.AllowContext(ctx, rl.ClientKey(r))
//
// Shape: a raw client address reaching a rate-limit key. A raw address
// is r.RemoteAddr, the result of net.ParseIP / netip.ParseAddr, the
// result of a function that returns one (a fact, so ratelimit.ClientIP
// and (*Limiter).ClientIP count from any package), and anything
// computed from a raw address — a host split off with SplitHostPort,
// a prefix concatenated on, a String() of a parsed IP — unless it went
// through a collapsing function: one that returns the result of
// (netip.Addr).Prefix, (net.IP).Mask or net.CIDRMask (or a String() of
// it), or of a function that does (a fact; credited by what the body
// returns, never by name, so a function that collapses one value and
// returns another earns nothing). The key positions are:
//   - a string argument of a limiter call: a method named Allow,
//     AllowN, AllowContext, Take, Hit, Wait, Reserve or Consume (any
//     case) on a type whose name says Limit, Bucket or Throttle;
//   - a func(*http.Request) string value that returns a raw address,
//     assigned to a field named KeyFunc, passed to a function whose
//     name says Key or Limit (MiddlewareByKey, RateLimitKey), or
//     returned by a function whose name says Key. A call through a
//     func-typed local counts as raw when any assignment to that local
//     is a raw key function (keyFunc = rl.ClientIP).
//
// Silent postures, deliberately:
//   - a key that does not come from the client address (a user id, an
//     API key, an agent URL): the rule never guesses a key is an IP;
//   - an address used for anything but a key (logs, audit rows, the
//     proxy-trust check): ClientIP stays the full address on purpose;
//   - a key function the caller supplies (a parameter never assigned a
//     raw function here);
//   - _test.go files.
package ratelimitkey

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "ratelimitkey",
	Doc:       "report a per-IP rate-limit key built from a client's full address instead of its IPv6 /64",
	Run:       run,
	FactTypes: []analysis.Fact{(*rawAddr)(nil), (*collapses)(nil)},
}

// rawAddr marks a function whose result is a client's full address.
type rawAddr struct{}

func (*rawAddr) AFact()         {}
func (*rawAddr) String() string { return "rawAddr" }

// collapses marks a function that reduces an address to a prefix.
type collapses struct{}

func (*collapses) AFact()         {}
func (*collapses) String() string { return "collapses" }

var (
	limiterType   = regexp.MustCompile(`(?i)limit|bucket|throttl`)
	limiterMethod = map[string]bool{"allow": true, "allown": true, "allowcontext": true, "take": true, "hit": true, "wait": true, "reserve": true, "consume": true}
	keyCallee     = regexp.MustCompile(`(?i)key|limit`)
	keyFuncName   = regexp.MustCompile(`(?i)key`)
)

type state struct {
	pass     *analysis.Pass
	byObj    map[*types.Func]*ast.FuncDecl
	raw      map[*types.Func]bool
	collapse map[*types.Func]bool
	// per-declaration context while checking
	bound   map[types.Object]ast.Expr
	assigns map[types.Object][]ast.Expr
}

func run(pass *analysis.Pass) (any, error) {
	s := &state{pass: pass, byObj: map[*types.Func]*ast.FuncDecl{}, raw: map[*types.Func]bool{}, collapse: map[*types.Func]bool{}}
	decls := pathflow.FuncsWithBodies(pass)
	for _, d := range decls {
		if fn, ok := pass.TypesInfo.Defs[d.Name].(*types.Func); ok {
			s.byObj[fn] = d
		}
	}
	// Collapsers: a fixed point over same-package callees.
	for changed := true; changed; {
		changed = false
		for fn, d := range s.byObj {
			if s.collapse[fn] || !addrResult(fn) {
				continue
			}
			s.enter(d.Body)
			if s.returnsCollapsed(d.Body) {
				s.collapse[fn] = true
				changed = true
			}
		}
	}
	// Raw-address functions: a fixed point too.
	for changed := true; changed; {
		changed = false
		for fn, d := range s.byObj {
			if s.raw[fn] || s.collapse[fn] || !addrResult(fn) {
				continue
			}
			s.enter(d.Body)
			if s.returnsRaw(d.Body, 0) {
				s.raw[fn] = true
				changed = true
			}
		}
	}
	for fn := range s.byObj {
		if s.collapse[fn] {
			pass.ExportObjectFact(fn, &collapses{})
		}
		if s.raw[fn] {
			pass.ExportObjectFact(fn, &rawAddr{})
		}
	}
	for _, d := range decls {
		s.enter(d.Body)
		s.checkSinks(d)
	}
	return nil, nil
}

// enter sets the binding context for body.
func (s *state) enter(body *ast.BlockStmt) {
	s.bound = pathflow.Bindings(s.pass, body)
	s.assigns = map[types.Object][]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, l := range as.Lhs {
			if id, ok := l.(*ast.Ident); ok {
				if obj := s.pass.TypesInfo.ObjectOf(id); obj != nil {
					s.assigns[obj] = append(s.assigns[obj], as.Rhs[i])
				}
			}
		}
		return true
	})
}

func (s *state) isCollapser(fn *types.Func) bool {
	if _, local := s.byObj[fn]; local {
		return s.collapse[fn]
	}
	return s.pass.ImportObjectFact(fn, new(collapses))
}

func (s *state) isRawFunc(fn *types.Func) bool {
	if s.isCollapser(fn) {
		return false
	}
	if _, local := s.byObj[fn]; local {
		return s.raw[fn]
	}
	return s.pass.ImportObjectFact(fn, new(rawAddr))
}

// returnsCollapsed reports whether a return of body (outside literals)
// yields a collapsed address: the function is a collapsing key helper.
// The result decides, not a call anywhere in the body, so a handler
// that collapses one value and returns another earns nothing.
func (s *state) returnsCollapsed(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) > 0 && s.isCollapsed(ret.Results[0], 0) {
			found = true
		}
		return true
	})
	return found
}

// isCollapsed reports whether e is an address reduced to a prefix:
// (netip.Addr).Prefix, (net.IP).Mask, net.CIDRMask, a collapsing
// function's result, or a method result (String) or binding of one.
func (s *state) isCollapsed(e ast.Expr, depth int) bool {
	if depth > 8 {
		return false
	}
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.Ident:
		if b, ok := s.bound[s.pass.TypesInfo.ObjectOf(x)]; ok {
			return s.isCollapsed(b, depth+1)
		}
	case *ast.CallExpr:
		fn, ok := astx.CalleeFunc(s.pass, ast.Unparen(x.Fun))
		if !ok {
			return false
		}
		fn = fn.Origin()
		switch {
		case recvIs(fn, "net/netip", "Addr") && fn.Name() == "Prefix",
			recvIs(fn, "net", "IP") && fn.Name() == "Mask",
			isPkgFunc(fn, "net", "CIDRMask"),
			s.isCollapser(fn):
			return true
		}
		if sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok {
			if _, isPkg := s.pass.TypesInfo.ObjectOf(identOf(sel.X)).(*types.PkgName); !isPkg {
				return s.isCollapsed(sel.X, depth+1)
			}
		}
	}
	return false
}

// returnsRaw reports whether a return of body (outside literals)
// yields a raw address first.
func (s *state) returnsRaw(body *ast.BlockStmt, depth int) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) > 0 && s.isRaw(ret.Results[0], depth) {
			found = true
		}
		return true
	})
	return found
}

// isRaw reports whether e is (derived from) a client's full address.
func (s *state) isRaw(e ast.Expr, depth int) bool {
	if depth > 8 {
		return false
	}
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.SelectorExpr:
		return x.Sel.Name == "RemoteAddr" && astx.IsNamed(astx.Deref(s.pass.TypesInfo.TypeOf(x.X)), "net/http", "Request")
	case *ast.Ident:
		if b, ok := s.bound[s.pass.TypesInfo.ObjectOf(x)]; ok {
			return s.isRaw(b, depth+1)
		}
	case *ast.BinaryExpr:
		return s.isRaw(x.X, depth+1) || s.isRaw(x.Y, depth+1)
	case *ast.CallExpr:
		if tv, ok := s.pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() {
			return len(x.Args) == 1 && s.isRaw(x.Args[0], depth+1)
		}
		fn, ok := astx.CalleeFunc(s.pass, ast.Unparen(x.Fun))
		if !ok {
			// A call through a func-typed local: raw when any
			// assignment to it is a raw key function.
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
				for _, a := range s.assigns[s.pass.TypesInfo.ObjectOf(id)] {
					if s.rawFuncValue(a, depth+1) {
						return true
					}
				}
			}
			return false
		}
		fn = fn.Origin()
		if s.isCollapser(fn) {
			return false
		}
		if s.isRawFunc(fn) || isPkgFunc(fn, "net", "ParseIP") || isPkgFunc(fn, "net/netip", "ParseAddr") || isPkgFunc(fn, "net/netip", "MustParseAddr") {
			return true
		}
		rt := s.pass.TypesInfo.TypeOf(x)
		if tup, ok := rt.(*types.Tuple); ok && tup.Len() > 0 {
			rt = tup.At(0).Type() // host, _, err := net.SplitHostPort(addr)
		}
		if !addrType(rt) {
			return false
		}
		if sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok {
			if _, isPkg := s.pass.TypesInfo.ObjectOf(identOf(sel.X)).(*types.PkgName); !isPkg && s.isRaw(sel.X, depth+1) {
				return true
			}
		}
		for _, a := range x.Args {
			if s.isRaw(a, depth+1) {
				return true
			}
		}
	}
	return false
}

func identOf(e ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(e).(*ast.Ident)
	if id == nil {
		return &ast.Ident{}
	}
	return id
}

// rawFuncValue reports whether e is a key function returning a raw
// address: a literal that returns one, or a function (or method value)
// with the raw fact.
func (s *state) rawFuncValue(e ast.Expr, depth int) bool {
	e = ast.Unparen(e)
	switch x := e.(type) {
	case *ast.FuncLit:
		return depth < 8 && s.returnsRaw(x.Body, depth+1)
	case *ast.Ident, *ast.SelectorExpr:
		var id *ast.Ident
		if sel, ok := x.(*ast.SelectorExpr); ok {
			id = sel.Sel
		} else {
			id = x.(*ast.Ident)
		}
		if fn, ok := s.pass.TypesInfo.Uses[id].(*types.Func); ok {
			return s.isRawFunc(fn.Origin())
		}
	}
	return false
}

// checkSinks reports raw addresses reaching key positions in d.
func (s *state) checkSinks(d *ast.FuncDecl) {
	if fn, ok := s.pass.TypesInfo.Defs[d.Name].(*types.Func); ok && keyFuncName.MatchString(d.Name.Name) && returnsKeyFunc(fn) {
		ast.Inspect(d.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false // a literal's own returns are not this function's
			}
			if ret, ok := n.(*ast.ReturnStmt); ok && len(ret.Results) == 1 && s.rawFuncValue(ret.Results[0], 0) {
				s.reportFunc(ret.Results[0], "is returned as a rate-limit key function")
			}
			return true
		})
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			s.checkCall(x)
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && strings.EqualFold(id.Name, "KeyFunc") && s.rawFuncValue(x.Value, 0) {
				s.reportFunc(x.Value, "is the KeyFunc")
			}
		case *ast.AssignStmt:
			if len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, l := range x.Lhs {
				if sel, ok := l.(*ast.SelectorExpr); ok && strings.EqualFold(sel.Sel.Name, "KeyFunc") && s.rawFuncValue(x.Rhs[i], 0) {
					s.reportFunc(x.Rhs[i], "is the KeyFunc")
				}
			}
		}
		return true
	})
}

func (s *state) checkCall(call *ast.CallExpr) {
	fn, ok := astx.CalleeFunc(s.pass, ast.Unparen(call.Fun))
	if !ok {
		return
	}
	sig, _ := fn.Type().(*types.Signature)
	if sig == nil {
		return
	}
	if sig.Recv() != nil && limiterMethod[strings.ToLower(fn.Name())] {
		if n := astx.NamedOf(astx.Deref(sig.Recv().Type())); n != nil && limiterType.MatchString(n.Obj().Name()) {
			for _, a := range call.Args {
				if isString(s.pass.TypesInfo.TypeOf(a)) && s.isRaw(a, 0) {
					s.pass.Reportf(a.Pos(),
						"ratelimitkey: the %s key %s is a client's full address: one IPv6 subscriber holds a whole /64 and rotates past a per-address limit; key on ratelimit.IPKey (or (*Limiter).ClientKey), which collapses IPv6 to its /64",
						fn.Name(), types.ExprString(a))
				}
			}
		}
	}
	if !keyCallee.MatchString(fn.Name()) {
		return
	}
	for _, a := range call.Args {
		if isKeyFuncType(s.pass.TypesInfo.TypeOf(a)) && s.rawFuncValue(a, 0) {
			s.reportFunc(a, "is passed to "+fn.Name())
		}
	}
}

func (s *state) reportFunc(e ast.Expr, where string) {
	what := types.ExprString(e)
	if _, ok := ast.Unparen(e).(*ast.FuncLit); ok {
		what = "this key function"
	}
	s.pass.Reportf(e.Pos(),
		"ratelimitkey: %s returns a client's full address and %s: one IPv6 subscriber holds a whole /64 and rotates past a per-address limit; key on ratelimit.IPKey (or (*Limiter).ClientKey), which collapses IPv6 to its /64",
		what, where)
}

func recvIs(fn *types.Func, pkg, name string) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() != nil && astx.IsNamed(astx.Deref(sig.Recv().Type()), pkg, name)
}

func isPkgFunc(fn *types.Func, pkg, name string) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Recv() == nil && fn.Pkg() != nil && fn.Pkg().Path() == pkg && fn.Name() == name
}

func isString(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// addrType reports whether t can carry an address: a string, net.IP or
// netip.Addr.
func addrType(t types.Type) bool {
	return isString(t) || astx.IsNamed(t, "net", "IP") || astx.IsNamed(t, "net/netip", "Addr")
}

// addrResult reports whether fn's first result can carry an address.
func addrResult(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Results().Len() > 0 && addrType(sig.Results().At(0).Type())
}

// isKeyFuncType reports whether t is func(*http.Request) string.
func isKeyFuncType(t types.Type) bool {
	if t == nil {
		return false
	}
	sig, ok := t.Underlying().(*types.Signature)
	return ok && sig.Params().Len() == 1 && sig.Results().Len() == 1 &&
		astx.IsNamed(astx.Deref(sig.Params().At(0).Type()), "net/http", "Request") &&
		isString(sig.Results().At(0).Type())
}

// returnsKeyFunc reports whether fn returns a func(*http.Request) string.
func returnsKeyFunc(fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	return ok && sig.Results().Len() == 1 && isKeyFuncType(sig.Results().At(0).Type())
}
