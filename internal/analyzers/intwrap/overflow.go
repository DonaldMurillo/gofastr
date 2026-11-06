package intwrap

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"reflect"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The 2026-11 bug-bash arms: signed multiplication and the +1 sentinel
// wrap past MaxInt64 the same way an unsigned conversion does, and a
// guard written AFTER the arithmetic sees the wrapped value. Each arm
// fires only on a caller-reachable operand (see rooted) with no upper
// bound compared before the arithmetic (see upperBoundedBefore).
//
// Shape D — `time.Duration(x) * time.<Unit>` (either operand order, any
// unit above Nanosecond). x is the seconds or minutes a caller sent:
// ttl_seconds of -2^55 wrapped to 0, a permanent token, and 584 years
// wrapped to 290ms (8b19113f2, battery/auth apitoken_routes.go); an
// OAuth expires_in did the same at four token-exchange sites
// (ce72591bd #7, battery/auth oauth2.go). A clamp after the multiply
// clamps the wrapped value, so it does not count. The fix spelling is
// tokenExpiry's: compare x against max/unit (maxTokenTTLSeconds, or
// int64(maxTokenLifetime/time.Second)) and refuse or clamp first.
//
// Shape E — `x + k` (k a positive constant) handed to io.LimitReader,
// io.CopyN or http.MaxBytesReader. x is a cap a caller configured: at math.MaxInt64 ("no cap") limit+1
// wraps to MinInt64 and io.LimitReader reads NOTHING, so every body
// reads as empty (c0d6e99e0, core/middleware readBodyLimit and
// readAndBufferCapped). Constants (`MaxFoo+1`) are compile-time
// checked and stay quiet. The fix spelling is c0d6e99e0's
// sentinelLimit: `if limit >= math.MaxInt64 { return limit }` and only
// then limit+1.
//
// Shape F — `(a-1)*b`, and `a*b` stored as an offset or skip (an
// offset/skip-named variable, field or key, or an Offset/Skip call's
// argument), where an operand is caller-reachable: ?page=MaxInt64
// wrapped the skip to an offset that passed the cap check and served
// page 1 labelled as that page (b973561c4, framework/crud
// requireBoundedOffset over pagination). The fix spellings are
// pagination.OffsetForPage's division pre-check (`page-1 >
// math.MaxInt/limit`), a math/bits Mul, or math/big; both operands
// bounded above also count.
//
// Caller-reachable (rooted) means: a parameter; a field with a json
// tag (a decoded body); a strconv.Atoi/Parse* result; a value read out
// of a map or an interface (`m["ttl"].(float64)`); a selector on any of
// these; a local assigned from any of these. Shape E also takes the
// method receiver's fields, but only a field some assignment or
// composite key in the package sets to a non-constant (an option): a
// field only ever set to a constant is the author's cap (core/webbotauth
// directory maxBody). A parameter of an unexported function that every
// in-package call hands a constant is not caller-reachable either
// (desktopcmd readLinuxInstallFile, always called with 64<<10 and the
// like); the function must be called at least once and never used as a
// value.
//
// Silent postures for these arms:
//   - constants and package-level values, which the compiler or the
//     author bounds;
//   - any ordered comparison of the operand, BEFORE the arithmetic,
//     against an upper bound: a constant above 1, a max/limit/cap-named
//     value, len/cap, or math.Max*; or a `x = min(x, …)` clamp before
//     it (battery/admin's `page = min(page, pages)`). A sign check
//     (`x < 0`, `limit <= 0`) bounds nothing above;
//   - for shape F, a comparison that divides by one of the operands
//     (`page-1 > math.MaxInt/limit`) or a math/bits Mul* call in the
//     function, and an operand the caller cannot reach (a range index
//     over a constant): only the reachable operands need a bound;
//   - make(): an unbounded caller length panics make with or without
//     the +1, so the sentinel's wrap adds no failure there;
//   - float operands (no wrap) and unsigned ones in shape D/F (the
//     conversion arm A covers those).
var maxNameRe = regexp.MustCompile(`(?i)max|limit|cap|ceil`)

var offsetNameRe = regexp.MustCompile(`(?i)^(offset|skip)|(offset|skip)$`)

