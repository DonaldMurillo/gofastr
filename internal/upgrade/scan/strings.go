package scan

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/retired"
	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/tools/go/packages"
)

// strScan holds one file's state for the strings matcher. The package
// context carries the type info and the parents of every file in the
// package, so a constant declared in one file and used in another still
// sees its uses' containers — and uses in other packages are judged in
// their own contexts at decision time.
type strScan struct {
	eng      *engine
	rel      string
	fset     *token.FileSet
	ctx      *pkgCtx
	exprs    []ast.Expr
	excluded map[token.Pos]bool
}

// stringsFile runs the Strings matchers over one file: every MAXIMAL
// constant string expression (a folded concatenation is one value; its
// operands are not), reported at the expression's start.
func (e *engine) stringsFile(rel string, f *ast.File, p *packages.Package, ctx *pkgCtx) {
	if len(e.strNotes) == 0 {
		return
	}
	s := &strScan{
		eng:      e,
		rel:      rel,
		fset:     p.Fset,
		ctx:      ctx,
		excluded: map[token.Pos]bool{},
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			s.excluded[x.Path.Pos()] = true
		case *ast.Field:
			if x.Tag != nil {
				s.excluded[x.Tag.Pos()] = true
			}
		case ast.Expr:
			s.exprs = append(s.exprs, x)
		}
		return true
	})
	s.run()
}

// buildParents records every node's syntactic parent across a package's
// files in one walk.
func buildParents(files []*ast.File) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if len(stack) > 0 {
				parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
	}
	return parents
}

// parentNoParens is parent with parenthesis layers stripped, so a value
// wrapped as f(("x")) or kv.Value=(x) is judged by its real container.
// It is a method on the package context so a use in any scanned package
// can be judged in its own syntax.
func (c *pkgCtx) parentNoParens(n ast.Node) ast.Node {
	par := c.parents[n]
	for {
		pe, ok := par.(*ast.ParenExpr)
		if !ok {
			return par
		}
		par = c.parents[pe]
	}
}

// constStringOf reads a node's constant string value.
func (s *strScan) constStringOf(n ast.Node) (string, bool) {
	expr, ok := n.(ast.Expr)
	if !ok {
		return "", false
	}
	return constStringOf(s.ctx.info, expr)
}

func (s *strScan) run() {
	for _, expr := range s.exprs {
		if s.excluded[expr.Pos()] {
			continue
		}
		if _, ok := s.constStringOf(expr); !ok {
			continue
		}
		if par := s.ctx.parents[expr]; par != nil {
			if _, ok := s.constStringOf(par); ok {
				continue // operand of a folded value, not maximal
			}
		}
		if s.isTestingArg(expr) {
			continue
		}
		v, _ := s.constStringOf(expr)
		s.matchValue(expr, v)
	}
}

// testingProse names the package testing methods whose arguments are
// prose: failure and log messages, skip reasons. Run is prose only in
// its subtest name. Setenv and every other method hand their arguments
// to the code under test, so those stay matched.
var testingProse = map[string]bool{
	"Log": true, "Logf": true, "Error": true, "Errorf": true,
	"Fatal": true, "Fatalf": true, "Skip": true, "Skipf": true,
}

