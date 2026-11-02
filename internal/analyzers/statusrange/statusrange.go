// Package statusrange catches a response status that reaches
// WriteHeader (or http.Error's code argument) without a range check.
//
// The bug: net/http panics on a WriteHeader code outside 100-999, and a
// code it accepts can still be the wrong class: an error sent with a
// 2xx reads as success to every client and cache. Found by the 2026-11
// bug bash:
//   - 8708ddd5: core/handler WriteError sent w.WriteHeader(herr.Code)
//     straight from the caller's *Error, so an Error{} with Code 0
//     panicked inside the error path and a Code of 200 answered an error
//     as success;
//   - 39de7827: core/middleware Recovery latched started before an
//     invalid WriteHeader returned, the other half of the same code-0
//     panic (wrapwriter's partial-latch arm owns that shape).
//
// The fix spelling is WriteError's: clamp before the call,
// `if code < 400 || code > 599 { code = http.StatusInternalServerError }`.
//
// Shape: a call x.WriteHeader(c) on an http.ResponseWriter, or
// http.Error(w, msg, c), or a call to a status sink (below) at its sink
// argument, where c is not known good. Known good is:
//   - a constant (http.StatusOK and friends);
//   - range-checked: an ordered comparison (<, <=, >, >=) of the same
//     variable or field path (herr.Code, code) against a constant, or a
//     call to a checker on it (a bool function that compares its int
//     parameter against a constant: kiln's validStatus), in an if or
//     case condition before the call in the same function;
//   - a safe result: a call to a function whose result at that index is
//     known good at every return (statusFor(err) switching over
//     constants, a clamp helper), directly or through a local bound by
//     `_, status, err := f()`; exported as a fact;
//   - a local whose every assignment is known good;
//   - a received status: resp.StatusCode of an *http.Response, the Code
//     of an httptest.ResponseRecorder (a peer already sent it).
//
// A function whose int parameter reaches such a call unchecked is a
// STATUS SINK at that index (writeJSON(w, status, v)): the call inside
// it is not reported, and every caller's argument is checked instead,
// so the report lands where the unchecked value enters. Exported as a
// fact, so a sink in one package is checked at its callers in another
// (i18n.HTTPError was one until it clamped). The same holds for a
// function literal bound to a local (core/handler respond's
// `begin := func(code int) {...}`), checked at its calls in the
// enclosing body. A method named WriteHeader forwarding its own
// parameter is a pass-through: its caller's WriteHeader is the checked
// call.
//
// A STATUS FIELD is an int field declared in the package, that only the
// package can write (unexported, or on an unexported type), and that is
// read as a status argument without a range check at the read. Its
// reads are quiet and its stores are checked instead, like a sink's
// callers: x.F = v and T{F: v} must store a known-good value, the
// WriteHeader method's own parameter (a buffering wrapper recording
// what it will replay: Compress, Timeout, the cache recorder), or a
// sink parameter of the storing function (crud's phraseRefusal(code)
// and entityui's refuse(status): their callers are checked). A keyed
// literal that leaves a status field out stores its zero and is
// reported, unless the field is a recorded one: a wrapper replays it
// only after WriteHeader set it. A field an importer can write
// (exported, on an exported type: handler.Error.Code,
// IdempotentResponse.Status) is never a status field: the package cannot
// see all of its stores, so its read is checked where it is written out,
// like any other value. That is what fires on 8708ddd5.
//
// Silent postures, deliberately:
//   - a field filled through its address (a row Scan): it holds what the
//     package stored;
//   - a status field's store made from another package (an exported
//     constructor's caller is checked through the sink fact; a literal of
//     an unexported type cannot be written outside);
//   - an interface-method call: the rule follows declared functions,
//     not dynamic dispatch;
//   - _test.go files.
package statusrange

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/rwcall"
)

var Analyzer = &analysis.Analyzer{
	Name:      "statusrange",
	Doc:       "report a non-constant status reaching WriteHeader or http.Error with no range check",
	Run:       run,
	FactTypes: []analysis.Fact{(*statusFacts)(nil)},
}

// statusFacts records what statusrange learned about a function: the
// result indexes that are known-good statuses, the int parameters that
// flow unchecked into a WriteHeader (sinks), and the int parameters a
// bool-returning function range-checks (checkers).
type statusFacts struct {
	Safe    []int
	Sinks   []int
	Checker []int
}

