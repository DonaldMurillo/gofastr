// Package optzero catches a numeric option that lets 0 (or a negative)
// replace a nonzero default nobody downstream re-checks.
//
// The bug class: a struct field carries a sensible nonzero default
// (a five-minute lease, ten attempts, a 32-slot buffer), and an option
// setter or a zero-means-default branch hands the field a caller value
// with no sign check. A zero lease made every DBQueue claim abandoned
// the moment it was written (a6a0d9cff: WithLeaseTimeout /
// SetLeaseTimeout), a zero Redis visibility window re-delivered a job
// its worker had just claimed (6a3a5b663: SetVisibilityTimeout, an
// atomic .Store(int64(d))), a zero MemoryQueue handler budget timed
// every handler out at once (d32b90af6: WithHandlerTimeout), and a zero
// outbox poll interval, batch size or attempt budget spun the relay,
// claimed nothing, or dead-lettered on the first try (e76a69ecb:
// WithPollInterval / WithBatchSize / WithMaxAttempts). The
// zero-means-default branch has the twin: `if sendBuf == 0 { sendBuf =
// 32 }` let a negative WSConfig.SendBuffer through to make(chan) after
// the hijack, which panicked and leaked the socket (df4727141).
//
// Two arms, both off in _test.go files:
//
//   - SETTER: an exported With*/Set* function, method, or package-level
//     function literal (var WithX = func(...)) that stores one
//     of its numeric parameters (time.Duration or any integer kind,
//     through conversions) into a struct field — `o.f = d`, or
//     `o.f.Store(int64(d))` on a sync/atomic field — when no ordered
//     comparison of the parameter GATES the store (the store sits in
//     the comparison's branch, or the branch refuses before it) and
//     the function never passes it to a call other than a conversion
//     or that Store. Gated to fields that (1) get a nonzero default
//     somewhere in the package (a composite-literal
//     key, a plain assignment or a Store of a nonzero constant or a
//     default-named value outside the setter), and (2) are never
//     compared to 0 or 1 anywhere in the package, directly, through
//     .Load(), or through a local copied from the field: a use-site
//     `<= 0` / `> 0` guard (framework/export_data.go pageSize, the acp
//     maxTurns, cache cleanupInterval) means zero is handled where the
//     value is read, and the setter's pass-through is fine.
//
//   - ZERO DEFAULT: `if x == 0 { x = <default> }` on a signed numeric
//     local, parameter or field, in a function that makes no ordered
//     comparison of x (`<`, `<=`, `>`, `>=`) and clamps it with no
//     max/min BEFORE the sink, when x then reaches a sink a negative
//     breaks: make(), a time.NewTicker / NewTimer / Tick / AfterFunc,
//     context.WithTimeout, a time.Time.Add (a lease or expiry), or a
//     call whose constant SQL argument holds LIMIT. Reported at the
//     if — a check after the sink guards nothing on the path that
//     reached it.
//
// Silent postures, deliberately:
//   - an ordered comparison of the setter parameter where it GATES the
//     store (`if d > 0 { o.f = d }`, the fix spelling; `if n <= 0 {
//     panic(...) }`, the refusal) and any non-conversion call taking
//     it (a validator, max(d, floor)); an `==` comparison never gates
//     (it refuses zero alone; a negative still replaces the default),
//     and neither does one that only logs;
//   - fields with no nonzero default in the package (zero IS the
//     default; storing it changes nothing) and fields compared to 0/1
//     at a use site (see above);
//   - unexported setters: the caller is the package itself;
//   - unsigned subjects in the zero-default arm (no negative to slip
//     through), and an x == 0 branch whose subject reaches no sink, or
//     that the function also sign-checks;
//   - non-numeric parameters and fields (strings, funcs, bools), and
//     named integer types other than time.Duration: os.FileMode and
//     enum levels are values, where zero is not a missing count
//     (battery/storage WithPermissions);
//   - a zero-default local whose SOURCE the function sign-checks
//     (`if inv.Timeout < 0 { return err }; timeout := inv.Timeout;
//     if timeout == 0 { ... }`, the evals codex runner).
//
// The fix spelling keeps the default on a non-positive value:
// `if d > 0 { o.f = d }`, or refuses it; the zero-default twin
// widens its test to `x <= 0`.
package optzero

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "optzero",
	Doc:  "forbids an exported With*/Set* option storing a numeric parameter unchecked into a field with a nonzero default nobody re-checks, and `if x == 0 { x = default }` with no negative check before make/timer/WithTimeout/Add/LIMIT; keep the default on d <= 0 (if d > 0 { o.f = d }) or refuse",
	Run:  run,
}

