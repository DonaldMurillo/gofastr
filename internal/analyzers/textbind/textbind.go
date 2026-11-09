// Package textbind catches request text bound into a SQL text parameter
// with no check that Postgres can store it.
//
// Postgres refuses a text bind holding a NUL byte or invalid UTF-8 with
// SQLSTATE 22021, so the request answers 500 (or 503 where the store is
// a dependency) while SQLite compares the bytes and answers 200. Query
// strings, headers, form values and path values all arrive
// percent-decoded: ?q=a%00b and ?q=a%FFb are real NUL and 0xff bytes by
// the time a handler reads them. The 2026-11-01 hunt fixed the shape
// once per surface:
//
//   - 145b37ab: ?q= reached filter.SearchConditions, whose LIKE args
//     bound the term as is (a 500 on Postgres, an empty 200 on SQLite);
//     SearchConditions now refuses it with storableText;
//   - 39de7827: an Idempotency-Key holding 0xff reached
//     SQLIdempotencyStore.Begin through the IdempotencyStore interface
//     and answered 503 "store down"; the middleware now refuses a key
//     outside printable ASCII (printableASCII);
//   - 1de1c216, 155cafcd: a nested ?rel.f_like= value and a CoerceValue
//     argument, and d6167d1a: an idempotency principal. The first two
//     flowed through struct fields (nestedFilter.Value, ParsedFilter
//     .Value) and back out through the declaring package's method
//     (BindValue), which the field and return-field arms below follow;
//     the third through a host callback (IdempotencyConfig.Principal),
//     which the callback arm follows.
//   - R8-TX-04: a saved view's name and filter reached the admin
//     store's INSERT through entityui.SavedView's fields, found by the
//     field arm on its first tree run.
//
// Sources: (*http.Request).FormValue/PostFormValue/PathValue/Referer/
// UserAgent/Cookie/Cookies, r.URL.Query() (on the literal r.URL or a
// local bound to it), r.Form and r.PostForm, r.Header (Get, Values,
// index), the Value field of a cookie the request supplied, and
// anything read out of them: url.Values Get and index, slices,
// concatenations, conversions, and the string results of calls handed
// them.
//
// Sinks, the value positions of a SQL bind: the variadic args of
// Exec/Query/QueryRow (and their Context forms) whose query string comes
// first (database/sql and the repo's db.Executor alike); the args of a
// Where/OrWhere/Having(cond string, args ...any) builder method (core/
// query, and a generic constraint spelling it); UpdateBuilder.Set's
// value and InsertBuilder.Values; and the Args field of a struct literal
// that carries a SQL string beside it (filter.Condition, hook
// .WhereClause).
//
// Struct fields carry request text too. A write fills a field when a
// struct literal (keyed or positional) or an assignment `v.f = x` stores
// request text in it, directly or through a parameter its callers hand
// request text (a constructor newEntry(r.PathValue("k"))). A read of a
// filled field is request text, in this package and, through an
// exported fact, in every package that imports it. The arm is
// field-wide, not per object: one unchecked write taints every read of
// the field. A check on the read is not credited — check before the
// value is stored, so the field never holds unvetted bytes. Field
// writes count only after the checks have converged, so a write vetted
// by a helper declared later in the file stays quiet.
//
// Flow crosses functions through per-function summaries, computed to a
// fixed point inside the package and exported as facts so callers in
// other packages see them: which parameters reach a sink unchecked
// (filter.SearchConditions' term, before 145b37ab), which reach the
// string result (a key builder like idemStoreKey), which the function
// refuses when they are not storable text, and whether the result is
// request text itself. A call through an interface method takes the
// union of the same-package types that implement it, so the middleware's
// cfg.Store.Begin(ctx, storeKey, fp) sees SQLIdempotencyStore.Begin.
// A method's summary also names the receiver fields its result hands
// back (RetFields): a call to it reads those fields on its own
// receiver, so a caller that filled one with request text — even
// though the declaring package never wrote it — taints the result
// (crud fills ParsedFilter.Value and binds f.BindValue()).
//
// Host callbacks carry request text: a call through a func-typed
// struct field (d6167d1a's IdempotencyConfig.Principal, whose result
// idemStoreKey folded into SQLIdempotencyStore.Begin's key bind). With
// fill sites in this package the call unions their summaries, named
// function or literal, field-wide like the data-field arm (one
// unchecked fill taints every call through the field). With no fill
// here, a field this package declares whose callback takes an
// *http.Request and returns a stringish value is read as request text:
// the host installs it to read the request, and the declaring package
// is where its result binds — that is the arm watching the Begin key
// check itself.
//
// strings.Builder output carries what was written into it:
// WriteString/Write add their argument's bytes to the builder's
// content; WriteByte/WriteRune follow indexed bytes and range values.
// fmt.Fprint/Fprintf/Fprintln and io.WriteString add their arguments.
// Reset drops content only when it dominates the String read.
//
// Silent postures:
//   - a dominating storable-text check on the value, credited by what
//     the checking function's body does, never by its name: a
//     utf8.Valid/ValidString of the parameter together with a NUL test
//     of it (strings.IndexByte(s, 0) < 0, ContainsRune(s, 0), a "\x00"
//     substring test, or a byte compared against 0), or a byte or rune
//     walk of the parameter that refuses everything past ASCII and the
//     control range (39de7827's printableASCII: c < 0x20 || c > 0x7e),
//     or a call that hands the parameter to such a function (filter
//     .CoerceValue delegates to storableText). The check dominates when
//     it sits in an enclosing if's condition or in a statement before
//     the use whose verdict is kept AND gated — a diverging
//     `if err != nil { return }`, an enclosing if on the verdict; a
//     verdict only logged or passed on vets nothing. A function's Check
//     fact (the credit its callers see) holds only when no bind of the
//     parameter escapes its check: a body that binds first and
//     validates afterwards is a Sink, not a Check.
//   - a value compared against an allow-list first: an enclosing if
//     that tests it == a constant or asserts its membership in a map or
//     a slices.Contains list; a diverging if that tests it != a constant
//     or denies membership (`if !allowed[v] { return }`); the comma-ok
//     lookup `p, ok := providers[v]` with `ok` tested by a diverging or
//     enclosing if (OAuth2Plugin's callback, the one live site this
//     posture quieted); or a switch on it whose constant case holds the
//     use. A dedup set is not an allow-list: `!seen[t]` around the use
//     runs exactly for the values the set has never vetted;
//   - a value parsed to a non-string type (strconv.Atoi, uuid.Parse,
//     time.Parse): only strings, byte slices, string slices, url.Values
//     and interface values carry the bytes;
//   - an encoder whose output is ASCII (encoding/hex, base64, base32,
//     strconv.Quote*/Itoa/Format*, url.QueryEscape/PathEscape) and a
//     hash of the value;
//   - the SQL string itself (a column or table name is not a bind);
//   - _test.go files.
//
// The first whole-tree run found the shape in five handlers, each a
// path value bound raw (a 500 on Postgres for %FF): crud's record routes
// (PathRecordID), battery/auth's unlink and token revoke, battery/admin's
// queue replay and battery/semantic's DELETE /doc. Each now refuses the
// value before any query.
//
// Residuals, stated so nobody assumes coverage: a method value or call
// through an interface whose implementation lives in another package
// (battery/admin's grant grid binds grantChange.role through
// access.GrantStore, R8-AA-01), a constructor in another package that
// stores its parameter in a field (other.New(r.FormValue("k")).Key: a
// function's parameter-to-field writes are not exported), a host
// callback filled in another package than the field's declaring
// package (framework/app.go installs ownerPrincipal on the middleware's
// IdempotencyConfig from a dependent, so the declaring package's
// no-fill posture stands in for the host's fill), and a builder local
// held somewhere other than a plain local (a field, a parameter).
//
// The fix spelling: refuse the value before it binds, with a 400 naming
// the field (filter's storableText: utf8.ValidString(s) &&
// strings.IndexByte(s, 0) < 0), or narrow it to the bytes the field may
// hold (middleware's printableASCII for a client-minted key).
package textbind

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
)

var Analyzer = &analysis.Analyzer{
	Name:      "textbind",
	Doc:       "report request text bound into a SQL parameter with no utf8/NUL check: Postgres refuses it with 22021 (a 500) where SQLite answers 200",
	Run:       run,
	FactTypes: []analysis.Fact{(*summary)(nil), (*fieldFact)(nil)},
}

// fieldFact marks a struct field some write fills with request text, so
// a read of it in an importing package is request text too.
type fieldFact struct{}

func (*fieldFact) AFact()         {}
func (*fieldFact) String() string { return "request text" }

// paramField is a write of a function's parameters (mask) into a field:
// a call handing request text in one of those positions fills it.
type paramField struct {
	mask  uint64
	field *types.Var
}

// summary is what a caller needs to know about a function: which
// parameters reach a SQL bind unchecked (Sink), which reach the string
// result (Return), which it refuses when they are not storable text
// (Check), whether its result is request text (Source), and — for a
// method — which stringish fields of its receiver its result hands
// back (RetFields): the caller may have marked one of them with request
// text even though this package never did (filter.ParsedFilter
// .BindValue returning Value, which framework/crud fills; 1de1c216,
// 155cafcd). Parameter indices exclude the receiver.
type summary struct {
	Sink, Return, Check uint64
	Source              bool
	RetFields           []string
}