func (*statusFacts) AFact() {}

func (f *statusFacts) String() string {
	return fmt.Sprintf("status(safe=%v sinks=%v checks=%v)", f.Safe, f.Sinks, f.Checker)
}

type checker struct {
	pass        *analysis.Pass
	decls       map[*types.Func]*ast.FuncDecl
	safe        map[*types.Func][]int
	sinks       map[*types.Func][]int
	checks      map[*types.Func][]int
	stores      []store
	statusField map[*types.Var]bool
	owners      map[*types.Var]*types.TypeName
	litSinks    map[types.Object][]int // locals bound to a sink literal
	litOf       map[*ast.FuncLit][]int // the sink literals themselves
}

func run(pass *analysis.Pass) (any, error) {
	c := &checker{
		pass:        pass,
		decls:       map[*types.Func]*ast.FuncDecl{},
		safe:        map[*types.Func][]int{},
		sinks:       map[*types.Func][]int{},
		checks:      map[*types.Func][]int{},
		statusField: map[*types.Var]bool{},
		litSinks:    map[types.Object][]int{},
		litOf:       map[*ast.FuncLit][]int{},
	}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					c.decls[fn] = fd
				}
			}
		}
	}
	c.computeCheckers()
	c.collectStores()
	// Safe results, sinks and status fields feed each other (a sink's
	// caller may pass a safe result; a status field's store may be a
	// sink parameter); all three only grow, so iterate to a joint fixed
	// point.
	for changed := true; changed; {
		changed = c.computeStatusFields()
		if c.computeSafe() {
			changed = true
		}
		if c.computeSinks() {
			changed = true
		}
	}
	keys := map[*types.Func]bool{}
	for _, m := range []map[*types.Func][]int{c.safe, c.sinks, c.checks} {
		for k := range m {
			keys[k] = true
		}
	}
	for _, fn := range slices.SortedFunc(maps.Keys(keys), func(a, b *types.Func) int { return int(a.Pos() - b.Pos()) }) {
		pass.ExportObjectFact(fn, &statusFacts{Safe: c.safe[fn], Sinks: c.sinks[fn], Checker: c.checks[fn]})
	}
	for _, f := range pass.Files {
		if pathflow.IsTestFile(pass, f) {
			continue
		}
		c.bindLiterals(f)
		for _, fn := range astx.AllFuncs(f) {
			c.scan(fn)
		}
	}
	for _, s := range c.stores {
		if !c.statusField[s.field] || c.storeOK(s) {
			continue
		}
		if s.zero {
			if c.recordedByWriteHeader(s.field) {
				continue // a wrapper's recorded status: replayed only once WriteHeader set it
			}
			pass.Reportf(s.at.Pos(),
				"statusrange: this literal leaves %s zero, and %s reaches WriteHeader; net/http panics on code 0 (set it)",
				s.field.Name(), s.field.Name())
			continue
		}
		pass.Reportf(s.at.Pos(),
			"statusrange: a status that is neither constant nor range-checked is stored in %s, which reaches WriteHeader; net/http panics on a code outside 100-999 and a 2xx sends an error as success (clamp it before it is stored)",
			s.field.Name())
	}
	return nil, nil
}

// facts returns fn's facts: local computation for this package, the
// imported fact otherwise.
func (c *checker) facts(fn *types.Func) statusFacts {
	if fn == nil {
		return statusFacts{}
	}
	fn = fn.Origin()
	if fn.Pkg() == c.pass.Pkg {
		return statusFacts{Safe: c.safe[fn], Sinks: c.sinks[fn], Checker: c.checks[fn]}
	}
	var f statusFacts
	c.pass.ImportObjectFact(fn, &f)
	return f
}