var defaultNameRe = regexp.MustCompile(`(?i)default`)

func run(pass *analysis.Pass) (any, error) {
	var files []*ast.File
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		files = append(files, f)
	}
	setters := map[*ast.FuncDecl]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil && isSetterName(fn.Name.Name) {
				setters[fn] = true
			}
		}
	}
	defaults := fieldDefaults(pass, files, setters)
	handled := fieldsComparedToZero(pass, files)

	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			if setters[fn] {
				checkSetter(pass, fn.Name.Name, fn.Type, fn.Body, defaults, handled)
			}
			checkZeroDefault(pass, fn.Body)
		}
	}
	// Package-level function literals (var WithX = func(...)): the
	// same two arms, with the variable's name standing in for the
	// declaration's.
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						break
					}
					lit, ok := ast.Unparen(vs.Values[i]).(*ast.FuncLit)
					if !ok || lit.Body == nil {
						continue
					}
					if isSetterName(name.Name) {
						checkSetter(pass, name.Name, lit.Type, lit.Body, defaults, handled)
					}
					checkZeroDefault(pass, lit.Body)
				}
			}
		}
	}
	return nil, nil
}

// isSetterName: an exported With*/Set* name with the prefix followed by
// an upper-case letter (WithTimeout, SetLease; not Without, Settle).
func isSetterName(name string) bool {
	for _, p := range []string{"With", "Set"} {
		rest, ok := strings.CutPrefix(name, p)
		if ok && rest != "" && unicode.IsUpper(rune(rest[0])) {
			return true
		}
	}
	return false
}

// isNumeric: time.Duration or an unnamed integer kind (int, int64,
// uint32, ...). Other named integer types are bit sets and enums
// (os.FileMode, a Level): a zero mode is a value, not a missing count.
func isNumeric(t types.Type) bool {
	if t == nil {
		return false
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		obj := n.Obj()
		if obj.Pkg() == nil || obj.Pkg().Path() != "time" || obj.Name() != "Duration" {
			return false
		}
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0
}

func isSigned(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&types.IsUnsigned == 0
}

// unconv strips parentheses and type conversions: int64(d), Duration(n).
func unconv(pass *analysis.Pass, e ast.Expr) ast.Expr {
	for {
		e = ast.Unparen(e)
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return e
		}
		if tv, ok := pass.TypesInfo.Types[call.Fun]; !ok || !tv.IsType() {
			return e
		}
		e = call.Args[0]
	}
}

// fieldOf returns the struct field a selector names, or nil.
func fieldOf(pass *analysis.Pass, e ast.Expr) *types.Var {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	s, ok := pass.TypesInfo.Selections[sel]
	if !ok || s.Kind() != types.FieldVal {
		return nil
	}
	v, _ := s.Obj().(*types.Var)
	return v
}

// storeCall matches `x.f.Store(arg)` and returns the field and arg.
func storeCall(pass *analysis.Pass, call *ast.CallExpr) (*types.Var, ast.Expr) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Store" || len(call.Args) != 1 {
		return nil, nil
	}
	f := fieldOf(pass, sel.X)
	if f == nil || !isAtomic(f.Type()) {
		return nil, nil
	}
	return f, call.Args[0]
}