func (*summary) AFact() {}

func (s *summary) String() string {
	return fmt.Sprintf("textbind(sink=%b return=%b check=%b source=%v retfields=%v)", s.Sink, s.Return, s.Check, s.Source, s.RetFields)
}

func (s summary) empty() bool {
	return s.Sink == 0 && s.Return == 0 && s.Check == 0 && !s.Source && len(s.RetFields) == 0
}

func (s summary) same(o summary) bool {
	return s.Sink == o.Sink && s.Return == o.Return && s.Check == o.Check && s.Source == o.Source &&
		slices.Equal(s.RetFields, o.RetFields)
}

// merge grows s with the facts of u (both fixed points only grow, so a
// plain union ends).
func (s *summary) merge(u summary) {
	s.Sink |= u.Sink
	s.Return |= u.Return
	s.Source = s.Source || u.Source
	for _, name := range u.RetFields {
		if !slices.Contains(s.RetFields, name) {
			s.RetFields = append(s.RetFields, name)
		}
	}
}

// taint is where a value's bytes came from: the request (src) and the
// enclosing function's parameters (params, a bit per index).
type taint struct {
	src    bool
	params uint64
}

func (t taint) or(u taint) taint  { return taint{t.src || u.src, t.params | u.params} }
func (t taint) clean() bool       { return !t.src && t.params == 0 }
func bit(i int) uint64            { return 1 << uint(i&63) }
func has(mask uint64, i int) bool { return mask&bit(i) != 0 }

const msg = "request text reaches a SQL bind%s with no storable-text check: invalid UTF-8 or a NUL byte is Postgres's 22021 (a 500 where SQLite answers 200); refuse it first with a 400 (utf8.ValidString(s) && strings.IndexByte(s, 0) < 0, filter's storableText)"

type checker struct {
	pass    *analysis.Pass
	local   map[*types.Func]*summary
	decls   map[*types.Func]*ast.FuncDecl
	allowed func(token.Pos) bool
	ifaces  map[*types.Func][]*types.Func // interface method -> same-package implementations

	// fields holds the package's struct fields some write fills with
	// request text; paramFields the writes of a function's parameters
	// into a field, resolved at its call sites. Both only grow, and a
	// new entry runs another round of the flow fixed point.
	fields      map[*types.Var]bool
	paramFields map[*types.Func][]paramField
	grew        bool
	// flows is set once the checks have converged. Field writes count
	// only then: a write judged before its check's callee was summarised
	// would mark the field for good.
	flows bool

	// fills maps a func-typed field to the functions and literals this
	// package assigns to it (a host callback's fill sites), scanned
	// once before the fixed points; lits are the literals among them,
	// summarised in every round like a declared function; litSums is
	// the running summary of each literal.
	fills   map[*types.Var][]fillSite
	lits    []*ast.FuncLit
	litSums map[*ast.FuncLit]*summary
}

// fillSite is one assignment of a function or literal to a func-typed
// field.
type fillSite struct {
	fn  *types.Func
	lit *ast.FuncLit
}

// markField records that field holds request text. A field another
// package declares is marked too, for reads in this package; only the
// declaring package exports the fact.
func (c *checker) markField(field *types.Var) {
	field = field.Origin()
	if field.Pkg() == nil || c.fields[field] {
		return
	}
	c.fields[field] = true
	c.grew = true
}

// addParamField records that fn writes the parameters in mask into field.
func (c *checker) addParamField(fn *types.Func, mask uint64, field *types.Var) {
	field = field.Origin()
	for i, pf := range c.paramFields[fn] {
		if pf.field == field {
			if pf.mask|mask != pf.mask {
				c.paramFields[fn][i].mask |= mask
				c.grew = true
			}
			return
		}
	}
	c.paramFields[fn] = append(c.paramFields[fn], paramField{mask, field})
	c.grew = true
}

// fieldHolds reports whether field carries request text: marked in this
// package, or by a fact from the package that declares it.
func (c *checker) fieldHolds(field *types.Var) bool {
	field = field.Origin()
	if c.fields[field] || field.Pkg() == c.pass.Pkg {
		return c.fields[field]
	}
	if field.Pkg() == nil {
		return false
	}
	return c.pass.ImportObjectFact(field, new(fieldFact))
}

// scanFills indexes every assignment of a function or function
// literal to a func-typed field — a host callback's fill sites — once,
// before the fixed points: the literals among them are summarised
// every round like a declared function, and a call through the field
// uses their summaries. _test.go files are skipped like the decls.
func (c *checker) scanFills() {
	info := c.pass.TypesInfo
	for _, f := range c.pass.Files {
		if strings.HasSuffix(c.pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.AssignStmt:
				if len(x.Lhs) != len(x.Rhs) {
					return true
				}
				for i, l := range x.Lhs {
					if fv, ok := c.fillField(l); ok {
						c.recordFill(fv, x.Rhs[i])
					}
				}
			case *ast.CompositeLit:
				for _, el := range x.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					id, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					if fv, ok := info.Uses[id].(*types.Var); ok && fv.IsField() && isFuncField(fv) {
						c.recordFill(fv.Origin(), kv.Value)
					}
				}
			}
			return true
		})
	}
}

// fillField resolves the func-typed field an assignment writes.
func (c *checker) fillField(l ast.Expr) (*types.Var, bool) {
	sx, ok := ast.Unparen(l).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	sel, ok := c.pass.TypesInfo.Selections[sx]
	if !ok || sel.Kind() != types.FieldVal {
		return nil, false
	}
	fv, ok := sel.Obj().(*types.Var)
	if !ok || !isFuncField(fv) {
		return nil, false
	}
	return fv.Origin(), true
}

func isFuncField(fv *types.Var) bool {
	_, ok := fv.Type().Underlying().(*types.Signature)
	return ok
}

func (c *checker) recordFill(fv *types.Var, rhs ast.Expr) {
	switch v := ast.Unparen(rhs).(type) {
	case *ast.FuncLit:
		c.fills[fv] = append(c.fills[fv], fillSite{lit: v})
		if !slices.ContainsFunc(c.lits, func(l *ast.FuncLit) bool { return l == v }) {
			c.lits = append(c.lits, v)
		}
	case *ast.Ident:
		if fn, ok := c.pass.TypesInfo.Uses[v].(*types.Func); ok {
			c.fills[fv] = append(c.fills[fv], fillSite{fn: fn.Origin()})
		}
	case *ast.SelectorExpr:
		if fn, ok := c.pass.TypesInfo.Uses[v.Sel].(*types.Func); ok {
			c.fills[fv] = append(c.fills[fv], fillSite{fn: fn.Origin()})
		}
	}
}

func run(pass *analysis.Pass) (any, error) {
	c := &checker{
		pass:    pass,
		local:   map[*types.Func]*summary{},
		decls:   map[*types.Func]*ast.FuncDecl{},
		allowed: allow.Allowed(pass, "textbind"),
		ifaces:  map[*types.Func][]*types.Func{},

		fields:      map[*types.Var]bool{},
		paramFields: map[*types.Func][]paramField{},
		fills:       map[*types.Var][]fillSite{},
		litSums:     map[*ast.FuncLit]*summary{},
	}
	var order []*types.Func
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			c.decls[fn] = fd
			c.local[fn] = &summary{}
			order = append(order, fn)
		}
	}
	c.scanFills()

	// Two fixed points. The checks first: whether a function refuses
	// unstorable text depends only on its own body and the checks it
	// calls. Then the flows, with every check known: a check clears
	// taint, so computing both at once would let a caller summarised
	// before its check's callee keep a taint the check removes. Each
	// field only grows within its phase and holds 64 bits, so both
	// terminate; the cap is a guard against a bug. The fill literals
	// ride the same rounds: their summaries feed the call sites that
	// invoke the field.
	merge := func(old *summary, s summary, flows bool) bool {
		merged := *old
		if flows {
			merged.merge(s)
			// A bind through a same-package helper is known only now:
			// a check after it vets nothing, so it loses the credit
			// phase 1 gave it. Check only shrinks here while Sink
			// grows, so this ends.
			merged.Check &^= merged.Sink
		} else {
			merged.Check |= s.Check
		}
		if merged.same(*old) {
			return false
		}
		*old = merged
		return true
	}
	for _, flows := range []bool{false, true} {
		c.flows = flows
		for round := 0; round < 32; round++ {
			c.grew = false
			changed := false
			for _, fn := range order {
				if merge(c.local[fn], c.summarize(fn, c.decls[fn], nil), flows) {
					changed = true
				}
			}
			for _, lit := range c.lits {
				if _, ok := c.litSums[lit]; !ok {
					c.litSums[lit] = &summary{}
				}
				if merge(c.litSums[lit], c.summarizeLit(lit, nil), flows) {
					changed = true
				}
			}
			if !changed && !c.grew {
				break
			}
		}
	}

	report := func(pos token.Pos, via string) { pass.Reportf(pos, msg, via) }
	for _, fn := range order {
		c.summarize(fn, c.decls[fn], report)
	}

	// The standard library's bodies are not summarised: the defaults
	// in throughCall stand for them.
	if !isStdlib(pass) {
		for _, fn := range order {
			if s := c.local[fn]; !s.empty() {
				cp := *s
				pass.ExportObjectFact(fn, &cp)
			}
		}
		for field := range c.fields {
			if field.Pkg() != pass.Pkg {
				continue
			}
			pass.ExportObjectFact(field, &fieldFact{})
		}
	}
	return nil, nil
}

