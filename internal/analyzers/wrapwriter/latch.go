package wrapwriter

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
)

// ---- arm three: partial latch -----------------------------------------

// latchSenders are the methods that send the response without a Write
// or WriteHeader call: a flush sends the headers (200 unless a status
// was set) and a ReadFrom writes the body. Hijack is not one: it sends
// no status through the writer (recoveryWriter leaves its status at 0
// after a bare hijack by design), and a WriteHeader after it fails with
// ErrHijacked rather than being dropped as superfluous.
var latchSenders = map[string]bool{"Flush": true, "FlushError": true, "ReadFrom": true}

// methodIndex is the package's method declarations by their *types.Func.
type methodIndex map[*types.Func]*ast.FuncDecl

func indexMethods(pass *analysis.Pass) methodIndex {
	out := methodIndex{}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Body == nil {
				continue
			}
			if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
				out[fn] = fd
			}
		}
	}
	return out
}

// checkLatch runs the three sub-arms over each dispatched wrapper type.
func checkLatch(pass *analysis.Pass, rw *types.Interface, dispatched map[*types.Named]bool, methods methodIndex) {
	reported := map[ast.Node]bool{}
	recoverReads := recoverReadFields(pass)
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			tn, ok := pass.TypesInfo.Defs[ts.Name].(*types.TypeName)
			if !ok || tn.IsAlias() {
				return true
			}
			named, ok := tn.Type().(*types.Named)
			if !ok || !dispatched[named] {
				return true
			}
			st, ok := named.Underlying().(*types.Struct)
			if !ok || !wrapsWriter(st, rw) {
				return true
			}
			if !types.Implements(named, rw) && !types.Implements(types.NewPointer(named), rw) {
				return true
			}
			mset := types.NewMethodSet(types.NewPointer(named))
			name := named.Obj().Name()
			checkSenders(pass, name, mset, methods, reported)
			checkReadFrom(pass, name, mset, methods, reported)
			checkEarlyLatch(pass, name, mset, methods, recoverReads, reported)
			return true
		})
	}
}

// checkSenders, sub-arm (a): a Flush, FlushError or ReadFrom declared in
// the package that leaves unset a latch an observing Write or
// WriteHeader sets.
func checkSenders(pass *analysis.Pass, name string, mset *types.MethodSet, methods methodIndex, reported map[ast.Node]bool) {
	latches := latchFields(pass, mset, methods)
	if len(latches) == 0 {
		return
	}
	for i := range mset.Len() {
		fn, ok := mset.At(i).Obj().(*types.Func)
		if !ok || !latchSenders[fn.Name()] {
			continue
		}
		fd := methods[fn]
		if fd == nil || reported[fd] || setsOwnFlag(pass, fd, methods, map[*ast.FuncDecl]bool{}) {
			continue
		}
		var missing []string
		for _, v := range latches {
			if !sets(pass, fd, v, methods, map[*ast.FuncDecl]bool{}) {
				missing = append(missing, v.Name())
			}
		}
		if len(missing) > 0 {
			reported[fd] = true
			pass.Reportf(fd.Name.Pos(),
				"wrapwriter: %s.%s sends the response but leaves %s unset, which Write/WriteHeader latch; a WriteHeader after it (dropped by net/http) or a panic then records a status the client never got (latch it here, or call the method that does)",
				name, fn.Name(), strings.Join(missing, ", "))
		}
	}
}

// checkReadFrom, sub-arm (b): an observing Write with no ReadFrom.
func checkReadFrom(pass *analysis.Pass, name string, mset *types.MethodSet, methods methodIndex, reported map[ast.Node]bool) {
	if mset.Lookup(nil, "ReadFrom") != nil {
		return
	}
	if wfd := passThroughWrite(pass, mset, methods); wfd != nil && !reported[wfd] {
		reported[wfd] = true
		pass.Reportf(wfd.Name.Pos(),
			"wrapwriter: %s forwards Write to the writer it wraps but declares no ReadFrom; io.Copy and http.ServeContent lose the connection's sendfile path behind it (forward ReadFrom with io.Copy(w.ResponseWriter, src), doing first what Write does)",
			name)
	}
}

// checkEarlyLatch, sub-arm (c): an observing WriteHeader that sets a
// latch a deferred recover reads before the forwarded WriteHeader
// returned. net/http panics on a code outside 100-999 before sending
// anything, so the recover reads an unsent response as started.
func checkEarlyLatch(pass *analysis.Pass, name string, mset *types.MethodSet, methods methodIndex, recoverReads map[*types.Var]bool, reported map[ast.Node]bool) {
	fd := declared(mset, methods, "WriteHeader")
	if fd == nil {
		return
	}
	at := forwardIndex(pass, fd, "WriteHeader")
	if at < 0 {
		return
	}
	for _, st := range fd.Body.List[:at] {
		ast.Inspect(st, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Lhs) != len(as.Rhs) {
				return true
			}
			for i, lhs := range as.Lhs {
				v := fieldOf(pass, lhs)
				if v == nil || !recoverReads[v] || !isTrue(as.Rhs[i]) || reported[lhs] {
					continue
				}
				reported[lhs] = true
				pass.Reportf(lhs.Pos(),
					"wrapwriter: %s.WriteHeader sets %s before the forwarded WriteHeader returns, and a deferred recover reads it; net/http panics on an invalid code before sending anything, so that recover treats an unsent response as started (latch after the call returns)",
					name, v.Name())
			}
			return true
		})
	}
}

