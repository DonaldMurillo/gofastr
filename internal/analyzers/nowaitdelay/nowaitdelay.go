// Package nowaitdelay catches an exec.CommandContext child with
// captured stdout started with no WaitDelay bound.
//
// The bug: exec.CommandContext's context cancellation kills ONLY the
// direct child. When stdout is captured through a pipe (Output,
// CombinedOutput, StdoutPipe, or a buffer assigned to Stdout), os/exec
// spawns a copier that reads until EVERY holder of the pipe's write
// end closes it — and a grandchild that inherited the pipe holds it
// for as long as it likes. Wait (and Run/Output/CombinedOutput, which
// end in Wait) then blocks forever after the kill: the cancellation
// the context promised never reaches the caller. Cmd.WaitDelay is the
// Go-team bound for exactly this shape ("a child process that exits
// but leaves its I/O pipes unclosed", the os/exec docs). Found by the
// 2026-09-07 red-probe round: cmd/kiln agent_watcher.go runOneAgentTurn
// runs third-party model-driven adapter CLIs with c.Output() and no
// WaitDelay — one adapter that shells out in the background wedges the
// watcher turn forever on cancel (agent_turn_red_test pins it).
//
// The fix spellings already in the tree: codegen/extension_command.go
// and framework/processmodule_probe.go set cmd.WaitDelay inline before
// the start ("Same fix as codegen/extension_command.go" — the probe
// runner's own comment), and the evals runners factor it into a
// same-package helper (configureCommandCancellation sets WaitDelay
// alongside the process-group teardown).
//
// Shape, all within one function: a local bound from
// exec.CommandContext whose stdout is captured — an Output()/
// CombinedOutput() call (which is also the start), a StdoutPipe()
// call, or a Stdout assignment of a non-nil value that is not an
// *os.File (a file writes straight to the fd: no copier, no hang) —
// is started (Start/Run/Output/CombinedOutput) with no `.WaitDelay =`
// assignment on that Cmd earlier in the function and no earlier call
// passing it to a same-package helper that assigns WaitDelay on the
// corresponding parameter (the configureCommandCancellation posture).
// The chained spelling exec.CommandContext(...).Output()/...
// CombinedOutput() fires outright: no variable exists to bound.
//
// Silent postures, deliberately:
//   - exec.Command with no context: there is no cancellation whose
//     aftermath WaitDelay would bound (framework/processmodule_probe's
//     runner builds its child with exec.Command and still sets
//     WaitDelay, but the rule does not demand it);
//   - a Cmd reassigned through a wrapper (bash.go's
//     `cmd = b.SandboxFn(cmd)`): the wrapper owns the cancellation
//     posture, and the rule cannot see through it;
//   - WaitDelay set inline, or via the same-package helper hop, before
//     the start;
//   - stdout not captured (Stdout nil or an *os.File): no copier
//     goroutine exists to strand; Stderr-only capture is not this
//     rule's shape;
//   - a Cmd never started in the function;
//   - _test.go files.
package nowaitdelay

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "nowaitdelay",
	Doc:  "report exec.CommandContext children with captured stdout started with no WaitDelay",
	Run:  run,
}

// startMethods start the child; Output/CombinedOutput also capture.
var startMethods = map[string]bool{
	"Start": true, "Run": true, "Output": true, "CombinedOutput": true,
}

// pipeMethods capture stdout through a pipe and imply the copier.
var pipeMethods = map[string]bool{
	"Output": true, "CombinedOutput": true, "StdoutPipe": true,
}

func run(pass *analysis.Pass) (any, error) {
	// Same-package helpers that bound a *exec.Cmd parameter with a
	// WaitDelay assignment (the configureCommandCancellation posture):
	// callee object → credited parameter positions.
	helpers := waitDelayHelpers(pass)

	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			// A test that hangs on a stray grandchild fails the test
			// run loudly; it is not a shipped hang.
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				checkBody(pass, helpers, d.Body)
			case *ast.GenDecl:
				// Top-level func literals (var h = func() {...}).
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, val := range vs.Values {
						if lit, ok := val.(*ast.FuncLit); ok && lit.Body != nil {
							checkBody(pass, helpers, lit.Body)
						}
					}
				}
			}
		}
	}
	return nil, nil
}