func checkOverflowArms(pass *analysis.Pass, fn *funcInfo, idx *pkgIndex) {
	r := newRoots(pass, fn, idx)
	usesBitsMul := false
	ast.Inspect(fn.body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if q := pkgFunc(pass, call.Fun); strings.HasPrefix(q, "math/bits.Mul") {
				usesBitsMul = true
			}
		}
		return true
	})
	offsetUses := offsetTargets(pass, fn.body)
	ast.Inspect(fn.body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op != token.MUL {
				return true
			}
			checkDurationMul(pass, fn, r, x)
			if !usesBitsMul {
				checkOffsetMul(pass, fn, r, x, offsetUses)
			}
		case *ast.CallExpr:
			checkSentinelPlus(pass, fn, r, x)
		}
		return true
	})
}

// checkDurationMul is shape D.
func checkDurationMul(pass *analysis.Pass, fn *funcInfo, r *roots, mul *ast.BinaryExpr) {
	for _, pair := range [][2]ast.Expr{{mul.X, mul.Y}, {mul.Y, mul.X}} {
		conv, unit := ast.Unparen(pair[0]), ast.Unparen(pair[1])
		if !isTimeUnit(pass, unit) {
			continue
		}
		call, ok := conv.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		if tv, ok := pass.TypesInfo.Types[call.Fun]; !ok || !tv.IsType() || !isDuration(tv.Type) {
			continue
		}
		// The converted operand is a signed int (`int64(f)` of a decoded
		// float counts: the product wraps the same way); the guard names
		// what is inside the conversions.
		subj := unconvert(pass, call.Args[0])
		if !isSignedIntExpr(pass, call.Args[0]) || isConst(pass, subj) {
			continue
		}
		if !r.rooted(subj, false) {
			continue
		}
		if upperBoundedBefore(pass, fn.body, subj, mul.Pos()) {
			continue
		}
		pass.Reportf(mul.Pos(),
			"time.Duration(%s) * %s with no upper bound on %s first: a large value wraps int64 (584 years reads as 290ms, -2^55 as 0); compare %s against max/unit and refuse or clamp before the multiply",
			types.ExprString(subj), types.ExprString(unit), types.ExprString(subj), types.ExprString(subj))
		return
	}
}

// checkSentinelPlus is shape E.
func checkSentinelPlus(pass *analysis.Pass, fn *funcInfo, r *roots, call *ast.CallExpr) {
	var args []ast.Expr
	name := ""
	switch q := pkgFunc(pass, call.Fun); q {
	case "io.LimitReader":
		if len(call.Args) == 2 {
			args, name = call.Args[1:], "io.LimitReader"
		}
	case "io.CopyN", "net/http.MaxBytesReader":
		if len(call.Args) == 3 {
			args, name = call.Args[2:], q[strings.LastIndex(q, "/")+1:]
		}
	}
	for _, a := range args {
		add, ok := ast.Unparen(a).(*ast.BinaryExpr)
		if !ok || add.Op != token.ADD {
			continue
		}
		// Either operand order: limit+1 and 1+limit are the same
		// sentinel.
		subj, k := ast.Unparen(add.X), ast.Unparen(add.Y)
		if !posConst(pass, k) {
			if posConst(pass, subj) {
				subj, k = add.Y, add.X
			} else {
				continue
			}
		}
		if !isSignedIntExpr(pass, subj) || isConst(pass, subj) || !r.rooted(subj, true) {
			continue
		}
		if upperBoundedBefore(pass, fn.body, subj, add.Pos()) {
			continue
		}
		pass.Reportf(add.Pos(),
			"%s to %s with no upper bound on %s: at math.MaxInt64 (no cap) the sum wraps negative and %s reads or allocates nothing; keep the cap when %s >= math.MaxInt64 and add only below it",
			types.ExprString(add), name, types.ExprString(subj), name, types.ExprString(subj))
	}
}

// posConst: e is an integer constant above zero.
func posConst(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.Int && constant.Sign(tv.Value) > 0
}