func isInt(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

// intParams maps a function's int parameters to their indexes.
func intParams(pass *analysis.Pass, ft *ast.FuncType) map[types.Object]int {
	out := map[types.Object]int{}
	i := 0
	for _, field := range ft.Params.List {
		if len(field.Names) == 0 {
			i++
			continue
		}
		for _, name := range field.Names {
			if obj := pass.TypesInfo.Defs[name]; obj != nil && isInt(obj.Type()) {
				out[obj] = i
			}
			i++
		}
	}
	return out
}

// computeCheckers: a bool-returning function whose body compares an
// int parameter against a constant with an ordered operator.
func (c *checker) computeCheckers() {
	for fn, fd := range c.decls {
		sig := fn.Type().(*types.Signature)
		if sig.Results().Len() != 1 {
			continue
		}
		if b, ok := sig.Results().At(0).Type().Underlying().(*types.Basic); !ok || b.Kind() != types.Bool {
			continue
		}
		for obj, i := range intParams(c.pass, fd.Type) {
			if comparesAgainstConst(c.pass, fd.Body, keyOfObj(obj), token.NoPos) {
				c.checks[fn] = append(c.checks[fn], i)
			}
		}
		slices.Sort(c.checks[fn])
	}
}

// store is one write of an int field: x.F = v, or T{F: v}.
type store struct {
	field *types.Var
	rhs   ast.Expr // nil when the write cannot be read as one value
	at    ast.Node
	fd    *ast.FuncDecl
	zero  bool // a keyed composite literal that leaves the field out
}

// collectStores records every int-field store in the package's
// non-test function declarations (literals inside them included). A
// field filled through its address (a row Scan) is what the package
// stored: neutral, neither a store nor a read.
func (c *checker) collectStores() {
	for _, f := range c.pass.Files {
		if pathflow.IsTestFile(c.pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.AssignStmt:
					for i, lhs := range n.Lhs {
						if v := fieldOf(c.pass, lhs); v != nil && isInt(v.Type()) {
							var rhs ast.Expr
							if len(n.Lhs) == len(n.Rhs) && (n.Tok == token.ASSIGN || n.Tok == token.DEFINE) {
								rhs = n.Rhs[i]
							}
							c.stores = append(c.stores, store{field: v, rhs: rhs, at: lhs, fd: fd})
						}
					}
				case *ast.CompositeLit:
					st, ok := astx.Deref(c.pass.TypesInfo.TypeOf(n)).Underlying().(*types.Struct)
					if !ok {
						return true
					}
					keyed := map[*types.Var]bool{}
					for _, elt := range n.Elts {
						kv, ok := elt.(*ast.KeyValueExpr)
						if !ok {
							return true // positional: every field is set
						}
						id, ok := kv.Key.(*ast.Ident)
						if !ok {
							continue
						}
						if v, ok := c.pass.TypesInfo.ObjectOf(id).(*types.Var); ok && v.IsField() && isInt(v.Type()) {
							keyed[v] = true
							c.stores = append(c.stores, store{field: v, rhs: kv.Value, at: kv.Value, fd: fd})
						}
					}
					// A keyed literal that leaves an int field out stores its
					// zero, which WriteHeader panics on.
					for i := range st.NumFields() {
						if v := st.Field(i); isInt(v.Type()) && !keyed[v] {
							c.stores = append(c.stores, store{field: v, rhs: nil, at: n, fd: fd, zero: true})
						}
					}
				}
				return true
			})
		}
	}
}

// computeStatusFields: an int field declared in this package and read
// as a status argument. Its stores are checked in place of its reads.
func (c *checker) computeStatusFields() bool {
	changed := false
	for _, f := range c.pass.Files {
		if pathflow.IsTestFile(c.pass, f) {
			continue
		}
		for _, fn := range astx.AllFuncs(f) {
			body := astx.BodyOf(fn)
			c.eachStatusCall(body, func(call *ast.CallExpr, arg ast.Expr) {
				v := fieldOf(c.pass, arg)
				if v == nil || v.Pkg() != c.pass.Pkg || c.statusField[v] || c.storableOutside(v) {
					return
				}
				// A read already range-checked where it is written out
				// (kiln's WriteTo validating r.Status) needs nothing from
				// the field's stores.
				if c.rangeChecked(arg, body, call.Pos()) {
					return
				}
				c.statusField[v] = true
				changed = true
			})
		}
	}
	return changed
}

// recordedByWriteHeader: some store of v is a WriteHeader method's own
// parameter, the status a buffering wrapper recorded. Its zero in the
// constructing literal means "not written yet", and the wrapper replays
// it only after WriteHeader set it (urlprefix's writer, Compress).
func (c *checker) recordedByWriteHeader(v *types.Var) bool {
	for _, s := range c.stores {
		if s.field == v && s.rhs != nil && c.passThrough(s.fd, s.rhs) {
			return true
		}
	}
	return false
}

