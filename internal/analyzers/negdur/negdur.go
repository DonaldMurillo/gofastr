// Package negdur catches time.Duration zero-tests that silently fold a
// NEGATIVE lifetime onto the "zero means default" or "nonzero means
// expiry" arm.
//
// Bug class: code special-cases 0 — "absent means pick a sensible
// default" or "absent means no expiry" — with a comparison that puts
// negative and zero on the SAME side (d <= 0 substituted with a
// default; d == 0 defaulted while a d > 0 decision arms expiry). A
// caller who passes a negative duration (clock skew, a unit mix-up, a
// hostile body) is then handed the strongest setting instead of the
// weakest: the default (often the longest lifetime the code knows) or
// immortality. Found by the 2026-09-04 red-probe round:
// TestSessionNegativeTTLFailsClosed (battery/auth session.go
// MemorySessionStore.Create: `if ttl <= 0 { ttl = 7 * 24 * time.Hour }`
// turned -1s into a one-week session) and
// TestMemoryCacheNegativeTTLNotImmortal (battery/cache memory.go
// MemoryCache.Set: `== 0` took the default while `hasExpiry: d > 0`
// made every negative TTL immortal). The reviewer then proved the
// same inversion needs no default at all (battery/auth apitoken.go):
// deleting validateTokenSpec's `spec.TTL < 0` rejection left
// `if spec.TTL > 0 { ExpiresAt = now.Add(spec.TTL) }` minting
// never-expiring tokens for negative TTLs. The sibling fix shapes
// show the postures: battery/auth validateTokenSpec rejects
// `spec.TTL < 0` outright; EntitySessionStore.Create returns an error
// on `ttl <= 0`.
//
// Three spellings fire, all only on CALLER-SUPPLIED durations — a
// function parameter, a duration field of a parameter struct whose
// type is not configuration-named, or a local derived from either;
// constants, package-level vars, and time.Since/time.Until results
// are not:
//
//   - SUBSTITUTION: `if d <= 0 { d = <default> }` (or `d < 1`, or the
//     operands reversed). The negative joins zero on the default arm
//     and silently becomes the strongest setting. Reported at the if.
//     The default must EXTEND the lifetime: a positive duration
//     constant, or a default/fallback-named field or call. A
//     substituted zero or a sentinel named disabled/off/never/forever/
//     unlimited extends nothing and stays quiet.
//
//   - NO-EXPIRY DECISION with default: `d > 0` (or `d >= 1`, reversed)
//     feeding an expiry/limit-named boolean (`hasExpiry: d > 0`) in a
//     function that ALSO gives `d == 0` (or `d <= 0`) the default
//     treatment. The zero arm proves 0 is special-cased; the decision
//     then makes negative mean forever. Reported at the comparison.
//
//   - NO-EXPIRY DECISION bare (the reviewer's apitoken proof): a
//     `d > 0` / `d != 0` comparison guarding a body that arms expiry —
//     an assignment to an ExpiresAt/expiry/deadline-named target, or a
//     time.After(d) / .Add(d) call on the subject — with no negative
//     rejection or clamp in scope, with or without an accompanying
//     default. "In scope" means in the function itself, or inside a
//     package-local validator the function calls with the same value
//     (IssueToken → validateTokenSpec: the unmutated call site stays
//     quiet because the helper's `spec.TTL < 0` rejection is the
//     rejection; delete it and the decision fires).
//
// Silent postures, deliberately:
//   - any comparison on the same value that rejects negatives —
//     `d < 0`, `d <= -k`, `d >= 0`, or the operands reversed — wherever
//     in the function it sits, and any clamp (`d = max(d, 0)`,
//     `if d < 0 { d = 0 }`): clamp-to-already-expired is a fix posture;
//   - a `d <= 0` arm that diverges with an error instead of
//     substituting (refusal is the EntitySessionStore fix posture), and
//     a bare `d == 0` substitution with no in-function no-expiry
//     decision: the negative keeps its sign on the wire to the
//     downstream API (battery/cache redis.go Set — the host client owns
//     the semantics there);
//   - developer configuration: subjects read off the method receiver
//     (`c.TokenTTL` in `(c *AuthConfig).defaults`) or off a
//     configuration-named type (Config, Options, …Opts, Settings, at
//     any selector hop). Per the adversarial-tests skill, host-authored
//     configuration has a different threat model from caller/request
//     data: a developer writing a negative timeout is a footgun, not an
//     inversion an attacker can reach;
//   - durations that are not caller-supplied: constants, literals,
//     time.Since/time.Until results, package-level state;
//   - values of other numeric types — only time.Duration participates;
//   - _test.go files.
package negdur

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "negdur",
	Doc:  "forbids time.Duration zero-tests that fold a negative lifetime onto the default or no-expiry arm (d <= 0 → default; d == 0 then d > 0 → hasExpiry; a bare > 0/!= 0 expiry decision with no rejection in scope); reject d < 0 or clamp to zero/expired first — receiver fields and config/options-typed fields are developer configuration and stay quiet",
	Run:  run,
}

