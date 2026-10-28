// Package resolvedial catches an SSRF guard that resolves a host name,
// checks the answer, and then lets the transport resolve the same name
// again to dial it.
//
// The bug: a guard that looks the request host up (net.LookupIP,
// (*net.Resolver).LookupIPAddr, ...) and refuses internal answers checks
// one DNS answer, while the inner transport's dialer asks again at
// connect time. A name whose owner controls its DNS answers public to
// the check and 169.254.169.254 or 127.0.0.1 to the dial (DNS
// rebinding), so the check passes and the connection lands inside.
// Found by the 2026-10-27 bug bash:
//   - B64: battery/webhook/ssrf.go:121-132 ssrfGuardedRoundTripper
//     resolves with LookupIPAddr, checks, and returns
//     g.inner.RoundTrip(r). ssrfGuardClient falls back to it for any
//     transport netguard.GuardPlainTransport refuses, which includes
//     the usual http.DefaultTransport.Clone() (it carries Proxy and
//     DialContext, core/netguard/transport.go:90-92).
//   - P54: core/a2a/push.go:420-437 guardedRoundTripper, the same
//     shape for A2A push notifications to a client-registered URL.
//   - H12: framework/experimental/harness/tool/builtins/webfetch.go
//     preflights the URL with assertPublicHost (net.LookupIP, :240) and
//     installs the dial-time guard only `if safeClient.Transport == nil`
//     (:129-131), so a host-supplied transport dials whatever the name
//     answers at connect time.
//
// The fix spelling checks the address actually dialled, at dial time:
// netguard.GuardedTransport (a net.Dialer.Control hook that runs
// netguard.Reason on the resolved numeric address) or
// netguard.GuardPlainTransport for a caller's plain *http.Transport.
// netguard's withDialer refuses a transport with its own Proxy or
// dialer because swapping either would break routing that reaches
// internal addresses on purpose; such a transport needs its own
// dial-time check (or the request rewritten to the checked IP), not a
// second, earlier lookup.
//
// Shape, all within one function body (a declaration or a literal,
// each walked on its own):
//   - resolve then pass through: a host resolution (a net lookup
//     call, or a call to a function that performs one, directly or
//     through further calls; the property travels across packages as
//     a fact) precedes, in source order, a call to a
//     RoundTrip(*http.Request) method, and the function never rewrites
//     the request target (no assignment to a url.URL's Host or a
//     request's URL). Reported at the RoundTrip call (B64, P54).
//   - nil-only guard install: the function resolves a host, and an
//     `if <client>.Transport == nil { ... }` with no else assigns the
//     client's Transport, so a non-nil, caller-supplied transport keeps
//     its own dialer behind a check that already happened. Reported at
//     the if (H12).
//
// Silent postures, deliberately:
//   - a RoundTrip call that comes before the lookup in source order:
//     push.go's IP-literal branch checks the literal and delegates
//     without resolving, and a literal cannot rebind;
//   - a request whose URL.Host (or URL) is rewritten in the function:
//     the dial then targets the checked IP, which is a correct spelling;
//   - a lookup inside a dialer (DialContext or Control) that dials the
//     IP it checked: no RoundTrip follows, and that is the fix;
//   - registration-time URL validation (validatePushURL,
//     validateSubscriberURL, webbotauth's checkHostPublic) that resolves
//     and returns without sending: the send happens elsewhere, behind
//     the dial-time guard;
//   - a client sent with Do/Get after a preflight lookup, when the
//     function does not install a transport conditionally: the rule
//     cannot see which transport the client carries, and a preflight
//     in front of a dial-guarded transport is defence in depth;
//   - an `if Transport == nil` install with an else (or else-if chain)
//     that also assigns the transport: every branch decided, and a
//     resolve-then-pass-through branch is caught by the first arm;
//   - _test.go files.
package resolvedial

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "resolvedial",
	Doc:       "report SSRF guards that resolve and check a host, then let the transport resolve it again at dial time (DNS rebinding)",
	Run:       run,
	FactTypes: []analysis.Fact{(*resolvesHost)(nil)},
}

const advice = "check the dialled address in a dial-time hook (netguard.GuardedTransport / GuardPlainTransport, or a DialContext that dials the IP it checked)"

// resolvesHost marks a function that resolves a host name through DNS,
// directly or through a call to another such function.
type resolvesHost struct{}

func (*resolvesHost) AFact() {}

func (*resolvesHost) String() string { return "resolvesHost" }

// lookups are the net resolution entry points, by types.Func FullName.
// LookupAddr (reverse) and LookupCNAME/MX/TXT do not yield the dial
// address and stay out.
var lookups = map[string]bool{
	"net.LookupIP":                 true,
	"net.LookupHost":               true,
	"(*net.Resolver).LookupIPAddr": true,
	"(*net.Resolver).LookupIP":     true,
	"(*net.Resolver).LookupHost":   true,
	"(*net.Resolver).LookupNetIP":  true,
}

