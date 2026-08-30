// Package reqparamlimit catches unclamped request-sourced integers
// flowing into limit/cap-shaped call parameters. A client-controlled
// limit is a denial-of-service lever: a request that says
// "limit: 9_999_999" makes the server size a search, query, or
// allocation to the attacker's number. Real instance: the MCP
// docs-search tool handler forwarded params["limit"] (type-switched
// out of the request map) verbatim into docs.SearchWithLimit, whose
// hit list then scaled with whatever the client sent
// (framework/mcp_introspection.go:282, the seed of this analyzer).
//
// Lane: vettool (type-aware), NOT the contracts pattern lane. Two
// judgements need types.Info and defeat string-pattern rules:
//   - the callee's parameter NAME at the argument position, resolved
//     from the callee's types.Signature — docs.SearchWithLimit's
//     second parameter is named "limit" whatever the import alias or
//     selector spelling at the call site;
//   - map type identity: only map[string]any-shaped indexes (string
//     key, empty-interface element — the decoded-JSON params shape,
//     named aliases included) count as request-sourced. Typed config
//     maps (map[string]int) and struct fields never produce an
//     extraction and stay silent.
//
// Sanctioned postures that stay silent:
//   - a clamp between extraction and use: any comparison of the
//     extracted variable against a constant literal (limit <= 0,
//     limit > 100) or a max*-prefixed identifier or field (limit >
//     maxHits, limit > c.maxHits) in the straight line — this covers
//     both reassignment clamps (limit = maxHits) and reject-guards
//     (if limit > maxHits { return err }).
//   - an expression clamp: limit := min(params["limit"].(int), 100) —
//     a builtin min/max call anywhere in the assigning expression
//     counts as clamped in place.
//   - clean reassignment: limit = defaultLimit after extraction
//     clears the taint, since the RHS carries none.
//   - limits sourced from constants, config structs, or typed maps.
//   - extraction feeding parameters whose name is outside the set (a
//     string term, a bool flag) — only limit-shaped parameter names
//     are sinks.
//
// Heuristics, exactly as implemented:
//   - extraction: a type assertion or type switch on m[K] where m is
//     map[string]any-shaped and K is a string literal matching
//     (?i)^(limit|max|hits|count|page_?size|batch_?size|top|take)$.
//     The value may then flow through conversions and plain
//     assignments; flow is local, forward, straight-line.
//   - use: the tainted value appears in a positional argument whose
//     callee parameter name matches the same set. Callee signatures
//     resolve through types (function or method object, then the
//     static type of the callee expression); signatures whose
//     parameters are unnamed match nothing. When no signature is
//     available at all, fall back to a call-site positional
//     heuristic — only the FINAL positional argument is treated as
//     limit-shaped — and builtins and conversions are excluded
//     outright. The fallback is stated here for completeness: in
//     compiling code the signature almost always resolves, so the
//     fallback is effectively unreachable.
//   - clamp: a comparison (==, !=, <, <=, >, >=) of a tainted
//     variable against a constant literal or a max* identifier or
//     field, appearing in an if/switch/for condition between
//     extraction and use, clears that variable. Taint learned inside
//     a conditional branch, loop body, or closure stays inside it
//     (closures are checked as fresh functions with no inherited
//     taint); assignments made inside type-switch clauses DO escape
//     the switch, because the switch statement itself always
//     executes.
//   - one diagnostic per call site: the first matching argument is
//     reported at the argument's position.
package reqparamlimit

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

const Doc = "report request-sourced limit-shaped map values passed to limit-shaped parameters without a clamp"

var Analyzer = &analysis.Analyzer{
	Name: "reqparamlimit",
	Doc:  Doc,
	Run:  run,
}

// nameRe is the request-param and limit-parameter name set.
var nameRe = regexp.MustCompile(`(?i)^(limit|max|hits|count|page_?size|batch_?size|top|take)$`)

// maxRe matches clamp-bound identifiers and fields (maxHits,
// maxEntries, MaxBatch).
var maxRe = regexp.MustCompile(`(?i)^max`)

func run(pass *analysis.Pass) (any, error) {
	// Function bodies: FuncDecls and FuncLits alike — HTTP and tool
	// handlers are often closures. Each is analyzed independently;
	// nothing flows across function boundaries.
	var bodies []*ast.BlockStmt
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch fn := n.(type) {
			case *ast.FuncDecl:
				if fn.Body != nil {
					bodies = append(bodies, fn.Body)
				}
			case *ast.FuncLit:
				bodies = append(bodies, fn.Body)
			}
			return true
		})
	}
	for _, b := range bodies {
		w := &walker{pass: pass, tainted: map[string]string{}}
		w.block(b.List)
	}
	return nil, nil
}