// defaultRe matches substituted values that state their role: the
// default the zero arm picks. A default extends a negative lifetime.
var defaultRe = regexp.MustCompile(`(?i)default|fallback|standard|normal`)

// sentinelRe matches substituted values from the code's own "off"
// vocabulary: substituting one is a documented contract (disabled =
// no timer), not the silent strongest-setting inversion.
var sentinelRe = regexp.MustCompile(`(?i)disabled|disabl|off$|never|forever|unlimited|noexp|sentinel|infinite`)

// expiryRe matches the boolean that carries a no-expiry decision:
// hasExpiry, ttlLive, deadlineSet...
var expiryRe = regexp.MustCompile(`(?i)expir|ttl|deadline|timeout|lifetime`)

// armExpiryRe is the STRICT vocabulary for the assignment that arms
// expiry in a guarded body: ExpiresAt, expiry, deadline, hasExpiry.
// A timeout-named plain field is not an expiry arm — an
// override-only-when-positive option setter (`WithListenTimeout(d) {
// if d > 0 { p.listenTimeout = d } }`) keeps its pre-set default on
// the skipped arm and never stores the negative, so no inversion
// exists.
var armExpiryRe = regexp.MustCompile(`(?i)expir|deadline`)

// configRe matches type names from the developer-configuration
// vocabulary: a duration field read off one of these is a host
// setting, not caller/request data.
var configRe = regexp.MustCompile(`(?i)config|options|opts$|settings|prefs$`)

func run(pass *analysis.Pass) (any, error) {
	duration, hasDuration := durationType(pass)
	if !hasDuration {
		return nil, nil
	}
	var funcs []*ast.FuncDecl
	funcByObj := map[types.Object]*ast.FuncDecl{}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			funcs = append(funcs, fn)
			if fn.Recv == nil {
				if obj := pass.TypesInfo.ObjectOf(fn.Name); obj != nil {
					funcByObj[obj] = fn
				}
			}
		}
	}
	// rejectsOf: per function, the parameter positions (and field
	// paths past them) on which it rejects negatives or clamps — the
	// validator evidence the bare-decision posture honors across a
	// call.
	rejectsOf := map[*ast.FuncDecl]map[int]map[string]bool{}
	for _, fn := range funcs {
		if r := paramRejects(pass, fn, duration); len(r) > 0 {
			rejectsOf[fn] = r
		}
	}
	pkg := &pkgCtx{funcByObj: funcByObj, rejectsOf: rejectsOf}
	for _, fn := range funcs {
		checkFunc(pass, fn, duration, pkg)
	}
	return nil, nil
}

// pkgCtx is the package-level view one function's analysis uses:
// which package funcs are callable by identifier, and what each of
// them rejects.
type pkgCtx struct {
	funcByObj map[types.Object]*ast.FuncDecl
	rejectsOf map[*ast.FuncDecl]map[int]map[string]bool
}