// isTestingArg reports whether the value is a prose argument of a
// package testing method (t.Fatal, t.Errorf, tb.Log, t.Run's name): a
// failure message or subtest name is never markup the app renders, so
// naming a retired class there is not a use of it.
func (s *strScan) isTestingArg(expr ast.Expr) bool {
	call, ok := s.ctx.parentNoParens(unparenNode(expr)).(*ast.CallExpr)
	if !ok || unparenNode(call.Fun) == unparenNode(expr) {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := s.ctx.info.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "testing" {
		return false
	}
	if fn.Name() == "Run" {
		return len(call.Args) > 0 && unparenNode(call.Args[0]) == unparenNode(expr)
	}
	return testingProse[fn.Name()]
}

// matchValue runs every note's string matchers over one value.
func (s *strScan) matchValue(expr ast.Expr, v string) {
	for _, n := range s.eng.strNotes {
		sm := n.Find.Strings
		if len(sm.Classes) > 0 && !s.onlySinks(expr) {
			if name, ok := classInValue(v, sm.Classes); ok {
				s.hit(n, expr, "class "+name)
			}
		}
		if len(sm.Attrs) > 0 {
			if key, isKey := s.mapKeyString(expr); isKey {
				if name, ok := attrKeyMatch(key, sm.Attrs); ok {
					s.hit(n, expr, "attr "+name)
				}
			} else if name, ok := attrInValue(v, sm.Attrs); ok {
				s.hit(n, expr, "attr "+name)
			}
		}
		if len(sm.Properties) > 0 {
			for _, name := range sm.Properties {
				if s.eng.propRegex(name).MatchString(v) {
					s.hit(n, expr, "property "+name)
					break
				}
			}
		}
		if sm.Match != nil && sm.Match.MatchString(v) {
			s.hit(n, expr, "match "+sm.Match.String())
		}
	}
}

func (s *strScan) hit(n *upgrade.Note, expr ast.Expr, why string) {
	pos := s.fset.Position(expr.Pos())
	s.eng.add(n, Hit{File: s.rel, Line: pos.Line, Col: pos.Column, Why: why})
}

// propRegex builds (once per scan) the bounded property matcher:
// --x matches unless it continues as --x-y.
func (e *engine) propRegex(name string) *regexp.Regexp {
	if e.propRes == nil {
		e.propRes = map[string]*regexp.Regexp{}
	}
	if re, ok := e.propRes[name]; ok {
		return re
	}
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_-]|$)`)
	e.propRes[name] = re
	return re
}

// onlySinks reports whether a class value reaches nothing but marker
// sinks: directly, or by initialising a variable or constant whose every
// use is one (one-level follow).
func (s *strScan) onlySinks(expr ast.Expr) bool {
	return s.ctx.isSinkExpr(s.eng, expr) || s.declReachesOnlySinks(expr)
}

// isSinkExpr reports whether the value lands directly in a marker-sink
// slot: a sink call argument, a sink field's value, or a map value under
// an attr_keys key. It lives on the package context so the decl follow
// below can judge a use in any scanned package in that package's syntax.
func (c *pkgCtx) isSinkExpr(eng *engine, n ast.Node) bool {
	up := unparenNode(n)
	// A qualified use (pkg.Const, cfg.Field) is judged at its whole
	// selector: that expression is the value sitting in the sink slot.
	if id, ok := up.(*ast.Ident); ok {
		if sel, ok := c.parents[id].(*ast.SelectorExpr); ok && sel.Sel == id {
			up = sel
		}
	}
	par := c.parentNoParens(up)
	switch pv := par.(type) {
	case *ast.CallExpr:
		for i, arg := range pv.Args {
			if unparenNode(arg) == up {
				return c.callSinkMatches(eng, pv, i)
			}
		}
	case *ast.KeyValueExpr:
		if unparenNode(pv.Value) != up {
			return false
		}
		if id, ok := pv.Key.(*ast.Ident); ok {
			if obj := c.info.Uses[id]; obj != nil {
				return eng.isSinkField(obj)
			}
		}
		if key, ok := constStringOf(c.info, pv.Key); ok {
			for _, k := range eng.sinks.AttrKeys {
				if key == k {
					return true
				}
			}
		}
	}
	return false
}

// callSinkMatches reports whether call's argIdx-th argument position is a
// listed marker sink.
func (c *pkgCtx) callSinkMatches(eng *engine, call *ast.CallExpr, argIdx int) bool {
	var obj types.Object
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		obj = c.info.Uses[fn]
	case *ast.SelectorExpr:
		obj = c.info.Uses[fn.Sel]
	}
	if obj == nil {
		return false
	}
	for _, sink := range eng.sinks.Calls {
		if sink.Arg != argIdx {
			continue
		}
		for _, sym := range eng.objectSymbols(obj) {
			if sym == sink.Func {
				return true
			}
		}
	}
	return false
}

// declReachesOnlySinks reports whether the value initialises a variable
// or constant whose every use is a marker sink. Uses are indexed
// engine-wide, so a constant declared in one package and sunk in
// another is still suppressed; an object with no uses anywhere is
// unknown flow and reported.
func (s *strScan) declReachesOnlySinks(n ast.Node) bool {
	up := unparenNode(n)
	var obj types.Object
	switch pv := s.ctx.parentNoParens(up).(type) {
	case *ast.ValueSpec:
		for i, val := range pv.Values {
			if unparenNode(val) != up && i < len(pv.Names) {
				continue
			}
			if unparenNode(val) == up {
				obj = s.ctx.info.Defs[pv.Names[i]]
			}
		}
	case *ast.AssignStmt:
		for i, rhs := range pv.Rhs {
			if unparenNode(rhs) != up || i >= len(pv.Lhs) {
				continue
			}
			if id, ok := pv.Lhs[i].(*ast.Ident); ok {
				obj = s.ctx.info.Defs[id]
			}
		}
	}
	if obj == nil {
		return false
	}
	uses := s.eng.objUses[obj]
	if len(uses) == 0 {
		return false // unknown flow: report it
	}
	for _, u := range uses {
		if !u.ctx.isSinkExpr(s.eng, u.id) {
			return false
		}
	}
	return true
}

// mapKeyString reports the constant key the value spells, when the
// expression is a map-literal key.
func (s *strScan) mapKeyString(n ast.Node) (string, bool) {
	up := unparenNode(n)
	if kv, ok := s.ctx.parentNoParens(up).(*ast.KeyValueExpr); ok && unparenNode(kv.Key) == up {
		return s.constStringOf(up)
	}
	return "", false
}

// unparenNode strips parentheses from an expression node.
func unparenNode(n ast.Node) ast.Node {
	expr, ok := n.(ast.Expr)
	if !ok {
		return n
	}
	return unparenExpr(expr)
}

// classInValue reports whether a class token of v equals (or is a BEM
// form of) a listed name. In markup — a value holding "<" and a class
// attribute — only tokens inside class attributes count.
func classInValue(v string, names []string) (string, bool) {
	tokens := strings.Fields(v)
	if markup, ok := markupClassTokens(v); ok {
		tokens = markup
	}
	for _, tok := range tokens {
		for _, name := range names {
			if tok == name || strings.HasPrefix(tok, name+"--") || strings.HasPrefix(tok, name+"__") {
				return name, true
			}
		}
	}
	// CSS written in a Go string (WithCustomCSS, an inline sheet): a
	// value holding a rule block counts ".name" class selectors too.
	if strings.Contains(v, "{") {
		for _, name := range names {
			if cssSelectorIn(v, name) {
				return name, true
			}
		}
	}
	return "", false
}

// cssSelectorIn reports whether v holds ".name" (or a BEM form of it)
// with an identifier boundary after it, so ".ui-button-group" is not
// ".ui-button".
func cssSelectorIn(v, name string) bool {
	sel := "." + name
	for i := 0; ; {
		j := strings.Index(v[i:], sel)
		if j < 0 {
			return false
		}
		end := i + j + len(sel)
		rest := v[end:]
		if rest == "" || !isIdentByte(rest[0]) || strings.HasPrefix(rest, "--") || strings.HasPrefix(rest, "__") {
			return true
		}
		i = end
	}
}

func isIdentByte(c byte) bool {
	return isWordByte(c) || c == '-'
}

// markupClassTokens reads v as markup when it holds a start tag and
// returns the class tokens a browser would build from it, through the
// same tokenizer the runtime retired-markup check uses: any attribute
// quoting, case and spacing, character references decoded, only the
// first of duplicate class attributes, nothing inside comments or
// raw-text elements. ok is false when v holds no start tag.
func markupClassTokens(v string) (tokens []string, ok bool) {
	if !strings.Contains(v, "<") {
		return nil, false
	}
	retired.Scan([]byte(v), func(_ []byte, _ [][]byte, classes [][]byte) {
		ok = true
		for _, c := range classes {
			tokens = append(tokens, string(c))
		}
	})
	return tokens, ok
}

// attrKeyMatch matches a map-literal key against the attr names: equal,
// or prefixed when the name ends in "-".
func attrKeyMatch(key string, names []string) (string, bool) {
	for _, name := range names {
		if key == name || (strings.HasSuffix(name, "-") && strings.HasPrefix(key, name)) {
			return name, true
		}
	}
	return "", false
}

// attrInValue reports whether the value mentions a listed attribute name
// with attribute boundaries: preceded by start, whitespace or a quote,
// followed by "=", whitespace, ">", a quote, "/" or end. A name ending
// in "-" matches every attribute with that prefix.
func attrInValue(v string, names []string) (string, bool) {
	for _, name := range names {
		prefix := strings.HasSuffix(name, "-")
		for i := 0; i+len(name) <= len(v); i++ {
			if v[i:i+len(name)] != name {
				continue
			}
			if i > 0 && !attrBoundary(v[i-1], false) {
				continue
			}
			if prefix {
				return name, true
			}
			if i+len(name) == len(v) || attrBoundary(v[i+len(name)], true) {
				return name, true
			}
		}
	}
	return "", false
}

// attrBoundary is the byte an attribute occurrence may sit against; the
// right side also accepts the value- and tag-closing characters.
func attrBoundary(c byte, right bool) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v', '"', '\'':
		return true
	case '>', '=', '/':
		return right
	}
	return false
}