// walker carries straight-line taint through one function body.
// tainted maps a variable name to the request-param key that sourced
// it (used for the diagnostic message).
type walker struct {
	pass    *analysis.Pass
	tainted map[string]string
}

func (w *walker) block(list []ast.Stmt) {
	for _, s := range list {
		w.stmt(s)
	}
}

func (w *walker) copy() map[string]string {
	c := make(map[string]string, len(w.tainted))
	for k, v := range w.tainted {
		c[k] = v
	}
	return c
}

func (w *walker) assign(name string, key string, tainted bool) {
	if name == "" || name == "_" {
		return
	}
	if tainted {
		w.tainted[name] = key
	} else {
		delete(w.tainted, name)
	}
}

func (w *walker) stmt(s ast.Stmt) {
	switch st := s.(type) {
	case *ast.AssignStmt:
		// Uses are checked against the pre-statement taint; the
		// assignment then re-taints or cleans its targets.
		w.checkUses(st.Rhs)
		key, tainted := w.rhsTaint(st.Rhs)
		for _, lhs := range st.Lhs {
			if id, ok := unwrapParen(lhs).(*ast.Ident); ok {
				w.assign(id.Name, key, tainted)
			}
		}
	case *ast.DeclStmt:
		gd, ok := st.Decl.(*ast.GenDecl)
		if !ok {
			return
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			w.checkUses(vs.Values)
			key, tainted := w.rhsTaint(vs.Values)
			for _, n := range vs.Names {
				w.assign(n.Name, key, tainted)
			}
		}
	case *ast.ExprStmt:
		w.checkUses([]ast.Expr{st.X})
	case *ast.ReturnStmt:
		w.checkUses(st.Results)
	case *ast.SendStmt:
		w.checkUses([]ast.Expr{st.Value})
	case *ast.GoStmt:
		w.checkUses([]ast.Expr{st.Call})
	case *ast.DeferStmt:
		w.checkUses([]ast.Expr{st.Call})
	case *ast.IncDecStmt:
		// limit++ reads and rewrites in place; taint unchanged.
	case *ast.IfStmt:
		if st.Init != nil {
			w.stmt(st.Init)
		}
		for _, id := range w.clampIDs(st.Cond) {
			delete(w.tainted, id)
		}
		saved := w.tainted
		w.tainted = w.copy()
		w.block(st.Body.List)
		w.tainted = saved
		if st.Else != nil {
			w.tainted = w.copy()
			w.stmt(st.Else)
			w.tainted = saved
		}
	case *ast.TypeSwitchStmt:
		w.typeSwitch(st)
	case *ast.SwitchStmt:
		if st.Init != nil {
			w.stmt(st.Init)
		}
		if st.Tag != nil {
			w.checkUses([]ast.Expr{st.Tag})
		}
		for _, cc := range st.Body.List {
			cl, ok := cc.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, e := range cl.List {
				for _, id := range w.clampIDs(e) {
					delete(w.tainted, id)
				}
			}
			saved := w.tainted
			w.tainted = w.copy()
			w.block(cl.Body)
			w.tainted = saved
		}
	case *ast.ForStmt:
		if st.Init != nil {
			w.stmt(st.Init)
		}
		if st.Cond != nil {
			for _, id := range w.clampIDs(st.Cond) {
				delete(w.tainted, id)
			}
		}
		saved := w.tainted
		w.tainted = w.copy()
		w.block(st.Body.List)
		w.tainted = saved
	case *ast.RangeStmt:
		if st.X != nil {
			w.checkUses([]ast.Expr{st.X})
		}
		saved := w.tainted
		w.tainted = w.copy()
		w.block(st.Body.List)
		w.tainted = saved
	case *ast.SelectStmt:
		if st.Body == nil {
			return
		}
		for _, cc := range st.Body.List {
			cl, ok := cc.(*ast.CommClause)
			if !ok {
				continue
			}
			switch comm := cl.Comm.(type) {
			case *ast.ExprStmt:
				w.checkUses([]ast.Expr{comm.X})
			case *ast.AssignStmt:
				w.checkUses(comm.Rhs)
			}
			saved := w.tainted
			w.tainted = w.copy()
			w.block(cl.Body)
			w.tainted = saved
		}
	case *ast.BlockStmt:
		saved := w.tainted
		w.tainted = w.copy()
		w.block(st.List)
		w.tainted = saved
	case *ast.LabeledStmt:
		w.stmt(st.Stmt)
	}
}