// storableOutside: an importer can write v (an exported field of an
// exported struct type), so the stores this package sees are not all of
// them, and a read of it is checked where it is written out
// (handler.Error.Code, which any caller constructs).
func (c *checker) storableOutside(v *types.Var) bool {
	if !v.Exported() {
		return false
	}
	if c.owners == nil {
		c.owners = map[*types.Var]*types.TypeName{}
		scope := c.pass.Pkg.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			if st, ok := tn.Type().Underlying().(*types.Struct); ok {
				for i := range st.NumFields() {
					c.owners[st.Field(i)] = tn
				}
			}
		}
	}
	owner := c.owners[v]
	return owner == nil || owner.Exported()
}

// storeOK: a store into a status field is known good, a WriteHeader
// method's own parameter (a recorded status), or an int parameter that
// is a sink of its function.
func (c *checker) storeOK(s store) bool {
	if s.rhs == nil {
		return false
	}
	if c.passThrough(s.fd, s.rhs) || c.knownGood(s.rhs, s.fd.Body, s.fd, s.at.Pos()) {
		return true
	}
	return c.sinkParam(s.fd, s.rhs)
}

// sinkParam: e is an int parameter of fd that is one of fd's sinks.
func (c *checker) sinkParam(fd *ast.FuncDecl, e ast.Expr) bool {
	id, ok := ast.Unparen(e).(*ast.Ident)
	if !ok {
		return false
	}
	fn, ok := c.pass.TypesInfo.Defs[fd.Name].(*types.Func)
	if !ok {
		return false
	}
	idx, ok := intParams(c.pass, fd.Type)[c.pass.TypesInfo.Uses[id]]
	return ok && slices.Contains(c.sinks[fn], idx)
}

// computeSafe: result indexes known good at every return. Reports
// whether anything changed.
func (c *checker) computeSafe() bool {
	changed := false
	for fn, fd := range c.decls {
		sig := fn.Type().(*types.Signature)
		for i := range sig.Results().Len() {
			if slices.Contains(c.safe[fn], i) || !isInt(sig.Results().At(i).Type()) {
				continue
			}
			all, any := true, false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				ret, ok := n.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				any = true
				if len(ret.Results) != sig.Results().Len() || !c.knownGood(ret.Results[i], fd.Body, fd, ret.Pos()) {
					all = false
				}
				return true
			})
			if any && all {
				c.safe[fn] = append(c.safe[fn], i)
				slices.Sort(c.safe[fn])
				changed = true
			}
		}
	}
	return changed
}

// computeSinks: int parameters that reach a status call unchecked.
func (c *checker) computeSinks() bool {
	changed := false
	for fn, fd := range c.decls {
		params := intParams(c.pass, fd.Type)
		if len(params) == 0 {
			continue
		}
		c.eachStatusCall(fd.Body, func(call *ast.CallExpr, arg ast.Expr) {
			id, ok := ast.Unparen(arg).(*ast.Ident)
			if !ok {
				return
			}
			idx, ok := params[c.pass.TypesInfo.Uses[id]]
			if !ok || slices.Contains(c.sinks[fn], idx) || c.passThrough(fd, arg) || c.knownGood(arg, fd.Body, fd, call.Pos()) {
				return
			}
			c.sinks[fn] = append(c.sinks[fn], idx)
			slices.Sort(c.sinks[fn])
			changed = true
		})
	}
	for _, s := range c.stores {
		if !c.statusField[s.field] || s.rhs == nil {
			continue
		}
		id, ok := ast.Unparen(s.rhs).(*ast.Ident)
		if !ok {
			continue
		}
		fn, ok := c.pass.TypesInfo.Defs[s.fd.Name].(*types.Func)
		if !ok {
			continue
		}
		idx, ok := intParams(c.pass, s.fd.Type)[c.pass.TypesInfo.Uses[id]]
		if !ok || slices.Contains(c.sinks[fn], idx) || c.passThrough(s.fd, s.rhs) || c.knownGood(s.rhs, s.fd.Body, s.fd, s.at.Pos()) {
			continue
		}
		c.sinks[fn] = append(c.sinks[fn], idx)
		slices.Sort(c.sinks[fn])
		changed = true
	}
	return changed
}

// eachStatusCall calls fn for each status-taking call in body (not in
// nested literals) with its status argument.
func (c *checker) eachStatusCall(body *ast.BlockStmt, fn func(*ast.CallExpr, ast.Expr)) {
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			for _, arg := range c.statusArgs(call) {
				fn(call, arg)
			}
		}
		return true
	})
}

