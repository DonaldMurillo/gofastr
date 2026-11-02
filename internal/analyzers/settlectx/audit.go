package settlectx

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/types/typeutil"
)

// audits marks a function that hands the context parameters at these
// argument indexes, still cancellable, to an audit sink: directly (an
// interface method on a *Sink/*Auditor value, or one named *Event or
// *Audit*) or through another audits function. A helper that detaches
// first (battery/auth's deliverAudit: WithTimeout over WithoutCancel)
// hands the sink no live context and so carries no fact: its callers are
// quiet by construction.
type audits struct{ Index []int }

func (*audits) AFact() {}

func (f *audits) String() string { return fmt.Sprintf("audits%v", f.Index) }

// isAuditCall reports whether call is an interface method that records
// an audit or security event: the receiver's interface is named *Sink or
// *Auditor, or the method is named *Event or carries Audit.
func (a *analyzer) isAuditCall(call *ast.CallExpr) bool {
	iface, fn := a.ifaceMethod(call)
	if fn == nil {
		return false
	}
	name := iface.Obj().Name()
	return strings.HasSuffix(name, "Sink") || strings.HasSuffix(name, "Auditor") ||
		strings.HasSuffix(fn.Name(), "Event") || strings.Contains(fn.Name(), "Audit")
}

// ifaceMethod returns the named interface and method call invokes, when
// it is a method called through an interface value.
func (a *analyzer) ifaceMethod(call *ast.CallExpr) (*types.Named, *types.Func) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, nil
	}
	s, ok := a.pass.TypesInfo.Selections[sel]
	if !ok || s.Kind() != types.MethodVal {
		return nil, nil
	}
	fn, ok := s.Obj().(*types.Func)
	if !ok {
		return nil, nil
	}
	recv := s.Recv()
	if p, ok := recv.(*types.Pointer); ok {
		recv = p.Elem()
	}
	named, ok := types.Unalias(recv).(*types.Named)
	if !ok {
		return nil, nil
	}
	if _, ok := named.Underlying().(*types.Interface); !ok {
		return nil, nil
	}
	return named, fn
}

// hostIface reports whether named is an interface a host implements:
// exported, and declared outside the standard library. An unexported
// interface is a package's own seam over its drivers (framework/migrate's
// execQueryer over *sql.DB and *sql.Conn), not a host extension point.
func hostIface(named *types.Named) bool {
	pkg := named.Obj().Pkg()
	return pkg != nil && named.Obj().Exported() && !stdlibPath(pkg.Path())
}

// stdlibPath: the first path element names a standard-library root. A
// fixture package ("a") is not one.
func stdlibPath(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return stdlibRoots[first]
}

var stdlibRoots = map[string]bool{
	"archive": true, "bufio": true, "bytes": true, "compress": true, "container": true,
	"context": true, "crypto": true, "database": true, "debug": true, "embed": true,
	"encoding": true, "errors": true, "expvar": true, "flag": true, "fmt": true,
	"go": true, "hash": true, "html": true, "image": true, "index": true, "io": true,
	"iter": true, "log": true, "maps": true, "math": true, "mime": true, "net": true,
	"os": true, "path": true, "plugin": true, "reflect": true, "regexp": true,
	"runtime": true, "slices": true, "sort": true, "strconv": true, "strings": true,
	"sync": true, "syscall": true, "testing": true, "text": true, "time": true,
	"unicode": true, "unique": true, "unsafe": true, "weak": true,
}

// auditIndexes returns the argument indexes at which fn hands a live
// context to an audit sink.
func (a *analyzer) auditIndexes(fn *types.Func) map[int]bool {
	fn = fn.Origin()
	if idx, ok := a.audit[fn]; ok {
		return idx
	}
	if fn.Pkg() == a.pass.Pkg {
		return nil
	}
	var f audits
	if !a.pass.ImportObjectFact(fn, &f) {
		return nil
	}
	out := map[int]bool{}
	for _, i := range f.Index {
		out[i] = true
	}
	return out
}