// waitDelayHelpers maps each package-local function to the parameter
// positions its own body bounds with a WaitDelay assignment on that
// parameter.
func waitDelayHelpers(pass *analysis.Pass) map[types.Object]map[int]bool {
	helpers := map[types.Object]map[int]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Type.Params == nil {
				continue
			}
			var params []*ast.Ident
			for _, field := range fn.Type.Params.List {
				if !isExecCmd(pass, field.Type) {
					continue
				}
				params = append(params, field.Names...)
			}
			if len(params) == 0 {
				continue
			}
			flagged := map[int]bool{}
			for i, name := range params {
				obj, ok := pass.TypesInfo.Defs[name].(*types.Var)
				if ok && assignsWaitDelayOn(pass, fn.Body, obj) {
					flagged[i] = true
				}
			}
			if len(flagged) == 0 {
				continue
			}
			if obj := pass.TypesInfo.Defs[fn.Name]; obj != nil {
				helpers[obj] = flagged
			}
		}
	}
	return helpers
}

// cmdEvent is one fact about a Cmd in the body, at a position.
type cmdEvent struct {
	pos    token.Pos
	kind   eventKind
	obj    types.Object // the tracked local, nil for the chained form
	method string       // the method name for method-call events
}

type eventKind int

const (
	kindBound     eventKind = iota // local (re)bound from exec.CommandContext
	kindUnbound                    // local reassigned from anything else
	kindWaitDelay                  // .WaitDelay = on the local
	kindHelper                     // local passed to a WaitDelay helper
	kindCapture                    // stdout captured (StdoutPipe / Stdout = buffer)
	kindStart                      // Start/Run/Output/CombinedOutput
	kindChained                    // exec.CommandContext(...).Output()/CombinedOutput()
)

// checkBody walks one function body collecting Cmd events in source
// order, then replays each tracked local's events as a small state
// machine and reports unbounded starts.
func checkBody(pass *analysis.Pass, helpers map[types.Object]map[int]bool, body *ast.BlockStmt) {
	var events []cmdEvent
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) != len(n.Rhs) {
				return true
			}
			for i, lhs := range n.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var)
				if !ok {
					continue
				}
				if isCommandContextCall(pass, n.Rhs[i]) {
					events = append(events, cmdEvent{pos: n.Pos(), kind: kindBound, obj: obj})
				} else {
					// A reassignment from anything else ends the
					// provenance: bash.go's cmd = SandboxFn(cmd).
					events = append(events, cmdEvent{pos: n.Pos(), kind: kindUnbound, obj: obj})
				}
			}
			// cmd.Stdout = w, and the cmd.Stdout, cmd.Stderr = w1, w2
			// spelling.
			for i, lhs := range n.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Stdout" {
					continue
				}
				obj := localOf(pass, sel.X)
				if obj == nil {
					continue
				}
				if stdoutCaptures(pass, n.Rhs[i]) {
					events = append(events, cmdEvent{pos: n.Pos(), kind: kindCapture, obj: obj})
				}
			}
			// cmd.WaitDelay = d.
			for _, lhs := range n.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "WaitDelay" {
					continue
				}
				if obj := localOf(pass, sel.X); obj != nil {
					events = append(events, cmdEvent{pos: n.Pos(), kind: kindWaitDelay, obj: obj})
				}
			}
		case *ast.CallExpr:
			sel, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr)
			if !ok {
				// A same-package helper taking the Cmd positionally.
				if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
					if callee, ok := pass.TypesInfo.ObjectOf(id).(*types.Func); ok {
						if flagged, ok := helpers[callee]; ok {
							for i, arg := range n.Args {
								if !flagged[i] {
									continue
								}
								if obj := localOf(pass, arg); obj != nil {
									events = append(events, cmdEvent{pos: n.Pos(), kind: kindHelper, obj: obj})
								}
							}
						}
					}
				}
				return true
			}
			// The chained spelling: no local exists to bound.
			if isCommandContextCall(pass, sel.X) && pipeMethods[sel.Sel.Name] && startMethods[sel.Sel.Name] {
				events = append(events, cmdEvent{pos: n.Pos(), kind: kindChained, method: sel.Sel.Name})
				return true
			}
			obj := localOf(pass, sel.X)
			if obj == nil {
				return true
			}
			switch {
			case pipeMethods[sel.Sel.Name] && !startMethods[sel.Sel.Name]:
				events = append(events, cmdEvent{pos: n.Pos(), kind: kindCapture, obj: obj, method: sel.Sel.Name})
			case startMethods[sel.Sel.Name]:
				events = append(events, cmdEvent{pos: n.Pos(), kind: kindStart, obj: obj, method: sel.Sel.Name})
			}
		}
		return true
	})

	// Replay per local: bound → (capture | bound-delay) → start.
	state := map[types.Object]*cmdState{}
	for _, ev := range events {
		switch ev.kind {
		case kindChained:
			pass.Reportf(ev.pos,
				"nowaitdelay: exec.CommandContext(...).%s captures stdout with no WaitDelay; after ctx cancel kills the child, a grandchild holding the inherited pipe blocks Wait forever (set cmd.WaitDelay)",
				ev.method)
			continue
		case kindBound:
			// A fresh CommandContext result: a new child, reset.
			state[ev.obj] = &cmdState{bound: true}
		case kindUnbound:
			delete(state, ev.obj)
		case kindStart, kindCapture, kindWaitDelay, kindHelper:
			st := state[ev.obj]
			if st == nil {
				continue
			}
			switch ev.kind {
			case kindCapture:
				st.captured = true
			case kindWaitDelay, kindHelper:
				st.bounded = true
			case kindStart:
				if pipeMethods[ev.method] {
					st.captured = true
				}
				if st.captured && !st.bounded {
					pass.Reportf(ev.pos,
						"nowaitdelay: exec.CommandContext child with captured stdout starts with no WaitDelay; after ctx cancel kills the child, a grandchild holding the inherited pipe blocks Wait forever (set cmd.WaitDelay)")
				}
			}
		}
	}
}