// typeSwitch handles `switch v := m[K].(type)`. The switch itself
// always executes, so assignments its clauses make to outer variables
// escape it (possible-assignment semantics); the clause variable does
// not — it is out of scope afterwards.
func (w *walker) typeSwitch(st *ast.TypeSwitchStmt) {
	vn, key, isEx := w.typeSwitchExtract(st)
	if isEx {
		w.tainted[vn] = key
	}
	type clauseOut struct {
		assigned map[string]bool
		taint    map[string]string
	}
	var outs []clauseOut
	for _, cc := range st.Body.List {
		cl, ok := cc.(*ast.CaseClause)
		if !ok {
			continue
		}
		saved := w.tainted
		w.tainted = w.copy()
		w.block(cl.Body)
		outs = append(outs, clauseOut{assigned: assignedIdents(cl.Body), taint: w.tainted})
		w.tainted = saved
	}
	if isEx {
		delete(w.tainted, vn)
		for _, o := range outs {
			for id := range o.assigned {
				if k, ok := o.taint[id]; ok {
					w.tainted[id] = k
				}
			}
		}
	}
}

// typeSwitchExtract returns (variable, key, true) when the statement
// is `v := m[K].(type)` with a request-param-shaped K on a
// map[string]any-shaped m.
func (w *walker) typeSwitchExtract(ts *ast.TypeSwitchStmt) (string, string, bool) {
	if ts.Assign == nil {
		return "", "", false
	}
	as, ok := ts.Assign.(*ast.AssignStmt)
	if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 1 {
		return "", "", false
	}
	assert, ok := as.Rhs[0].(*ast.TypeAssertExpr)
	if !ok || assert.Type != nil {
		return "", "", false
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	if !ok {
		return "", "", false
	}
	key, ok := w.extractKey(assert.X)
	if !ok {
		return "", "", false
	}
	return id.Name, key, true
}

// extractKey reports the request-param key when x is m[K] with K a
// name-set string literal and m a map with string keys and an
// empty-interface element (the decoded-JSON params shape).
func (w *walker) extractKey(x ast.Expr) (string, bool) {
	idx, ok := x.(*ast.IndexExpr)
	if !ok {
		return "", false
	}
	lit, ok := idx.Index.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	key, err := strconv.Unquote(lit.Value)
	if err != nil || !nameRe.MatchString(key) {
		return "", false
	}
	mt, ok := w.pass.TypesInfo.TypeOf(idx.X).Underlying().(*types.Map)
	if !ok {
		return "", false
	}
	if b, ok := mt.Key().(*types.Basic); !ok || b.Info()&types.IsString == 0 {
		return "", false
	}
	itf, ok := mt.Elem().Underlying().(*types.Interface)
	if !ok || itf.NumMethods() != 0 {
		return "", false
	}
	return key, true
}

// rhsTaint reports whether the expressions carry taint (a tainted
// identifier or an inline extraction), and the sourcing key. A builtin
// min/max call anywhere in them counts as an expression clamp and
// wins over taint.
func (w *walker) rhsTaint(exprs []ast.Expr) (string, bool) {
	clamped, found := false, false
	var key string
	for _, e := range exprs {
		ast.Inspect(e, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			switch n2 := n.(type) {
			case *ast.Ident:
				if k, ok := w.tainted[n2.Name]; ok && !found {
					found, key = true, k
				}
			case *ast.CallExpr:
				if b, ok := typeutil.Callee(w.pass.TypesInfo, n2).(*types.Builtin); ok && (b.Name() == "min" || b.Name() == "max") {
					clamped = true
				}
			case *ast.TypeAssertExpr:
				if k, ok := w.extractKey(n2.X); ok && !found {
					found, key = true, k
				}
			}
			return true
		})
	}
	if clamped {
		return "", false
	}
	return key, found
}

// argTaint reports whether a call argument carries taint, and the
// sourcing key.
func (w *walker) argTaint(arg ast.Expr) (string, bool) {
	var key string
	found := false
	ast.Inspect(arg, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		switch e := n.(type) {
		case *ast.Ident:
			if k, ok := w.tainted[e.Name]; ok && !found {
				found, key = true, k
			}
		case *ast.TypeAssertExpr:
			if k, ok := w.extractKey(e.X); ok && !found {
				found, key = true, k
			}
		}
		return true
	})
	return key, found
}