// auditCtxArgs returns the context arguments of call that reach an
// audit sink: every context argument of a direct sink call, the audits
// indexes of a helper call.
func (a *analyzer) auditCtxArgs(call *ast.CallExpr) (args []ast.Expr, helper *types.Func) {
	info := a.pass.TypesInfo
	var idx map[int]bool
	if a.isAuditCall(call) {
		idx = nil
	} else if fn := typeutil.StaticCallee(info, call); fn != nil {
		if idx = a.auditIndexes(fn); len(idx) == 0 {
			return nil, nil
		}
		helper = fn.Origin()
	} else {
		return nil, nil
	}
	for i, arg := range call.Args {
		if idx != nil && !idx[i] {
			continue
		}
		if t := info.TypeOf(arg); t != nil && isContext(t) {
			args = append(args, arg)
		}
	}
	return args, helper
}

// collectAudits computes, to a fixed point, the local functions that hand
// one of their context parameters, still cancellable, to an audit sink.
func (a *analyzer) collectAudits(fns []*ast.FuncDecl) {
	for changed := true; changed; {
		changed = false
		for _, fn := range fns {
			obj, ok := a.pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok {
				continue
			}
			params := ctxParams(a.pass.TypesInfo, fn)
			if len(params) == 0 {
				continue
			}
			r := a.newResolver(fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncLit, *ast.GoStmt:
					return false
				case *ast.CallExpr:
					args, _ := a.auditCtxArgs(n)
					for _, arg := range args {
						s := r.classify(arg, 0)
						pi, ok := params[s.root]
						if s.kind != live || !ok {
							continue
						}
						if !slices.Contains(a.auditSinks[obj], n) {
							a.auditSinks[obj] = append(a.auditSinks[obj], n)
						}
						if a.audit[obj][pi] {
							continue
						}
						if a.audit[obj] == nil {
							a.audit[obj] = map[int]bool{}
						}
						a.audit[obj][pi] = true
						changed = true
					}
				}
				return true
			})
		}
	}
}

// ctxParams maps fn's context parameters to their argument indexes.
func ctxParams(info *types.Info, fn *ast.FuncDecl) map[types.Object]int {
	params := map[types.Object]int{}
	i := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			i++
			continue
		}
		for _, id := range field.Names {
			if po := info.Defs[id]; po != nil && isContext(po.Type()) {
				params[po] = i
			}
			i++
		}
	}
	return params
}

// isRequest reports whether obj is an *http.Request: r in r.Context().
func isRequest(obj types.Object) bool {
	if obj == nil {
		return false
	}
	p, ok := obj.Type().(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "net/http" && n.Obj().Name() == "Request"
}

// checkAudit walks one body (not its nested literals) for the two audit
// shapes: an audit event written on the request's own context after a
// change the request made on it (79cc5197, 9eaba945), and an
// uncancellable context with no bound handed to a host interface
// (d799883c).
func (a *analyzer) checkAudit(r *resolver, body *ast.BlockStmt) {
	if strings.HasSuffix(a.pass.Fset.Position(body.Pos()).Filename, "_test.go") {
		return
	}
	info := a.pass.TypesInfo
	type change struct {
		end   token.Pos
		root  types.Object
		line  int
		audit bool
	}
	var changes []change
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit, *ast.GoStmt:
			return false
		case *ast.CallExpr:
			a.checkHostDetach(r, n)
			if args, helper := a.auditCtxArgs(n); len(args) > 0 {
				for _, arg := range args {
					s := r.classify(arg, 0)
					if s.kind != live || !isRequest(s.root) {
						continue
					}
					var prior *change
					for i := range changes {
						if changes[i].root == s.root && changes[i].end <= n.Pos() && !changes[i].audit {
							prior = &changes[i]
						}
					}
					if prior == nil {
						continue
					}
					switch {
					case helper != nil && helper.Pkg() == a.pass.Pkg:
						if _, seen := a.auditUses[helper]; !seen {
							a.auditUses[helper] = n.Pos()
						}
					case !a.reported[n.Pos()]:
						a.reported[n.Pos()] = true
						a.pass.Reportf(n.Pos(), "%s writes the audit event on the request's context after the change on line %d; a client that hangs up between them cancels the write and the change leaves no row: %s",
							calleeName(info, n), prior.line, advice)
					}
					break
				}
				changes = append(changes, change{end: n.End(), audit: true})
				return true
			}
			if fn := typeutil.StaticCallee(info, n); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == "context" {
				return true
			}
			if returnsContext(info.TypeOf(n)) {
				return true // a derivation, not a change
			}
			for _, arg := range n.Args {
				if t := info.TypeOf(arg); t == nil || !isContext(t) {
					continue
				}
				if s := r.classify(arg, 0); s.kind == live {
					changes = append(changes, change{end: n.End(), root: s.root, line: a.pass.Fset.Position(n.Pos()).Line})
				}
			}
		}
		return true
	})
}

