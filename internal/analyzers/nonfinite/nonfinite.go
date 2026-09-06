// Package nonfinite catches a strconv.ParseFloat result stored or
// returned without a NaN/Inf gate.
//
// The bug: ParseFloat accepts "NaN", "Inf", "+Infinity" — case-
// insensitively, with a nil error — so a non-finite value slips every
// `< min || > max` guard (IEEE-754: every NaN comparison is false).
// Probe TestLoadFloatRejectsNonFinite (2026-09-05 red round):
// core/config config.go setField bound `RATE=NaN` straight into a
// float config field with `v.SetFloat(f)`; every min/max guard on
// that config was bypassable by one environment variable. Site:
// core/config/config.go:315.
//
// Shape — a strconv.ParseFloat call in a non-test function whose
// first result, captured into a variable, reaches a STORE sink or a
// bare return:
//   - a store: a struct field or map/slice element assignment, a
//     field of a composite literal, or a Set*-named method call
//     carrying the value (reflect's SetFloat, a setter) — the value
//     leaves the function as state;
//   - a bare return: `return f` where the function's results carry
//     the float with no error/bool companion to gate it. A
//     `(float64, error)` or `(float64, bool)` result pair is its own
//     gate and stays quiet.
//
// Silent postures, deliberately:
//   - math.IsNaN(f) / math.IsInf(f, ·) on the value anywhere in the
//     enclosing function, or a self-inequality `f != f` (the
//     spelling kiln/expr toInt uses) — the fix posture
//     (core/schema validateFloat:143, validateDecimal:184);
//   - the value passed to a same-package validator-named helper
//     (valid*/check*/sanit*/verif*/finite*/represent*/bound*/reject*/
//     assert*/guard*) whose body carries an IsNaN/IsInf check — the
//     negdur IssueToken → validateTokenSpec posture;
//   - a grammar gate on the parsed STRING in the ParseFloat's own if
//     condition or a dominating one: a regexp MatchString or
//     strings.Contains-family test naming the operand (core/yaml:
//     `err == nil && strings.ContainsAny(raw, ".eE")` — "NaN" carries
//     none of those runes and can never reach the parse);
//   - a compile-time constant operand: the value is visible to the
//     compiler and to review;
//   - results that only feed comparisons, arithmetic, or formatting —
//     they never become state, and NaN there is loudly wrong rather
//     than quietly stored (i18n's Accept-Language q, resource.go's
//     money sum);
//   - a non-parameter-rooted operand (a receiver field, a slice of
//     the receiver's buffer, an AST literal's text): scanner-fed
//     grammars that cannot spell NaN (core/webbotauth sfv.go's digit
//     scanner, core/jcs, cmd/gofastr's literal classifier) — the
//     negdur convention: receiver fields are developer data, not
//     caller input;
//   - a slice-append aggregation returned alongside the error
//     (battery/semantic parseVector): the parse's own error gate
//     covers the value's domain and the store shape is a list, not a
//     scalar config value;
//   - _test.go files.
package nonfinite

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "nonfinite",
	Doc:  "forbids storing or returning a strconv.ParseFloat result with no IsNaN/IsInf gate; RATE=NaN parses fine and slips every min/max bound",
	Run:  run,
}

func run(pass *analysis.Pass) (any, error) {
	pkgFuncs := map[string][]*ast.FuncDecl{}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				pkgFuncs[fn.Name.Name] = append(pkgFuncs[fn.Name.Name], fn)
			}
		}
	}
	for _, f := range pass.Files {
		if isTestFile(pass, f) {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkFunc(pass, fn, pkgFuncs)
		}
	}
	return nil, nil
}

