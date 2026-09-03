// Package compositekey catches composite map keys built as
// `a + SEP + b` string concatenations.
//
// Bug class: a delimiter-joined string key is not injective over the pair
// it encodes. When either input can itself contain SEP, two distinct pairs
// collide on one entry, and a prefix scan (`strings.HasPrefix(key,
// owner + SEP)`) matches the rows of both. Found by the 2026-09-01
// adversarial probes TestStoreOwnerKeyDelimiterNoLeak (an owner named
// "alice\x00t1" listed and deleted alice's tasks through the a2a memory
// store, which keyed `tasks[owner + "\x00" + id]`) and
// TestIdemShardAmbiguousUnderNULPrincipal (the idempotency middleware
// sharded `storeKey := principal + "\x00" + key`, so ("a\x00b", "c") and
// ("a", "b\x00c") collapsed onto one claim and cross-poisoned
// fingerprints). Both were fixed in b79942f7: the store moved to struct
// keys, the shard to a length-prefixed encoding.

// The join is recognized through the indirections a real bug wears: a
// local or struct field bound from it, a one-result helper whose body
// reduces to returning it (statements that only rebind the helper's
// parameters in front of the return do not hide it), and the one-arg
// string-preserving normalizers around it at the sink
// (strings.ToLower/ToUpper/TrimSpace — the usual key normalizers).
//
// The rule is deliberately silent on:
//   - printable separators (":", ".", " ", "|", "-", "/"...): those key
//     legitimate, human-readable domain spaces (route "GET /x" keys,
//     dotted message ids, pipe-joined cache shards) all over this repo;
//     the bug class is the INVISIBLE delimiter, so only control
//     characters count.
//   - concatenations never used as a key: ambiguity nobody indexes on is
//     not a collision.
//   - fmt.Sprintf keys: interpolation belongs to the emitident /
//     GOFASTR1401 families, not this one.
//   - keyed-store arguments whose callee parameter name does not say
//     "key": the Store.Begin/Finish leg fires only when the parameter
//     name contains "key" (key, storeKey, cacheKey). A keyed store
//     behind an interface leaves no type-level trace that an argument
//     is used as a key — the parameter name is the only intent signal
//     available — so a store naming it claim/name/id stays silent by
//     declaration, not by oversight. A join that also reaches a map,
//     map-literal key, or prefix scan in this package is reported
//     regardless of the name.
//   - length-prefixed joins: any operand of the form strconv.Itoa(len(x))
//     or fmt.Sprint(len(x)) with x also an operand pins the boundary and
//     makes the join injective (the fixed idempotency shard).
package compositekey

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "gofastrcompositekey",
	Doc:  "forbids delimiter-joined concatenations (a + \"\\x00\" + b, single-char SEP) used as map keys, prefix scans, or keyed-store arguments; use a struct key or length-prefix the first field",
	Run:  run,
}

// maxDepth bounds binding and helper resolution: this is a gate, not a
// dataflow engine.
const maxDepth = 8

func run(pass *analysis.Pass) (any, error) {
	helpers := joinHelpers(pass)
	for _, f := range pass.Files {
		checkFile(pass, f, helpers)
	}
	return nil, nil
}

// joinHelpers collects package functions with exactly one result whose
// body reduces to a single `return <delimiter join>` — the taskKey/
// pushKey shape, where the join hides behind a helper call at the sink.
// Statements in front of the return are allowed while they only rebind
// the parameters: the guarded `if owner == "" { owner = "anon" }`
// prefix is the realistic spelling of such a helper.
func joinHelpers(pass *analysis.Pass) map[*types.Func]ast.Expr {
	out := map[*types.Func]ast.Expr{}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type.Results == nil {
				continue
			}
			if len(fd.Type.Results.List) != 1 || len(fd.Body.List) == 0 {
				continue
			}
			ret, ok := fd.Body.List[len(fd.Body.List)-1].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			if !rebindsOnly(pass, fd.Body.List[:len(fd.Body.List)-1], fn) {
				continue
			}
			if joinSeparator(pass, ret.Results[0], nil) != "" {
				out[fn] = ret.Results[0]
			}
		}
	}
	return out
}