// statusArgs returns the status arguments of a WriteHeader on a
// ResponseWriter, of http.Error, or of a call to a status sink.
func (c *checker) statusArgs(call *ast.CallExpr) []ast.Expr {
	fun := ast.Unparen(call.Fun)
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if sel.Sel.Name == "WriteHeader" && len(call.Args) == 1 {
			if _, isMethod := c.pass.TypesInfo.Selections[sel]; isMethod && rwcall.IsWriter(c.pass.TypesInfo.TypeOf(sel.X)) {
				return call.Args
			}
		}
		if pathflow.QualifiedFunc(c.pass, sel) == "net/http.Error" && len(call.Args) == 3 {
			return call.Args[2:]
		}
	}
	var idx []int
	if id, ok := fun.(*ast.Ident); ok {
		if obj := c.pass.TypesInfo.Uses[id]; obj != nil {
			idx = c.litSinks[obj]
		}
	}
	if idx == nil {
		if f, ok := astx.CalleeFunc(c.pass, fun); ok {
			idx = c.facts(f).Sinks
		}
	}
	var out []ast.Expr
	for _, i := range idx {
		if i < len(call.Args) {
			out = append(out, call.Args[i])
		}
	}
	return out
}

// passThrough: fd is a method named WriteHeader and arg is its own
// parameter.
func (c *checker) passThrough(fn ast.Node, arg ast.Expr) bool {
	fd, ok := fn.(*ast.FuncDecl)
	if !ok || fd.Recv == nil || fd.Name.Name != "WriteHeader" {
		return false
	}
	id, ok := ast.Unparen(arg).(*ast.Ident)
	if !ok {
		return false
	}
	_, isParam := intParams(c.pass, fd.Type)[c.pass.TypesInfo.Uses[id]]
	return isParam
}

// scan reports unchecked status arguments in one function body. Local
// func literals bound to a variable whose int parameter reaches a status
// call become sinks checked at their calls in this body.
func (c *checker) scan(fn ast.Node) {
	body := astx.BodyOf(fn)
	if body == nil {
		return
	}
	var fnObj *types.Func
	if fd, ok := fn.(*ast.FuncDecl); ok {
		fnObj, _ = c.pass.TypesInfo.Defs[fd.Name].(*types.Func)
	}
	params := map[types.Object]int{}
	switch f := fn.(type) {
	case *ast.FuncDecl:
		params = intParams(c.pass, f.Type)
	case *ast.FuncLit:
		params = intParams(c.pass, f.Type)
	}
	c.eachStatusCall(body, func(call *ast.CallExpr, arg ast.Expr) {
		if c.passThrough(fn, arg) || c.knownGood(arg, body, fn, call.Pos()) {
			return
		}
		if id, ok := ast.Unparen(arg).(*ast.Ident); ok {
			if idx, isParam := params[c.pass.TypesInfo.Uses[id]]; isParam {
				if fnObj != nil && slices.Contains(c.sinks[fnObj], idx) {
					return // a sink: its callers are checked
				}
				if lit, ok := fn.(*ast.FuncLit); ok && slices.Contains(c.litOf[lit], idx) {
					return // checked at the literal's calls
				}
			}
		}
		c.pass.Reportf(arg.Pos(),
			"statusrange: a status that is neither constant nor range-checked reaches %s; net/http panics on a code outside 100-999 and a 2xx sends an error as success (clamp it first: if c < 400 || c > 599 { c = http.StatusInternalServerError })",
			c.label(call))
	})
}

// bindLiterals finds func literals bound to a local
// (`begin := func(code int) {...}`) and records the int parameters that
// reach a status call unchecked as sinks of that local, so its calls in
// the enclosing body are checked instead of the call inside it.
func (c *checker) bindLiterals(f *ast.File) {
	bind := func(id *ast.Ident, rhs ast.Expr) {
		lit, ok := ast.Unparen(rhs).(*ast.FuncLit)
		if !ok {
			return
		}
		obj := c.pass.TypesInfo.ObjectOf(id)
		if obj == nil {
			return
		}
		params := intParams(c.pass, lit.Type)
		var idx []int
		c.eachStatusCall(lit.Body, func(call *ast.CallExpr, arg ast.Expr) {
			pid, ok := ast.Unparen(arg).(*ast.Ident)
			if !ok {
				return
			}
			i, ok := params[c.pass.TypesInfo.Uses[pid]]
			if ok && !slices.Contains(idx, i) && !c.knownGood(arg, lit.Body, lit, call.Pos()) {
				idx = append(idx, i)
			}
		})
		if len(idx) > 0 {
			c.litSinks[obj] = idx
			c.litOf[lit] = idx
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				if id, ok := n.Lhs[0].(*ast.Ident); ok {
					bind(id, n.Rhs[0])
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) == 1 && len(n.Values) == 1 {
				bind(n.Names[0], n.Values[0])
			}
		}
		return true
	})
}

