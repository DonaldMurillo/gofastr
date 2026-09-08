package analyzers

import (
	"go/ast"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// ----------------------------------------------------------------------
// GOFASTR1413: Header().Set("Vary", …) in a middleware chain.
// ----------------------------------------------------------------------

// Bug class: Vary written with Set anywhere in the tree. Vary is
// append-only in a middleware chain: a response crosses CORS (which
// Adds "Vary: Origin"), an idempotency layer, a cache layer, and any
// later Set replaces every entry the earlier middlewares added — a
// shared cache then serves one principal's variant to another. The
// 2026-09-07 red probe TestIdempotencyVaryEatsCors pinned it live:
// CORS(...)(Idempotency(...)) with an over-cap POST produced a
// response carrying ACAO but Vary listing ONLY Idempotency-Key,
// because core/middleware/idempotency.go's body-too-large bypass arm
// wrote Set("Vary", "Idempotency-Key") over the CORS Add. The repo's
// rule (core/middleware/cors.go) and every other Vary writer already
// spell Add; this rule keeps the one Set from coming back.
//
// Deliberately silent on:
//   - .Add("Vary", …), the append-only spelling every correct site
//     uses (cors.go, wellknown.go, embed/middleware.go, uihost, bff);
//   - Set of any other header (this rule is about Vary only);
//   - _test.go and generated files (AppFiles already excludes both);
//   - any site annotated //gofastr:allow(GOFASTR1413) <why>.
func ruleVarySet(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Set" {
			return true
		}
		name, ok := stringLit(call.Args[0])
		if !ok || name != "Vary" {
			return true
		}
		out = append(out, diag(p, contracts.RuleVarySet, rel, call.Pos(),
			`Vary is append-only in a middleware chain: Set replaces the entries earlier middlewares added (CORS Adds "Vary: Origin"; a later Set leaves a cache serving one variant to everyone, the TestIdempotencyVaryEatsCors probe) — use .Add("Vary", …)`))
		return true
	})
	return out
}
