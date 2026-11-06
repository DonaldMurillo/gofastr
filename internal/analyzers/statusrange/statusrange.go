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
//   - range-checked on the REACHING PATH, in the 2026-11-03 polarity
//     model: a check counts only when it proves c inside 100-999 — a
//     diverging guard whose condition, when FALSE, leaves it in range
//     (`if !validStatus(c) { return }`), a clamp whose false side is in
//     range and whose body reassigns an in-range constant, or the
//     condition of an enclosing if/for/case arm that HELD (`if c >= 400
//     && c <= 599 { WriteHeader(c) }`). A log-only comparison, an
//     equality with a value outside the range (`c != 0`), or a
//     one-sided bound proves nothing. A checker call (a bool function
//     whose TRUE result proves the window: kiln's validStatus) counts
//     when held;
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
// (i18n.HTTPError was one until it clamped). A status call in a CLOSURE
// capturing the parameter counts too (`defer func() {
// w.WriteHeader(code) }()`): the sink is the enclosing function's, and
// the call inside the closure is not reported. The same holds for a
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
// callers: x.F = v, T{F: v} (keyed or positional — element i sets field
// i), stores at package level (`var boot = refusal{code: parse(s)}`),
// and literals inside package-level function literals must store a
// known-good value: the WriteHeader method's own parameter (a buffering
// wrapper recording what it will replay: Compress, Timeout, the cache
// recorder), or a sink parameter of the storing function (crud's
// phraseRefusal(code) and entityui's refuse(status): their callers are
// checked). A keyed literal that leaves a status field out stores its
// zero and is reported, unless the field is a recorded one: a wrapper
// replays it only after WriteHeader set it. A field an importer can
// write (exported, on an exported type: handler.Error.Code,
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
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/astx"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
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
		c.scanTree(f)
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

// computeCheckers: a bool-returning function whose TRUE result proves
// an int parameter inside 100-999 — every return expression must
// establish the window (a comparison tree, or a literal true backed by
// a diverging guard before it).
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
			if c.provesWindow(fd.Body, keyOfObj(obj)) {
				c.checks[fn] = append(c.checks[fn], i)
			}
		}
		slices.Sort(c.checks[fn])
	}
}

