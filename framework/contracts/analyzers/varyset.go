package analyzers

import (
	"go/ast"
	"go/types"
	"net/textproto"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1413: Vary written with Set, Del or a map write.

func ruleVarySet(p *contracts.Pass, rel string, file *ast.File) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	ast.Inspect(file, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for i, lhs := range as.Lhs {
				ix, ok := lhs.(*ast.IndexExpr)
				if !ok {
					continue
				}
				if len(as.Rhs) == len(as.Lhs) && appendsToItself(ix, as.Rhs[i]) {
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
		if !ok || sel.Sel == nil || (sel.Sel.Name != "Set" && sel.Sel.Name != "Del") {
			return true
		}
		name, ok := stringLit(call.Args[0])
		if !ok || textproto.CanonicalMIMEHeaderKey(name) != "Vary" {
			return true
		}
		out = append(out, diag(p, contracts.RuleVarySet, rel, call.Pos(),
			`Vary is append-only in a middleware chain: Set replaces the entries earlier middlewares added (CORS Adds "Vary: Origin"; a later Set leaves a cache serving one variant to everyone, the TestIdempotencyVaryEatsCors probe) — use .Add("Vary", …)`))
		return true
	})
	return out
}

// appendsToItself reports whether rhs is append(<same map>["Vary"], …), which is Add spelled as a map write.
func appendsToItself(lhs *ast.IndexExpr, rhs ast.Expr) bool {
	call, ok := ast.Unparen(rhs).(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return false
	}
	if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "append" {
		return false
	}
	src, ok := ast.Unparen(call.Args[0]).(*ast.IndexExpr)
	if !ok {
		return false
	}
	key, ok := stringLit(src.Index)
	return ok && key == "Vary" && types.ExprString(src.X) == types.ExprString(lhs.X)
}
