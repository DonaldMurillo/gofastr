// Package silentskip catches a loop that backs off and retries past an
// error without a word: `if err != nil { wait(); continue }` whose block
// never reads err.
//
// The bug (c9f60506f): DBQueue.workerLoop claimed a job, and on any
// error slept the poll interval and went round again:
//
//	job, err := q.dequeue(ctx, lane, types)
//	if err != nil {
//		// ErrNoJob is the steady state, sleep briefly and retry.
//		if wait() {
//			return
//		}
//		continue
//	}
//
// The comment named the one error the branch was written for; the
// condition took every error. A lost database, a dropped table or a row
// the claim could not decode stalled every job on the queue, and the
// worker said nothing at all: an outage looked exactly like an empty
// queue. The fix logs the outage's onset and recovery and tests the
// steady state with errors.Is(err, ErrNoJob).
//
// Shape: inside a for or range loop, an `if err != nil` (err of type
// error, the bare comparison, no else) whose block ends in `continue`,
// WAITS before it (time.Sleep, time.After, a timer or ticker, a select,
// or a call to a local closure or a same-package function that does one
// of those), and never reads err.
//
// Silent postures, deliberately:
//   - the block reads err in any way: logs it, wraps it, returns it,
//     records it, compares it with errors.Is or errors.As, hands it to a
//     note helper (the fix's noteClaimErr);
//   - the block calls a logger (log, log/slog, or a function or method
//     whose name leads with print, log, warn, error, info, debug or
//     note) even without err, or counts (x++, x += n): the skip leaves a
//     trace;
//   - a condition that is not the bare `err != nil` (errors.Is(err,
//     ErrNoJob), `err != nil && !retryable(err)`): the error was looked
//     at;
//   - a skip that does not wait (`if err != nil { continue }`): measured
//     below, that is mostly a deliberate filter;
//   - nested function literals are their own scope, and _test.go files
//     are skipped.
//
// Measured over ./... on 2026-11-02, before narrowing. Every loop's
// unread `if err != nil { continue }`, plus walk callbacks returning nil
// over an unread err: 101 sites. Scoped to polling loops (no condition,
// or a select, sleep or timer in the body) and file walks (ranging a
// directory listing, or reading a file in the body): 50 sites, nearly
// all deliberate: a tail -f that retries a racing Stat, an SSE loop
// dropping one unencodable event, a dev tool skipping a mid-edit file
// that does not parse, each with a comment saying so. bb4d0b62a (the
// upgrade scan skipping a gofastr.yml that failed to parse) was one of
// the 50 and is not separable from them by shape, so the walk arm was
// not shipped. The retry-after-wait shape found none once c9f60506f had
// landed: a hit here means the claim-loop bug came back.
package silentskip

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

var Analyzer = &analysis.Analyzer{
	Name: "silentskip",
	Doc:  "report a loop that waits and retries past an error without reading it (no log, no wrap, no errors.Is, no count): an outage reads as an empty poll",
	Run:  run,
}

type checker struct {
	pass *analysis.Pass
	// closures are the locals bound to a func literal
	// (wait := func() bool { select { … } }).
	closures map[types.Object]*ast.FuncLit
	// waitFuncs are this package's declared functions whose bodies wait.
	waitFuncs map[*types.Func]bool
}

func run(pass *analysis.Pass) (any, error) {
	c := &checker{pass: pass, closures: map[types.Object]*ast.FuncLit{}, waitFuncs: map[*types.Func]bool{}}
	for _, f := range pass.Files {
		c.index(f)
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ForStmt:
				c.checkLoop(x.Body)
			case *ast.RangeStmt:
				c.checkLoop(x.Body)
			}
			return true
		})
	}
	return nil, nil
}

func (c *checker) index(f *ast.File) {
	info := c.pass.TypesInfo
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Body != nil && c.waitsIn(x.Body, false) {
				if fn, ok := info.Defs[x.Name].(*types.Func); ok {
					c.waitFuncs[fn] = true
				}
			}
		case *ast.AssignStmt:
			if len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, lhs := range x.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if lit, ok := ast.Unparen(x.Rhs[i]).(*ast.FuncLit); ok {
					if obj := info.ObjectOf(id); obj != nil {
						c.closures[obj] = lit
					}
				}
			}
		}
		return true
	})
}

