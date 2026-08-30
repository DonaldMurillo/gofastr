// Package hygiene holds the small checks whose whole point is that they
// currently find nothing.
//
// Every rule here corresponds to a class this codebase has already driven
// to zero — SQL assembled with Sprintf, an error branch that does nothing,
// an http.Client with no deadline, a handler that starts work on a context
// nobody can cancel. Adding them costs no cleanup. It converts "we fixed
// that" into "that cannot come back", which is the difference between a
// habit and a guarantee, and it is why these ship as gates rather than as
// a paragraph in a review checklist.
//
// A rule that starts finding things has not become noisy: something
// regressed.
package hygiene

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// SprintfSQL is deliberately absent. It was written, run, and dropped:
// 287 hits across every DB-touching package, all of them a table or column
// identifier interpolated into DDL or DML. SQL placeholders bind values,
// not identifiers, so a migration engine has no other way to say it, and
// the sites already route through quoting helpers. The risk the rule was
// reaching for — a request-derived value reaching the format string — is
// covered for host apps by the contracts catalog (GOFASTR1401). A gate
// with 287 permanent exceptions teaches people to ignore gates.

// EmptyErrBranchAnalyzer forbids `if err != nil {}` with an empty body.
var EmptyErrBranchAnalyzer = &analysis.Analyzer{
	Name: "gofastremptyerrbranch",
	Doc:  "forbids an error branch with an empty body; handle it, or say why ignoring is right",
	Run:  runEmptyErrBranch,
}

func runEmptyErrBranch(pass *analysis.Pass) (any, error) {
	eachFile(pass, func(f *ast.File) {
		ast.Inspect(f, func(n ast.Node) bool {
			ifs, ok := n.(*ast.IfStmt)
			if !ok || ifs.Body == nil || len(ifs.Body.List) != 0 {
				return true
			}
			bin, ok := ifs.Cond.(*ast.BinaryExpr)
			if !ok || !isErrorTyped(pass, bin.X) {
				return true
			}
			pass.Reportf(ifs.Pos(),
				"error branch with an empty body: the check reads as handling and does nothing. Handle it, or drop the branch and assign to _ where ignoring is deliberate")
			return true
		})
	})
	return nil, nil
}

// ClientTimeoutAnalyzer forbids an http.Client literal with no Timeout
// and no per-call deadline. A client whose every request carries a
// context deadline does not need Client.Timeout, and core/webbotauth is
// the model: it deadlines each fetch and would only be made noisier by a
// redundant field. Same file-scoped convention as unboundedbody's cap
// check — the deadline is conventionally set beside the client it governs.
var ClientTimeoutAnalyzer = &analysis.Analyzer{
	Name: "gofastrclienttimeout",
	Doc:  "forbids constructing an http.Client without a Timeout",
	Run:  runClientTimeout,
}

func runClientTimeout(pass *analysis.Pass) (any, error) {
	eachFile(pass, func(f *ast.File) {
		if fileDeadlinesCalls(pass, f) {
			return
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isHTTPClient(pass, lit.Type) {
				return true
			}
			for _, el := range lit.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Timeout" {
						return true
					}
				}
			}
			pass.Reportf(lit.Pos(),
				"http.Client with no Timeout: the zero value waits forever, so one unresponsive peer holds the caller (and its goroutine) indefinitely")
			return true
		})
	})
	return nil, nil
}

// HandlerContext is deliberately absent. It was written, run, and dropped:
// all six hits were correct code. Work that must outlive the response is a
// real and common shape — an idempotency claim released after the handler
// answers, a cache write a disconnect must not abort, an SSE stream whose
// lifetime is deliberately decoupled from its request. Distinguishing
// those from work the client is still waiting on needs to know whether the
// response was written, which is not visible here. Every site was already
// carrying a comment explaining itself, which is the control that works.

// fileDeadlinesCalls reports whether this file bounds its requests with a
// context deadline, which makes Client.Timeout redundant rather than
// missing.
func fileDeadlinesCalls(pass *analysis.Pass, f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch qualified(pass, call.Fun) {
		case "context.WithTimeout", "context.WithDeadline":
			found = true
		}
		return !found
	})
	return found
}

// ---- shared helpers -------------------------------------------------

func eachFile(pass *analysis.Pass, fn func(*ast.File)) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		fn(f)
	}
}

// qualified renders a call target as "pkg.Func", resolving the import
// through the type checker so an aliased import is still the real package.
func qualified(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkg.Imported().Name() + "." + sel.Sel.Name
}

func isErrorTyped(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	named, ok := tv.Type.(*types.Named)
	return ok && named.Obj() != nil && named.Obj().Name() == "error"
}

func isHTTPClient(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	named, ok := tv.Type.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Name() == "Client" && named.Obj().Pkg().Path() == "net/http"
}