func run(pass *analysis.Pass) (any, error) {
	var fns []*ast.FuncDecl
	var files []*ast.File
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		files = append(files, f)
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				fns = append(fns, fn)
			}
		}
	}
	local := resolvers(pass, fns)
	for _, obj := range slices.SortedFunc(maps.Keys(local), func(a, b *types.Func) int { return int(a.Pos() - b.Pos()) }) {
		if !obj.Exported() {
			// Only an exported name is callable from another package.
			continue
		}
		pass.ExportObjectFact(obj, &resolvesHost{})
	}
	for _, f := range files {
		for _, fn := range astx.AllFuncs(f) {
			checkBody(pass, astx.BodyOf(fn), local)
		}
	}
	return nil, nil
}

// resolvers is the fixpoint of package functions whose body (literals
// included: a lookup in a goroutine still resolves) reaches a lookup.
func resolvers(pass *analysis.Pass, fns []*ast.FuncDecl) map[*types.Func]bool {
	local := map[*types.Func]bool{}
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok || local[obj] {
				continue
			}
			found := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if found {
					return false
				}
				if call, ok := n.(*ast.CallExpr); ok && isResolve(pass, call, local) {
					found = true
				}
				return true
			})
			if found {
				local[obj] = true
				changed = true
			}
		}
	}
	return local
}

// isResolve: call is a net lookup or a call to a resolving function.
func isResolve(pass *analysis.Pass, call *ast.CallExpr, local map[*types.Func]bool) bool {
	fn, ok := astx.CalleeFunc(pass, ast.Unparen(call.Fun))
	if !ok {
		return false
	}
	fn = fn.Origin()
	if lookups[fn.FullName()] {
		return true
	}
	if local[fn] {
		return true
	}
	if fn.Pkg() != nil && fn.Pkg() != pass.Pkg {
		var fact resolvesHost
		return pass.ImportObjectFact(fn, &fact)
	}
	return false
}

// checkBody walks one function body without descending into nested
// literals (each literal is its own body).
func checkBody(pass *analysis.Pass, body *ast.BlockStmt, local map[*types.Func]bool) {
	if body == nil {
		return
	}
	firstResolve := token.NoPos
	rewrites := false
	var trips []*ast.CallExpr
	var nilInstalls []*ast.IfStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if isResolve(pass, n, local) && (firstResolve == token.NoPos || n.Pos() < firstResolve) {
				firstResolve = n.Pos()
			}
			if isRoundTrip(pass, n) {
				trips = append(trips, n)
			}
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if rewritesTarget(pass, lhs) {
					rewrites = true
				}
			}
		case *ast.IfStmt:
			if nilOnlyTransportInstall(pass, n) {
				nilInstalls = append(nilInstalls, n)
			}
		}
		return true
	})
	if firstResolve == token.NoPos {
		return
	}
	if !rewrites {
		for _, call := range trips {
			if call.Pos() > firstResolve {
				pass.Reportf(call.Pos(),
					"resolvedial: RoundTrip after a resolve-and-check lets the inner transport resolve the host again to dial; a DNS-rebinding name answers public to the check and internal to the dial (%s)", advice)
			}
		}
	}
	for _, s := range nilInstalls {
		pass.Reportf(s.Pos(),
			"resolvedial: the dial-time guard is installed only when Transport is nil; a caller-supplied transport re-resolves at dial after the resolve-and-check passed, so DNS rebinding reaches internal addresses (%s)", advice)
	}
}

// isRoundTrip: call is a RoundTrip(*http.Request) (*http.Response,
// error) method call, on an interface or a concrete transport.
func isRoundTrip(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "RoundTrip" || len(call.Args) != 1 {
		return false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || sig.Params().Len() != 1 || sig.Results().Len() != 2 {
		return false
	}
	return isHTTP(sig.Params().At(0).Type(), "Request", true) &&
		isHTTP(sig.Results().At(0).Type(), "Response", true)
}

// rewritesTarget: lhs is `<url>.Host` on a url.URL, or `<req>.URL` on
// an http.Request: the request then dials something the function chose.
func rewritesTarget(pass *analysis.Pass, lhs ast.Expr) bool {
	sel, ok := ast.Unparen(lhs).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	if t == nil {
		return false
	}
	switch sel.Sel.Name {
	case "Host":
		return astx.IsNamed(astx.Deref(t), "net/url", "URL")
	case "URL":
		return isHTTP(t, "Request", false)
	}
	return false
}

// nilOnlyTransportInstall: `if x.Transport == nil { ... x.Transport = ... }`
// on an http.Client with no else branch.
func nilOnlyTransportInstall(pass *analysis.Pass, s *ast.IfStmt) bool {
	if s.Else != nil {
		return false
	}
	bin, ok := ast.Unparen(s.Cond).(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return false
	}
	x, other := bin.X, bin.Y
	if isNil(pass, x) {
		x, other = other, x
	}
	if !isNil(pass, other) {
		return false
	}
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Transport" || !isClient(pass, sel.X) {
		return false
	}
	assigned := false
	ast.Inspect(s.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if l, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok && l.Sel.Name == "Transport" && isClient(pass, l.X) {
				assigned = true
			}
		}
		return true
	})
	return assigned
}

func isNil(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	return ok && tv.IsNil()
}

func isClient(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	return t != nil && isHTTP(t, "Client", false)
}

// isHTTP: t is net/http.<name>, or a pointer to it (mustPtr demands
// the pointer).
func isHTTP(t types.Type, name string, mustPtr bool) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	} else if mustPtr {
		return false
	}
	return astx.IsNamed(t, "net/http", name)
}