// checkLoop reports, in a loop's own body (not nested loops or
// literals), an `if err != nil { …wait…; continue }` that never reads
// err.
func (c *checker) checkLoop(body *ast.BlockStmt) {
	inspectOwn(body, func(ifs *ast.IfStmt) {
		errObj := c.nilCheckedErr(ifs.Cond)
		if errObj == nil || ifs.Else != nil {
			return
		}
		br, ok := lastStmt(ifs.Body).(*ast.BranchStmt)
		if !ok || br.Tok != token.CONTINUE {
			return
		}
		if !c.waitsIn(ifs.Body, true) || c.accounted(ifs.Body, errObj) {
			return
		}
		c.pass.Reportf(ifs.Pos(), "%s is waited out and retried with no trace: every error, an outage included, reads as an empty poll and nobody hears of it; log its onset, or test the expected case with errors.Is and handle the rest", errObj.Name())
	})
}

// inspectOwn visits the if statements of body, not descending into
// nested loops or function literals (their own checks cover them).
func inspectOwn(body *ast.BlockStmt, fn func(*ast.IfStmt)) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit, *ast.ForStmt, *ast.RangeStmt:
			return false
		case *ast.IfStmt:
			fn(x)
		}
		return true
	})
}

// nilCheckedErr returns the error variable of a bare `err != nil`.
func (c *checker) nilCheckedErr(cond ast.Expr) types.Object {
	bin, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || bin.Op != token.NEQ {
		return nil
	}
	x, y := ast.Unparen(bin.X), ast.Unparen(bin.Y)
	switch {
	case isNil(y):
		return c.errVar(x)
	case isNil(x):
		return c.errVar(y)
	}
	return nil
}

func isNil(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "nil"
}

func (c *checker) errVar(e ast.Expr) types.Object {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil
	}
	obj, ok := c.pass.TypesInfo.ObjectOf(id).(*types.Var)
	if !ok || !types.Identical(obj.Type(), types.Universe.Lookup("error").Type()) {
		return nil
	}
	return obj
}

func lastStmt(b *ast.BlockStmt) ast.Stmt {
	if b == nil || len(b.List) == 0 {
		return nil
	}
	return b.List[len(b.List)-1]
}

// accounted reports whether the block leaves a trace of the failure: it
// reads err, logs, or counts.
func (c *checker) accounted(body *ast.BlockStmt, errObj types.Object) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if c.pass.TypesInfo.Uses[x] == errObj {
				found = true
			}
		case *ast.IncDecStmt:
			found = true
		case *ast.AssignStmt:
			if x.Tok == token.ADD_ASSIGN {
				found = true
			}
		case *ast.CallExpr:
			if c.isLogCall(x) {
				found = true
			}
		}
		return !found
	})
	return found
}

// logVerbs are the leading words of a logging call.
var logVerbs = []string{"print", "log", "warn", "error", "info", "debug", "note"}

func (c *checker) isLogCall(call *ast.CallExpr) bool {
	fn, ok := typeutil.Callee(c.pass.TypesInfo, call).(*types.Func)
	if !ok {
		return false
	}
	if p := fn.Pkg(); p != nil && (p.Path() == "log" || p.Path() == "log/slog") {
		return true
	}
	name := strings.ToLower(fn.Name())
	for _, v := range logVerbs {
		if strings.HasPrefix(name, v) {
			return true
		}
	}
	return false
}

// waitsIn reports whether body waits: a select, a time.Sleep / After /
// Tick / NewTimer / NewTicker call, and (deep) a call to a local closure
// or a same-package function that waits.
func (c *checker) waitsIn(body *ast.BlockStmt, deep bool) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.SelectStmt:
			found = true
		case *ast.CallExpr:
			if c.waits(x) {
				found = true
			} else if deep {
				if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
					if lit := c.closures[c.pass.TypesInfo.Uses[id]]; lit != nil && c.waitsIn(lit.Body, false) {
						found = true
					}
				}
				if fn := typeutil.StaticCallee(c.pass.TypesInfo, x); fn != nil && c.waitFuncs[fn.Origin()] {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// waits matches time.Sleep, time.After, time.Tick, time.NewTimer and
// time.NewTicker.
func (c *checker) waits(call *ast.CallExpr) bool {
	fn := typeutil.StaticCallee(c.pass.TypesInfo, call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "time" {
		return false
	}
	switch fn.Name() {
	case "Sleep", "After", "Tick", "NewTimer", "NewTicker":
		return true
	}
	return false
}