// dstate tracks one duration value through a function.
type dstate struct {
	name        string // source spelling, for the diagnostic
	negRejected bool   // some comparison rejects negatives, or a clamp exists
	zeroDefault bool   // an == 0 / <= 0 arm substituted an extending default
}

// cmpclass classifies a zero-comparison on a duration value.
type cmpclass int

const (
	cmpNone       cmpclass = iota
	cmpZeroOrLess          // d <= 0, d < 1: negative and zero on the same arm
	cmpEqualZero           // d == 0
	cmpPositive            // d > 0, d >= 1, d != 0: negative and zero on the same arm
	cmpNegReject           // d < 0, d <= -k, d >= 0: negatives get their own arm
)

func checkFunc(pass *analysis.Pass, fn *ast.FuncDecl, duration types.Type, pkg *pkgCtx) {
	fc := newFuncCtx(pass, fn, duration, pkg)

	// Pass 1: negative rejections and clamps anywhere in the function.
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if key, name, c, _ := fc.classify(x); c == cmpNegReject {
				fc.state(key, name).negRejected = true
			}
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if key, name, ok := fc.resolveKey(lhs); ok && fc.clampsToZero(x) {
					fc.state(key, name).negRejected = true
				}
			}
		}
		return true
	})

	// Pass 2: the SUBSTITUTION shape and zero-default recording, then
	// the no-expiry decisions (which read zeroDefault for the whole
	// function, so they run after it is complete).
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if ifst, ok := n.(*ast.IfStmt); ok {
			checkIf(fc, ifst)
		}
		return true
	})
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.KeyValueExpr:
			checkKeyValue(fc, x)
		case *ast.IfStmt:
			checkDecisionIf(fc, x)
		}
		return true
	})
}

// newFuncCtx builds one function's resolution context. The RECEIVER
// is deliberately absent from params: receiver fields are developer
// configuration (the (c *AuthConfig).defaults posture), not
// caller-supplied values.
func newFuncCtx(pass *analysis.Pass, fn *ast.FuncDecl, duration types.Type, pkg *pkgCtx) *funcCtx {
	fc := &funcCtx{
		pass:       pass,
		fn:         fn,
		params:     map[types.Object]bool{},
		paramIndex: map[string]int{},
		duration:   duration,
		states:     map[string]*dstate{},
		allBound:   allBindings(pass, fn.Body),
		pkg:        pkg,
	}
	idx := 0
	if fn.Type.Params != nil {
		for _, f := range fn.Type.Params.List {
			for _, name := range f.Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
					fc.params[obj] = true
					fc.paramIndex[fmt.Sprintf("%p", obj)] = idx
				}
				idx++
			}
			if len(f.Names) == 0 {
				idx++ // unnamed parameter still occupies a position
			}
		}
	}
	return fc
}

// paramRejects records, per parameter position, the field paths on
// which fn rejects negatives (a `d < 0`-class comparison) or clamps
// to zero — the evidence the bare-decision posture honors when a
// caller hands the same value to fn. Rooted at parameters only: what
// fn does to its own receiver or config fields says nothing about
// the caller's request data.
func paramRejects(pass *analysis.Pass, fn *ast.FuncDecl, duration types.Type) map[int]map[string]bool {
	fc := newFuncCtx(pass, fn, duration, nil)
	out := map[int]map[string]bool{}
	record := func(e ast.Expr) {
		root, suffix, ok := fc.rootOfBase(e, 0)
		if !ok {
			return
		}
		idx, ok := fc.paramIndex[root]
		if !ok {
			return
		}
		if out[idx] == nil {
			out[idx] = map[string]bool{}
		}
		out[idx][suffix] = true
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if _, _, c, dur := fc.classify(x); c == cmpNegReject && dur != nil {
				record(dur)
			}
		case *ast.AssignStmt:
			if fc.clampsToZero(x) {
				for _, lhs := range x.Lhs {
					record(lhs)
				}
			}
		}
		return true
	})
	return out
}