// isStdlib reports whether the package is the standard library's: an
// import path with no dot in its first element, outside a testdata
// tree (the fixtures share the spelling). Its bodies are summarised by
// the defaults in throughCall instead: strings.ToLower goes through a
// Builder; calling it a sanitiser without checking its bytes would
// clear every value it touches.
func isStdlib(pass *analysis.Pass) bool {
	if strings.Contains(strings.SplitN(pass.Pkg.Path(), "/", 2)[0], ".") {
		return false
	}
	for _, f := range pass.Files {
		return !strings.Contains(filepath.ToSlash(pass.Fset.Position(f.Pos()).Filename), "/testdata/")
	}
	return false
}

// fnState is one function body under evaluation. recv is the method
// receiver's object, set for declared methods only.
type fnState struct {
	c       *checker
	fn      *types.Func
	fd      *ast.FuncDecl
	recv    types.Object
	params  map[*types.Var]int
	results map[*types.Var]bool
	parents map[ast.Node]ast.Node
	defs    map[types.Object][]def
	resets  map[*types.Var][]ast.Node
	memo    map[ast.Node]taint
	busy    map[ast.Node]bool
	report  func(token.Pos, string)
	out     summary
}

// def is one assignment to a local: rhs is evaluated for its taint;
// rangeOf marks a range variable (the element of rangeOf); weak marks an
// element store (args[i] = v), which adds to the slice's taint.
type def struct {
	pos     token.Pos
	at      ast.Node
	rhs     ast.Expr
	rangeOf ast.Expr
}

func (c *checker) summarize(fn *types.Func, fd *ast.FuncDecl, report func(token.Pos, string)) summary {
	sig := fn.Type().(*types.Signature)
	st := c.newState(sig, fd.Body, report)
	st.fn = fn
	st.fd = fd
	if fd.Recv != nil && len(fd.Recv.List) > 0 && len(fd.Recv.List[0].Names) > 0 {
		st.recv = objOf(c.pass.TypesInfo, fd.Recv.List[0].Names[0])
	}
	st.collectDefs()

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		st.walk(n)
		return true
	})
	return st.finish()
}

// summarizeLit evaluates a function literal assigned to a func-typed
// field (a host callback fill site) the way summarize does a declared
// function, so the call through the field can use its summary. The
// literal's parameters are its own; it writes no param-field records
// (its call sites are the field's invocation, not a constructor call).
func (c *checker) summarizeLit(lit *ast.FuncLit, report func(token.Pos, string)) summary {
	sig, _ := c.pass.TypesInfo.Types[lit].Type.Underlying().(*types.Signature)
	if sig == nil {
		return summary{}
	}
	st := c.newState(sig, lit.Body, report)
	st.fd = &ast.FuncDecl{Type: lit.Type, Body: lit.Body}
	i := 0
	for _, field := range lit.Type.Params.List {
		for _, name := range field.Names {
			if v, ok := objOf(c.pass.TypesInfo, name).(*types.Var); ok {
				st.params[v] = i
			}
			i++
		}
		if len(field.Names) == 0 {
			i++
		}
	}
	st.collectDefs()

	ast.Inspect(lit.Body, func(n ast.Node) bool {
		st.walk(n)
		return true
	})
	return st.finish()
}

// walk is the shared body inspection of summarize and summarizeLit.
func (st *fnState) walk(n ast.Node) {
	switch x := n.(type) {
	case *ast.CallExpr:
		st.callSinks(x)
		st.callFields(x)
	case *ast.CompositeLit:
		st.literalSink(x)
		st.literalFields(x)
	case *ast.AssignStmt:
		st.assignFields(x)
	case *ast.ReturnStmt:
		if st.enclosingLit(x) {
			return
		}
		st.returnTaint(x)
	}
}

// finish computes the parameter checks after the walk, so a parameter
// whose bytes already reached a bind unchecked (the Sink bit) is not
// credited: a check that runs only after the parameter bound vets
// nothing.
func (st *fnState) finish() summary {
	for v, i := range st.params {
		if stringish(v.Type()) && st.bodyChecks(v) && !has(st.out.Sink, i) {
			st.out.Check |= bit(i)
		}
	}
	return st.out
}

func (c *checker) newState(sig *types.Signature, body ast.Node, report func(token.Pos, string)) *fnState {
	st := &fnState{
		c:       c,
		params:  map[*types.Var]int{},
		results: map[*types.Var]bool{},
		parents: dominance.Parents(body),
		defs:    map[types.Object][]def{},
		resets:  map[*types.Var][]ast.Node{},
		memo:    map[ast.Node]taint{},
		busy:    map[ast.Node]bool{},
		report:  report,
	}
	for i := 0; i < sig.Params().Len(); i++ {
		st.params[sig.Params().At(i)] = i
	}
	for i := 0; i < sig.Results().Len(); i++ {
		st.results[sig.Results().At(i)] = true
	}
	return st
}

// enclosingLit reports whether n sits in a function literal inside the
// declaration: its return is the literal's, not the function's.
func (st *fnState) enclosingLit(n ast.Node) bool {
	for p := st.parents[n]; p != nil; p = st.parents[p] {
		if _, ok := p.(*ast.FuncLit); ok {
			return true
		}
	}
	return false
}

func (st *fnState) returnTaint(ret *ast.ReturnStmt) {
	var t taint
	if len(ret.Results) == 0 {
		for v := range st.results {
			if stringish(v.Type()) {
				t = t.or(st.identTaint(v, ret))
			}
		}
	}
	for _, r := range ret.Results {
		tv, ok := st.c.pass.TypesInfo.Types[r]
		if !ok || !stringish(tv.Type) {
			continue
		}
		t = t.or(st.exprTaint(r))
		st.recordRetField(r)
	}
	st.out.Return |= t.params
	if t.src {
		st.out.Source = true
	}
}

// recordRetField notes a return of the receiver's own field, the
// BindValue shape: a method handing a field back to a package that
// filled it with request text. Only stringish fields matter; the name
// is resolved on the call site's receiver type when the summary is
// used.
func (st *fnState) recordRetField(r ast.Expr) {
	if st.recv == nil {
		return
	}
	sel, ok := ast.Unparen(r).(*ast.SelectorExpr)
	if !ok {
		return
	}
	if id, ok := ast.Unparen(sel.X).(*ast.Ident); !ok || objOf(st.c.pass.TypesInfo, id) != st.recv {
		return
	}
	s, ok := st.c.pass.TypesInfo.Selections[sel]
	if !ok || s.Kind() != types.FieldVal {
		return
	}
	if fv, ok := s.Obj().(*types.Var); ok && fv.IsField() && stringish(fv.Type()) {
		if !slices.Contains(st.out.RetFields, fv.Name()) {
			st.out.RetFields = append(st.out.RetFields, fv.Name())
		}
	}
}

// sink records a value reaching a SQL bind.
func (st *fnState) sink(e ast.Expr, via string) {
	t := st.exprTaint(e)
	if t.clean() || st.c.allowed(e.Pos()) {
		return
	}
	st.out.Sink |= t.params
	if t.src && st.report != nil {
		st.report(e.Pos(), via)
	}
}

var execNames = map[string]bool{
	"Exec": true, "ExecContext": true, "Query": true, "QueryContext": true,
	"QueryRow": true, "QueryRowContext": true,
}

var whereNames = map[string]bool{"Where": true, "OrWhere": true, "Having": true}

func (st *fnState) callSinks(call *ast.CallExpr) {
	pass := st.c.pass
	sel, isSel := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	sig, _ := typeOf(pass, call.Fun).(*types.Signature)
	if sig == nil {
		return
	}
	if isSel {
		name := sel.Sel.Name
		switch {
		case execNames[name] && sig.Variadic() && queryThenArgs(sig):
			for _, a := range call.Args[sig.Params().Len()-1:] {
				st.sink(a, "")
			}
			return
		case whereNames[name] && sig.Variadic() && sig.Params().Len() == 2 && isString(sig.Params().At(0).Type()):
			for _, a := range call.Args[1:] {
				st.sink(a, "")
			}
			return
		case name == "Set" && inQueryPkg(pass, sel) && len(call.Args) == 2:
			st.sink(call.Args[1], "")
			return
		case name == "Values" && inQueryPkg(pass, sel):
			for _, a := range call.Args {
				st.sink(a, "")
			}
			return
		}
	}
	// A call to a function whose summary says a parameter reaches a
	// bind: the argument in that position does.
	for _, callee := range st.c.callees(call) {
		s := st.c.summaryOf(callee)
		if s == nil || s.Sink == 0 {
			continue
		}
		csig := callee.Type().(*types.Signature)
		for i, a := range call.Args {
			pi := i
			if csig.Variadic() && pi >= csig.Params().Len()-1 {
				pi = csig.Params().Len() - 1
			}
			if has(s.Sink, pi) {
				st.sink(a, " through "+calleeLabel(callee, csig, pi))
			}
		}
	}
}