// checkOffsetMul is shape F.
func checkOffsetMul(pass *analysis.Pass, fn *funcInfo, r *roots, mul *ast.BinaryExpr, offsetUses map[ast.Expr]bool) {
	a, b := ast.Unparen(mul.X), ast.Unparen(mul.Y)
	if !isSignedIntExpr(pass, a) || !isSignedIntExpr(pass, b) || isConst(pass, a) || isConst(pass, b) {
		return
	}
	if !isMinusOne(pass, a) && !isMinusOne(pass, b) && !offsetUses[mul] {
		return
	}
	ops := []ast.Expr{operandOf(pass, a), operandOf(pass, b)}
	if divisionPrecheck(pass, fn.body, ops, mul.Pos()) {
		return
	}
	// An operand the caller cannot reach is the author's (a constant
	// page count, a range index over one); each one the caller can
	// reach must be bounded above before the multiply.
	unbounded := false
	for _, op := range ops {
		if r.rooted(op, false) && !upperBoundedBefore(pass, fn.body, op, mul.Pos()) {
			unbounded = true
		}
	}
	if !unbounded {
		return
	}
	pass.Reportf(mul.Pos(),
		"offset %s with no overflow pre-check: a large page or limit wraps int and the skip lands inside the cap (?page=MaxInt64 served page 1); check %s > math.MaxInt/%s first (pagination.OffsetForPage), or use math/bits.Mul",
		types.ExprString(mul), types.ExprString(ops[0]), types.ExprString(ops[1]))
}

// operandOf strips the `-1` of `page-1`: the guard names page.
func operandOf(pass *analysis.Pass, e ast.Expr) ast.Expr {
	if isMinusOne(pass, e) {
		return ast.Unparen(e.(*ast.BinaryExpr).X)
	}
	return e
}

func isMinusOne(pass *analysis.Pass, e ast.Expr) bool {
	bin, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok || bin.Op != token.SUB {
		return false
	}
	tv, ok := pass.TypesInfo.Types[bin.Y]
	return ok && tv.Value != nil && constant.Compare(tv.Value, token.EQL, constant.MakeInt64(1))
}

// offsetTargets collects multiplications stored as an offset: assigned
// to an offset/skip-named variable or field, a composite key so named,
// or passed to an Offset/Skip-named call.
func offsetTargets(pass *analysis.Pass, body *ast.BlockStmt) map[ast.Expr]bool {
	out := map[ast.Expr]bool{}
	mark := func(name string, e ast.Expr) {
		if offsetNameRe.MatchString(name) {
			if bin, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && bin.Op == token.MUL {
				out[bin] = true
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Lhs) == len(x.Rhs) {
				for i, l := range x.Lhs {
					mark(lastName(l), x.Rhs[i])
				}
			}
		case *ast.ValueSpec:
			for i, v := range x.Values {
				if i < len(x.Names) {
					mark(x.Names[i].Name, v)
				}
			}
		case *ast.KeyValueExpr:
			mark(lastName(x.Key), x.Value)
		case *ast.CallExpr:
			for _, a := range x.Args {
				mark(lastName(x.Fun), a)
			}
		}
		return true
	})
	return out
}

func lastName(e ast.Expr) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.BasicLit:
		if x.Kind == token.STRING {
			return strings.Trim(x.Value, "\"`")
		}
	}
	return ""
}

// divisionPrecheck: a comparison before pos that divides by one of the
// operands (`page-1 > math.MaxInt/limit`).
func divisionPrecheck(pass *analysis.Pass, body *ast.BlockStmt, ops []ast.Expr, pos token.Pos) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil || n.Pos() >= pos {
			return !found
		}
		bin, ok := n.(*ast.BinaryExpr)
		if !ok || !isComparison(bin.Op) {
			return true
		}
		ast.Inspect(bin, func(m ast.Node) bool {
			q, ok := m.(*ast.BinaryExpr)
			if ok && q.Op == token.QUO {
				for _, op := range ops {
					if sameExpr(pass, q.Y, op) {
						found = true
					}
				}
			}
			return !found
		})
		return true
	})
	return found
}