func isAtomic(t types.Type) bool {
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "sync/atomic"
}

// isDefaultValue: a nonzero numeric constant, or a value whose name says
// it is the default (defaultLease, DefaultTimeout, cfg.DefaultTTL).
func isDefaultValue(pass *analysis.Pass, e ast.Expr) bool {
	e = unconv(pass, e)
	if tv, ok := pass.TypesInfo.Types[e]; ok && tv.Value != nil {
		return tv.Value.Kind() == constant.Int && constant.Sign(tv.Value) != 0
	}
	switch x := e.(type) {
	case *ast.Ident:
		return defaultNameRe.MatchString(x.Name)
	case *ast.SelectorExpr:
		return defaultNameRe.MatchString(x.Sel.Name)
	}
	return false
}

// fieldDefaults collects the numeric fields that get a nonzero default
// in the package outside a setter: a composite-literal key, a plain
// assignment, or an atomic Store.
func fieldDefaults(pass *analysis.Pass, files []*ast.File, setters map[*ast.FuncDecl]bool) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && setters[fn] {
				continue
			}
			ast.Inspect(d, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.KeyValueExpr:
					id, ok := x.Key.(*ast.Ident)
					if !ok {
						return true
					}
					if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok && v.IsField() && isNumeric(v.Type()) && isDefaultValue(pass, x.Value) {
						out[v] = true
					}
				case *ast.AssignStmt:
					if x.Tok != token.ASSIGN || len(x.Lhs) != len(x.Rhs) {
						return true
					}
					for i, l := range x.Lhs {
						if v := fieldOf(pass, l); v != nil && isNumeric(v.Type()) && isDefaultValue(pass, x.Rhs[i]) {
							out[v] = true
						}
					}
				case *ast.CallExpr:
					if v, arg := storeCall(pass, x); v != nil && isDefaultValue(pass, arg) {
						out[v] = true
					}
				}
				return true
			})
		}
	}
	return out
}

// fieldsComparedToZero collects the fields some use site compares to 0
// or 1 (directly, through .Load(), through a conversion, or through a
// local copied from the field), or clamps with max/min: zero is handled
// where the value is read.
func fieldsComparedToZero(pass *analysis.Pass, files []*ast.File) map[*types.Var]bool {
	out := map[*types.Var]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			// locals copied from a field: d := q.lease, n := q.n.Load()
			copies := map[types.Object]*types.Var{}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != len(as.Rhs) {
					return true
				}
				for i, l := range as.Lhs {
					id, ok := l.(*ast.Ident)
					if !ok {
						continue
					}
					if v := fieldRead(pass, as.Rhs[i]); v != nil {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							copies[obj] = v
						}
					}
				}
				return true
			})
			resolve := func(e ast.Expr) *types.Var {
				if v := fieldRead(pass, e); v != nil {
					return v
				}
				if id, ok := unconv(pass, e).(*ast.Ident); ok {
					return copies[pass.TypesInfo.ObjectOf(id)]
				}
				return nil
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BinaryExpr:
					if !isComparison(x.Op) {
						return true
					}
					if v := resolve(x.X); v != nil && isZeroOrOne(pass, x.Y) {
						out[v] = true
					}
					if v := resolve(x.Y); v != nil && isZeroOrOne(pass, x.X) {
						out[v] = true
					}
				case *ast.CallExpr:
					if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && (id.Name == "max" || id.Name == "min") {
						if _, builtin := pass.TypesInfo.Uses[id].(*types.Builtin); builtin {
							for _, a := range x.Args {
								if v := resolve(a); v != nil {
									out[v] = true
								}
							}
						}
					}
				}
				return true
			})
		}
	}
	return out
}

// fieldRead resolves `x.f`, `x.f.Load()`, and conversions of either to
// the field.
func fieldRead(pass *analysis.Pass, e ast.Expr) *types.Var {
	e = unconv(pass, e)
	if v := fieldOf(pass, e); v != nil {
		return v
	}
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Load" {
		return nil
	}
	if v := fieldOf(pass, sel.X); v != nil && isAtomic(v.Type()) {
		return v
	}
	return nil
}