func isTestFile(pass *analysis.Pass, f *ast.File) bool {
	return strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go")
}

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl, pkgFuncs map[string][]*ast.FuncDecl) {
	params := map[types.Object]bool{}
	if fn.Type.Params != nil {
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
					params[obj] = true
				}
			}
		}
	}
	// object → the expressions assigned to it in this function
	assigns := map[types.Object][]ast.Expr{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if st, ok := n.(*ast.AssignStmt); ok {
			for i, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && i < len(st.Rhs) {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						assigns[obj] = append(assigns[obj], st.Rhs[i])
					}
				}
			}
		}
		if rs, ok := n.(*ast.RangeStmt); ok {
			// range variables root to the ranged expression
			for _, id := range []*ast.Ident{rangeIdent(rs.Key), rangeIdent(rs.Value)} {
				if id == nil || id.Name == "_" {
					continue
				}
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					assigns[obj] = append(assigns[obj], rs.X)
				}
			}
		}
		return true
	})
	parents := astParents(fn.Body)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Rhs) != 1 || len(st.Lhs) < 1 {
			return true
		}
		call, ok := st.Rhs[0].(*ast.CallExpr)
		if !ok || qualifiedFunc(pass, call.Fun) != "strconv.ParseFloat" || len(call.Args) != 2 {
			return true
		}
		if tv, ok := pass.TypesInfo.Types[call.Args[0]]; ok && tv.Value != nil {
			return true // a compile-time constant operand
		}
		val, ok := st.Lhs[0].(*ast.Ident)
		if !ok || val.Name == "_" {
			return true
		}
		obj := pass.TypesInfo.ObjectOf(val)
		if obj == nil {
			return true
		}
		if !paramRooted(pass, call.Args[0], params, assigns, 0) {
			return true // scanner/receiver-fed grammar: see the doc comment
		}
		if grammarGated(pass, call, parents) {
			return true // the string's charset cannot spell NaN/Inf
		}
		if checkedInFunc(pass, obj, fn.Body) || validatorHop(pass, obj, fn.Body, pkgFuncs) {
			return true
		}
		if !reachesStoreOrBareReturn(pass, obj, fn) {
			return true
		}
		pass.Reportf(call.Pos(),
			"strconv.ParseFloat result stored with no math.IsNaN/math.IsInf gate: %q parses to NaN with a nil error and slips every < min || > max bound (RATE=NaN, probe TestLoadFloatRejectsNonFinite) — reject non-finite like core/schema validateFloat, or gate the string's grammar first",
			renderOperand(call.Args[0]))
		return true
	})
}

// paramRooted reports whether e is, or resolves through plain local
// assignments, string-wrapper calls (strings.TrimSpace/Split/Join…),
// selectors, and indexes to one of the function's parameters.
// Receiver-field chains do not root: the negdur convention —
// developer data, not caller input.
func paramRooted(pass *analysis.Pass, e ast.Expr, params map[types.Object]bool, assigns map[types.Object][]ast.Expr, depth int) bool {
	if depth > 6 {
		return false
	}
	switch x := e.(type) {
	case *ast.Ident:
		if params[pass.TypesInfo.ObjectOf(x)] {
			return true
		}
		obj := pass.TypesInfo.ObjectOf(x)
		if obj == nil {
			return false
		}
		for _, rhs := range assigns[obj] {
			if paramRooted(pass, rhs, params, assigns, depth+1) {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		switch qualifiedFunc(pass, x.Fun) {
		case "strings.TrimSpace", "strings.TrimSuffix", "strings.TrimPrefix", "strings.Trim", "strings.TrimLeft", "strings.TrimRight", "strings.Join", "strings.ToUpper", "strings.ToLower", "strings.ReplaceAll", "fmt.Sprint", "fmt.Sprintf":
			for _, a := range x.Args {
				if paramRooted(pass, a, params, assigns, depth+1) {
					return true
				}
			}
		}
		return false
	case *ast.IndexExpr:
		return paramRooted(pass, x.X, params, assigns, depth+1)
	case *ast.SelectorExpr:
		return paramRooted(pass, x.X, params, assigns, depth+1)
	case *ast.SliceExpr:
		return paramRooted(pass, x.X, params, assigns, depth+1)
	case *ast.BinaryExpr:
		return paramRooted(pass, x.X, params, assigns, depth+1) ||
			paramRooted(pass, x.Y, params, assigns, depth+1)
	case *ast.ParenExpr:
		return paramRooted(pass, x.X, params, assigns, depth+1)
	}
	return false
}

// checkedInFunc reports whether the function mentions math.IsNaN(v) /
// math.IsInf(v, ·) on the parsed variable, or the self-inequality NaN
// spelling v != v.
func checkedInFunc(pass *analysis.Pass, obj types.Object, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			qf := qualifiedFunc(pass, x.Fun)
			if qf == "math.IsNaN" || qf == "math.IsInf" {
				for _, a := range x.Args {
					if isObj(pass, a, obj) {
						found = true
					}
				}
			}
		case *ast.BinaryExpr:
			if (x.Op == token.NEQ || x.Op == token.EQL) &&
				isObj(pass, x.X, obj) && isObj(pass, x.Y, obj) {
				found = true // v != v: the NaN test
			}
		}
		return !found
	})
	return found
}