// rebindsOnly reports whether every statement only rebinds parameters
// of fn: `=` assignments to the parameters, and ifs whose branches
// contain nothing but such assignments. A `:=` declares a NEW binding
// (never a parameter of fn) and disqualifies the helper.
func rebindsOnly(pass *analysis.Pass, stmts []ast.Stmt, fn *types.Func) bool {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	params := sig.Params()
	isParam := func(id *ast.Ident) bool {
		obj := pass.TypesInfo.ObjectOf(id)
		for i := range params.Len() {
			if params.At(i) == obj {
				return true
			}
		}
		return false
	}
	for _, st := range stmts {
		switch s := st.(type) {
		case *ast.AssignStmt:
			// token.ASSIGN only: a := declares a new binding, whose
			// object is never one of fn's parameters.
			if s.Tok != token.ASSIGN {
				return false
			}
			for _, lhs := range s.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || !isParam(id) {
					return false
				}
			}
		case *ast.IfStmt:
			if s.Init != nil {
				return false
			}
			branch := append([]ast.Stmt{}, s.Body.List...)
			if s.Else != nil {
				eb, ok := s.Else.(*ast.BlockStmt)
				if !ok {
					return false
				}
				branch = append(branch, eb.List...)
			}
			if !rebindsOnly(pass, branch, fn) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// checkFile reports every delimiter join that reaches a keyed sink. Each
// join construction is reported once, at the site it is built: the
// concatenation itself, the local bound from it, or the helper whose body
// is the join.
func checkFile(pass *analysis.Pass, f *ast.File, helpers map[*types.Func]ast.Expr) {
	bound := boundExprs(pass, f)
	type tag struct {
		obj types.Object
		pos token.Pos
	}
	reported := map[tag]bool{}

	report := func(x ast.Expr) {
		src := keyOrigin(pass, x, bound, helpers, 0)
		if src == nil {
			return
		}
		sep := joinSeparator(pass, src.join, bound)
		if src.obj != nil {
			t := tag{obj: src.obj}
			if reported[t] {
				return
			}
			reported[t] = true
			pass.Reportf(src.obj.Pos(), "%s joins parts with the %q separator into a key: a value containing it in one part makes two distinct pairs collide; use a struct key or a length-prefixed encoding", src.obj.Name(), sep)
			return
		}
		t := tag{pos: src.node.Pos()}
		if reported[t] {
			return
		}
		reported[t] = true
		pass.Reportf(src.node.Pos(), "this concatenation joins parts with the %q separator into a key: a value containing it in one part makes two distinct pairs collide; use a struct key or a length-prefixed encoding", sep)
	}

	isKey := func(x ast.Expr) bool {
		return keyOrigin(pass, x, bound, helpers, 0) != nil
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.IndexExpr:
			if _, isMap := mapUnderlying(pass, e.X); isMap && isKey(e.Index) {
				report(e.Index)
			}
		case *ast.CompositeLit:
			// Key position of a map composite literal.
			if _, isMap := mapUnderlying(pass, e); isMap {
				for _, elt := range e.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if isKey(kv.Key) {
						report(kv.Key)
					}
				}
			}
		case *ast.CallExpr:
			if qualifiedCallee(pass, e.Fun) == "strings.HasPrefix" && len(e.Args) == 2 && isKey(e.Args[1]) {
				report(e.Args[1])
				return true
			}
			// An argument sitting in a parameter whose name says it is a
			// key (key, storeKey, cacheKey...): the idempotency
			// Store.Begin / Finish shape, where the join feeds a keyed
			// store behind an interface rather than a map in this file.
			if fn, ok := calleeFunc(pass, e.Fun); ok {
				if sig, ok := fn.Type().(*types.Signature); ok {
					params := sig.Params()
					for i, arg := range e.Args {
						if i >= params.Len() {
							break
						}
						if strings.Contains(strings.ToLower(params.At(i).Name()), "key") && isKey(arg) {
							report(arg)
						}
					}
				}
			}
		}
		return true
	})
}

// keySource is one way a delimiter join reaches a sink: the concatenation
// expression itself, a local variable bound from it, or a one-result
// helper whose body is the join. obj names the thing to report (local or
// helper); join is the flattest join expression found.
type keySource struct {
	obj  types.Object
	join ast.Expr
	node ast.Node
}