// provesWindow: every return statement's value proves the key inside
// 100-999 when true.
func (c *checker) provesWindow(body *ast.BlockStmt, key string) bool {
	ok := true
	ast.Inspect(body, func(n ast.Node) bool {
		ret, isRet := n.(*ast.ReturnStmt)
		if !isRet || !ok || len(ret.Results) != 1 {
			return !isRet
		}
		v := ast.Unparen(ret.Results[0])
		if id, isIdent := v.(*ast.Ident); isIdent && c.pass.TypesInfo.ObjectOf(id) != nil {
			tv, hasVal := c.pass.TypesInfo.Types[v]
			if hasVal && tv.Value != nil && tv.Value.Kind() == constant.Bool && !constant.BoolVal(tv.Value) {
				return false // a literal false says nothing
			}
			// literal true: the diverging guard before the return did
			// the proving
			found := false
			c.scanBlock(body.List, key, ret.Pos(), &found)
			if !found {
				ok = false
			}
			return false
		}
		if !c.condSides(v, key, true).both() {
			ok = false
		}
		return false
	})
	return ok
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
// non-test code: function declarations (literals inside them included)
// and package-level variable values (a direct composite literal, or a
// function literal's body). A field filled through its address (a row
// Scan) is what the package stored: neutral, neither a store nor a
// read.
func (c *checker) collectStores() {
	collect := func(root ast.Node, fd *ast.FuncDecl) {
		ast.Inspect(root, func(n ast.Node) bool {
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
				positional := false
				for _, elt := range n.Elts {
					if _, ok := elt.(*ast.KeyValueExpr); !ok {
						positional = true
					}
				}
				if positional {
					// element i sets field i
					for i, elt := range n.Elts {
						if i < st.NumFields() {
							if v := st.Field(i); isInt(v.Type()) {
								c.stores = append(c.stores, store{field: v, rhs: elt, at: elt, fd: fd})
							}
						}
					}
					return true
				}
				for _, elt := range n.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
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
	for _, f := range c.pass.Files {
		if pathflow.IsTestFile(c.pass, f) {
			continue
		}
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Body != nil {
					collect(decl.Body, decl)
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, v := range vs.Values {
						collect(v, nil)
					}
				}
			}
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
	if s.fd == nil {
		// a package-level store: only the value itself can vouch
		return c.knownGood(s.rhs, nil, nil, s.at.Pos())
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
		c.eachStatusCallDeep(fd.Body, func(call *ast.CallExpr, arg ast.Expr) {
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
		if s.fd == nil {
			continue // a package-level store: no function to sink
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

// eachStatusCallDeep calls fn for each status-taking call anywhere in
// body, closures included: a deferred closure that captures the
// enclosing function's parameter still sinks that parameter (the
// parameter's object is the same through the capture; a closure's own
// parameters are different objects and never match).
func (c *checker) eachStatusCallDeep(body *ast.BlockStmt, fn func(*ast.CallExpr, ast.Expr)) {
	ast.Inspect(body, func(n ast.Node) bool {
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
	if !ok || fd == nil || fd.Recv == nil || fd.Name.Name != "WriteHeader" {
		return false
	}
	id, ok := ast.Unparen(arg).(*ast.Ident)
	if !ok {
		return false
	}
	_, isParam := intParams(c.pass, fd.Type)[c.pass.TypesInfo.Uses[id]]
	return isParam
}

// scanTree reports unchecked status arguments through a file's
// functions, closures included: a literal that captures an enclosing
// function's sink parameter is that function's sink, checked at its
// callers, and the call inside the closure is not reported.
func (c *checker) scanTree(f *ast.File) {
	var walk func(fn ast.Node, outer map[types.Object]bool)
	walk = func(fn ast.Node, outer map[types.Object]bool) {
		body := astx.BodyOf(fn)
		if body == nil {
			return
		}
		params := map[types.Object]int{}
		switch f := fn.(type) {
		case *ast.FuncDecl:
			params = intParams(c.pass, f.Type)
		case *ast.FuncLit:
			params = intParams(c.pass, f.Type)
		}
		// This function's own sink parameters: an exported fact for a
		// declaration, the bound-literal record for a literal.
		var sinkIdx []int
		if fd, ok := fn.(*ast.FuncDecl); ok {
			if fnObj, ok := c.pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
				sinkIdx = c.sinks[fnObj]
			}
		} else if lit, ok := fn.(*ast.FuncLit); ok {
			sinkIdx = c.litOf[lit]
		}
		inner := map[types.Object]bool{}
		for o := range outer {
			inner[o] = true
		}
		for _, idx := range sinkIdx {
			for obj, i := range params {
				if i == idx {
					inner[obj] = true
				}
			}
		}
		c.eachStatusCall(body, func(call *ast.CallExpr, arg ast.Expr) {
			if c.passThrough(fn, arg) || c.knownGood(arg, body, fn, call.Pos()) {
				return
			}
			if id, ok := ast.Unparen(arg).(*ast.Ident); ok {
				if inner[c.pass.TypesInfo.Uses[id]] {
					return // a sink parameter: its callers are checked
				}
			}
			c.pass.Reportf(arg.Pos(),
				"statusrange: a status that is neither constant nor range-checked reaches %s; net/http panics on a code outside 100-999 and a 2xx sends an error as success (clamp it first: if c < 400 || c > 599 { c = http.StatusInternalServerError })",
				c.label(call))
		})
		ast.Inspect(body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.FuncLit); ok {
				walk(lit, inner)
				return false
			}
			return true
		})
	}
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			walk(decl, map[types.Object]bool{})
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, v := range vs.Values {
					if lit, ok := ast.Unparen(v).(*ast.FuncLit); ok {
						walk(lit, map[types.Object]bool{})
					}
				}
			}
		}
	}
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
			// The writeBlock spelling: the store's own block clamps
			// the value on every path out of it (status =
			// res.Decision.Status; if status < 400 || status > 599 {
			// status = http.StatusInternalServerError }). The clamp
			// runs after this store wherever the store's value
			// survives to pos.
			if a.block == nil || !c.rangeChecked(id, a.block, pos) {
				return false
			}
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
// index of a multi-value call; ok false when it cannot be read. block
// is the innermost block holding the store (nil when body is nil).
type assignment struct {
	rhs   ast.Expr
	call  *ast.CallExpr
	index int
	ok    bool
	block *ast.BlockStmt
}

func (c *checker) assignments(body *ast.BlockStmt, v *types.Var) []assignment {
	if body == nil {
		return nil // a package-level store: no function body
	}
	// Every block in the body, to find the tightest one holding each
	// store without a parent map. A case clause's body is not a
	// BlockStmt node, so it gets a synthesized one (the per-assignment
	// clamp check walks it).
	var blocks []*ast.BlockStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch b := n.(type) {
		case *ast.BlockStmt:
			blocks = append(blocks, b)
		case *ast.CaseClause:
			if len(b.Body) > 0 {
				blocks = append(blocks, &ast.BlockStmt{List: b.Body, Lbrace: b.Pos(), Rbrace: b.End()})
			}
		}
		return true
	})
	enclosing := func(n ast.Node) *ast.BlockStmt {
		var best *ast.BlockStmt
		for _, b := range blocks {
			if b.Pos() <= n.Pos() && n.End() <= b.End() && (best == nil || b.Pos() > best.Pos()) {
				best = b
			}
		}
		return best
	}
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
					out = append(out, assignment{rhs: n.Rhs[i], ok: true, block: enclosing(n)})
				case len(n.Rhs) == 1:
					call, ok := ast.Unparen(n.Rhs[0]).(*ast.CallExpr)
					out = append(out, assignment{call: call, index: i, ok: ok, block: enclosing(n)})
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
					out = append(out, assignment{rhs: n.Values[i], ok: true, block: enclosing(n)})
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

// rangeChecked: on the path through body to pos, a check proves the
// path key inside 100-999 — a diverging guard whose condition, when
// FALSE, leaves it in range (`if !validStatus(c) { return }`), a clamp
// guard whose false side is in range and whose body reassigns an
// in-range constant (`if c < 400 || c > 599 { c = 500 }`), or the
// condition of an enclosing if/for/case arm that HELD (`if c >= 400 &&
// c <= 599 { WriteHeader(c) }`). A log-only comparison, an equality
// with a value outside the range, or a one-sided bound proves nothing.
func (c *checker) rangeChecked(e ast.Expr, body *ast.BlockStmt, pos token.Pos) bool {
	if body == nil {
		return false // a package-level store: no path to check
	}
	key := pathKey(c.pass, e)
	if key == "" {
		return false
	}
	found := false
	c.scanBlock(body.List, key, pos, &found)
	return found
}

// scanBlock walks one block's statements toward pos: guards entirely
// before it are checked (they ran first), then the statement holding
// it is descended into. A guard nested inside an earlier sibling
// statement is not on the reaching path and never counts.
func (c *checker) scanBlock(stmts []ast.Stmt, key string, pos token.Pos, found *bool) {
	for _, st := range stmts {
		if *found || st.Pos() > pos {
			return
		}
		if st.End() <= pos {
			if iff, ok := st.(*ast.IfStmt); ok {
				if c.condSides(iff.Cond, key, false).both() && (dominance.Diverges(iff.Body) || c.clampAssigns(iff.Body, key)) {
					*found = true
					return
				}
			}
			continue
		}
		c.scanHolder(st, key, pos, found)
		return
	}
}

// scanHolder descends into the statement holding pos.
func (c *checker) scanHolder(st ast.Stmt, key string, pos token.Pos, found *bool) {
	switch s := st.(type) {
	case *ast.IfStmt:
		if s.Body != nil && pos > s.Body.Pos() && pos < s.Body.End() {
			if c.condSides(s.Cond, key, true).both() {
				*found = true
				return
			}
			c.scanBlock(s.Body.List, key, pos, found)
			return
		}
		if s.Else != nil && pos > s.Else.Pos() && pos < s.Else.End() {
			if c.condSides(s.Cond, key, false).both() {
				*found = true
				return
			}
			c.scanHolder(s.Else, key, pos, found)
		}
	case *ast.ForStmt:
		if s.Cond != nil && c.condSides(s.Cond, key, true).both() {
			*found = true
			return
		}
		if s.Body != nil {
			c.scanBlock(s.Body.List, key, pos, found)
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			c.scanBlock(s.Body.List, key, pos, found)
		}
	case *ast.SwitchStmt:
		if s.Body != nil {
			c.scanHolder(s.Body, key, pos, found)
		}
	case *ast.TypeSwitchStmt:
		if s.Body != nil {
			c.scanHolder(s.Body, key, pos, found)
		}
	case *ast.SelectStmt:
		if s.Body != nil {
			c.scanHolder(s.Body, key, pos, found)
		}
	case *ast.CaseClause:
		// The matched expression is unknown when several are listed:
		// every one must prove the range when true.
		ok := len(s.List) > 0
		for _, x := range s.List {
			if !c.condSides(x, key, true).both() {
				ok = false
				break
			}
		}
		if ok {
			*found = true
			return
		}
		c.scanBlock(s.Body, key, pos, found)
	case *ast.BlockStmt:
		c.scanBlock(s.List, key, pos, found)
	default:
		// A literal or block nested in the statement (a deferred
		// closure, a call argument) still holds pos.
		ast.Inspect(st, func(n ast.Node) bool {
			if n == nil || *found {
				return true
			}
			switch inner := n.(type) {
			case *ast.FuncLit:
				if inner.Body != nil && pos > inner.Body.Pos() && pos < inner.Body.End() {
					c.scanBlock(inner.Body.List, key, pos, found)
				}
				return false
			case *ast.BlockStmt:
				if pos > inner.Pos() && pos < inner.End() {
					c.scanBlock(inner.List, key, pos, found)
				}
				return false
			}
			return pos >= n.Pos() && pos <= n.End()
		})
	}
}

// clampAssigns: the guard body reassigns the path key an in-range
// constant.
func (c *checker) clampAssigns(body *ast.BlockStmt, key string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for i, l := range as.Lhs {
				if i < len(as.Rhs) && pathKey(c.pass, l) == key {
					tv, ok := c.pass.TypesInfo.Types[ast.Unparen(as.Rhs[i])]
					if ok && tv.Value != nil {
						if cv, ok := constant.Int64Val(constant.ToInt(tv.Value)); ok && cv >= 100 && cv <= 999 {
							found = true
						}
					}
				}
			}
		}
		return !found
	})
	return found
}

// sides: which bounds of the 100-999 window a truth value of a
// condition establishes for the path key.
type sides struct{ lower, upper bool }

func (s sides) both() bool { return s.lower && s.upper }

// condSides walks a condition's operator tree and reports which bounds
// of 100-999 its given truth value proves for key. Checker calls count
// as both bounds when held.
func (c *checker) condSides(cond ast.Expr, key string, holds bool) sides {
	switch x := ast.Unparen(cond).(type) {
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return c.condSides(x.X, key, !holds)
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case token.LAND:
			if holds {
				a, b := c.condSides(x.X, key, true), c.condSides(x.Y, key, true)
				return sides{a.lower || b.lower, a.upper || b.upper}
			}
			// ¬(A∧B) is ¬A ∨ ¬B: which operand failed is unknown, so
			// one operand's failure must prove the whole window.
			a, b := c.condSides(x.X, key, false), c.condSides(x.Y, key, false)
			switch {
			case a.both():
				return a
			case b.both():
				return b
			}
			return sides{}
		case token.LOR:
			if holds {
				// which operand held is unknown: one must prove the
				// whole window alone.
				a, b := c.condSides(x.X, key, true), c.condSides(x.Y, key, true)
				switch {
				case a.both():
					return a
				case b.both():
					return b
				}
				return sides{}
			}
			// ¬(A∨B) is ¬A ∧ ¬B: both failed; the bounds combine.
			a, b := c.condSides(x.X, key, false), c.condSides(x.Y, key, false)
			return sides{a.lower || b.lower, a.upper || b.upper}
		}
		return c.comparisonSides(x, key, holds)
	case *ast.CallExpr:
		if f, ok := astx.CalleeFunc(c.pass, ast.Unparen(x.Fun)); ok {
			for _, i := range c.facts(f).Checker {
				if i < len(x.Args) && pathKey(c.pass, x.Args[i]) == key && holds {
					return sides{true, true}
				}
			}
		}
	}
	return sides{}
}

// comparisonSides: `key op C` (either operand order) against a
// constant, and which window bounds each truth value establishes.
func (c *checker) comparisonSides(be *ast.BinaryExpr, key string, holds bool) sides {
	var other ast.Expr
	subjectOnLeft := pathKey(c.pass, be.X) == key
	switch {
	case subjectOnLeft:
		other = be.Y
	case pathKey(c.pass, be.Y) == key:
		other = be.X
	default:
		return sides{}
	}
	tv, ok := c.pass.TypesInfo.Types[other]
	if !ok || tv.Value == nil {
		return sides{} // not a constant bound
	}
	cv, ok := constant.Int64Val(constant.ToInt(tv.Value))
	if !ok {
		return sides{}
	}
	var tLower, tUpper, fLower, fUpper bool
	switch be.Op {
	case token.GTR: // key > C ⇔ C < key
		tLower, fUpper = cv >= 99, cv <= 999
	case token.GEQ: // key >= C ⇔ C <= key
		tLower, fUpper = cv >= 100, cv <= 1000
	case token.LSS: // key < C ⇔ C > key
		tUpper, fLower = cv <= 1000, cv >= 99
	case token.LEQ: // key <= C ⇔ C >= key
		tUpper, fLower = cv <= 999, cv >= 100
	case token.EQL, token.NEQ:
		inRange := cv >= 100 && cv <= 999
		if be.Op == token.EQL {
			tLower, tUpper = inRange, inRange
		} else {
			fLower, fUpper = inRange, inRange
		}
	}
	if !subjectOnLeft {
		tLower, tUpper, fLower, fUpper = tUpper, tLower, fUpper, fLower
	}
	if holds {
		return sides{tLower, tUpper}
	}
	return sides{fLower, fUpper}
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
