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
	"go/token"
	"go/types"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
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
	Name: "emptyerrbranch",
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
// and no per-call deadline, and — since the 2026-09-07 round — the
// spellings that skip the literal entirely: the http.Get/http.Post/
// http.Head/http.PostForm sugar and http.DefaultClient, which all run
// on a client with no deadline at all. A client whose every request
// carries a context deadline does not need Client.Timeout, and
// core/webbotauth is the model: it deadlines each fetch and would
// only be made noisier by a redundant field. Same file-scoped
// convention as unboundedbody's cap check — the deadline is
// conventionally set beside the client it governs.
//
// Probes for the sugar arm: cmd/kiln portFree (an http.Get against a
// port that may be a wedged service, in a CLI whose operator is
// staring at a hang), cmd/bench-resources' load loop, cmd/gofastr
// remoteQuery/remoteGet. A per-request context deadline in the same
// function (context.WithTimeout + http.NewRequestWithContext —
// evalrunner/mcpprobe) is the fix posture and is quiet.
//
// Silent postures, deliberately: _test.go files (a test hanging is a
// failed test, and the suite owns the deadline); code emitted as
// STRING text into generated files (blueprint.go's e2e templates,
// generate_cli_openapi_render.go) — the analyzer reads ASTs, and
// template text has none until the generator runs.
var ClientTimeoutAnalyzer = &analysis.Analyzer{
	Name: "clienttimeout",
	Doc:  "forbids a zero-timeout HTTP client: an http.Client literal with no Timeout, the http.Get/Post/Head/PostForm sugar, or http.DefaultClient",
	Run:  runClientTimeout,
}

const bareClientMsg = "zero-timeout HTTP client: %s waits forever on an unresponsive peer, so one wedged endpoint holds the caller (and its goroutine) indefinitely — use a client with a Timeout, or deadline the request context"

func runClientTimeout(pass *analysis.Pass) (any, error) {
	eachFile(pass, func(f *ast.File) {
		fileDeadlined := fileDeadlinesCalls(pass, f)
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
			if !fileDeadlined {
				pass.Reportf(lit.Pos(),
					"http.Client with no Timeout: the zero value waits forever, so one unresponsive peer holds the caller (and its goroutine) indefinitely")
			}
			return true
		})

		// The sugar helpers and DefaultClient skip the literal: no
		// Timeout field exists to read, so the per-call context
		// deadline (this function) or the file-level convention can be
		// the only credit.
		fnHasReqCtx := map[*ast.FuncDecl]bool{}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ctx, reqCtx := false, false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					switch astx.PkgFuncName(pass, call.Fun) {
					case "context.WithTimeout", "context.WithDeadline":
						ctx = true
					case "http.NewRequestWithContext":
						reqCtx = true
					}
				}
				return true
			})
			fnHasReqCtx[fd] = ctx && reqCtx
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				q := astx.PkgFuncName(pass, v.Fun)
				switch q {
				case "http.Get", "http.Post", "http.Head", "http.PostForm":
					if !fileDeadlined {
						pass.Reportf(v.Pos(), bareClientMsg, q)
					}
				}
				return true
			case *ast.SelectorExpr:
				// Any value read of http.DefaultClient — bare mention,
				// DefaultClient.Do, handed to a helper. Inspect visits
				// the inner selector of DefaultClient.Do, so one match
				// covers every spelling.
				id, ok := v.X.(*ast.Ident)
				if !ok || v.Sel.Name != "DefaultClient" {
					return true
				}
				pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
				if !ok || pkg.Imported().Path() != "net/http" {
					return true
				}
				tv, ok := pass.TypesInfo.Types[v]
				if !ok || !tv.IsValue() {
					return true
				}
				if fileDeadlined || enclosingCredit(v.Pos(), fnHasReqCtx) {
					return true
				}
				pass.Reportf(v.Pos(), bareClientMsg, "http.DefaultClient")
				return true
			}
			return true
		})
	})
	return nil, nil
}

// enclosingCredit reports whether the function enclosing pos carries
// a per-call deadline: context.WithTimeout/WithDeadline plus
// http.NewRequestWithContext (mcpprobe's shape).
func enclosingCredit(pos token.Pos, credit map[*ast.FuncDecl]bool) bool {
	for fd, ok := range credit {
		if ok && fd.Pos() <= pos && pos <= fd.End() {
			return true
		}
	}
	return false
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
		switch astx.PkgFuncName(pass, call.Fun) {
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