func keyOrigin(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr, helpers map[*types.Func]ast.Expr, depth int) *keySource {
	if depth > maxDepth {
		return nil
	}
	switch e := x.(type) {
	case *ast.ParenExpr:
		return keyOrigin(pass, e.X, bound, helpers, depth+1)
	case *ast.BinaryExpr:
		if joinSeparator(pass, e, bound) != "" {
			return &keySource{join: e, node: e}
		}
		return nil
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(e)
		if obj == nil {
			return nil
		}
		if src, ok := bound[obj]; ok {
			if o := keyOrigin(pass, src, bound, helpers, depth+1); o != nil {
				if o.obj != nil {
					return o // a helper or upstream local is the origin
				}
				return &keySource{obj: obj, join: o.join, node: src}
			}
		}
		return nil
	case *ast.SelectorExpr:
		// A join parked on a struct field (`h.k = a + SEP + b`) and
		// keyed later: boundExprs binds the field object.
		obj := pass.TypesInfo.ObjectOf(e.Sel)
		if obj == nil {
			return nil
		}
		if src, ok := bound[obj]; ok {
			if o := keyOrigin(pass, src, bound, helpers, depth+1); o != nil {
				if o.obj != nil {
					return o // a helper or upstream binding is the origin
				}
				return &keySource{obj: obj, join: o.join, node: src}
			}
		}
		return nil
	case *ast.CallExpr:
		if fn, ok := calleeFunc(pass, e.Fun); ok {
			if join, ok := helpers[fn]; ok {
				return &keySource{obj: fn, join: join, node: e}
			}
		}
		// Peel the one-argument string-preserving normalizers — the
		// most common key wrapper is strings.ToLower around the join.
		switch qualifiedCallee(pass, e.Fun) {
		case "strings.ToLower", "strings.ToUpper", "strings.TrimSpace":
			if len(e.Args) == 1 {
				return keyOrigin(pass, e.Args[0], bound, helpers, depth+1)
			}
		}
		return nil
	}
	return nil
}

// joinSeparator reports the control-character separator constant of a
// '+' chain, or "" when the expression is not a delimiter join in this
// rule's sense. Only control characters (NUL, newline, tab, DEL...) count:
// those are the invisible delimiters chosen on the assumption "this can
// never appear in a part", which is exactly the assumption the probes
// broke. Printable separators (":", ".", " ", "|", "-", "/"...) key
// legitimate, human-readable domain spaces all over this repo and stay
// silent. Also disqualifies joins with no non-constant operand (a
// fixed-string key has nothing to smuggle), non-string results, and
// length-prefixed joins.
func joinSeparator(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr) string {
	var ops []ast.Expr
	if !flattenJoin(x, &ops) {
		return ""
	}
	var sep string
	nonConst := 0
	for _, op := range ops {
		if cv, ok := stringConstantResolving(pass, op, bound, 0); ok {
			if len(cv) == 1 && (cv[0] < 0x20 || cv[0] == 0x7f) {
				sep = cv
			}
			continue
		}
		nonConst++
	}
	if sep == "" || nonConst == 0 {
		return ""
	}
	if tv, ok := pass.TypesInfo.Types[x]; ok && tv.Type != nil {
		if b, ok := tv.Type.Underlying().(*types.Basic); !ok || b.Info()&types.IsString == 0 {
			return ""
		}
	}
	if lengthPrefixed(pass, ops) {
		return ""
	}
	return sep
}

// flattenJoin collects the operands of a paren-free '+' chain.
func flattenJoin(x ast.Expr, ops *[]ast.Expr) bool {
	switch e := x.(type) {
	case *ast.ParenExpr:
		return flattenJoin(e.X, ops)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return false
		}
		return flattenJoin(e.X, ops) && flattenJoin(e.Y, ops)
	default:
		*ops = append(*ops, x)
		return true
	}
}

// lengthPrefixed reports whether any operand is strconv.Itoa(len(x)) or
// fmt.Sprint(len(x)) with x also an operand: the join then carries its
// own boundary and is injective.
func lengthPrefixed(pass *analysis.Pass, ops []ast.Expr) bool {
	for _, op := range ops {
		call, ok := op.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		switch qualifiedCallee(pass, call.Fun) {
		case "strconv.Itoa", "fmt.Sprint":
			lenArg, ok := call.Args[0].(*ast.CallExpr)
			if !ok || qualifiedCallee(pass, lenArg.Fun) != "len" || len(lenArg.Args) != 1 {
				continue
			}
			for _, other := range ops {
				if exprPrintsSame(lenArg.Args[0], other) {
					return true
				}
			}
		}
	}
	return false
}

