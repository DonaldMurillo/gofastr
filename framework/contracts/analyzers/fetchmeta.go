package analyzers

import (
	"go/ast"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// ----------------------------------------------------------------------
// GOFASTR1411: a private copy of the Sec-Fetch-Site cross-site predicate.
// ----------------------------------------------------------------------

// Bug class: a file outside core/handler reads the Sec-Fetch-Site
// header directly. The repo has ONE cross-site predicate —
// core/handler.IsCrossSiteRequest, Sec-Fetch-Site first with the
// Origin-host compare as the fallback — and every private copy has
// been a divergence waiting to be exploited: the 2026-09-06/07
// adversarial round (red probes TestSetupRedSameSiteOriginRefused,
// TestCSRFRedSameSiteFallsThrough) found two copies that early-allow
// "same-site" (a sibling subdomain IS same-site, so the Strict cookie
// rides and only the Origin compare can refuse the attack), the
// battery/admin copy that omitted the Origin fallback, and nine
// surfaces in total whose copies disagreed about which values are
// trusted. The rule's job is not to judge a copy's cases — it is to
// keep the tenth copy from appearing: the predicate is
// security-critical and lives in exactly one place.
//
// The read is Get("Sec-Fetch-Site") on a receiver ending in .Header or
// on a bare identifier (h http.Header handed to a helper), or the
// canonical-key map form Header["Sec-Fetch-Site"]. Prose mentioning
// the header (a ticket body in examples/site) is a string literal in
// another position and cannot match.
//
// Deliberately silent on:
//   - core/handler/**, which owns the one predicate;
//   - a file that never reads the header: battery/auth/magiclink.go
//     and battery/admin/admin.go route through their packages'
//     rejectCrossSiteForm rather than reading the header themselves,
//     and examples/site/screen_workspace.go only mentions it in a
//     display string — the consolidation the fix round performs makes
//     every one of them call handler.IsCrossSiteRequest, at which
//     point nothing outside core/handler reads the header at all;
//   - _test.go and generated files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1411) <why>.
func ruleFetchMetadata(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	if rel == "core/handler" || strings.HasPrefix(rel, "core/handler/") {
		return nil
	}
	var out []contracts.Diagnostic
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "Get" || len(v.Args) != 1 {
				return true
			}
			if !headerish(sel.X) {
				return true
			}
			name, ok := stringLit(v.Args[0])
			if !ok || name != "Sec-Fetch-Site" {
				return true
			}
			out = append(out, diag(p, contracts.RuleFetchMetadata, rel, v.Pos(),
				"this file reads Sec-Fetch-Site itself: the cross-site predicate is security-critical and lives in one place, and every private copy so far has diverged (the setup and kiln copies early-allowed \"same-site\", a sibling subdomain the Strict cookie still rides) — call handler.IsCrossSiteRequest (Sec-Fetch-Site first, Origin-host compare as the fallback) and keep the response shape local"))
		case *ast.IndexExpr:
			name, ok := stringLit(v.Index)
			if !ok || name != "Sec-Fetch-Site" || !headerish(v.X) {
				return true
			}
			out = append(out, diag(p, contracts.RuleFetchMetadata, rel, v.Pos(),
				"this file reads Sec-Fetch-Site itself: the cross-site predicate is security-critical and lives in one place, and every private copy so far has diverged (the setup and kiln copies early-allowed \"same-site\", a sibling subdomain the Strict cookie still rides) — call handler.IsCrossSiteRequest (Sec-Fetch-Site first, Origin-host compare as the fallback) and keep the response shape local"))
		}
		return true
	})
	return out
}

// headerish reports whether e is a header map the way the forwarded-
// proto rule reads it: a selector ending in .Header (r.Header) or a
// bare identifier (h http.Header passed to a helper — the same map).
func headerish(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel != nil && v.Sel.Name == "Header"
	case *ast.Ident:
		return true
	}
	return false
}