func isZeroOrOne(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[unconv(pass, e)]
	if !ok || tv.Value == nil {
		return false
	}
	iv := constant.ToInt(tv.Value)
	if iv.Kind() != constant.Int {
		return false
	}
	i, ok := constant.Int64Val(iv)
	return ok && (i == 0 || i == 1)
}

func isComparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

// checkSetter reports a numeric parameter stored unchecked into a
// defaulted, use-site-unchecked field. A comparison of the parameter
// quiets a store only when it GATES it: the store sits inside the
// comparison's branch, or the branch refuses (return, panic) before
// it. A comparison that merely logs (`if d == 0 { logf(...) }` beside
// the store) gates nothing.
func checkSetter(pass *analysis.Pass, name string, ftype *ast.FuncType, body *ast.BlockStmt, defaults, handled map[*types.Var]bool) {
	params := map[types.Object]bool{}
	for _, fld := range ftype.Params.List {
		for _, n := range fld.Names {
			if obj := pass.TypesInfo.ObjectOf(n); obj != nil && isNumeric(obj.Type()) {
				params[obj] = true
			}
		}
	}
	if len(params) == 0 {
		return
	}
	isParam := func(e ast.Expr) types.Object {
		id, ok := unconv(pass, e).(*ast.Ident)
		if !ok {
			return nil
		}
		if obj := pass.TypesInfo.ObjectOf(id); params[obj] {
			return obj
		}
		return nil
	}
	// Comparisons of the parameters, and the ifs holding them: a
	// comparison credits a STORE only when it gates that store — the
	// store sits inside its branch, or the branch refuses (return,
	// goto, panic) before it. Only ordered comparisons (<, <=, >, >=)
	// credit: a diverging `== 0` refuses zero alone and a negative
	// still replaces the default. A comparison that merely logs
	// gates nothing.
	var ifs []*ast.IfStmt
	type cmpRef struct {
		pos token.Pos
		p   types.Object
	}
	var cmps []cmpRef
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IfStmt:
			ifs = append(ifs, x)
		case *ast.BinaryExpr:
			switch x.Op {
			case token.LSS, token.LEQ, token.GTR, token.GEQ:
			default:
				return true
			}
			for _, side := range []ast.Expr{x.X, x.Y} {
				if p := isParam(side); p != nil {
					cmps = append(cmps, cmpRef{side.Pos(), p})
				}
			}
		}
		return true
	})
	gates := func(cmpPos, storePos token.Pos) bool {
		for _, iff := range ifs {
			if cmpPos < iff.Cond.Pos() || cmpPos > iff.Cond.End() {
				continue
			}
			if storePos >= iff.Body.Pos() && storePos < iff.Body.End() {
				return true
			}
			if setterBranchDiverges(iff.Body) {
				return true
			}
		}
		return false
	}
	paramChecked := func(p types.Object, storePos token.Pos) bool {
		for _, c := range cmps {
			if c.p == p && gates(c.pos, storePos) {
				return true
			}
		}
		return false
	}
	// A call that is neither a conversion nor the atomic Store takes
	// the parameter: a validator, max(d, floor).
	callChecked := map[types.Object]bool{}
	type store struct {
		at    ast.Node
		field *types.Var
		param types.Object
	}
	var stores []store
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			if tv, ok := pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() {
				return true // a conversion
			}
			if v, arg := storeCall(pass, x); v != nil {
				if p := isParam(arg); p != nil {
					stores = append(stores, store{x, v, p})
					return true
				}
			}
			for _, a := range x.Args {
				if p := isParam(a); p != nil {
					callChecked[p] = true
				}
			}
		case *ast.AssignStmt:
			if x.Tok != token.ASSIGN || len(x.Lhs) != len(x.Rhs) {
				return true
			}
			for i, l := range x.Lhs {
				v := fieldOf(pass, l)
				if v == nil {
					continue
				}
				if p := isParam(x.Rhs[i]); p != nil {
					stores = append(stores, store{x, v, p})
				}
			}
		}
		return true
	})
	for _, s := range stores {
		if callChecked[s.param] || paramChecked(s.param, s.at.Pos()) || !defaults[s.field] || handled[s.field] {
			continue
		}
		pass.Reportf(s.at.Pos(),
			"%s stores %s into %s with no check: 0 or a negative replaces the field's nonzero default and nothing downstream re-checks it; keep the default on a non-positive value (if %s > 0 { ... }) or refuse it",
			name, s.param.Name(), s.field.Name(), s.param.Name())
	}
}