// checkUses reports unclamped limit-shaped argument flows in every
// call inside the expressions. One diagnostic per call site.
func (w *walker) checkUses(exprs []ast.Expr) {
	for _, e := range exprs {
		ast.Inspect(e, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if call, ok := n.(*ast.CallExpr); ok {
				w.checkCall(call)
			}
			return true
		})
	}
}

const (
	calleeBuiltin = iota // conversion or builtin call: never a sink here
	calleeResolved
	calleeUnresolved
)

// callee classifies the callee of call and returns its signature and
// short name when resolved.
func (w *walker) callee(call *ast.CallExpr) (int, *types.Signature, string) {
	if tv, ok := w.pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
		// A conversion (int(x)) or instantiation, not a call.
		return calleeBuiltin, nil, ""
	}
	switch fn := typeutil.Callee(w.pass.TypesInfo, call).(type) {
	case *types.Builtin:
		return calleeBuiltin, nil, ""
	case *types.Func:
		if sig, ok := fn.Type().(*types.Signature); ok {
			return calleeResolved, sig, fn.Name()
		}
	}
	if tv, ok := w.pass.TypesInfo.Types[call.Fun]; ok && tv.Type != nil {
		if sig, ok := tv.Type.Underlying().(*types.Signature); ok {
			return calleeResolved, sig, ""
		}
	}
	return calleeUnresolved, nil, ""
}

// paramNameAt returns the callee parameter name at argument position
// i, folding variadic tails onto the last parameter.
func paramNameAt(sig *types.Signature, i int) string {
	params := sig.Params()
	n := params.Len()
	if n == 0 {
		return ""
	}
	if sig.Variadic() && i >= n-1 {
		i = n - 1
	}
	if i >= n {
		return ""
	}
	return params.At(i).Name()
}

func (w *walker) checkCall(call *ast.CallExpr) {
	if call.Ellipsis.IsValid() {
		return // spread argument has no single position
	}
	kind, sig, fnName := w.callee(call)
	if kind == calleeBuiltin {
		return
	}
	for i, arg := range call.Args {
		key, tainted := w.argTaint(arg)
		if !tainted {
			continue
		}
		if kind == calleeResolved {
			name := paramNameAt(sig, i)
			if nameRe.MatchString(name) {
				if fnName == "" {
					fnName = "callee"
				}
				w.pass.Reportf(arg.Pos(), "reqparamlimit: request-sourced params[%q] passed unclamped to %s's %q parameter; clamp it against a constant or max* bound before the call, or cap it with min/max", key, fnName, name)
				return
			}
		} else if i == len(call.Args)-1 {
			// Positional fallback, documented above: signature
			// unavailable, final argument treated as limit-shaped.
			w.pass.Reportf(arg.Pos(), "reqparamlimit: request-sourced params[%q] passed unclamped to the final parameter of an unresolved callee; clamp it against a constant or max* bound before the call", key)
			return
		}
	}
}

// clampIDs returns tainted variables compared against a constant
// literal or a max* identifier/field anywhere in cond.
func (w *walker) clampIDs(cond ast.Expr) []string {
	var cleared []string
	ast.Inspect(cond, func(n ast.Node) bool {
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		switch bin.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			return true
		}
		lx, rx := unwrapParen(bin.X), unwrapParen(bin.Y)
		if li, ok := lx.(*ast.Ident); ok && w.tainted[li.Name] != "" && isBound(rx) {
			cleared = append(cleared, li.Name)
		}
		if ri, ok := rx.(*ast.Ident); ok && w.tainted[ri.Name] != "" && isBound(lx) {
			cleared = append(cleared, ri.Name)
		}
		return true
	})
	return cleared
}

// isBound reports whether e is a clamp bound: a constant literal or a
// max*-prefixed identifier or field selector.
func isBound(e ast.Expr) bool {
	e = unwrapParen(e)
	if _, ok := e.(*ast.BasicLit); ok {
		return true
	}
	if id, ok := e.(*ast.Ident); ok {
		return maxRe.MatchString(id.Name)
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return maxRe.MatchString(sel.Sel.Name)
	}
	return false
}

// assignedIdents collects every identifier assigned anywhere in the
// statement list, regardless of path.
func assignedIdents(list []ast.Stmt) map[string]bool {
	m := map[string]bool{}
	for _, s := range list {
		ast.Inspect(s, func(n ast.Node) bool {
			if as, ok := n.(*ast.AssignStmt); ok {
				for _, l := range as.Lhs {
					if id, ok := unwrapParen(l).(*ast.Ident); ok && id.Name != "_" {
						m[id.Name] = true
					}
				}
			}
			return true
		})
	}
	return m
}

func unwrapParen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}