// upperBoundedBefore: an ordered comparison of subj before pos against
// a constant above 1, a max/limit/cap-named value, or math.Max*. A
// comparison after pos (a clamp of the wrapped product) does not count.
func upperBoundedBefore(pass *analysis.Pass, body *ast.BlockStmt, subj ast.Expr, pos token.Pos) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if n.Pos() >= pos {
			return false
		}
		// `page = min(page, pages)`: a clamp before the arithmetic.
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == len(as.Rhs) {
			for i, l := range as.Lhs {
				if sameExpr(pass, l, subj) && isBuiltinCall(pass, as.Rhs[i], "min") {
					found = true
				}
			}
		}
		bin, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		switch bin.Op {
		case token.LSS, token.LEQ, token.GTR, token.GEQ:
		default:
			return true
		}
		if sameExpr(pass, bin.X, subj) && isUpperBound(pass, bin.Y) ||
			sameExpr(pass, bin.Y, subj) && isUpperBound(pass, bin.X) {
			found = true
		}
		return true
	})
	return found
}

func isBuiltinCall(pass *analysis.Pass, e ast.Expr, name string) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := pass.TypesInfo.Uses[id].(*types.Builtin)
	return ok && b.Name() == name
}

func isUpperBound(pass *analysis.Pass, e ast.Expr) bool {
	e = unconvert(pass, e)
	if tv, ok := pass.TypesInfo.Types[e]; ok && tv.Value != nil {
		iv := constant.ToInt(tv.Value)
		if iv.Kind() == constant.Int {
			return constant.Compare(iv, token.GTR, constant.MakeInt64(1))
		}
		if f, ok := constant.Float64Val(constant.ToFloat(tv.Value)); ok {
			return f > 1
		}
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			if maxNameRe.MatchString(x.Name) {
				found = true
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "len" || id.Name == "cap") {
				found = true
			}
		}
		return !found
	})
	return found
}

// sameExpr compares two operand expressions through conversions: the
// same object for identifiers, the same printed form for selectors.
func sameExpr(pass *analysis.Pass, a, b ast.Expr) bool {
	a, b = unconvert(pass, a), unconvert(pass, b)
	if ia, ok := a.(*ast.Ident); ok {
		ib, ok := b.(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(ia) != nil && pass.TypesInfo.ObjectOf(ia) == pass.TypesInfo.ObjectOf(ib)
	}
	return types.ExprString(a) == types.ExprString(b)
}

// unconvert strips parentheses and integer/float type conversions.
func unconvert(pass *analysis.Pass, e ast.Expr) ast.Expr {
	for {
		e = ast.Unparen(e)
		call, ok := e.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return e
		}
		tv, ok := pass.TypesInfo.Types[call.Fun]
		if !ok || !tv.IsType() {
			return e
		}
		if _, ok := tv.Type.Underlying().(*types.Basic); !ok {
			return e
		}
		e = call.Args[0]
	}
}

func isConst(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	return ok && tv.Value != nil
}

func isSignedIntExpr(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsInteger != 0 && b.Info()&types.IsUnsigned == 0
}

func isDuration(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "time" && n.Obj().Name() == "Duration"
}

func isTimeUnit(pass *analysis.Pass, e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	obj, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Const)
	if !ok || obj.Pkg() == nil || obj.Pkg().Path() != "time" {
		return false
	}
	switch obj.Name() {
	case "Microsecond", "Millisecond", "Second", "Minute", "Hour":
		return true
	}
	return false
}

