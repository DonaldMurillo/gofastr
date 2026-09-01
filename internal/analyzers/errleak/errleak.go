// Package errleak catches an internal error string being handed to the
// client on a 5xx response.
//
// A 4xx carrying err.Error() is usually fine and often helpful — the
// caller sent something malformed and the parser's complaint is the most
// useful thing to say. A 5xx is different: the error is the server's own,
// and its text is written for an operator reading a log. Wrapped chains
// reach the wire carrying DSNs, absolute paths, SQL fragments, and driver
// internals. The repo has fixed instances of exactly this (a dotenv parse
// error echoing file content, a stream close code echoed unsanitized)
// without anything stopping the next one.
//
// The check keys on a call that carries BOTH a 5xx status and an
// error-typed .Error() result, rather than on http.Error by name, so a
// project's own writeJSONError/writeError helpers are covered the same
// way. The fix is to log the error and send a fixed string.
package errleak

import (
	"go/ast"
	"go/constant"
	gotoken "go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrerrleak",
	Doc:  "forbids sending an error's text on a 5xx response; log it and write a fixed message instead",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !hasServerErrorStatus(pass, call.Args) {
				return true
			}
			for _, arg := range call.Args {
				if pos, found := findErrorText(pass, arg); found {
					pass.Reportf(pos,
						"sends an internal error's text on a 5xx response: a server-side error string carries DSNs, paths and SQL to the client — log it and write a fixed message")
					return true
				}
			}
			return true
		})
	}
	return nil, nil
}

// hasServerErrorStatus reports whether any argument is a 5xx status, as
// an http.Status* constant or a bare 5xx literal.
func hasServerErrorStatus(pass *analysis.Pass, args []ast.Expr) bool {
	for _, a := range args {
		tv, ok := pass.TypesInfo.Types[a]
		if !ok || tv.Value == nil {
			continue
		}
		n, ok := constant.Int64Val(constant.ToInt(tv.Value))
		if !ok {
			continue
		}
		if n >= 500 && n <= 599 {
			return true
		}
	}
	return false
}

// findErrorText looks for a call to Error() on an error-typed receiver
// anywhere inside expr, so both err.Error() and "prefix: "+err.Error()
// are caught.
func findErrorText(pass *analysis.Pass, expr ast.Expr) (pos gotoken.Pos, found bool) {
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Error" || len(call.Args) != 0 {
			return true
		}
		tv, ok := pass.TypesInfo.Types[sel.X]
		if !ok {
			return true
		}
		if types.Implements(tv.Type, errorInterface) {
			pos, found = call.Pos(), true
			return false
		}
		return true
	})
	return pos, found
}

var errorInterface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
