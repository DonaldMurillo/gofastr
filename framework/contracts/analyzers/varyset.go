package analyzers

import (
	"go/ast"
	"net/textproto"

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
// The header name is compared the way Header.Set canonicalizes it, so
// Set("vary", …) and Set("VARY", …) are the same finding. Two more
// spellings clobber the same way and report too (2026-11-02 guard
// bash, both were silent): Del("Vary"), which drops every earlier entry
// before whatever Add follows, and the map write h["Vary"] = …, which
// replaces them. A map write under a non-canonical key ("vary") is a
// separate header line, not a clobber.
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
		// h["Vary"] = []string{…}: the map write replaces every entry
		// exactly as Set does. Only the canonical key is the same entry;
		// h["vary"] is a second header line, not a clobber.
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range as.Lhs {
				ix, ok := lhs.(*ast.IndexExpr)
				if !ok {
					continue
				}
				if key, ok := stringLit(ix.Index); ok && key == "Vary" {
					out = append(out, diag(p, contracts.RuleVarySet, rel, lhs.Pos(),
						`Vary is append-only in a middleware chain: assigning h["Vary"] replaces the entries earlier middlewares added (CORS Adds "Vary: Origin") — use .Add("Vary", …)`))
				}
			}
			return true
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		// Del drops every entry earlier middlewares added; an Add after
		// it leaves the same single-entry header Set would.
		if !ok || sel.Sel == nil || (sel.Sel.Name != "Set" && sel.Sel.Name != "Del") {
			return true
		}
		name, ok := stringLit(call.Args[0])
		// Header.Set canonicalizes its key: "vary" and "VARY" are the
		// same clobbering write.
		if !ok || textproto.CanonicalMIMEHeaderKey(name) != "Vary" {
			return true
		}
		out = append(out, diag(p, contracts.RuleVarySet, rel, call.Pos(),
			`Vary is append-only in a middleware chain: Set replaces the entries earlier middlewares added (CORS Adds "Vary: Origin"; a later Set leaves a cache serving one variant to everyone, the TestIdempotencyVaryEatsCors probe) — use .Add("Vary", …)`))
		return true
	})
	return out
}