// cmdState is one tracked local's progress between its CommandContext
// binding and its start.
type cmdState struct {
	bound    bool
	captured bool
	bounded  bool
}

// stdoutCaptures reports whether an Stdout assignment RHS is a capture
// buffer: non-nil and not an *os.File (a file writes straight to the
// fd; no copier goroutine, no pipe to hold).
func stdoutCaptures(pass *analysis.Pass, rhs ast.Expr) bool {
	if id, ok := rhs.(*ast.Ident); ok && id.Name == "nil" {
		return false
	}
	return !isOsFile(pass, rhs)
}

// localOf: e is an identifier naming a local variable.
func localOf(pass *analysis.Pass, e ast.Expr) types.Object {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return nil
	}
	if obj, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
		return obj
	}
	return nil
}

// isCommandContextCall: e is a call to os/exec.CommandContext.
func isCommandContextCall(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "CommandContext" {
		return false
	}
	return pkgPathBehind(pass, sel.X) == "os/exec"
}

// pkgPathBehind renders the imported package path behind an identifier
// use, "" when it does not name a package.
func pkgPathBehind(pass *analysis.Pass, e ast.Expr) string {
	id, ok := e.(*ast.Ident)
	if !ok {
		return ""
	}
	pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName)
	if !ok {
		return ""
	}
	return pkg.Imported().Path()
}

// isOsFile: e's type is *os.File.
func isOsFile(pass *analysis.Pass, e ast.Expr) bool {
	return isPtrToNamed(pass, e, "os", "File")
}

// isExecCmd: e's type is *exec.Cmd.
func isExecCmd(pass *analysis.Pass, e ast.Expr) bool {
	return isPtrToNamed(pass, e, "os/exec", "Cmd")
}

func isPtrToNamed(pass *analysis.Pass, e ast.Expr, pkgPath, name string) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok {
		return false
	}
	p, ok := tv.Type.(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := p.Elem().(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkgPath && n.Obj().Name() == name
}

// assignsWaitDelayOn reports whether body assigns `.WaitDelay =` on the
// parameter obj.
func assignsWaitDelayOn(pass *analysis.Pass, body *ast.BlockStmt, obj types.Object) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "WaitDelay" {
				continue
			}
			if id, ok := sel.X.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
