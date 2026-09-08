// Package errleak catches an internal error string being handed to the
// client on a 5xx response — or, since the 2026-09-07 round, into a
// JSON-RPC internal-error response.
//
// A 4xx carrying err.Error() is usually fine and often helpful — the
// caller sent something malformed and the parser's complaint is the most
// useful thing to say. A 5xx is different: the error is the server's
// own, and its text is written for an operator reading a log. Wrapped
// chains reach the wire carrying DSNs, absolute paths, SQL fragments, and
// driver internals. The repo has fixed instances of exactly this (a
// dotenv parse error echoing file content, a stream close code echoed
// unsanitized) without anything stopping the next one.
//
// The checks key on a call that carries BOTH a sink signal and an
// error-typed .Error() result, rather than on helper names, so a
// project's own writeJSONError/newErrorResponse helpers are covered the
// same way.
//
// Sinks:
//
// HTTP 5xx: any argument is a 5xx status, as an http.Status* constant
// or a bare 5xx literal. The fix is to log the error and send a fixed
// string.
//
// JSON-RPC internal error (round 5): any argument is an identifier
// containing "Internal" — the internal-error code constant every
// JSON-RPC surface in this repo spells ErrInternalError /
// CodeInternalError — and another argument is an error's text, either
// an error-typed .Error() result or fmt.Sprintf("%v"/"%s", err).
// Probes: core/mcp prompts.go handlePromptsGet (the plain-error branch)
// and resources.go handleResourcesRead, both
// newErrorResponse(req.ID, ErrInternalError, err.Error()), pinned by
// prompt_resource_errleak_red_test.go. Quiet postures: the fixed-string
// answer (callTool's "internal tool error", a2a's internalErr "internal
// error" — the code constant is there but no error text is), the
// *RPCError passthrough (a deliberate, caller-authored message), and
// 4xx codes echoing parser text (ErrInvalidParams + err.Error() is the
// useful answer for malformed input, and matches the HTTP arm's
// posture). Format+args spellings ("...: %v", err) are deliberately
// not resolved: the format string reaches the helper, not the wire,
// and the helper's own gate decides.
package errleak

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "errleak",
	Doc:  "forbids sending an error's text to the client on a 5xx response or inside a JSON-RPC internal-error response; log it and write a fixed message instead",
	Run:  run,
}

const internalMsg = "sends an internal error's text in a JSON-RPC internal-error response: a server-side error string carries DSNs, paths and SQL to the caller — log it and answer a fixed message (core/mcp callTool's \"internal tool error\")"

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
			if hasServerErrorStatus(pass, call.Args) {
				for _, arg := range call.Args {
					if pos, found := findErrorText(pass, arg); found {
						pass.Reportf(pos,
							"sends an internal error's text on a 5xx response: a server-side error string carries DSNs, paths and SQL to the client — log it and write a fixed message")
						return true
					}
				}
			}
			if hasInternalCodeArg(pass, call.Args) {
				for _, arg := range call.Args {
					if pos, found := findErrorText(pass, arg); found {
						pass.Reportf(pos, internalMsg)
						return true
					}
					if pos, found := findSprintfErr(pass, arg); found {
						pass.Reportf(pos, internalMsg)
						return true
					}
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

// hasInternalCodeArg reports whether any argument is an identifier
// whose name contains "Internal" — the JSON-RPC internal-error code
// constant spelling (ErrInternalError, CodeInternalError).
func hasInternalCodeArg(pass *analysis.Pass, args []ast.Expr) bool {
	for _, a := range args {
		id, ok := a.(*ast.Ident)
		if !ok {
			continue
		}
		if strings.Contains(id.Name, "Internal") {
			return true
		}
	}
	return false
}

// findErrorText looks for a call to Error() on an error-typed receiver
// anywhere inside expr, so both err.Error() and "prefix: "+err.Error()
// are caught.
func findErrorText(pass *analysis.Pass, expr ast.Expr) (pos token.Pos, found bool) {
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

// findSprintfErr looks for fmt.Sprintf whose format is "%v" or "%s"
// with an error-typed argument — the other spelling of "hand the
// error's text over".
func findSprintfErr(pass *analysis.Pass, expr ast.Expr) (pos token.Pos, found bool) {
	ast.Inspect(expr, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" {
			return true
		}
		xid, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		pkg, ok := pass.TypesInfo.Uses[xid].(*types.PkgName)
		if !ok || pkg.Imported().Path() != "fmt" {
			return true
		}
		if len(call.Args) < 2 {
			return true
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		if s, e := strconv.Unquote(lit.Value); e == nil && (s == "%v" || s == "%s") {
			for _, a := range call.Args[1:] {
				tv, ok := pass.TypesInfo.Types[a]
				if !ok {
					continue
				}
				if types.Implements(tv.Type, errorInterface) {
					pos, found = call.Pos(), true
					return false
				}
			}
		}
		return true
	})
	return pos, found
}

var errorInterface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