func (c *checker) label(call *ast.CallExpr) string {
	fun := ast.Unparen(call.Fun)
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		if sel.Sel.Name == "WriteHeader" {
			return "WriteHeader"
		}
		if pathflow.QualifiedFunc(c.pass, sel) == "net/http.Error" {
			return "http.Error"
		}
	}
	return "a status sink (" + astx.CalleeName(fun) + ")"
}

// knownGood: see the package doc.
func (c *checker) knownGood(e ast.Expr, body *ast.BlockStmt, fn ast.Node, pos token.Pos) bool {
	return c.knownGoodDepth(e, body, fn, pos, 0)
}

func (c *checker) knownGoodDepth(e ast.Expr, body *ast.BlockStmt, fn ast.Node, pos token.Pos, depth int) bool {
	e = ast.Unparen(e)
	if tv, ok := c.pass.TypesInfo.Types[e]; ok && tv.Value != nil {
		return true
	}
	if depth > 4 {
		return false
	}
	switch x := e.(type) {
	case *ast.CallExpr:
		if tv, ok := c.pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() && len(x.Args) == 1 {
			return c.knownGoodDepth(x.Args[0], body, fn, pos, depth+1) // int(code)
		}
		if f, ok := astx.CalleeFunc(c.pass, ast.Unparen(x.Fun)); ok && slices.Contains(c.facts(f).Safe, 0) {
			return true
		}
	case *ast.SelectorExpr:
		if receivedStatus(c.pass, x) {
			return true
		}
		if v := fieldOf(c.pass, x); v != nil && c.statusField[v] {
			return true // its stores are checked instead
		}
	}
	if c.rangeChecked(e, body, pos) {
		return true
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := c.pass.TypesInfo.Uses[id].(*types.Var)
	if !ok || v.Parent() == nil || v.Parent() == c.pass.Pkg.Scope() || isParam(c.pass, fn, v) {
		return false
	}
	assigned := c.assignments(body, v)
	if len(assigned) == 0 {
		return false
	}
	for _, a := range assigned {
		if !a.ok {
			return false
		}
		if a.call != nil {
			f, ok := astx.CalleeFunc(c.pass, ast.Unparen(a.call.Fun))
			if !ok || !slices.Contains(c.facts(f).Safe, a.index) {
				return false
			}
			continue
		}
		if !c.knownGoodDepth(a.rhs, body, fn, a.rhs.Pos(), depth+1) {
			return false
		}
	}
	return true
}

// receivedStatus: x is resp.StatusCode on an *http.Response, or the Code
// of an httptest.ResponseRecorder: a code an HTTP peer already sent.
func receivedStatus(pass *analysis.Pass, x *ast.SelectorExpr) bool {
	t := pass.TypesInfo.TypeOf(x.X)
	if t == nil {
		return false
	}
	switch {
	case x.Sel.Name == "StatusCode" && astx.IsNamed(astx.Deref(t), "net/http", "Response"):
		return true
	case x.Sel.Name == "Code" && astx.IsNamed(astx.Deref(t), "net/http/httptest", "ResponseRecorder"):
		return true
	}
	return false
}

func isParam(pass *analysis.Pass, fn ast.Node, v *types.Var) bool {
	var ft *ast.FuncType
	switch f := fn.(type) {
	case *ast.FuncDecl:
		ft = f.Type
	case *ast.FuncLit:
		ft = f.Type
	}
	if ft == nil || ft.Params == nil {
		return false
	}
	for _, field := range ft.Params.List {
		for _, name := range field.Names {
			if pass.TypesInfo.Defs[name] == v {
				return true
			}
		}
	}
	return false
}

// assignment is one store into a local: a single value (rhs), or result
// index of a multi-value call; ok false when it cannot be read.
type assignment struct {
	rhs   ast.Expr
	call  *ast.CallExpr
	index int
	ok    bool
}

func (c *checker) assignments(body *ast.BlockStmt, v *types.Var) []assignment {
	var out []assignment
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				id, ok := ast.Unparen(lhs).(*ast.Ident)
				if !ok || c.pass.TypesInfo.ObjectOf(id) != v {
					continue
				}
				switch {
				case n.Tok != token.ASSIGN && n.Tok != token.DEFINE:
					out = append(out, assignment{})
				case len(n.Lhs) == len(n.Rhs):
					out = append(out, assignment{rhs: n.Rhs[i], ok: true})
				case len(n.Rhs) == 1:
					call, ok := ast.Unparen(n.Rhs[0]).(*ast.CallExpr)
					out = append(out, assignment{call: call, index: i, ok: ok})
				default:
					out = append(out, assignment{})
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				if c.pass.TypesInfo.ObjectOf(id) != v {
					continue
				}
				switch {
				case len(n.Values) == len(n.Names):
					out = append(out, assignment{rhs: n.Values[i], ok: true})
				case len(n.Values) == 0:
					out = append(out, assignment{rhs: &ast.BasicLit{Kind: token.INT, Value: "0", ValuePos: id.Pos()}, ok: true})
				default:
					out = append(out, assignment{})
				}
			}
		case *ast.UnaryExpr:
			if n.Op == token.AND {
				if id, ok := ast.Unparen(n.X).(*ast.Ident); ok && c.pass.TypesInfo.ObjectOf(id) == v {
					out = append(out, assignment{}) // address taken: anything may write it
				}
			}
		case *ast.IncDecStmt:
			if id, ok := ast.Unparen(n.X).(*ast.Ident); ok && c.pass.TypesInfo.ObjectOf(id) == v {
				out = append(out, assignment{})
			}
		}
		return true
	})
	return out
}