func calleeLabel(fn *types.Func, sig *types.Signature, i int) string {
	name := fn.Name()
	if recv := sig.Recv(); recv != nil {
		if n := namedOf(recv.Type()); n != nil {
			name = n.Obj().Name() + "." + name
		}
	} else if fn.Pkg() != nil {
		name = fn.Pkg().Name() + "." + name
	}
	if i < sig.Params().Len() {
		return name + " (param " + sig.Params().At(i).Name() + ")"
	}
	return name
}

// queryThenArgs: the signature's last parameter is ...any and the one
// before it is the query string.
func queryThenArgs(sig *types.Signature) bool {
	n := sig.Params().Len()
	if n < 2 || !isString(sig.Params().At(n-2).Type()) {
		return false
	}
	sl, ok := sig.Params().At(n - 1).Type().(*types.Slice)
	return ok && isEmptyInterface(sl.Elem())
}

func inQueryPkg(pass *analysis.Pass, sel *ast.SelectorExpr) bool {
	s := pass.TypesInfo.Selections[sel]
	if s == nil || s.Obj().Pkg() == nil {
		return false
	}
	p := s.Obj().Pkg().Path()
	return p == "github.com/DonaldMurillo/gofastr/core/query" || strings.HasSuffix(p, "/core/query")
}

// literalSink: the Args field of a struct that carries a SQL string
// beside it.
func (st *fnState) literalSink(lit *ast.CompositeLit) {
	t := typeOf(st.c.pass, lit)
	if t == nil {
		return
	}
	s, ok := t.Underlying().(*types.Struct)
	if !ok || !sqlArgsStruct(s) {
		return
	}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Args" {
			st.sink(kv.Value, "")
		}
	}
}

func sqlArgsStruct(s *types.Struct) bool {
	var sqlField, argsField bool
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		switch f.Name() {
		case "SQL":
			sqlField = isString(f.Type())
		case "Args":
			sl, ok := f.Type().(*types.Slice)
			argsField = ok && isEmptyInterface(sl.Elem())
		}
	}
	return sqlField && argsField
}

// callees resolves a call to the functions it may run: the static
// callee, or for an interface method every same-package type's method
// that implements it.
func (c *checker) callees(call *ast.CallExpr) []*types.Func {
	pass := c.pass
	var obj types.Object
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		obj = pass.TypesInfo.Uses[f]
	case *ast.SelectorExpr:
		obj = pass.TypesInfo.Uses[f.Sel]
	case *ast.IndexExpr: // generic instantiation
		if id, ok := f.X.(*ast.Ident); ok {
			obj = pass.TypesInfo.Uses[id]
		} else if s, ok := f.X.(*ast.SelectorExpr); ok {
			obj = pass.TypesInfo.Uses[s.Sel]
		}
	}
	fn, ok := obj.(*types.Func)
	if !ok {
		return nil
	}
	fn = fn.Origin()
	sig := fn.Type().(*types.Signature)
	if recv := sig.Recv(); recv != nil {
		if iface, ok := recv.Type().Underlying().(*types.Interface); ok {
			return c.implementations(fn, iface)
		}
	}
	return []*types.Func{fn}
}

func (c *checker) implementations(m *types.Func, iface *types.Interface) []*types.Func {
	if impls, ok := c.ifaces[m]; ok {
		return impls
	}
	var out []*types.Func
	scope := c.pass.Pkg.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		if _, isIface := tn.Type().Underlying().(*types.Interface); isIface {
			continue
		}
		for _, t := range []types.Type{tn.Type(), types.NewPointer(tn.Type())} {
			if !types.Implements(t, iface) {
				continue
			}
			obj, _, _ := types.LookupFieldOrMethod(t, true, c.pass.Pkg, m.Name())
			if fn, ok := obj.(*types.Func); ok {
				out = append(out, fn)
			}
			break
		}
	}
	c.ifaces[m] = out
	return out
}

func (c *checker) summaryOf(fn *types.Func) *summary {
	if s, ok := c.local[fn]; ok {
		return s
	}
	if fn.Pkg() == nil || fn.Pkg() == c.pass.Pkg {
		return nil
	}
	var s summary
	if c.pass.ImportObjectFact(fn, &s) {
		return &s
	}
	return nil
}

// collectDefs indexes every assignment to a local in the body,
// literals included (a closure reads the variables it captures).
func (st *fnState) collectDefs() {
	info := st.c.pass.TypesInfo
	add := func(lhs ast.Expr, d def) {
		switch l := ast.Unparen(lhs).(type) {
		case *ast.Ident:
			if obj := objOf(info, l); obj != nil {
				st.defs[obj] = append(st.defs[obj], d)
			}
		case *ast.IndexExpr:
			// args[i] = v adds v to the slice's bytes.
			if id, ok := ast.Unparen(l.X).(*ast.Ident); ok {
				if obj := objOf(info, id); obj != nil {
					st.defs[obj] = append(st.defs[obj], d)
				}
			}
		}
	}
	ast.Inspect(st.fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, l := range x.Lhs {
				var rhs ast.Expr
				if len(x.Rhs) == len(x.Lhs) {
					rhs = x.Rhs[i]
				} else if len(x.Rhs) == 1 {
					rhs = x.Rhs[0]
				}
				if rhs == nil {
					continue
				}
				if x.Tok != token.ASSIGN && x.Tok != token.DEFINE {
					// s += v
					rhs = &ast.BinaryExpr{X: l, Op: token.ADD, Y: rhs, OpPos: x.TokPos}
				}
				add(l, def{pos: x.Pos(), at: x, rhs: rhs})
			}
		case *ast.ValueSpec:
			for i, name := range x.Names {
				if i < len(x.Values) {
					add(name, def{pos: x.Pos(), at: x, rhs: x.Values[i]})
				} else if len(x.Values) == 1 {
					add(name, def{pos: x.Pos(), at: x, rhs: x.Values[0]})
				}
			}
		case *ast.RangeStmt:
			if x.Value != nil {
				add(x.Value, def{pos: x.Pos(), at: x, rangeOf: x.X})
			}
			// A map's or an iterator's key is an element too; a slice's
			// or a string's is an int, which carries nothing.
			if x.Key != nil {
				add(x.Key, def{pos: x.Pos(), at: x, rangeOf: x.X})
			}
		case *ast.CallExpr:
			// Standard writer helpers write to the same local builder.
			if sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok && len(x.Args) >= 2 {
				if id, ok := sel.X.(*ast.Ident); ok {
					if pkg, ok := info.Uses[id].(*types.PkgName); ok {
						writes := pkg.Imported().Path() == "fmt" && (sel.Sel.Name == "Fprint" || sel.Sel.Name == "Fprintf" || sel.Sel.Name == "Fprintln") || pkg.Imported().Path() == "io" && sel.Sel.Name == "WriteString"
						target := ast.Unparen(x.Args[0])
						if addr, ok := target.(*ast.UnaryExpr); ok && addr.Op == token.AND {
							target = ast.Unparen(addr.X)
						}
						if id, ok := target.(*ast.Ident); ok && writes {
							if v, ok := objOf(info, id).(*types.Var); ok && isBuilder(v.Type()) {
								for _, arg := range x.Args[1:] {
									st.defs[v] = append(st.defs[v], def{pos: x.Pos(), at: x, rhs: arg})
								}
							}
						}
					}
				}
			}
			// A strings.Builder collects what is written into it;
			// String hands those bytes back.
			if sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok {
				if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok {
					if v, ok := objOf(info, id).(*types.Var); ok && isBuilder(v.Type()) {
						switch sel.Sel.Name {
						case "WriteString", "Write", "WriteByte", "WriteRune":
							if len(x.Args) == 1 {
								st.defs[v] = append(st.defs[v], def{pos: x.Pos(), at: x, rhs: x.Args[0]})
							}
						case "Reset":
							// Not a clear: a read before this reset still
							// sees the writes before it; builderTaint
							// decides by position.
							st.resets[v] = append(st.resets[v], x)
						}
					}
				}
			}
		}
		return true
	})
}

func objOf(info *types.Info, id *ast.Ident) types.Object {
	if o := info.Defs[id]; o != nil {
		return o
	}
	return info.Uses[id]
}

// exprTaint is the taint of e where it stands.
func (st *fnState) exprTaint(e ast.Expr) taint {
	if e == nil {
		return taint{}
	}
	if t, ok := st.memo[e]; ok {
		return t
	}
	if st.busy[e] {
		return taint{}
	}
	st.busy[e] = true
	t := st.exprTaint1(e)
	delete(st.busy, e)
	st.memo[e] = t
	return t
}