// checkHostDetach reports context.WithoutCancel with no bound handed to a
// host interface: the caller has given up its own cancellation, so a
// host implementation that blocks (a sink POSTing to a remote) holds it
// for as long as the remote does (d799883c).
func (a *analyzer) checkHostDetach(r *resolver, call *ast.CallExpr) {
	iface, fn := a.ifaceMethod(call)
	if fn == nil || !hostIface(iface) || a.reported[call.Pos()] {
		return
	}
	for _, arg := range call.Args {
		if t := a.pass.TypesInfo.TypeOf(arg); t == nil || !isContext(t) {
			continue
		}
		if s := r.classify(arg, 0); s.kind == detached && !s.bounded {
			a.reported[call.Pos()] = true
			a.pass.Reportf(call.Pos(), "%s hands a host %s context.WithoutCancel with no timeout; a blocking implementation holds this caller as long as its remote does: %s",
				fn.Name(), iface.Obj().Name(), advice)
			return
		}
	}
}

// reportAuditHelpers reports, at a local audit helper's own sink calls,
// the helpers some caller in this package hands a request context after
// a change: one report at the place the fix goes (emitSecurity's
// sink.SecurityEvent before 79cc5197), not one per call site.
//
// A helper that hands the context on to another local helper passes the
// use down: the report lands on the innermost direct sink call (or the
// call into an imported helper), so a chain emitSecurity → deliverAudit →
// sink.SecurityEvent reports once, inside deliverAudit.
func (a *analyzer) reportAuditHelpers() {
	queue := slices.SortedFunc(maps.Keys(a.auditUses), byPos)
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		use := a.pass.Fset.Position(a.auditUses[fn])
		for _, sink := range a.auditSinks[fn] {
			if !a.isAuditCall(sink) {
				if h := typeutil.StaticCallee(a.pass.TypesInfo, sink); h != nil && h.Origin().Pkg() == a.pass.Pkg {
					if _, seen := a.auditUses[h.Origin()]; !seen {
						a.auditUses[h.Origin()] = a.auditUses[fn]
						queue = append(queue, h.Origin())
					}
					continue
				}
			}
			if a.reported[sink.Pos()] {
				continue
			}
			a.reported[sink.Pos()] = true
			a.pass.Reportf(sink.Pos(), "%s writes the audit event on its caller's request context (%s:%d hands it r.Context() after a change); a client that hangs up cancels the write and the change leaves no row: %s",
				calleeName(a.pass.TypesInfo, sink), shortFile(use.Filename), use.Line, advice)
		}
	}
}

func calleeName(info *types.Info, call *ast.CallExpr) string {
	if fn, ok := typeutil.Callee(info, call).(*types.Func); ok {
		return fn.Name()
	}
	return "call"
}

func shortFile(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