// declared: the package's declaration of the named method in mset, nil
// when it has none or the method comes from another package.
func declared(mset *types.MethodSet, methods methodIndex, name string) *ast.FuncDecl {
	sel := mset.Lookup(nil, name)
	if sel == nil {
		return nil
	}
	fn, ok := sel.Obj().(*types.Func)
	if !ok {
		return nil
	}
	return methods[fn]
}

// recoverReadFields: the fields read inside a deferred function literal
// that calls recover(), anywhere in the package's non-test files.
func recoverReadFields(pass *analysis.Pass) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ds, ok := n.(*ast.DeferStmt)
			if !ok {
				return true
			}
			lit, ok := ast.Unparen(ds.Call.Fun).(*ast.FuncLit)
			if !ok || !callsRecover(pass, lit.Body) {
				return true
			}
			ast.Inspect(lit.Body, func(n ast.Node) bool {
				if s, ok := n.(*ast.SelectorExpr); ok {
					if v, ok := pass.TypesInfo.Uses[s.Sel].(*types.Var); ok && v.IsField() {
						out[v] = true
					}
				}
				return true
			})
			return true
		})
	}
	return out
}

// callsRecover: body calls the builtin recover (not inside a nested
// literal, which recovers nothing for this frame).
func callsRecover(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
				if b, ok := pass.TypesInfo.Uses[id].(*types.Builtin); ok && b.Name() == "recover" {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

// forwardIndex: the index of fd's first top-level statement that calls
// <recv>.<writer field>.<method>(...) outside any nested block, -1 when
// none does. A forward under an if (timeoutWriter forwards only once
// streaming) marks a buffering wrapper, not an observing one.
func forwardIndex(pass *analysis.Pass, fd *ast.FuncDecl, method string) int {
	for i, st := range fd.Body.List {
		found := false
		ast.Inspect(st, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit, *ast.BlockStmt:
				return false
			case *ast.CallExpr:
				if s, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr); ok && s.Sel.Name == method {
					if inner, ok := ast.Unparen(s.X).(*ast.SelectorExpr); ok && isWriterField(pass, inner) {
						found = true
					}
				}
			}
			return !found
		})
		if found {
			return i
		}
	}
	return -1
}

// observing: fd forwards Write or WriteHeader to the wrapped writer at
// its top level.
func observing(pass *analysis.Pass, fd *ast.FuncDecl) bool {
	return forwardIndex(pass, fd, "Write") >= 0 || forwardIndex(pass, fd, "WriteHeader") >= 0
}

// latchFields: the fields an observing Write or WriteHeader (promoted or
// declared, transitively through the package's own methods) latches: a
// bool set to true, or an integer set to a constant (webmcp's
// `if s.status == 0 { s.status = http.StatusOK }`). `status = code`
// records, it does not latch.
func latchFields(pass *analysis.Pass, mset *types.MethodSet, methods methodIndex) []*types.Var {
	var out []*types.Var
	seen := map[*types.Var]bool{}
	for _, name := range []string{"WriteHeader", "Write"} {
		fd := declared(mset, methods, name)
		if fd == nil || !observing(pass, fd) {
			continue
		}
		collectLatches(pass, fd, methods, map[*ast.FuncDecl]bool{}, func(v *types.Var) {
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		})
	}
	return out
}

// collectLatches walks fd (and the same-package methods it calls) for
// latch assignments.
func collectLatches(pass *analysis.Pass, fd *ast.FuncDecl, methods methodIndex, seen map[*ast.FuncDecl]bool, add func(*types.Var)) {
	if seen[fd] {
		return
	}
	seen[fd] = true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if len(n.Lhs) != len(n.Rhs) {
				return true
			}
			for i, lhs := range n.Lhs {
				v := fieldOf(pass, lhs)
				if v == nil {
					continue
				}
				b, ok := v.Type().Underlying().(*types.Basic)
				if !ok {
					continue
				}
				switch {
				case b.Kind() == types.Bool:
					if isTrue(n.Rhs[i]) {
						add(v)
					}
				case b.Info()&types.IsInteger != 0:
					if tv, ok := pass.TypesInfo.Types[n.Rhs[i]]; ok && tv.Value != nil {
						add(v)
					}
				}
			}
		case *ast.CallExpr:
			if callee := calledMethod(pass, n, methods); callee != nil {
				collectLatches(pass, callee, methods, seen, add)
			}
		}
		return true
	})
}