// checkDecisionIf fires the NO-EXPIRY DECISION shapes in their
// statement spelling: `if d > 0 { e.expiresAt = ... }` — the else arm
// is "no expiry", and a negative lands in it.
func checkDecisionIf(fc *funcCtx, ifst *ast.IfStmt) {
	key, name, dur := "", "", ast.Expr(nil)
	ast.Inspect(ifst.Cond, func(n ast.Node) bool {
		if be, ok := n.(*ast.BinaryExpr); ok {
			if k, nm, c, d := fc.classify(be); c == cmpPositive {
				key, name, dur = k, nm, d
			}
		}
		return true
	})
	if key == "" || !fc.bodyArmsExpiry(ifst.Body, key) {
		return
	}
	fc.reportDecision(key, name, dur, ifst.Pos())
}

// bodyArmsExpiry reports whether the guarded body arms expiry off the
// subject: an assignment to an expiry-named target, or a
// time.After/.Add call that takes the subject itself as an argument.
func (fc *funcCtx) bodyArmsExpiry(body *ast.BlockStmt, subjKey string) bool {
	armed := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if armExpiryRe.MatchString(exprName(lhs)) {
					armed = true
				}
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "After" || sel.Sel.Name == "Add") {
				for _, arg := range x.Args {
					if k, _, ok := fc.resolveKey(arg); ok && k == subjKey {
						armed = true
					}
				}
			}
		}
		return true
	})
	return armed
}

// checkIf fires the SUBSTITUTION shape and records zero-defaulting.
func checkIf(fc *funcCtx, ifst *ast.IfStmt) {
	zeroKey, zeroName := "", ""
	eqKey, eqName := "", ""
	ast.Inspect(ifst.Cond, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		key, name, c, _ := fc.classify(be)
		switch c {
		case cmpZeroOrLess:
			zeroKey, zeroName = key, name
		case cmpEqualZero:
			eqKey, eqName = key, name
		}
		return true
	})
	if eqKey != "" {
		if substInBody(fc, ifst.Body, eqKey) == substExtending {
			fc.state(eqKey, eqName).zeroDefault = true
		}
	}
	if zeroKey == "" {
		return
	}
	if substInBody(fc, ifst.Body, zeroKey) != substExtending {
		return
	}
	fc.state(zeroKey, zeroName).zeroDefault = true
	if st := fc.state(zeroKey, zeroName); !st.negRejected {
		fc.pass.Reportf(ifst.Pos(),
			"%s <= 0 folds a NEGATIVE duration onto the default arm: a caller asking for -N silently gets the strongest setting instead of the weakest; reject %s < 0 (or clamp to zero/expired) before defaulting",
			st.name, st.name)
	}
}

// checkKeyValue fires the NO-EXPIRY DECISION shapes in their
// composite-literal spelling: `hasExpiry: d > 0`.
func checkKeyValue(fc *funcCtx, kv *ast.KeyValueExpr) {
	be, ok := kv.Value.(*ast.BinaryExpr)
	if !ok || !expiryRe.MatchString(exprName(kv.Key)) {
		return
	}
	key, name, c, dur := fc.classify(be)
	if c != cmpPositive {
		return
	}
	fc.reportDecision(key, name, dur, be.Pos())
}

// reportDecision fires the NO-EXPIRY DECISION: with a zero-default in
// the function (the memory.go spelling) or bare, with no rejection or
// clamp in scope (the reviewer's mutated-apitoken proof). A rejection
// delegated to a package-local validator called with the same value
// counts as in scope and stays quiet (the unmutated IssueToken).
func (fc *funcCtx) reportDecision(key, name string, dur ast.Expr, pos token.Pos) {
	st := fc.state(key, name)
	if st.negRejected {
		return
	}
	if root, suffix, ok := fc.rootOfBase(dur, 0); ok && fc.delegatedReject(root, suffix) {
		return
	}
	if st.zeroDefault {
		fc.pass.Reportf(pos,
			"%s > 0 treats a NEGATIVE duration as no-expiry while 0 means default: a negative silently means forever; reject %s < 0 (or clamp to zero/expired) before the decision",
			name, name)
		return
	}
	fc.pass.Reportf(pos,
		"%s > 0 arms expiry with no negative rejection in scope: a NEGATIVE duration silently means no-expiry (forever); reject %s < 0 (or clamp to zero/expired) before the decision",
		name, name)
}

