package analyzers

import (
	"go/ast"
	"net/textproto"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1411: a private copy of the Sec-Fetch-Site cross-site predicate.

func ruleFetchMetadata(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	if rel == "core/handler" || strings.HasPrefix(rel, "core/handler/") {
		return nil
	}
	var out []contracts.Diagnostic
	ast.Inspect(file, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			sel, ok := v.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || (sel.Sel.Name != "Get" && sel.Sel.Name != "Values") || len(v.Args) != 1 {
				return true
			}
			if !headerish(sel.X) {
				return true
			}
			name, ok := stringLit(v.Args[0])
			if !ok || textproto.CanonicalMIMEHeaderKey(name) != "Sec-Fetch-Site" {
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

func headerish(e ast.Expr) bool {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel != nil && v.Sel.Name == "Header"
	case *ast.Ident:
		return true
	}
	return false
}