// validatorHop reports whether the parsed variable is passed as an
// argument to a same-package validator-named helper whose body carries
// an IsNaN/IsInf check.
func validatorHop(pass *analysis.Pass, obj types.Object, body *ast.BlockStmt, pkgFuncs map[string][]*ast.FuncDecl) bool {
	hop := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok || !validatorName.MatchString(id.Name) {
			return true
		}
		mentions := false
		for _, a := range call.Args {
			if isObj(pass, a, obj) {
				mentions = true
			}
		}
		if !mentions {
			return true
		}
		for _, decl := range pkgFuncs[id.Name] {
			if bodyChecksFinite(pass, decl.Body) {
				hop = true
			}
		}
		return !hop
	})
	return hop
}

var validatorName = regexp.MustCompile(`(?i)^(?:valid|check|sanit|verif|finite|represent|bound|reject|assert|guard|isfinite|parse)`)

func bodyChecksFinite(pass *analysis.Pass, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			qf := qualifiedFunc(pass, call.Fun)
			if qf == "math.IsNaN" || qf == "math.IsInf" {
				found = true
			}
		}
		return !found
	})
	return found
}

// grammarGated reports whether the ParseFloat call sits in a condition
// that also tests the parsed string's charset: a regexp MatchString or
// strings.Contains-family call naming the operand. core/yaml's
// `err == nil && strings.ContainsAny(raw, ".eE")` is the model: "NaN"
// carries none of those runes, so the parse is unreachable for it.
func grammarGated(pass *analysis.Pass, call *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	var conds []ast.Expr
	for node := ast.Node(call); node != nil && len(conds) < 4; node = parents[node] {
		if iff, ok := node.(*ast.IfStmt); ok {
			conds = append(conds, iff.Cond)
		}
	}
	for _, cond := range conds {
		for _, leaf := range flattenBool(cond) {
			if charsetTest(pass, leaf.X, call) || charsetTest(pass, leaf.Y, call) {
				return true
			}
		}
	}
	return false
}

// charsetTest reports whether e is a regexp MatchString or
// strings.Contains/ContainsAny/ContainsRune call naming the ParseFloat
// operand.
func charsetTest(pass *analysis.Pass, e ast.Expr, pf *ast.CallExpr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch qualifiedFunc(pass, call.Fun) {
	case "regexp.MatchString", "strings.Contains", "strings.ContainsAny", "strings.ContainsRune":
	default:
		return false
	}
	for _, a := range call.Args {
		if sameOperand(a, pf.Args[0]) {
			return true
		}
	}
	return false
}

func sameOperand(a, b ast.Expr) bool {
	return types.ExprString(a) == types.ExprString(b)
}

// flattenBool reduces a boolean expression to its comparison leaves.
func flattenBool(e ast.Expr) []*ast.BinaryExpr {
	var out []*ast.BinaryExpr
	var walk func(ast.Expr)
	walk = func(x ast.Expr) {
		switch v := x.(type) {
		case *ast.ParenExpr:
			walk(v.X)
		case *ast.UnaryExpr:
			walk(v.X)
		case *ast.BinaryExpr:
			if v.Op == token.LAND || v.Op == token.LOR {
				walk(v.X)
				walk(v.Y)
				return
			}
			out = append(out, v)
		}
	}
	walk(e)
	return out
}