// delegatedReject reports whether the function hands the value rooted
// at (root, suffix) to a package-local validator whose own body
// rejects negatives on or clamps exactly that value: the helper call
// puts the rejection in scope (IssueToken → validateTokenSpec). One
// call level, positional arguments, identifier-called package
// functions.
func (fc *funcCtx) delegatedReject(root, suffix string) bool {
	if fc.pkg == nil || root == "" {
		return false
	}
	validated := false
	ast.Inspect(fc.fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		obj := fc.pass.TypesInfo.ObjectOf(id)
		if obj == nil {
			return true
		}
		g, ok := fc.pkg.funcByObj[obj]
		if !ok {
			return true
		}
		rejects := fc.pkg.rejectsOf[g]
		for j, arg := range call.Args {
			r, s, ok := fc.rootOfBase(arg, 0)
			if !ok || r != root || !strings.HasPrefix(suffix, s) {
				continue
			}
			if rejects[j][strings.TrimPrefix(suffix, s)] {
				validated = true
			}
		}
		return true
	})
	return validated
}

// substKind classifies what an if-arm substitutes onto the value.
type substKind int

const (
	substNone      substKind = iota
	substExtending           // a positive duration constant or a default-named value
	substOther               // zero, a sentinel, or anything unproven
)

// substInBody reports what body assigns onto the value.
func substInBody(fc *funcCtx, body *ast.BlockStmt, key string) substKind {
	kind := substNone
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range st.Lhs {
			if i >= len(st.Rhs) {
				continue
			}
			if k, _, ok := fc.resolveKey(lhs); ok && k == key {
				switch fc.substKind(st.Rhs[i]) {
				case substExtending:
					kind = substExtending
				case substOther:
					if kind == substNone {
						kind = substOther
					}
				}
			}
		}
		return true
	})
	return kind
}

// funcCtx carries one function's resolution context.
type funcCtx struct {
	pass       *analysis.Pass
	fn         *ast.FuncDecl
	params     map[types.Object]bool
	paramIndex map[string]int // param root key → position
	duration   types.Type
	states     map[string]*dstate
	allBound   map[types.Object][]ast.Expr
	pkg        *pkgCtx
}

func (fc *funcCtx) state(key, name string) *dstate {
	st, ok := fc.states[key]
	if !ok {
		st = &dstate{}
		fc.states[key] = st
	}
	if name != "" {
		st.name = name
	}
	return st
}

// classify reports the comparison class of be when one side is a
// caller-supplied duration and the other an integer constant, with
// the value's key, spelling, and the duration-side expression.
func (fc *funcCtx) classify(be *ast.BinaryExpr) (key, name string, c cmpclass, dur ast.Expr) {
	if v, ok := constInt(fc.pass, be.Y); ok {
		if k, n, ok := fc.resolveKey(be.X); ok {
			return k, n, classifyOp(be.Op, v), be.X
		}
	}
	if v, ok := constInt(fc.pass, be.X); ok {
		if k, n, ok := fc.resolveKey(be.Y); ok {
			return k, n, classifyOp(reverse(be.Op), v), be.Y
		}
	}
	return "", "", cmpNone, nil
}

func classifyOp(op token.Token, v int64) cmpclass {
	switch op {
	case token.LEQ:
		if v == 0 {
			return cmpZeroOrLess
		}
		if v < 0 {
			return cmpNegReject
		}
	case token.LSS:
		if v == 1 {
			return cmpZeroOrLess
		}
		if v <= 0 {
			return cmpNegReject
		}
	case token.EQL:
		if v == 0 {
			return cmpEqualZero
		}
	case token.GTR:
		if v == 0 {
			return cmpPositive
		}
	case token.GEQ:
		if v == 1 {
			return cmpPositive
		}
		if v == 0 {
			return cmpNegReject
		}
	case token.NEQ:
		if v == 0 {
			return cmpPositive
		}
	}
	return cmpNone
}