// setterBranchDiverges: every top-level path through the block leaves
// the function (return, goto, or a panic call).
func setterBranchDiverges(b *ast.BlockStmt) bool {
	for _, st := range b.List {
		switch x := st.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.BranchStmt:
			return x.Tok == token.GOTO
		case *ast.ExprStmt:
			if call, ok := x.X.(*ast.CallExpr); ok {
				if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok && id.Name == "panic" {
					return true
				}
			}
		}
	}
	return false
}

// checkZeroDefault reports `if x == 0 { x = <default> }` on a signed
// numeric subject the function never sign-checks, when x then reaches a
// sink a negative breaks.
func checkZeroDefault(pass *analysis.Pass, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		iff, ok := n.(*ast.IfStmt)
		if !ok || iff.Else != nil {
			return true
		}
		bin, ok := ast.Unparen(iff.Cond).(*ast.BinaryExpr)
		if !ok || bin.Op != token.EQL {
			return true
		}
		var subj ast.Expr
		switch {
		case isZero(pass, bin.Y):
			subj = bin.X
		case isZero(pass, bin.X):
			subj = bin.Y
		default:
			return true
		}
		subj = ast.Unparen(subj)
		key := subjectKey(pass, subj)
		if key == "" || !isSigned(pass.TypesInfo.TypeOf(subj)) {
			return true
		}
		if !assignsDefault(pass, iff.Body, key) {
			return true
		}
		// The sink comes first: only a sign check BEFORE it guards the
		// path that reaches it — a log-only check after the sink
		// guards nothing.
		sink, sinkPos := firstSink(pass, body, key, iff.End())
		if sink == "" {
			return true
		}
		if signChecked(pass, body, key, sinkPos) {
			return true
		}
		// `timeout := inv.Timeout` then `if timeout == 0`: a sign check
		// on the source the local copied is a sign check on the local
		// (evals codex.go refuses inv.Timeout < 0 before the copy).
		if src := copiedFrom(pass, body, subj); src != "" && signChecked(pass, body, src, sinkPos) {
			return true
		}
		pass.Reportf(iff.Pos(),
			"%s == 0 takes the default but a negative %s passes through to %s; test %s <= 0 so a negative keeps the default too, or refuse it",
			key, key, sink, key)
		return true
	})
}

// copiedFrom returns the key of the expression a local subject was
// declared from (`timeout := inv.Timeout` gives "inv.Timeout"), or "".
func copiedFrom(pass *analysis.Pass, body *ast.BlockStmt, subj ast.Expr) string {
	id, ok := subj.(*ast.Ident)
	if !ok {
		return ""
	}
	obj := pass.TypesInfo.ObjectOf(id)
	src := ""
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || as.Tok != token.DEFINE || len(as.Lhs) != len(as.Rhs) {
			return true
		}
		for i, l := range as.Lhs {
			if lid, ok := l.(*ast.Ident); ok && pass.TypesInfo.Defs[lid] == obj && obj != nil {
				src = subjectKey(pass, unconv(pass, ast.Unparen(as.Rhs[i])))
			}
		}
		return true
	})
	return src
}

func isZero(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.Int && constant.Sign(tv.Value) == 0
}