// rangeChecked: before pos in body, an if/case condition compares e's
// path against a constant with an ordered operator, or passes it to a
// checker.
func (c *checker) rangeChecked(e ast.Expr, body *ast.BlockStmt, pos token.Pos) bool {
	key := pathKey(c.pass, e)
	if key == "" {
		return false
	}
	found := false
	check := func(cond ast.Expr) {
		if comparesAgainstConst(c.pass, cond, key, token.NoPos) {
			found = true
			return
		}
		ast.Inspect(cond, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || found {
				return !found
			}
			f, ok := astx.CalleeFunc(c.pass, ast.Unparen(call.Fun))
			if !ok {
				return true
			}
			for _, i := range c.facts(f).Checker {
				if i < len(call.Args) && pathKey(c.pass, call.Args[i]) == key {
					found = true
				}
			}
			return true
		})
	}
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil || n.Pos() >= pos {
			return false
		}
		switch n := n.(type) {
		case *ast.IfStmt:
			check(n.Cond)
		case *ast.CaseClause:
			for _, x := range n.List {
				check(x)
			}
		}
		return true
	})
	return found
}

// comparesAgainstConst: n holds an ordered comparison of the path key
// against a constant (anywhere, when before is NoPos).
func comparesAgainstConst(pass *analysis.Pass, n ast.Node, key string, _ token.Pos) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || found {
			return !found
		}
		switch be.Op {
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			return true
		}
		for _, pair := range [][2]ast.Expr{{be.X, be.Y}, {be.Y, be.X}} {
			tv, ok := pass.TypesInfo.Types[pair[1]]
			if ok && tv.Value != nil && pathKey(pass, pair[0]) == key {
				found = true
			}
		}
		return true
	})
	return found
}

func keyOfObj(obj types.Object) string { return fmt.Sprintf("%d", obj.Pos()) }

// pathKey renders an identifier or selector chain over objects as a
// stable key (the objects' positions), "" for anything else.
func pathKey(pass *analysis.Pass, e ast.Expr) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if obj := pass.TypesInfo.ObjectOf(x); obj != nil {
			return keyOfObj(obj)
		}
	case *ast.SelectorExpr:
		if base := pathKey(pass, x.X); base != "" {
			if obj := pass.TypesInfo.ObjectOf(x.Sel); obj != nil {
				return base + "." + keyOfObj(obj)
			}
		}
	case *ast.CallExpr:
		if tv, ok := pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() && len(x.Args) == 1 {
			return pathKey(pass, x.Args[0])
		}
	}
	return ""
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