func reverse(op token.Token) token.Token {
	switch op {
	case token.LEQ:
		return token.GEQ
	case token.GEQ:
		return token.LEQ
	case token.LSS:
		return token.GTR
	case token.GTR:
		return token.LSS
	}
	return op
}

// substKind classifies a substituted value: only a positive duration
// constant or a default-named value extends a negative lifetime.
func (fc *funcCtx) substKind(e ast.Expr) substKind {
	if v, ok := constInt(fc.pass, e); ok {
		if v > 0 {
			return substExtending
		}
		return substOther // zero (or negative): no lifetime extension
	}
	switch x := e.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		n := exprName(x)
		if defaultRe.MatchString(n) {
			if !sentinelRe.MatchString(n) {
				return substExtending
			}
		}
		if sentinelRe.MatchString(n) {
			return substOther
		}
	case *ast.CallExpr:
		n := exprName(x.Fun)
		if defaultRe.MatchString(n) && !sentinelRe.MatchString(n) {
			return substExtending
		}
	case *ast.BinaryExpr:
		// Constant arithmetic like 7 * 24 * time.Hour.
		if v, ok := constInt(fc.pass, e); ok && v > 0 {
			return substExtending
		}
	}
	return substOther
}

// clampsToZero reports whether st is `x = max(x, 0)` (builtin max).
func (fc *funcCtx) clampsToZero(st *ast.AssignStmt) bool {
	if len(st.Lhs) != 1 || len(st.Rhs) != 1 {
		return false
	}
	call, ok := st.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "max" || len(call.Args) != 2 {
		return false
	}
	zero := func(e ast.Expr) bool {
		return fc.isDuration(e) && constIsZero(fc.pass, e)
	}
	dur := func(e ast.Expr) bool {
		_, _, ok := fc.resolveKey(e)
		return ok
	}
	return (dur(call.Args[0]) && zero(call.Args[1])) || (zero(call.Args[0]) && dur(call.Args[1]))
}

func (fc *funcCtx) resolveKey(e ast.Expr) (string, string, bool) {
	return fc.resolve(e, 0)
}

// resolve resolves e to the key of a caller-supplied duration value: a
// duration parameter, a duration field of a parameter (through
// non-configuration selector hops), or a local derived from either. A
// local is keyed by ITS OWN object (its uses and its re-assignments
// share one identity), and counts as caller-supplied when ANY of its
// bindings resolves to one.
func (fc *funcCtx) resolve(e ast.Expr, depth int) (string, string, bool) {
	if depth > 6 {
		return "", "", false
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return fc.resolve(x.X, depth+1)
	case *ast.Ident:
		if !fc.isDuration(x) {
			return "", "", false
		}
		obj := fc.pass.TypesInfo.ObjectOf(x)
		if obj == nil {
			return "", "", false
		}
		key := fmt.Sprintf("%p", obj)
		if fc.params[obj] {
			return key, x.Name, true
		}
		if _, isVar := obj.(*types.Var); isVar {
			for _, b := range fc.allBound[obj] {
				if _, _, ok := fc.resolve(b, depth+1); ok {
					return key, x.Name, true
				}
			}
		}
		return "", "", false
	case *ast.SelectorExpr:
		// The SELECTED field is the duration; its base only needs to
		// be caller-supplied (cfg.TokenTTL where cfg is a param) —
		// but a base whose type is configuration-named is developer
		// configuration, not caller data.
		if !fc.isDuration(x) {
			return "", "", false
		}
		if fc.isConfigTyped(x.X) {
			return "", "", false
		}
		if r, s, ok := fc.rootOfBase(x.X, depth+1); ok {
			return r + s, x.Sel.Name, true
		}
		return "", "", false
	}
	return "", "", false
}