// subjectKey names an identifier or field selector chain; other
// expressions have no key.
func subjectKey(pass *analysis.Pass, e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if fieldOf(pass, x) == nil {
			return ""
		}
		if subjectKey(pass, ast.Unparen(x.X)) == "" {
			return ""
		}
		return types.ExprString(x)
	}
	return ""
}

func sameSubject(pass *analysis.Pass, e ast.Expr, key string) bool {
	e = unconv(pass, e)
	return subjectKey(pass, e) == key
}

// assignsDefault: the then-branch assigns the subject a nonzero value.
func assignsDefault(pass *analysis.Pass, body *ast.BlockStmt, key string) bool {
	for _, st := range body.List {
		as, ok := st.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != len(as.Rhs) {
			continue
		}
		for i, l := range as.Lhs {
			if sameSubject(pass, l, key) && !isZero(pass, as.Rhs[i]) {
				return true
			}
		}
	}
	return false
}

// signChecked: the function makes an ordered comparison of the subject
// before pos (a check after the sink guards nothing on the path that
// reached it), or clamps it with max/min before pos.
func signChecked(pass *analysis.Pass, body *ast.BlockStmt, key string, before token.Pos) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil || found || n.Pos() >= before {
			return false
		}
		switch x := n.(type) {
		case *ast.BinaryExpr:
			switch x.Op {
			case token.LSS, token.LEQ, token.GTR, token.GEQ:
				if sameSubject(pass, x.X, key) || sameSubject(pass, x.Y, key) {
					found = true
				}
			}
		case *ast.CallExpr:
			if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok && (id.Name == "max" || id.Name == "min") {
				for _, a := range x.Args {
					if sameSubject(pass, a, key) {
						found = true
					}
				}
			}
		}
		return true
	})
	return found
}

// firstSink names the first sink after pos that takes the subject: an
// argument of make, a timer constructor, context.WithTimeout, a
// time.Time.Add, or a call whose constant SQL holds LIMIT. It returns
// the sink's name and position (signChecked's bound).
func firstSink(pass *analysis.Pass, body *ast.BlockStmt, key string, after token.Pos) (string, token.Pos) {
	sink := ""
	pos := token.NoPos
	ast.Inspect(body, func(n ast.Node) bool {
		if sink != "" {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || call.Pos() < after {
			return true
		}
		takes := false
		for _, a := range call.Args {
			if sameSubject(pass, a, key) {
				takes = true
			}
		}
		if !takes {
			return true
		}
		if name := sinkName(pass, call); name != "" {
			sink = name
			pos = call.Pos()
		}
		return true
	})
	return sink, pos
}

var limitRe = regexp.MustCompile(`(?i)\bLIMIT\b`)

func sinkName(pass *analysis.Pass, call *ast.CallExpr) string {
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		if b, ok := pass.TypesInfo.Uses[fun].(*types.Builtin); ok && b.Name() == "make" {
			return "make()"
		}
	case *ast.SelectorExpr:
		if obj := pass.TypesInfo.Uses[fun.Sel]; obj != nil && obj.Pkg() != nil {
			if _, isFunc := obj.(*types.Func); isFunc {
				switch obj.Pkg().Path() + "." + obj.Name() {
				case "time.NewTicker", "time.NewTimer", "time.Tick", "time.AfterFunc", "context.WithTimeout":
					return obj.Pkg().Name() + "." + obj.Name()
				}
			}
		}
		if fun.Sel.Name == "Add" {
			if t := pass.TypesInfo.TypeOf(fun.X); t != nil && isNamedTime(t) {
				return "time.Time.Add"
			}
		}
	}
	for _, a := range call.Args {
		if tv, ok := pass.TypesInfo.Types[a]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
			if limitRe.MatchString(constant.StringVal(tv.Value)) {
				return "a SQL LIMIT"
			}
		}
	}
	return ""
}

func isNamedTime(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "time" && n.Obj().Name() == "Time"
}
