// Package astbase holds the one go/ast helper that two trees with
// different dependency budgets both need: framework/contracts, which
// must build without golang.org/x/tools, and internal/analyzers'
// astx, which sits on golang.org/x/tools/go/analysis. Neither can
// import the other, so the shared body lives here, importing nothing
// but go/ast. Everything in this package is behavior-neutral: it
// answers one syntactic question and decides nothing.
package astbase

import "go/ast"

// RecvBaseName returns the identifier at the base of fd's receiver
// type (T or *T), or "", including when fd has no receiver. A
// generic receiver (T[P] or *T[P]) also returns "": the bracketed
// type is not an identifier, and no caller has ever needed the
// generic base, so the refusal is deliberate.
func RecvBaseName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	var t ast.Expr
	switch r := fd.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		t = r.X
	case *ast.Ident:
		t = r
	default:
		return ""
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
