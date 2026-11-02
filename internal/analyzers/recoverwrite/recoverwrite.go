// Package recoverwrite catches a deferred recover that answers a panic
// by writing a response without first asking whether the response had
// already started.
//
// The bug: once a handler has sent its status (and maybe part of its
// body), a panic cannot become an error response. net/http drops the
// second WriteHeader as superfluous and appends the error body to the
// bytes already sent, so the client reads a 200 whose body ends in an
// error document, or a JSON array with `{"error":...}` spliced on. Found
// by the 2026-11 bug bash:
//   - 8708ddd5: core/handler HandlerAdapter's recover called
//     WriteError(w, r, Fail(500, ...)) unconditionally; a panic inside a
//     ResponseType's WriteBody, after the 200 went out, spliced a JSON
//     error onto the body;
//   - c0d6e99e: core/middleware RecoveryFn's recover wrote
//     i18n.HTTPError(w, r, 500, ...) unconditionally, the same splice for
//     every panic after a status, a flush or body bytes.
//
// The fix spelling is the one both carry now: track whether the
// response started (HandlerAdapter's `started` set just before the
// status goes out; Recovery's recoveryWriter latching started on
// WriteHeader, Write, Flush, ReadFrom and a successful Hijack), and once
// it has, log and panic(http.ErrAbortHandler) so net/http cuts the
// connection: the client sees a truncated response, never a successful
// one with error text in it.
//
// Shape: a `defer` of a function literal (or of a same-package function
// declaration) whose body calls the builtin recover() outside any
// nested literal, and that then writes a response: a WriteHeader or
// Write on an http.ResponseWriter, http.Error/Redirect/NotFound/
// ServeContent/ServeFile on one, fmt.Fprint*/io.WriteString/io.Copy into
// one, json.NewEncoder(w).Encode, or a call to a respond helper — a
// function that writes its own ResponseWriter parameter, computed to a
// fixed point in the package and exported as a writesResponse fact, so
// handler.WriteError and i18n.HTTPError count from any importer.
// Reported at the write, unless the same deferred body reads, at a
// position before the write, a variable, field or method whose name
// says the response state: started, wrote/written, wroteHeader,
// committed, sent/headerSent, flushed, hijacked (camel-case words, so
// `rw.started`, `w.Written()`, `headerSent` all count).
//
// Silent postures, deliberately:
//   - a guard read anywhere earlier in the deferred body, whether or not
//     it dominates the write: `cut = cut || rw.started; ...; if cut {
//     panic(...) }` is Recovery's spelling, and following the flag
//     through locals buys nothing a reviewer cannot see;
//   - a deferred recover that only logs, records a metric or re-panics
//     (metrics, tracing, idempotency): no write, no splice;
//   - a deferred function with no recover() in it: it answers no
//     panic (a deferred write after an early return is doublewrite's);
//   - a write inside a nested function literal in the deferred body (a
//     callback built there and run later): not this recover's write;
//   - _test.go files: a test's recover is asserting the panic, not
//     answering a client.
package recoverwrite

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/rwcall"
)

var Analyzer = &analysis.Analyzer{
	Name:      "recoverwrite",
	Doc:       "report a deferred recover that writes a response with no check that the response had not already started",
	Run:       run,
	FactTypes: []analysis.Fact{(*writesResponse)(nil)},
}

// writesResponse marks a function that writes the ResponseWriter
// parameters at these argument indexes (a respond helper).
type writesResponse struct{ Index []int }

func (*writesResponse) AFact() {}

func (f *writesResponse) String() string { return fmt.Sprintf("writesResponse%v", f.Index) }

func run(pass *analysis.Pass) (any, error) {
	imported := func(fn *types.Func) []int {
		var f writesResponse
		if fn.Pkg() != pass.Pkg && pass.ImportObjectFact(fn, &f) {
			return f.Index
		}
		return nil
	}
	local := rwcall.Writers(pass, imported)
	for _, fn := range slices.SortedFunc(maps.Keys(local), func(a, b *types.Func) int { return int(a.Pos() - b.Pos()) }) {
		pass.ExportObjectFact(fn, &writesResponse{Index: local[fn]})
	}
	helpers := func(fn *types.Func) []int {
		if idx, ok := local[fn]; ok {
			return idx
		}
		return imported(fn)
	}
	decls := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					decls[fn] = fd
				}
			}
		}
	}
	reported := map[token.Pos]bool{}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ds, ok := n.(*ast.DeferStmt)
			if !ok {
				return true
			}
			var body *ast.BlockStmt
			switch fun := ast.Unparen(ds.Call.Fun).(type) {
			case *ast.FuncLit:
				body = fun.Body
			default:
				if fn, ok := astx.CalleeFunc(pass, fun); ok {
					if fd := decls[fn.Origin()]; fd != nil {
						body = fd.Body
					}
				}
			}
			if body == nil {
				return true
			}
			check(pass, body, helpers, reported)
			return true
		})
	}
	return nil, nil
}

// check reports the response writes in a deferred body that calls
// recover, when no response-state read precedes them.
func check(pass *analysis.Pass, body *ast.BlockStmt, helpers rwcall.HelperIndex, reported map[token.Pos]bool) {
	recoverAt := token.NoPos
	var guards []token.Pos
	var writes []*ast.CallExpr
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
				if b, ok := pass.TypesInfo.Uses[id].(*types.Builtin); ok && b.Name() == "recover" {
					if !recoverAt.IsValid() {
						recoverAt = n.Pos()
					}
					return true
				}
			}
			if rwcall.Target(pass, n, helpers) != nil {
				writes = append(writes, n)
			}
		case *ast.Ident:
			if stateName(n.Name) {
				if _, isVar := pass.TypesInfo.Uses[n].(*types.Var); isVar {
					guards = append(guards, n.Pos())
				} else if _, isFunc := pass.TypesInfo.Uses[n].(*types.Func); isFunc {
					guards = append(guards, n.Pos())
				}
			}
		}
		return true
	})
	if !recoverAt.IsValid() {
		return
	}
	for _, w := range writes {
		if w.Pos() < recoverAt || reported[w.Pos()] {
			continue
		}
		guarded := false
		for _, g := range guards {
			if g < w.Pos() {
				guarded = true
			}
		}
		if guarded {
			continue
		}
		reported[w.Pos()] = true
		pass.Reportf(w.Pos(),
			"recoverwrite: a deferred recover writes a response with no check that the response had not started; a panic after the status went out splices this onto the partial body (track started in the writer, and once it is set log and panic(http.ErrAbortHandler) instead)")
	}
}

// stateWords are the camel-case words that name response state.
var stateWords = map[string]bool{
	"started": true, "wrote": true, "written": true, "committed": true,
	"sent": true, "flushed": true, "hijacked": true, "streamed": true,
}

func stateName(name string) bool {
	for _, w := range astx.SplitWords(name) {
		if stateWords[strings.ToLower(w)] {
			return true
		}
	}
	return false
}