// pkgFunc returns "pkgpath.Name" for a call of a package-level function.
func pkgFunc(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := ast.Unparen(fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return ""
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// roots answers whether an operand is caller-reachable in fn.
type roots struct {
	pass   *analysis.Pass
	idx    *pkgIndex
	params map[types.Object]bool
	recv   types.Object
	// locals maps a local to the right-hand sides assigned to it.
	locals map[types.Object][]ast.Expr
	// parsed marks locals bound to a strconv result or an interface /
	// map extraction.
	parsed map[types.Object]bool
}

func newRoots(pass *analysis.Pass, fn *funcInfo, idx *pkgIndex) *roots {
	r := &roots{pass: pass, idx: idx, params: map[types.Object]bool{}, locals: map[types.Object][]ast.Expr{}, parsed: map[types.Object]bool{}}
	i := 0
	for _, f := range fn.ft.Params.List {
		for _, n := range f.Names {
			// An unexported function every in-package caller hands a
			// constant for this parameter: the author bounded it. A
			// package-level literal has no callers to check.
			if obj := pass.TypesInfo.ObjectOf(n); obj != nil && (fn.obj == nil || !idx.constantOnly(fn.obj, i)) {
				r.params[obj] = true
			}
			i++
		}
		if len(f.Names) == 0 {
			i++
		}
	}
	if fn.recv != nil && len(fn.recv.List) == 1 && len(fn.recv.List[0].Names) == 1 {
		r.recv = pass.TypesInfo.ObjectOf(fn.recv.List[0].Names[0])
	}
	ast.Inspect(fn.body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if len(x.Rhs) == 1 && len(x.Lhs) >= 1 {
				rhs := ast.Unparen(x.Rhs[0])
				if isParseCall(pass, rhs) || isExtraction(pass, rhs) {
					if id, ok := x.Lhs[0].(*ast.Ident); ok {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							r.parsed[obj] = true
						}
					}
				}
			}
			if len(x.Lhs) == len(x.Rhs) {
				for i, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							r.locals[obj] = append(r.locals[obj], x.Rhs[i])
						}
					}
				}
			}
		case *ast.ValueSpec:
			for i, v := range x.Values {
				if i < len(x.Names) {
					if obj := pass.TypesInfo.ObjectOf(x.Names[i]); obj != nil {
						r.locals[obj] = append(r.locals[obj], v)
					}
				}
			}
		}
		return true
	})
	return r
}

// rooted reports whether e is caller-reachable; withRecv also counts the
// method receiver (shape E's configured cap).
func (r *roots) rooted(e ast.Expr, withRecv bool) bool {
	return r.rootedDepth(e, withRecv, 0)
}

func (r *roots) rootedDepth(e ast.Expr, withRecv bool, depth int) bool {
	if depth > 8 {
		return false
	}
	e = unconvert(r.pass, e)
	switch x := e.(type) {
	case *ast.Ident:
		obj := r.pass.TypesInfo.ObjectOf(x)
		if obj == nil {
			return false
		}
		if r.params[obj] || r.parsed[obj] || (withRecv && obj == r.recv) {
			return true
		}
		for _, rhs := range r.locals[obj] {
			if r.rootedDepth(rhs, withRecv, depth+1) {
				return true
			}
		}
	case *ast.SelectorExpr:
		if s, ok := r.pass.TypesInfo.Selections[x]; ok && s.Kind() == types.FieldVal {
			if hasJSONTag(s) {
				return true
			}
			// A receiver field the package only ever sets to constants
			// (webbotauth's maxBody: defaultMaxDirectoryBytes, no
			// option) is the author's cap, not a configured one.
			if base, ok := ast.Unparen(x.X).(*ast.Ident); ok && withRecv && r.recv != nil && r.pass.TypesInfo.ObjectOf(base) == r.recv {
				return r.idx.nonConstField[s.Obj()]
			}
			return r.rootedDepth(x.X, withRecv, depth+1)
		}
	case *ast.StarExpr:
		return r.rootedDepth(x.X, withRecv, depth+1)
	case *ast.IndexExpr, *ast.TypeAssertExpr:
		return isExtraction(r.pass, e)
	case *ast.BinaryExpr:
		return r.rootedDepth(x.X, withRecv, depth+1) || r.rootedDepth(x.Y, withRecv, depth+1)
	}
	return false
}

// hasJSONTag: the selected field carries a json struct tag, so a
// request or response body decodes into it.
func hasJSONTag(s *types.Selection) bool {
	t := s.Recv()
	path := s.Index()
	for i, idx := range path {
		if p, ok := t.Underlying().(*types.Pointer); ok {
			t = p.Elem()
		}
		st, ok := t.Underlying().(*types.Struct)
		if !ok || idx >= st.NumFields() {
			return false
		}
		if i == len(path)-1 {
			_, tagged := reflect.StructTag(st.Tag(idx)).Lookup("json")
			return tagged
		}
		t = st.Field(idx).Type()
	}
	return false
}