// setsOwnFlag: the sender sets a bool field of its receiver to true,
// itself or through a same-package method on that receiver
// (idempotencyRecorder's Flush → FlushError sets streamed; the cache
// recorder's overflow): the wrapper decided what a flush means for what
// it records, and the latch it skips is not read for a streamed
// response.
func setsOwnFlag(pass *analysis.Pass, fd *ast.FuncDecl, methods methodIndex, seen map[*ast.FuncDecl]bool) bool {
	recv := receiverObj(pass, fd)
	if recv == nil || seen[fd] {
		return false
	}
	seen[fd] = true
	found := false
	onRecv := func(x ast.Expr) bool {
		id, ok := ast.Unparen(x).(*ast.Ident)
		return ok && pass.TypesInfo.Uses[id] == recv
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if len(n.Lhs) != len(n.Rhs) {
				break
			}
			for i, lhs := range n.Lhs {
				if s, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok && isTrue(n.Rhs[i]) && onRecv(s.X) {
					found = true
				}
			}
		case *ast.CallExpr:
			s, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr)
			if !ok || !onRecv(s.X) {
				break
			}
			if callee := calledMethod(pass, n, methods); callee != nil && setsOwnFlag(pass, callee, methods, seen) {
				found = true
			}
		}
		return !found
	})
	return found
}

// sets: fd assigns field v (any value), calls a same-package method that
// does, or hands its receiver whole to a call (io.Copy(writerOnly{w},
// src) routes the body through Write).
func sets(pass *analysis.Pass, fd *ast.FuncDecl, v *types.Var, methods methodIndex, seen map[*ast.FuncDecl]bool) bool {
	if seen[fd] {
		return false
	}
	seen[fd] = true
	recv := receiverObj(pass, fd)
	found := false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if fieldOf(pass, lhs) == v {
					found = true
				}
			}
		case *ast.CallExpr:
			if callee := calledMethod(pass, n, methods); callee != nil && sets(pass, callee, v, methods, seen) {
				found = true
				return false
			}
			if recv != nil {
				for _, a := range n.Args {
					if mentionsBare(pass, a, recv) {
						found = true
					}
				}
			}
		}
		return true
	})
	return found
}

// passThroughWrite: the type's Write (declared in this package) hands
// its byte parameter to a Write on the writer it wraps and calls no
// other Write: an observing wrapper, not a buffer or a transform.
func passThroughWrite(pass *analysis.Pass, mset *types.MethodSet, methods methodIndex) *ast.FuncDecl {
	fd := declared(mset, methods, "Write")
	if fd == nil || len(fd.Type.Params.List) != 1 || len(fd.Type.Params.List[0].Names) != 1 {
		return nil
	}
	param := pass.TypesInfo.Defs[fd.Type.Params.List[0].Names[0]]
	if param == nil {
		return nil
	}
	forwards, other := false, false
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		s, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || s.Sel.Name != "Write" {
			return true
		}
		inner, ok := ast.Unparen(s.X).(*ast.SelectorExpr)
		if ok && isWriterField(pass, inner) && len(call.Args) == 1 && isObj(pass, call.Args[0], param) {
			forwards = true
		} else {
			other = true
		}
		return true
	})
	if forwards && !other {
		return fd
	}
	return nil
}

// isWriterField: e is <x>.F with F a field whose type is an interface
// carrying WriteHeader (http.ResponseWriter or one embedding it).
func isWriterField(pass *analysis.Pass, e *ast.SelectorExpr) bool {
	v, ok := pass.TypesInfo.Uses[e.Sel].(*types.Var)
	if !ok || !v.IsField() {
		return false
	}
	if _, ok := v.Type().Underlying().(*types.Interface); !ok {
		return false
	}
	return types.NewMethodSet(v.Type()).Lookup(nil, "WriteHeader") != nil
}

func fieldOf(pass *analysis.Pass, e ast.Expr) *types.Var {
	s, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	v, ok := pass.TypesInfo.Uses[s.Sel].(*types.Var)
	if !ok || !v.IsField() {
		return nil
	}
	return v
}

func calledMethod(pass *analysis.Pass, call *ast.CallExpr, methods methodIndex) *ast.FuncDecl {
	s, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	fn, ok := pass.TypesInfo.Uses[s.Sel].(*types.Func)
	if !ok {
		return nil
	}
	return methods[fn.Origin()]
}

func receiverObj(pass *analysis.Pass, fd *ast.FuncDecl) types.Object {
	if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return nil
	}
	return pass.TypesInfo.Defs[fd.Recv.List[0].Names[0]]
}

// mentionsBare: e uses obj other than as the base of a selector (w, not
// w.ResponseWriter).
func mentionsBare(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := ast.Unparen(s.X).(*ast.Ident); ok && pass.TypesInfo.Uses[id] == obj {
				return false
			}
		}
		if id, ok := n.(*ast.Ident); ok && pass.TypesInfo.Uses[id] == obj {
			found = true
		}
		return true
	})
	return found
}

func isObj(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && pass.TypesInfo.Uses[id] == obj
}

func isTrue(e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	return ok && id.Name == "true"
}
