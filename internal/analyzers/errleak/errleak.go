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
//
// Both arms read the error's text in every spelling that builds the
// body string in place: err.Error(), "prefix: "+err.Error(),
// fmt.Sprint(err), and fmt.Sprintf with a text verb (%v %s %q, with
// any flags or an explicit [n] argument index) and the error among its
// operands — fmt.Sprintf("query failed: %v", err) is a built string,
// not a format handed to a helper; the format may be a named constant
// (the type checker resolves it). Until the 2026-11-02 guard bash the HTTP
// arm read only .Error(), and the JSON-RPC arm only the bare
// "%v"/"%s" format; until the 2026-11-03 gap round an [n] index or a
// constant format slipped both.
//
// Two arms from the 2026-11 bug bashes ask where the error came from;
// their contract, sinks and silences are in origin.go. Decode
// (1a4d9e2a): an error from json.Unmarshal / (*json.Decoder).Decode,
// directly, %w-wrapped, or returned by a helper (rawJSONErr fact), whose
// text reaches a call with a 4xx/5xx HTTP status or a JSON-RPC error
// code, or a Code/Message literal. Fix: handler.JSONErrorDetail(err).
// Rewrap (d01462e8a): `&T{Message: err.Error()}` (or a helper returning a
// T-carrying value) for an error type T, where err is a callback's error
// (foreignErr fact) or a parameter and the function never tests it for
// *T first. Fix: pass a *T through (errors.AsType[*T]).
//
// First whole-tree run: two decode hits in core/acp (the envelope parse
// error and decodeSessionParams, fixed); six rewrap hits in core/mcp on
// handler.JSONErrorDetail(err).Error(), removed by treating a local bound
// only to func literals (JSONErrorDetail's pick helper) as the function's
// own code rather than a callback.
package errleak

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

var Analyzer = &analysis.Analyzer{
	Name:      "errleak",
	Doc:       "forbids sending an error's text to the client on a 5xx response or inside a JSON-RPC internal-error response, encoding/json's error text in 4xx/5xx HTTP and JSON-RPC error responses, and rebuilding a typed error from its own text; log it and write a fixed message instead",
	Run:       run,
	FactTypes: []analysis.Fact{(*rawJSONErr)(nil), (*foreignErr)(nil)},
}

const internalMsg = "sends an internal error's text in a JSON-RPC internal-error response: a server-side error string carries DSNs, paths and SQL to the caller — log it and answer a fixed message (core/mcp callTool's \"internal tool error\")"

func run(pass *analysis.Pass) (any, error) {
	reported := map[*ast.CallExpr]bool{}
	var decls []*ast.FuncDecl
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var message string
			switch {
			case hasServerErrorStatus(pass, call.Args):
				message = "sends an internal error's text on a 5xx response: a server-side error string carries DSNs, paths and SQL to the client — log it and write a fixed message"
			case hasInternalCodeArg(pass, call.Args):
				message = internalMsg
			}
			if message != "" {
				for _, arg := range call.Args {
					if pos := firstErrorText(pass, arg); pos.IsValid() {
						pass.Reportf(pos, "%s", message)
						reported[call] = true
						break
					}
				}
			}
			return true
		})
	}
	// The decode and rewrap arms (origin.go) need to know where an
	// error came from; the summaries are computed to a fixed point and
	// exported as facts first.
	c := summarize(pass, decls)
	for _, fd := range decls {
		checkDecode(pass, c, fd, reported)
		checkRewrap(pass, c, fd)
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

func firstErrorText(pass *analysis.Pass, expr ast.Expr) token.Pos {
	var formatted token.Pos
	for call := range errorTexts(pass, expr) {
		if len(call.Args) == 0 {
			return call.Pos() // Prefer a direct Error call within this argument.
		}
		if !formatted.IsValid() {
			formatted = call.Pos()
		}
	}
	return formatted
}

var errorInterface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