func (st *fnState) exprTaint1(e ast.Expr) taint {
	pass := st.c.pass
	if tv, ok := pass.TypesInfo.Types[e]; ok {
		if tv.Value != nil || !stringish(tv.Type) {
			return taint{}
		}
	}
	switch x := e.(type) {
	case *ast.ParenExpr:
		return st.exprTaint(x.X)
	case *ast.Ident:
		v, ok := objOf(pass.TypesInfo, x).(*types.Var)
		if !ok || v.IsField() {
			return taint{}
		}
		return st.identTaint(v, x)
	case *ast.SelectorExpr:
		// A field some write filled with request text.
		if sel, ok := pass.TypesInfo.Selections[x]; ok && sel.Kind() == types.FieldVal {
			if fv, ok := sel.Obj().(*types.Var); ok && st.c.fieldHolds(fv) {
				return taint{src: true}
			}
		}
		// r.Form, r.PostForm, r.Header on the inbound request.
		if isRequest(typeOf(pass, x.X)) {
			switch x.Sel.Name {
			case "Form", "PostForm", "Header":
				return taint{src: true}
			}
		}
		// c.Value where c is a cookie the inbound request supplied
		// (r.Cookie's pair, an element of r.Cookies()).
		if x.Sel.Name == "Value" && isCookie(typeOf(pass, x.X)) && st.cookieFromRequest(x.X) {
			return taint{src: true}
		}
		return taint{}
	case *ast.IndexExpr:
		return st.exprTaint(x.X)
	case *ast.SliceExpr:
		return st.exprTaint(x.X)
	case *ast.StarExpr:
		return st.exprTaint(x.X)
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			return st.exprTaint(x.X)
		}
		return taint{}
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return st.exprTaint(x.X).or(st.exprTaint(x.Y))
		}
		return taint{}
	case *ast.CompositeLit:
		var t taint
		for _, el := range x.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				el = kv.Value
			}
			if _, isStruct := typeOf(pass, x).Underlying().(*types.Struct); isStruct {
				continue // a struct's fields are not followed
			}
			t = t.or(st.exprTaint(el))
		}
		return t
	case *ast.TypeAssertExpr:
		return st.exprTaint(x.X)
	case *ast.CallExpr:
		return st.callTaint(x)
	}
	return taint{}
}

// identTaint is v's taint at use: its parameter slot, plus every
// assignment before the use, unless a storable check or an allow-list
// test of v dominates the use after the latest assignment.
func (st *fnState) identTaint(v *types.Var, use ast.Node) taint {
	if !stringish(v.Type()) {
		return taint{}
	}
	var t taint
	latest := token.NoPos
	if i, ok := st.params[v]; ok {
		t.params |= bit(i)
	}
	for _, d := range st.defs[v] {
		if d.pos >= use.Pos() && !st.inLoopWith(d.at, use) {
			continue
		}
		if d.pos < use.Pos() && d.pos > latest {
			latest = d.pos
		}
		if d.rangeOf != nil {
			t = t.or(st.rangeTaint(d.rangeOf))
		} else {
			t = t.or(st.defTaint(d))
		}
	}
	if t.clean() {
		return t
	}
	if st.vetted(v, use, latest) {
		return taint{}
	}
	return t
}

// inLoopWith reports whether a later assignment at reaches use through
// a loop back edge: both sit in the same for/range body.
func (st *fnState) inLoopWith(at, use ast.Node) bool {
	for p := st.parents[use]; p != nil; p = st.parents[p] {
		switch l := p.(type) {
		case *ast.ForStmt:
			return contains(l.Body, at)
		case *ast.RangeStmt:
			return contains(l.Body, at)
		case *ast.FuncLit:
			return false
		}
	}
	return false
}

func contains(outer, inner ast.Node) bool {
	return outer != nil && inner != nil && outer.Pos() <= inner.Pos() && inner.End() <= outer.End()
}

// rangeTaint is the taint of the elements a range over x yields: x's
// own, or for a range over an iterator call (strings.FieldsSeq(term))
// the taint of the call's arguments and receiver.
func (st *fnState) rangeTaint(x ast.Expr) taint {
	call, ok := ast.Unparen(x).(*ast.CallExpr)
	if !ok {
		return st.exprTaint(x)
	}
	if t := typeOf(st.c.pass, call); t == nil {
		return taint{}
	} else if _, isFunc := t.Underlying().(*types.Signature); !isFunc {
		return st.exprTaint(x)
	}
	var t taint
	for _, a := range call.Args {
		t = t.or(st.exprTaint(a))
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if _, isPkg := st.c.pass.TypesInfo.Uses[identOf(sel.X)].(*types.PkgName); !isPkg {
			t = t.or(st.exprTaint(sel.X))
		}
	}
	return t
}

func (st *fnState) defTaint(d def) taint {
	if st.busy[d.at] {
		return taint{}
	}
	st.busy[d.at] = true
	defer delete(st.busy, d.at)
	return st.exprTaint(d.rhs)
}

// asciiProducers re-encode every byte into ASCII: their output always
// binds.
var asciiProducers = map[string]bool{
	"encoding/hex": true, "encoding/base64": true, "encoding/base32": true,
	"crypto/sha256": true, "crypto/sha1": true, "crypto/sha512": true, "crypto/md5": true,
	"hash/fnv": true, "hash/crc32": true,
}

var asciiFuncs = map[string]bool{
	"strconv.Quote": true, "strconv.QuoteToASCII": true, "strconv.Itoa": true,
	"strconv.FormatInt": true, "strconv.FormatUint": true, "strconv.FormatFloat": true,
	"strconv.FormatBool": true, "net/url.QueryEscape": true, "net/url.PathEscape": true,
}

func (st *fnState) callTaint(call *ast.CallExpr) taint {
	pass := st.c.pass
	// Conversions: string(b), []byte(s), any(s).
	if tv, ok := pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
		if len(call.Args) == 1 {
			return st.exprTaint(call.Args[0])
		}
		return taint{}
	}
	if id, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		if b, ok := pass.TypesInfo.Uses[id].(*types.Builtin); ok {
			if b.Name() == "append" {
				var t taint
				for _, a := range call.Args {
					t = t.or(st.exprTaint(a))
				}
				return t
			}
			return taint{}
		}
	}
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		if src, ok := st.sourceCall(sel); ok {
			return src
		}
		if t, ok := st.builderTaint(call, sel); ok {
			return t
		}
		if t, ok := st.hostCallbackTaint(call, sel); ok {
			return t
		}
	}

	callees := st.c.callees(call)
	if len(callees) == 0 {
		// A func value: its result is its own business.
		return taint{}
	}
	var t taint
	for _, fn := range callees {
		t = t.or(st.throughCall(call, fn))
	}
	return t
}

// builderTaint is the taint of a strings.Builder's collected bytes,
// read through String: every WriteString/Write into it
// before the use (loop back edges included), minus what a Reset
// dropped.
func (st *fnState) builderTaint(call *ast.CallExpr, sel *ast.SelectorExpr) (taint, bool) {
	if sel.Sel.Name != "String" {
		return taint{}, false
	}
	id, ok := ast.Unparen(sel.X).(*ast.Ident)
	if !ok {
		return taint{}, false
	}
	v, ok := objOf(st.c.pass.TypesInfo, id).(*types.Var)
	if !ok || v.IsField() || !isBuilder(v.Type()) {
		return taint{}, false
	}
	var t taint
	for _, d := range st.defs[v] {
		if d.pos >= call.Pos() && !st.inLoopWith(d.at, call) {
			continue
		}
		if st.resetBetween(v, d, call) {
			continue
		}
		t = t.or(st.builderWriteTaint(d.rhs, d.at, map[types.Object]bool{}))
	}
	return t, true
}

// builderWriteTaint also follows byte/rune values derived from request text.
// Ordinary integer parameters and range offsets carry no text provenance.
func (st *fnState) builderWriteTaint(e ast.Expr, use ast.Node, seen map[types.Object]bool) taint {
	if e == nil {
		return taint{}
	}
	typ := typeOf(st.c.pass, e)
	if typ == nil {
		return taint{}
	}
	basic, scalar := typ.Underlying().(*types.Basic)
	if !scalar || basic.Info()&types.IsInteger == 0 {
		return st.exprTaint(e)
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.IndexExpr:
		return st.exprTaint(x.X)
	case *ast.CallExpr:
		if tv, ok := st.c.pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() && len(x.Args) == 1 {
			return st.builderWriteTaint(x.Args[0], use, seen)
		}
	case *ast.Ident:
		v, ok := objOf(st.c.pass.TypesInfo, x).(*types.Var)
		if !ok || seen[v] {
			return taint{}
		}
		seen[v] = true
		defer delete(seen, v)
		var out taint
		for _, d := range st.defs[v] {
			if d.pos >= use.Pos() && !st.inLoopWith(d.at, use) {
				continue
			}
			if d.rangeOf != nil {
				r, ok := d.at.(*ast.RangeStmt)
				if ok {
					if value, ok := r.Value.(*ast.Ident); ok && objOf(st.c.pass.TypesInfo, value) == v {
						out = out.or(st.rangeTaint(d.rangeOf))
					}
				}
			} else {
				out = out.or(st.builderWriteTaint(d.rhs, d.at, seen))
			}
		}
		return out
	}
	return taint{}
}

// resetBetween reports whether a Reset of the builder v sits between
// the write d and the read call AND provably ran: a reset nested in a
// conditional branch the read is not in may never execute, so it
// clears nothing (fail closed — the write may still be in the
// builder).
func (st *fnState) resetBetween(v *types.Var, d def, call *ast.CallExpr) bool {
	for _, r := range st.resets[v] {
		if r.Pos() <= d.pos || r.End() > call.Pos() {
			continue
		}
		if !st.unconditionalTo(r, call) {
			continue
		}
		return true
	}
	return false
}