// exprPrintsSame is a syntactic same-expression check (normalizing
// parentheses), enough for matching a len() argument against an operand.
func exprPrintsSame(a, b ast.Expr) bool {
	if p, ok := a.(*ast.ParenExpr); ok {
		return exprPrintsSame(p.X, b)
	}
	if p, ok := b.(*ast.ParenExpr); ok {
		return exprPrintsSame(a, p.X)
	}
	switch av := a.(type) {
	case *ast.Ident:
		bv, ok := b.(*ast.Ident)
		return ok && av.Name == bv.Name
	case *ast.SelectorExpr:
		bv, ok := b.(*ast.SelectorExpr)
		return ok && exprPrintsSame(av.X, bv.X) && av.Sel.Name == bv.Sel.Name
	}
	return false
}

// stringConstant resolves a basic literal or named constant to its string
// value.
func stringConstant(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// stringConstantResolving resolves a literal, named constant, or local
// bound to one (transitively) to its constant string value: a local
// holding a fixed string is a fixed string.
func stringConstantResolving(pass *analysis.Pass, e ast.Expr, bound map[types.Object]ast.Expr, depth int) (string, bool) {
	if depth > 4 {
		return "", false
	}
	if s, ok := stringConstant(pass, e); ok {
		return s, true
	}
	if id, ok := unparen(e).(*ast.Ident); ok {
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			if src, ok := bound[obj]; ok {
				return stringConstantResolving(pass, src, bound, depth+1)
			}
		}
	}
	return "", false
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func mapUnderlying(pass *analysis.Pass, x ast.Expr) (*types.Map, bool) {
	tv, ok := pass.TypesInfo.Types[x]
	if !ok || tv.Type == nil {
		return nil, false
	}
	m, ok := tv.Type.Underlying().(*types.Map)
	return m, ok
}

func boundExprs(pass *analysis.Pass, f *ast.File) map[types.Object]ast.Expr {
	m := map[types.Object]ast.Expr{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch st := n.(type) {
		case *ast.AssignStmt:
			if st.Tok != token.ASSIGN && st.Tok != token.DEFINE {
				return true
			}
			if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
				return true
			}
			switch lhs := st.Lhs[0].(type) {
			case *ast.Ident:
				if lhs.Name != "_" {
					if obj := pass.TypesInfo.ObjectOf(lhs); obj != nil {
						m[obj] = st.Rhs[0]
					}
				}
			case *ast.SelectorExpr:
				// `h.k = a + SEP + b`: bind the field's object, so a join
				// parked on a struct field and keyed later is recognized.
				if obj := pass.TypesInfo.ObjectOf(lhs.Sel); obj != nil {
					m[obj] = st.Rhs[0]
				}
			}
		case *ast.ValueSpec:
			if len(st.Names) != 1 || len(st.Values) != 1 || st.Names[0].Name == "_" {
				return true
			}
			if obj := pass.TypesInfo.ObjectOf(st.Names[0]); obj != nil {
				m[obj] = st.Values[0]
			}
		}
		return true
	})
	return m
}

func calleeFunc(pass *analysis.Pass, fun ast.Expr) (*types.Func, bool) {
	switch f := fun.(type) {
	case *ast.Ident:
		fn, ok := pass.TypesInfo.Uses[f].(*types.Func)
		return fn, ok
	case *ast.SelectorExpr:
		fn, ok := pass.TypesInfo.Uses[f.Sel].(*types.Func)
		return fn, ok
	}
	return nil, false
}

// qualifiedCallee renders a call target as "pkg.Func" through the type
// checker, so an aliased import still resolves to the real package.
func qualifiedCallee(pass *analysis.Pass, fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			if pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName); ok {
				return pkg.Imported().Name() + "." + f.Sel.Name
			}
		}
	case *ast.Ident:
		if b, ok := pass.TypesInfo.Uses[f].(*types.Builtin); ok {
			return b.Name()
		}
		if fn, ok := pass.TypesInfo.Uses[f].(*types.Func); ok && fn.Pkg() != nil {
			return fn.Pkg().Name() + "." + fn.Name()
		}
	}
	return ""
}