// reachesStoreOrBareReturn reports whether the parsed variable is
// stored into state or returned bare from the function.
func reachesStoreOrBareReturn(pass *analysis.Pass, obj types.Object, fn *ast.FuncDecl) bool {
	gatedResult := false // an error or bool companion rides along
	if fn.Type.Results != nil {
		for _, field := range fn.Type.Results.List {
			t := pass.TypesInfo.TypeOf(field.Type)
			if t == nil {
				continue
			}
			if named, ok := t.(*types.Named); ok && named.Obj().Name() == "error" && named.Obj().Pkg() == nil {
				gatedResult = true
			}
			if ifc, ok := t.Underlying().(*types.Interface); ok && ifc.NumMethods() == 1 && ifc.Method(0).Name() == "Error" {
				gatedResult = true
			}
			if b, ok := t.(*types.Basic); ok && b.Kind() == types.Bool {
				gatedResult = true
			}
		}
	}
	stored := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				// obj on the LEFT into a field/index is state; a plain
				// local rebind is not.
				if isObj(pass, lhs, obj) {
					if _, isIdent := lhs.(*ast.Ident); !isIdent {
						stored = true
						return false
					}
				}
				// obj on the RIGHT into a field/map/slice element
				if _, isIdent := lhs.(*ast.Ident); !isIdent && i < len(x.Rhs) && isObj(pass, x.Rhs[i], obj) {
					stored = true
					return false
				}
			}
			for _, rhs := range x.Rhs {
				if lit, ok := rhs.(*ast.CompositeLit); ok && literalNames(pass, lit, obj) {
					stored = true
					return false
				}
			}
		case *ast.CompositeLit:
			if literalNames(pass, x, obj) {
				stored = true
				return false
			}
		case *ast.ReturnStmt:
			for _, r := range x.Results {
				if isObj(pass, r, obj) && !gatedResult {
					stored = true // a bare float return: no gate rides along
					return false
				}
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(sel.Sel.Name, "Set") {
				for _, a := range x.Args {
					if isObj(pass, a, obj) {
						stored = true // SetFloat & friends: the value leaves as state
						return false
					}
				}
			}
		}
		return !stored
	})
	return stored
}

func literalNames(pass *analysis.Pass, lit *ast.CompositeLit, obj types.Object) bool {
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok && isObj(pass, kv.Value, obj) {
			return true
		}
	}
	return false
}

func isObj(pass *analysis.Pass, e ast.Expr, obj types.Object) bool {
	id, ok := e.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(id) == obj
}

func qualifiedFunc(pass *analysis.Pass, e ast.Expr) string {
	switch x := e.(type) {
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				if _, ok := obj.(*types.PkgName); ok {
					return id.Name + "." + x.Sel.Name
				}
			}
		}
	}
	return ""
}

func renderOperand(e ast.Expr) string {
	s := types.ExprString(e)
	if len(s) > 24 {
		s = s[:21] + "..."
	}
	return s
}

// astParents maps every node in body to its parent node.
func astParents(body ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var walk func(n ast.Node)
	walk = func(n ast.Node) {
		for _, child := range childNodes(n) {
			parents[child] = n
			walk(child)
		}
	}
	walk(body)
	return parents
}

// childNodes returns n's direct AST children (nil-free).
func childNodes(n ast.Node) []ast.Node {
	var out []ast.Node
	ast.Inspect(n, func(child ast.Node) bool {
		if child == nil || child == n {
			return true
		}
		out = append(out, child)
		return false
	})
	return out
}

func rangeIdent(e ast.Expr) *ast.Ident {
	if id, ok := e.(*ast.Ident); ok {
		return id
	}
	return nil
}