// isParseCall: strconv.Atoi / ParseInt / ParseUint / ParseFloat.
func isParseCall(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	switch pkgFunc(pass, call.Fun) {
	case "strconv.Atoi", "strconv.ParseInt", "strconv.ParseUint", "strconv.ParseFloat":
		return true
	}
	return false
}

// isExtraction: a value read out of a map of interfaces or out of an
// interface (`m["ttl"]`, `v.(float64)`, `m["ttl"].(float64)`).
func isExtraction(pass *analysis.Pass, e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.TypeAssertExpr:
		if t := pass.TypesInfo.TypeOf(x.X); t != nil {
			if iface, ok := t.Underlying().(*types.Interface); ok && iface.NumMethods() == 0 {
				return true
			}
		}
	case *ast.IndexExpr:
		if t := pass.TypesInfo.TypeOf(x.X); t != nil {
			if m, ok := t.Underlying().(*types.Map); ok {
				if iface, ok := m.Elem().Underlying().(*types.Interface); ok && iface.NumMethods() == 0 {
					return true
				}
			}
		}
	}
	return false
}

// pkgIndex holds the package-wide facts the overflow arms consult.
type pkgIndex struct {
	// calls lists, per same-package function, the argument lists of its
	// direct calls; referenced marks a function also used as a value
	// (its callers are then unknown).
	calls      map[*types.Func][][]ast.Expr
	referenced map[*types.Func]bool
	// nonConstField marks the fields some assignment or composite key in
	// the package sets to a non-constant value.
	nonConstField map[types.Object]bool
	pass          *analysis.Pass
}

func newPkgIndex(pass *analysis.Pass) *pkgIndex {
	idx := &pkgIndex{
		calls:         map[*types.Func][][]ast.Expr{},
		referenced:    map[*types.Func]bool{},
		nonConstField: map[types.Object]bool{},
		pass:          pass,
	}
	callee := map[*ast.Ident]bool{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				var id *ast.Ident
				switch fun := ast.Unparen(x.Fun).(type) {
				case *ast.Ident:
					id = fun
				case *ast.SelectorExpr:
					id = fun.Sel
				}
				if id != nil {
					if fn, ok := pass.TypesInfo.Uses[id].(*types.Func); ok && fn.Pkg() == pass.Pkg {
						idx.calls[fn] = append(idx.calls[fn], x.Args)
						callee[id] = true
					}
				}
			case *ast.AssignStmt:
				if len(x.Lhs) == len(x.Rhs) {
					for i, l := range x.Lhs {
						if sel, ok := ast.Unparen(l).(*ast.SelectorExpr); ok && !isConst(pass, x.Rhs[i]) {
							if obj := pass.TypesInfo.Uses[sel.Sel]; obj != nil {
								idx.nonConstField[obj] = true
							}
						}
					}
				} else {
					for _, l := range x.Lhs {
						if sel, ok := ast.Unparen(l).(*ast.SelectorExpr); ok {
							if obj := pass.TypesInfo.Uses[sel.Sel]; obj != nil {
								idx.nonConstField[obj] = true
							}
						}
					}
				}
			case *ast.KeyValueExpr:
				if key, ok := x.Key.(*ast.Ident); ok && !isConst(pass, x.Value) {
					if v, ok := pass.TypesInfo.Uses[key].(*types.Var); ok && v.IsField() {
						idx.nonConstField[v] = true
					}
				}
			}
			return true
		})
	}
	for id, obj := range pass.TypesInfo.Uses {
		if fn, ok := obj.(*types.Func); ok && fn.Pkg() == pass.Pkg && !callee[id] {
			idx.referenced[fn] = true
		}
	}
	return idx
}

// constantOnly reports whether fn is unexported, called at least once in
// the package, never used as a value, and handed a constant for
// parameter i at every call.
func (idx *pkgIndex) constantOnly(fn *types.Func, i int) bool {
	if fn == nil || fn.Exported() || idx.referenced[fn] {
		return false
	}
	calls := idx.calls[fn]
	if len(calls) == 0 {
		return false
	}
	for _, args := range calls {
		if i >= len(args) || !isConst(idx.pass, args[i]) {
			return false
		}
	}
	return true
}