// unconditionalTo reports whether node r, which stands before use, is
// not nested inside a construct that can skip it on the path to use:
// a conditional (if/switch/select) whose taken branch may be another
// arm, or a loop that may run zero times. Every path from the
// function entry to use must pass r, as far as this syntactic check
// can tell.
func (st *fnState) unconditionalTo(r, use ast.Node) bool {
	for p := st.parents[r]; p != nil; p = st.parents[p] {
		switch b := p.(type) {
		case *ast.IfStmt:
			if !contains(b, use) || !sameArm(b, r, use) {
				return false
			}
		case *ast.CaseClause, *ast.CommClause:
			if !contains(p, use) {
				return false
			}
		case *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
			if !contains(p, use) {
				return false
			}
		case *ast.ForStmt, *ast.RangeStmt:
			// A loop may run zero times; when use is inside the
			// loop too, resetBetween's inLoopWith already spoke.
			if !contains(p, use) {
				return false
			}
		case *ast.FuncLit:
			return false
		}
	}
	return true
}

// sameArm reports whether r and use sit in the same direct arm of the
// if: both in Body, or both in the same Else subtree. A read in the
// sibling arm runs on the path where the reset never did.
func sameArm(b *ast.IfStmt, r, use ast.Node) bool {
	inBody := contains(b.Body, r)
	if inBody {
		return contains(b.Body, use)
	}
	if b.Else == nil {
		return false
	}
	return contains(b.Else, r) && contains(b.Else, use)
}

func isBuilder(t types.Type) bool {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	return isNamed(t, "strings", "Builder")
}

// hostCallbackTaint is the taint of a call through a func-typed struct
// field — a host callback, d6167d1a's IdempotencyConfig.Principal,
// whose result reached SQLIdempotencyStore.Begin's key bind unchecked.
// With fill sites in this package the call sees what they return: the
// fill's own summary, named function or literal. With no fill here, a
// field this package declares whose callback takes an *http.Request
// and returns a stringish value is read as request text: the host
// installs it to read the request, and the declaring package is where
// its result binds. The fill in another package (framework/app.go's
// ownerPrincipal, installed from a dependent) stays a residual below.
func (st *fnState) hostCallbackTaint(call *ast.CallExpr, sel *ast.SelectorExpr) (taint, bool) {
	fv, ok := st.c.pass.TypesInfo.Uses[sel.Sel].(*types.Var)
	if !ok || !fv.IsField() {
		return taint{}, false
	}
	if _, ok := fv.Type().Underlying().(*types.Signature); !ok {
		return taint{}, false
	}
	fv = fv.Origin()
	fills := st.c.fills[fv]
	if len(fills) == 0 {
		if fv.Pkg() != st.c.pass.Pkg || !requestCallback(fv.Type().Underlying().(*types.Signature)) {
			return taint{}, false
		}
		return taint{src: true}, true
	}
	var t taint
	for _, f := range fills {
		if f.fn != nil {
			t = t.or(st.throughCall(call, f.fn))
		}
		if f.lit != nil {
			t = t.or(st.litTaint(call, f.lit))
		}
	}
	return t, true
}

// litTaint applies a fill literal's summary at the call through the
// field: the arguments its return reads, minus the parameters it
// checks, plus its own request reads.
func (st *fnState) litTaint(call *ast.CallExpr, lit *ast.FuncLit) taint {
	s, ok := st.c.litSums[lit]
	if !ok {
		return taint{}
	}
	sig, _ := st.c.pass.TypesInfo.Types[lit].Type.Underlying().(*types.Signature)
	if sig == nil {
		return taint{}
	}
	var t taint
	for i, a := range call.Args {
		pi := i
		if sig.Variadic() && pi >= sig.Params().Len()-1 {
			pi = sig.Params().Len() - 1
		}
		if has(s.Return&^s.Check, pi) {
			t = t.or(st.exprTaint(a))
		}
	}
	if s.Source {
		t.src = true
	}
	return t
}

// requestCallback: a callback handed the request and returning a
// stringish value — the shape a host installs to read the request.
func requestCallback(sig *types.Signature) bool {
	req := false
	for i := range sig.Params().Len() {
		if isRequest(sig.Params().At(i).Type()) {
			req = true
			break
		}
	}
	if !req {
		return false
	}
	for i := range sig.Results().Len() {
		if stringish(sig.Results().At(i).Type()) {
			return true
		}
	}
	return false
}

// throughCall is the taint a call to fn passes on to its result.
func (st *fnState) throughCall(call *ast.CallExpr, fn *types.Func) taint {
	sig := fn.Type().(*types.Signature)
	argTaint := func(mask uint64, all bool) taint {
		var t taint
		for i, a := range call.Args {
			pi := i
			if sig.Variadic() && pi >= sig.Params().Len()-1 {
				pi = sig.Params().Len() - 1
			}
			if all || has(mask, pi) {
				t = t.or(st.exprTaint(a))
			}
		}
		return t
	}
	if s := st.c.summaryOf(fn); s != nil {
		t := argTaint(s.Return&^s.Check, false)
		if s.Source {
			t.src = true
		}
		t = t.or(st.retFieldTaint(call, s.RetFields))
		return t
	}
	// No summary. A function from a module (a dot in the path's first
	// element) has an empty one: nothing of its arguments reaches its
	// result. A standard-library function passes its arguments, and a
	// method its receiver, through to a string result, except the
	// encoders whose output is ASCII.
	if fn.Pkg() == nil {
		return taint{}
	}
	path := fn.Pkg().Path()
	if strings.Contains(strings.SplitN(path, "/", 2)[0], ".") || asciiProducers[path] || asciiFuncs[path+"."+fn.Name()] {
		return taint{}
	}
	t := argTaint(0, true)
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sig.Recv() != nil {
		if _, isPkg := st.c.pass.TypesInfo.Uses[identOf(sel.X)].(*types.PkgName); !isPkg {
			t = t.or(st.exprTaint(sel.X))
		}
	}
	return t
}

// retFieldTaint is the taint a call reads out of the receiver's named
// fields (RetFields): a method returning its receiver's field hands
// back whatever the caller stored in it, request text included, even
// though the declaring package never wrote the field itself.
func (st *fnState) retFieldTaint(call *ast.CallExpr, names []string) taint {
	if len(names) == 0 {
		return taint{}
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return taint{}
	}
	recvT := typeOf(st.c.pass, sel.X)
	if recvT == nil {
		return taint{}
	}
	for _, name := range names {
		obj, _, _ := types.LookupFieldOrMethod(recvT, true, st.c.pass.Pkg, name)
		if fv, ok := obj.(*types.Var); ok && st.c.fieldHolds(fv) {
			return taint{src: true}
		}
	}
	return taint{}
}

func identOf(e ast.Expr) *ast.Ident {
	id, _ := ast.Unparen(e).(*ast.Ident)
	if id == nil {
		return &ast.Ident{}
	}
	return id
}

// sourceCall recognises the request reads.
func (st *fnState) sourceCall(sel *ast.SelectorExpr) (taint, bool) {
	pass := st.c.pass
	recv := typeOf(pass, sel.X)
	if recv == nil {
		return taint{}, false
	}
	switch {
	case isRequest(recv):
		switch sel.Sel.Name {
		case "FormValue", "PostFormValue", "PathValue", "Referer", "UserAgent", "Cookie", "Cookies":
			return taint{src: true}, true
		}
	case isNamed(recv, "net/url", "URL") || isPtrNamed(recv, "net/url", "URL"):
		if sel.Sel.Name == "Query" {
			if inner, ok := ast.Unparen(sel.X).(*ast.SelectorExpr); ok && isRequest(typeOf(pass, inner.X)) {
				return taint{src: true}, true
			}
			// u := r.URL: the local carries the request's URL, so
			// u.Query() reads the request's query.
			if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok {
				if v, ok := objOf(pass.TypesInfo, id).(*types.Var); ok {
					for _, d := range st.defs[v] {
						if inner, ok := ast.Unparen(d.rhs).(*ast.SelectorExpr); ok && inner.Sel.Name == "URL" && isRequest(typeOf(pass, inner.X)) {
							return taint{src: true}, true
						}
					}
				}
			}
			return taint{}, true
		}
	}
	return taint{}, false
}

// cookieFromRequest reports whether e names a cookie the inbound
// request supplied: an index into r.Cookies(), or a local bound to
// r.Cookie(...) / r.Cookies() (a range element included).
func (st *fnState) cookieFromRequest(e ast.Expr) bool {
	x := ast.Unparen(e)
	for {
		ix, ok := x.(*ast.IndexExpr)
		if !ok {
			break
		}
		x = ast.Unparen(ix.X)
	}
	if call, ok := x.(*ast.CallExpr); ok {
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return false
		}
		_, isSrc := st.sourceCall(sel)
		return isSrc
	}
	id, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := objOf(st.c.pass.TypesInfo, id).(*types.Var)
	if !ok {
		return false
	}
	for _, d := range st.defs[v] {
		rhs := d.rhs
		if d.rangeOf != nil {
			rhs = d.rangeOf
		}
		call, ok := ast.Unparen(rhs).(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if _, isSrc := st.sourceCall(sel); isSrc {
			return true
		}
	}
	return false
}

// isCookie: *http.Cookie (or an http.Cookie value).
func isCookie(t types.Type) bool {
	return isPtrNamed(t, "net/http", "Cookie") || isNamed(t, "net/http", "Cookie")
}

