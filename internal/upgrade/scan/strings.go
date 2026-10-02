package scan

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/tools/go/packages"
)

// strScan holds one file's state for the strings matcher. Parents come
// from the package-wide map goPackage builds, so a constant declared in
// one file and used in another still sees its uses' containers.
type strScan struct {
	eng      *engine
	rel      string
	fset     *token.FileSet
	info     *types.Info
	exprs    []ast.Expr
	excluded map[token.Pos]bool
}

// stringsFile runs the Strings matchers over one file: every MAXIMAL
// constant string expression (a folded concatenation is one value; its
// operands are not), reported at the expression's start.
func (e *engine) stringsFile(rel string, f *ast.File, p *packages.Package) {
	if len(e.strNotes) == 0 {
		return
	}
	s := &strScan{
		eng:      e,
		rel:      rel,
		fset:     p.Fset,
		info:     p.TypesInfo,
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

// parent returns n's syntactic parent.
func (s *strScan) parent(n ast.Node) ast.Node { return s.eng.parents[n] }

// parentNoParens is parent with parenthesis layers stripped, so a value
// wrapped as f(("x")) or kv.Value=(x) is judged by its real container.
func (s *strScan) parentNoParens(n ast.Node) ast.Node {
	par := s.parent(n)
	for {
		pe, ok := par.(*ast.ParenExpr)
		if !ok {
			return par
		}
		par = s.parent(pe)
	}
}

// constStringOf reads a node's constant string value.
func (s *strScan) constStringOf(n ast.Node) (string, bool) {
	expr, ok := n.(ast.Expr)
	if !ok {
		return "", false
	}
	return constStringOf(s.info, expr)
}

func (s *strScan) run() {
	for _, expr := range s.exprs {
		if s.excluded[expr.Pos()] {
			continue
		}
		if _, ok := s.constStringOf(expr); !ok {
			continue
		}
		if par := s.parent(expr); par != nil {
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

// isTestingArg reports whether the value is an argument to a function or
// method of package testing (t.Fatal, t.Errorf, t.Run, tb.Log): a failure message
// or subtest name is prose, never markup the app renders, so naming a
// retired class there is not a use of it.
func (s *strScan) isTestingArg(expr ast.Expr) bool {
	call, ok := s.parentNoParens(unparenNode(expr)).(*ast.CallExpr)
	if !ok || unparenNode(call.Fun) == unparenNode(expr) {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := s.info.Uses[sel.Sel].(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "testing"
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
	return s.isSinkExpr(expr) || s.declReachesOnlySinks(expr)
}

// isSinkExpr reports whether the value lands directly in a marker-sink
// slot: a sink call argument, a sink field's value, or a map value under
// an attr_keys key.
func (s *strScan) isSinkExpr(n ast.Node) bool {
	up := unparenNode(n)
	par := s.parentNoParens(up)
	switch pv := par.(type) {
	case *ast.CallExpr:
		for i, arg := range pv.Args {
			if unparenNode(arg) == up {
				return s.callSinkMatches(pv, i)
			}
		}
	case *ast.KeyValueExpr:
		if unparenNode(pv.Value) != up {
			return false
		}
		if id, ok := pv.Key.(*ast.Ident); ok {
			if obj := s.info.Uses[id]; obj != nil {
				return s.eng.isSinkField(obj)
			}
		}
		if key, ok := s.constStringOf(pv.Key); ok {
			for _, k := range s.eng.sinks.AttrKeys {
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
func (s *strScan) callSinkMatches(call *ast.CallExpr, argIdx int) bool {
	var obj types.Object
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		obj = s.info.Uses[fn]
	case *ast.SelectorExpr:
		obj = s.info.Uses[fn.Sel]
	}
	if obj == nil {
		return false
	}
	for _, sink := range s.eng.sinks.Calls {
		if sink.Arg != argIdx {
			continue
		}
		for _, sym := range s.eng.objectSymbols(obj) {
			if sym == sink.Func {
				return true
			}
		}
	}
	return false
}

// declReachesOnlySinks reports whether the value initialises a variable
// or constant whose every use (via Info.Uses) is a marker sink.
func (s *strScan) declReachesOnlySinks(n ast.Node) bool {
	up := unparenNode(n)
	var obj types.Object
	switch pv := s.parentNoParens(up).(type) {
	case *ast.ValueSpec:
		for i, val := range pv.Values {
			if unparenNode(val) != up && i < len(pv.Names) {
				continue
			}
			if unparenNode(val) == up {
				obj = s.info.Defs[pv.Names[i]]
			}
		}
	case *ast.AssignStmt:
		for i, rhs := range pv.Rhs {
			if unparenNode(rhs) != up || i >= len(pv.Lhs) {
				continue
			}
			if id, ok := pv.Lhs[i].(*ast.Ident); ok {
				obj = s.info.Defs[id]
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
		if !s.isSinkExpr(u) {
			return false
		}
	}
	return true
}

// mapKeyString reports the constant key the value spells, when the
// expression is a map-literal key.
func (s *strScan) mapKeyString(n ast.Node) (string, bool) {
	up := unparenNode(n)
	if kv, ok := s.parentNoParens(up).(*ast.KeyValueExpr); ok && unparenNode(kv.Key) == up {
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
// form of) a listed name. In markup — a value holding "<" and "class=" —
// only tokens inside class attributes count.
func classInValue(v string, names []string) (string, bool) {
	tokens := strings.Fields(v)
	if strings.Contains(v, "<") && strings.Contains(v, "class=") {
		tokens = classAttrTokens(v)
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

// classAttrTokens splits the contents of every class="..." / class='...'
// attribute in a markup fragment.
func classAttrTokens(v string) []string {
	var tokens []string
	rest := v
	for {
		i := strings.Index(rest, "class=")
		if i < 0 {
			return tokens
		}
		rest = rest[i+len("class="):]
		j := 0
		for j < len(rest) && isSpaceByte(rest[j]) {
			j++
		}
		if j >= len(rest) || (rest[j] != '"' && rest[j] != '\'') {
			continue
		}
		quote := rest[j]
		end := strings.IndexByte(rest[j+1:], quote)
		if end < 0 {
			return tokens
		}
		tokens = append(tokens, strings.Fields(rest[j+1:j+1+end])...)
		rest = rest[j+1+end+1:]
	}
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

func isSpaceByte(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}