// rootOfBase resolves e to (param-root key, field-path suffix): the
// parameter the value flows from and the selector path past it, with
// locals unwrapped to their bindings. The RECEIVER is not a root —
// receiver fields are developer configuration — and no selector hop
// may read off a configuration-named type.
func (fc *funcCtx) rootOfBase(e ast.Expr, depth int) (string, string, bool) {
	if depth > 6 {
		return "", "", false
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return fc.rootOfBase(x.X, depth+1)
	case *ast.Ident:
		obj := fc.pass.TypesInfo.ObjectOf(x)
		if obj == nil {
			return "", "", false
		}
		key := fmt.Sprintf("%p", obj)
		if fc.params[obj] {
			return key, "", true
		}
		if _, isVar := obj.(*types.Var); isVar {
			for _, b := range fc.allBound[obj] {
				if r, s, ok := fc.rootOfBase(b, depth+1); ok {
					return r, s, true
				}
			}
		}
		return "", "", false
	case *ast.SelectorExpr:
		if fc.isConfigTyped(x.X) {
			return "", "", false
		}
		if r, s, ok := fc.rootOfBase(x.X, depth+1); ok {
			return r, s + "." + x.Sel.Name, true
		}
		return "", "", false
	}
	return "", "", false
}

// isConfigTyped reports whether e's type is (a pointer to) a named
// type from the developer-configuration vocabulary.
func (fc *funcCtx) isConfigTyped(e ast.Expr) bool {
	t := fc.pass.TypesInfo.TypeOf(e)
	for {
		p, ok := t.(*types.Pointer)
		if !ok {
			break
		}
		t = p.Elem()
	}
	switch n := t.(type) {
	case *types.Named:
		return configRe.MatchString(n.Obj().Name())
	case *types.Alias:
		return configRe.MatchString(n.Obj().Name())
	}
	return false
}

func (fc *funcCtx) isDuration(e ast.Expr) bool {
	t := fc.pass.TypesInfo.TypeOf(e)
	return t != nil && types.Identical(t, fc.duration)
}

// durationType returns time.Duration from the package's imports.
func durationType(pass *analysis.Pass) (types.Type, bool) {
	for _, imp := range pass.Pkg.Imports() {
		if imp.Name() == "time" {
			if obj := imp.Scope().Lookup("Duration"); obj != nil {
				return obj.Type(), true
			}
		}
	}
	return nil, false
}

// allBindings maps each local to EVERY expression it was bound to
// (single- and multi-value assignments); a local rebound in an arm
// keeps both.
func allBindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object][]ast.Expr {
	m := map[types.Object][]ast.Expr{}
	add := func(id *ast.Ident, e ast.Expr) {
		if id == nil || id.Name == "_" {
			return
		}
		if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
			m[obj] = append(m[obj], e)
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		switch {
		case len(st.Lhs) == len(st.Rhs):
			for i, lhs := range st.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					add(id, st.Rhs[i])
				}
			}
		case len(st.Lhs) == 2 && len(st.Rhs) == 1: // v, err := call()
			if id, ok := st.Lhs[0].(*ast.Ident); ok {
				add(id, st.Rhs[0])
			}
		}
		return true
	})
	return m
}

// constInt returns the constant integer value of e, if it has one.
func constInt(pass *analysis.Pass, e ast.Expr) (int64, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil {
		return 0, false
	}
	switch tv.Value.Kind() {
	case constant.Int:
		return constant.Int64Val(tv.Value)
	case constant.Float:
		f, _ := constant.Float64Val(tv.Value)
		return int64(f), true
	}
	return 0, false
}

func constIsZero(pass *analysis.Pass, e ast.Expr) bool {
	v, ok := constInt(pass, e)
	return ok && v == 0
}

// exprName renders the name-bearing token of e for the name regexes.
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.StarExpr:
		return exprName(x.X)
	case *ast.CallExpr:
		return exprName(x.Fun)
	case *ast.ParenExpr:
		return exprName(x.X)
	}
	return ""
}