// vetted reports whether a storable check or an allow-list test of v
// dominates use, made after v's latest assignment (after).
func (st *fnState) vetted(v *types.Var, use ast.Node, after token.Pos) bool {
	body := st.fd.Body
	for _, cond := range dominance.EnclosingIfConds(use, st.parents, body) {
		if cond.Pos() < after {
			continue
		}
		if st.checksIn(cond, v) || allowListed(st.c.pass, cond, v, true) {
			return true
		}
		// if p, ok := providers[v]; ok { … }
		if iff, ok := st.parents[cond].(*ast.IfStmt); ok && st.commaOKLookup(iff.Init, v) != nil {
			return true
		}
	}
	// A switch on v whose constant case holds the use.
	for c, p := ast.Node(use), st.parents[use]; p != nil && p != body; c, p = p, st.parents[p] {
		cc, ok := p.(*ast.CaseClause)
		if !ok || len(cc.List) == 0 || !containsNode(cc.Body, c) {
			continue
		}
		if sw, ok := st.parents[st.parents[cc]].(*ast.SwitchStmt); ok && sw.Pos() >= after && isIdentOf(st.c.pass, sw.Tag, v) && allConst(st.c.pass, cc.List) {
			return true
		}
	}
	prefix := dominance.Prefix(use, st.parents, body)
	for _, stmts := range prefix {
		var okVars []types.Object // comma-ok lookups of v in a map
		for _, s := range stmts {
			if s.Pos() < after {
				continue
			}
			if ok := st.commaOKLookup(s, v); ok != nil {
				okVars = append(okVars, ok)
			}
			if iff := dominance.IfStmtOf(s); iff != nil && iff.Cond != nil && dominance.Diverges(iff.Body) {
				if st.checksIn(iff.Cond, v) || allowListed(st.c.pass, iff.Cond, v, false) {
					return true
				}
				// _, ok := allowed[v]; if !ok { return }, in one
				// statement or two.
				if st.commaOKLookup(iff.Init, v) != nil || mentionsAny(st.c.pass, iff.Cond, okVars) {
					return true
				}
			}
			if verdicts := st.keptCheckIn(s, v); len(verdicts) > 0 && st.verdictGates(verdicts, s, prefix, use) {
				return true
			}
		}
	}
	return false
}

// commaOKLookup returns the ok variable of `x, ok := m[v]` (or `=`)
// when s is that statement: a membership test of v in a map, the
// allow-list spelling of a configured set (OAuth2Plugin.providers).
func (st *fnState) commaOKLookup(s ast.Stmt, v *types.Var) types.Object {
	as, ok := s.(*ast.AssignStmt)
	if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 1 {
		return nil
	}
	ix, ok := ast.Unparen(as.Rhs[0]).(*ast.IndexExpr)
	if !ok || !isIdentOf(st.c.pass, ix.Index, v) {
		return nil
	}
	if t := typeOf(st.c.pass, ix.X); t == nil {
		return nil
	} else if _, isMap := t.Underlying().(*types.Map); !isMap {
		return nil
	}
	id, ok := as.Lhs[1].(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil
	}
	return objOf(st.c.pass.TypesInfo, id)
}

func mentionsAny(pass *analysis.Pass, e ast.Expr, objs []types.Object) bool {
	if len(objs) == 0 {
		return false
	}
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && slices.Contains(objs, pass.TypesInfo.Uses[id]) {
			found = true
		}
		return !found
	})
	return found
}

func containsNode(list []ast.Stmt, n ast.Node) bool {
	for _, s := range list {
		if s == n {
			return true
		}
	}
	return false
}

// checksIn reports whether e calls a storable check on v, or tests v
// inline with both halves of one (utf8 validity and a NUL test).
func (st *fnState) checksIn(e ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok && st.checkCall(call, v) {
			found = true
		}
		return true
	})
	if found {
		return true
	}
	return utf8Check(st.c.pass, e, v) && nulCheck(st.c.pass, e, v)
}

// keptCheckIn returns the verdict variables a statement s keeps from a
// storable check on v: `_, err := CoerceValue(…, v)` and `ok :=
// storableText(v)` keep theirs; a call statement or an assignment to _
// throws the verdict away. A kept verdict vets v only where it gates
// the path to the use (verdictGates).
func (st *fnState) keptCheckIn(s ast.Stmt, v *types.Var) []types.Object {
	switch x := s.(type) {
	case *ast.AssignStmt:
		for _, l := range x.Lhs {
			if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
				for _, r := range x.Rhs {
					if call, ok := ast.Unparen(r).(*ast.CallExpr); ok && st.checkCall(call, v) {
						return []types.Object{objOf(st.c.pass.TypesInfo, id)}
					}
				}
			}
		}
	case *ast.IfStmt:
		// if _, err := CoerceValue(…, v); err != nil { return err }
		if as, ok := x.Init.(*ast.AssignStmt); ok {
			return st.keptCheckIn(as, v)
		}
	}
	return nil
}

// verdictGates reports whether one of verdicts gates the path from the
// kept check (statement s) to the use: an enclosing if at the use
// tests a verdict, or a diverging statement between the two tests one
// (`if err != nil { return }`). The prefix lists hold only statements
// that dominate the use, so a divergence found there gates it.
func (st *fnState) verdictGates(verdicts []types.Object, s ast.Stmt, stmts [][]ast.Stmt, use ast.Node) bool {
	for _, cond := range dominance.EnclosingIfConds(use, st.parents, st.fd.Body) {
		if mentionsAny(st.c.pass, cond, verdicts) {
			return true
		}
	}
	for _, list := range stmts {
		for _, t := range list {
			if t.Pos() < s.Pos() {
				continue
			}
			iff := dominance.IfStmtOf(t)
			if iff == nil || iff.Cond == nil || !dominance.Diverges(iff.Body) {
				continue
			}
			if mentionsAny(st.c.pass, iff.Cond, verdicts) {
				return true
			}
		}
	}
	return false
}

// checkCall: call hands v to a parameter its callee refuses when it is
// not storable text.
func (st *fnState) checkCall(call *ast.CallExpr, v *types.Var) bool {
	for _, fn := range st.c.callees(call) {
		s := st.c.summaryOf(fn)
		if s == nil || s.Check == 0 {
			continue
		}
		for i, a := range call.Args {
			if has(s.Check, i) && isIdentOf(st.c.pass, a, v) {
				return true
			}
		}
	}
	return false
}

// bodyChecks reports whether the function body refuses non-storable
// text in parameter v: inline (a utf8 validity call and a NUL test of
// v), as an ASCII walk of v, or by handing v to a check.
func (st *fnState) bodyChecks(v *types.Var) bool {
	pass := st.c.pass
	body := st.fd.Body
	if utf8Check(pass, body, v) && nulCheck(pass, body, v) {
		return true
	}
	if asciiWalk(pass, body, v) {
		return true
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok && !found && st.checkCall(call, v) {
			found = true
		}
		return !found
	})
	return found
}

// utf8Check: utf8.ValidString(v) or utf8.Valid([]byte(v)) inside n.
func utf8Check(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		call, ok := m.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		if q := qualified(pass, call.Fun); (q == "unicode/utf8.ValidString" || q == "unicode/utf8.Valid") && len(call.Args) == 1 && isIdentOf(pass, call.Args[0], v) {
			found = true
		}
		return true
	})
	return found
}

// nulCheck: a NUL test of v inside n — IndexByte/IndexRune/ContainsRune
// with 0, Contains/Index/ContainsAny with a "\x00"-bearing literal, or a
// byte of v compared against 0.
func nulCheck(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	// c := v[i]: a byte of v under its own name.
	bytesOfV := map[types.Object]bool{}
	ast.Inspect(n, func(m ast.Node) bool {
		if as, ok := m.(*ast.AssignStmt); ok && len(as.Lhs) == len(as.Rhs) {
			for i, l := range as.Lhs {
				if id, ok := l.(*ast.Ident); ok && indexOf(pass, as.Rhs[i], v) {
					bytesOfV[objOf(pass.TypesInfo, id)] = true
				}
			}
		}
		return true
	})
	isByte := func(e ast.Expr) bool {
		if indexOf(pass, e, v) {
			return true
		}
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && bytesOfV[pass.TypesInfo.Uses[id]]
	}
	found := false
	ast.Inspect(n, func(m ast.Node) bool {
		if found {
			return false
		}
		switch x := m.(type) {
		case *ast.CallExpr:
			q := qualified(pass, x.Fun)
			if !strings.HasPrefix(q, "strings.") && !strings.HasPrefix(q, "bytes.") || len(x.Args) != 2 || !isIdentOf(pass, x.Args[0], v) {
				return true
			}
			tv := pass.TypesInfo.Types[x.Args[1]]
			if tv.Value == nil {
				return true
			}
			switch tv.Value.Kind() {
			case constant.Int:
				if n, ok := constant.Int64Val(tv.Value); ok && n == 0 {
					found = true
				}
			case constant.String:
				if strings.Contains(constant.StringVal(tv.Value), "\x00") {
					found = true
				}
			}
		case *ast.BinaryExpr:
			if (x.Op == token.EQL || x.Op == token.NEQ) && (isByte(x.X) && isZero(pass, x.Y) || isByte(x.Y) && isZero(pass, x.X)) {
				found = true
			}
		}
		return true
	})
	return found
}

// asciiWalk: n indexes or ranges over v and compares against both ends
// of printable ASCII — an upper bound at 0x7e/0x7f/0x80 (or
// unicode.MaxASCII) and a lower bound covering NUL (c < k for k in
// 1..0x21, c == 0, c <= k). A walk like that refuses every byte that is
// not valid single-byte UTF-8 and every NUL.
func asciiWalk(pass *analysis.Pass, n ast.Node, v *types.Var) bool {
	walks := false
	ast.Inspect(n, func(m ast.Node) bool {
		switch x := m.(type) {
		case *ast.IndexExpr:
			if isIdentOf(pass, x.X, v) {
				walks = true
			}
		case *ast.RangeStmt:
			if isIdentOf(pass, x.X, v) {
				walks = true
			}
		}
		return !walks
	})
	if !walks {
		return false
	}
	var upper, lower bool
	ast.Inspect(n, func(m ast.Node) bool {
		b, ok := m.(*ast.BinaryExpr)
		if !ok {
			return true
		}
		op, k, ok := constOperand(pass, b)
		if !ok {
			return true
		}
		switch op {
		case token.GTR:
			upper = upper || (k >= 0x7e && k <= 0x7f)
		case token.GEQ:
			upper = upper || (k >= 0x7f && k <= 0x80)
		case token.LSS:
			lower = lower || (k >= 1 && k <= 0x21)
		case token.LEQ:
			lower = lower || (k >= 0 && k <= 0x20)
		case token.EQL:
			lower = lower || k == 0
		}
		return true
	})
	return upper && lower
}

// constOperand returns the comparison with the constant on the right.
func constOperand(pass *analysis.Pass, b *ast.BinaryExpr) (token.Token, int64, bool) {
	if v := pass.TypesInfo.Types[b.Y].Value; v != nil && pass.TypesInfo.Types[b.X].Value == nil {
		if n, ok := constant.Int64Val(constant.ToInt(v)); ok {
			return b.Op, n, true
		}
	}
	if v := pass.TypesInfo.Types[b.X].Value; v != nil && pass.TypesInfo.Types[b.Y].Value == nil {
		if n, ok := constant.Int64Val(constant.ToInt(v)); ok {
			return flip(b.Op), n, true
		}
	}
	return 0, 0, false
}

func flip(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.GTR:
		return token.LSS
	case token.LEQ:
		return token.GEQ
	case token.GEQ:
		return token.LEQ
	}
	return op
}

func indexOf(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	ix, ok := ast.Unparen(e).(*ast.IndexExpr)
	return ok && isIdentOf(pass, ix.X, v)
}

func isZero(pass *analysis.Pass, e ast.Expr) bool {
	tv := pass.TypesInfo.Types[e]
	if tv.Value == nil {
		return false
	}
	n, ok := constant.Int64Val(constant.ToInt(tv.Value))
	return ok && n == 0
}

// allowListed: cond tests v against a constant (== in an enclosing
// if, != in a diverging one), or tests v's membership in a map or a
// slices.Contains list: asserted in an enclosing if, denied (under !)
// in a diverging one. A positive lookup in a diverging if, or a denied
// one around the use, is a dedup set (`!seen[t]`), not an allow-list:
// the use then runs exactly for the values the set has never vetted.
func allowListed(pass *analysis.Pass, cond ast.Expr, v *types.Var, enclosing bool) bool {
	negated := map[ast.Expr]bool{}
	ast.Inspect(cond, func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.NOT {
			negated[ast.Unparen(u.X)] = true
		}
		return true
	})
	member := func(e ast.Expr) bool { return negated[e] != enclosing }
	found := false
	ast.Inspect(cond, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.BinaryExpr:
			want := token.EQL
			if !enclosing {
				want = token.NEQ
			}
			if x.Op == want {
				if isIdentOf(pass, x.X, v) && isConstString(pass, x.Y) || isIdentOf(pass, x.Y, v) && isConstString(pass, x.X) {
					found = true
				}
			}
		case *ast.IndexExpr:
			if t := typeOf(pass, x.X); t != nil {
				if _, isMap := t.Underlying().(*types.Map); isMap && isIdentOf(pass, x.Index, v) && member(x) {
					found = true
				}
			}
		case *ast.CallExpr:
			if q := qualified(pass, x.Fun); q == "slices.Contains" && len(x.Args) == 2 && isIdentOf(pass, x.Args[1], v) && member(x) {
				found = true
			}
		}
		return true
	})
	return found
}

func isConstString(pass *analysis.Pass, e ast.Expr) bool {
	tv := pass.TypesInfo.Types[e]
	return tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) != ""
}

func allConst(pass *analysis.Pass, list []ast.Expr) bool {
	return !slices.ContainsFunc(list, func(e ast.Expr) bool { return !isConstString(pass, e) })
}

func isIdentOf(pass *analysis.Pass, e ast.Expr, v *types.Var) bool {
	e = ast.Unparen(e)
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if tv, ok := pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
			e = ast.Unparen(call.Args[0]) // []byte(v), string(v)
		}
	}
	id, ok := e.(*ast.Ident)
	return ok && pass.TypesInfo.Uses[id] == v
}

func qualified(pass *analysis.Pass, fun ast.Expr) string {
	sel, ok := ast.Unparen(fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

func typeOf(pass *analysis.Pass, e ast.Expr) types.Type {
	if tv, ok := pass.TypesInfo.Types[e]; ok {
		return tv.Type
	}
	if id, ok := e.(*ast.Ident); ok {
		if o := objOf(pass.TypesInfo, id); o != nil {
			return o.Type()
		}
	}
	return nil
}

// stringish: a type that carries request bytes — string kinds, byte
// and string slices, url.Values-shaped maps, interfaces.
func stringish(t types.Type) bool {
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&types.IsString != 0
	case *types.Slice:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Byte {
			return true
		}
		return stringish(u.Elem())
	case *types.Array:
		return false
	case *types.Map:
		return stringish(u.Elem())
	case *types.Interface:
		return u.Empty()
	case *types.Pointer:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok {
			return b.Info()&types.IsString != 0
		}
	}
	return false
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isEmptyInterface(t types.Type) bool {
	i, ok := t.Underlying().(*types.Interface)
	return ok && i.Empty()
}

func isRequest(t types.Type) bool {
	return isPtrNamed(t, "net/http", "Request")
}

func isPtrNamed(t types.Type, pkg, name string) bool {
	if t == nil {
		return false
	}
	p, ok := t.(*types.Pointer)
	return ok && isNamed(p.Elem(), pkg, name)
}

func isNamed(t types.Type, pkg, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}

func namedOf(t types.Type) *types.Named {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := types.Unalias(t).(*types.Named)
	return n
}

// fieldWrite records a write of rhs into field: request text marks the
// field, a parameter of this function records a write the call sites
// resolve.
func (st *fnState) fieldWrite(field *types.Var, rhs ast.Expr) {
	if !st.c.flows || field == nil || !stringish(field.Type()) {
		return
	}
	t := st.exprTaint(rhs)
	if t.src {
		st.c.markField(field)
	}
	if t.params != 0 {
		st.c.addParamField(st.fn, t.params, field)
	}
}

// literalFields records the field writes of a struct literal, keyed or
// positional.
func (st *fnState) literalFields(lit *ast.CompositeLit) {
	info := st.c.pass.TypesInfo
	strct, ok := typeOf(st.c.pass, lit).Underlying().(*types.Struct)
	if !ok {
		return
	}
	for i, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				if fv, ok := info.Uses[id].(*types.Var); ok && fv.IsField() {
					st.fieldWrite(fv, kv.Value)
				}
			}
			continue
		}
		if i < strct.NumFields() {
			st.fieldWrite(strct.Field(i), el)
		}
	}
}

// assignFields records v.f = rhs.
func (st *fnState) assignFields(as *ast.AssignStmt) {
	if len(as.Lhs) != len(as.Rhs) {
		return
	}
	info := st.c.pass.TypesInfo
	for i, l := range as.Lhs {
		sx, ok := ast.Unparen(l).(*ast.SelectorExpr)
		if !ok {
			continue
		}
		sel, ok := info.Selections[sx]
		if !ok || sel.Kind() != types.FieldVal {
			continue
		}
		fv, _ := sel.Obj().(*types.Var)
		rhs := as.Rhs[i]
		if as.Tok != token.ASSIGN {
			rhs = &ast.BinaryExpr{X: l, Op: token.ADD, Y: rhs, OpPos: as.TokPos}
		}
		st.fieldWrite(fv, rhs)
	}
}

// callFields resolves a callee's parameter-to-field writes at this call:
// request text in one of those positions fills the field, and a
// parameter of this function there is a write this function makes too.
func (st *fnState) callFields(call *ast.CallExpr) {
	for _, callee := range st.c.callees(call) {
		pfs := st.c.paramFields[callee]
		if len(pfs) == 0 {
			continue
		}
		csig := callee.Type().(*types.Signature)
		for _, pf := range pfs {
			for i, a := range call.Args {
				pi := i
				if csig.Variadic() && pi >= csig.Params().Len()-1 {
					pi = csig.Params().Len() - 1
				}
				if has(pf.mask, pi) {
					st.fieldWrite(pf.field, a)
				}
			}
		}
	}
}
