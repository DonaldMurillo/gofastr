// check-csp:ignore-file
// The analyzer's diagnostic quotes the <script> reassembly it detects;
// the directive exempts this file from the inline-script lint.

// Package sanitizefix catches a scrub that removes a multi-character
// match in one pass: the removal splices the match's neighbours
// together, and they can spell the very thing it removed.
//
// The bug (36abe258): framework/uihost's stripInlineScripts, the head
// scrub behind WithHeadHTML and SEOScreen.HeadHTML, ran each of its
// regexp removals once. `<scr<script></script>ipt>` lost its inner
// script element and left `<script>`; `http-equiv="ref/autofocusresh"`
// lost its autofocus attribute and left a live meta-refresh. The fix
// runs the pass until its output stops changing (capped, failing closed
// at the cap). The same shape is the textbook incomplete sanitisation:
// strings.ReplaceAll(p, "../", "") turns "....//" into "../".
//
// It fires on a removal inside a function whose name says it cleans
// (scrub, strip, sanitize, clean):
//   - a *regexp.Regexp ReplaceAllString / ReplaceAllLiteralString with
//     "" (or ReplaceAll / ReplaceAllLiteral with an empty or nil byte
//     slice), whose pattern can match more than one character;
//   - strings/bytes ReplaceAll, or Replace with any count, of a constant
//     of two or more characters with "".
//
// Silent postures:
//   - a fixpoint: the removal sits in a loop that compares its output
//     with its input (`if next == s { return s }`) or tests for what
//     remains (`for strings.Contains(s, x)`), or the function is a
//     single pass every same-package caller runs in such a loop
//     (stripInlineScriptsOnce, called only from stripInlineScripts' loop);
//   - a single-character removal: a literal of one character, a
//     character class, or a repetition of one (`[\x00-\x1f]+`). Removing
//     characters of a class joins characters outside it, which cannot
//     form a new match;
//   - a delimited pattern `L[^R]*R` / `L[^R]+R` (L != R, the class not
//     excluding L), html2md's `<[^>]+>`: a new match across a removed
//     one would need an L left of it with no R before the removed span,
//     and the leftmost-first engine would have matched from that L
//     instead. Provably idempotent, so no loop is owed;
//   - a non-empty replacement: a separator left in the gap keeps the
//     neighbours apart;
//   - functions whose names say nothing about cleaning (a normaliser
//     like dialectdrift's identifier unquoting is not a security
//     boundary), and _test.go files.
//
// Fix spelling: loop until the output stops changing, with a pass cap
// that fails closed (stripInlineScripts), or refuse input that still
// matches after one pass.
package sanitizefix

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"regexp/syntax"
	"strings"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/dominance"
)

var Analyzer = &analysis.Analyzer{
	Name: "sanitizefix",
	Doc:  "report a scrub that removes a multi-character match in one pass: the removal can reassemble what it removes; loop to a fixpoint",
	Run:  run,
}

var cleanName = regexp.MustCompile(`(?i)scrub|strip|sanitiz|clean`)

func run(pass *analysis.Pass) (any, error) {
	var decls []*ast.FuncDecl
	byObj := map[types.Object]*ast.FuncDecl{}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				decls = append(decls, fd)
				if obj := pass.TypesInfo.Defs[fd.Name]; obj != nil {
					byObj[obj] = fd
				}
			}
		}
	}
	patterns := regexpVars(pass)
	callers := callSites(pass, decls, byObj)

	for _, fd := range decls {
		if !cleanName.MatchString(fd.Name.Name) {
			continue
		}
		parents := dominance.Parents(fd.Body)
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !recombines(pass, call, patterns) {
				return true
			}
			if inFixpointLoop(pass, call, parents, fd.Body) {
				return true
			}
			if sites := callers[pass.TypesInfo.Defs[fd.Name]]; len(sites) > 0 && allInFixpoint(pass, sites) {
				return true
			}
			pass.Reportf(call.Pos(),
				"sanitizefix: %s removes a multi-character match in one pass: the removal splices its neighbours, which can spell what it removed (<scr<script></script>ipt> leaves <script>, 36abe258); repeat until the output stops changing, failing closed at a pass cap",
				fd.Name.Name)
			return true
		})
	}
	return nil, nil
}

// recombines reports whether call is a removal whose match can be more
// than one character.
func recombines(pass *analysis.Pass, call *ast.CallExpr, patterns map[types.Object]string) bool {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() {
	case "regexp":
		switch fn.Name() {
		case "ReplaceAllString", "ReplaceAllLiteralString", "ReplaceAll", "ReplaceAllLiteral":
		default:
			return false
		}
		if len(call.Args) != 2 || !emptyReplacement(pass, call.Args[1]) {
			return false
		}
		pat, known := patternOf(pass, sel.X, patterns)
		if !known {
			return true // an unknown pattern is assumed multi-character
		}
		return multiChar(pat)
	case "strings", "bytes":
		var old, repl ast.Expr
		switch fn.Name() {
		case "ReplaceAll":
			if len(call.Args) != 3 {
				return false
			}
		case "Replace":
			if len(call.Args) != 4 {
				return false
			}
		default:
			return false
		}
		old, repl = call.Args[1], call.Args[2]
		if !emptyReplacement(pass, repl) {
			return false
		}
		if conv, ok := ast.Unparen(old).(*ast.CallExpr); ok && len(conv.Args) == 1 {
			if tv, ok := pass.TypesInfo.Types[conv.Fun]; ok && tv.IsType() {
				old = conv.Args[0] // []byte("<!--")
			}
		}
		s, ok := constString(pass, old)
		return ok && utf8.RuneCountInString(s) >= 2
	}
	return false
}

func emptyReplacement(pass *analysis.Pass, e ast.Expr) bool {
	if s, ok := constString(pass, e); ok {
		return s == ""
	}
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok && id.Name == "nil" {
		_, isNil := pass.TypesInfo.Uses[id].(*types.Nil)
		return isNil
	}
	// []byte("") or []byte{}
	switch x := e.(type) {
	case *ast.CallExpr:
		if tv, ok := pass.TypesInfo.Types[x.Fun]; ok && tv.IsType() && len(x.Args) == 1 {
			s, ok := constString(pass, x.Args[0])
			return ok && s == ""
		}
	case *ast.CompositeLit:
		return len(x.Elts) == 0
	}
	return false
}

func constString(pass *analysis.Pass, e ast.Expr) (string, bool) {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// regexpVars maps package-level variables initialised with
// regexp.MustCompile(const) / regexp.Compile to their pattern.
func regexpVars(pass *analysis.Pass) map[types.Object]string {
	out := map[types.Object]string{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if pat, ok := compiledPattern(pass, vs.Values[i]); ok {
						out[pass.TypesInfo.Defs[name]] = pat
					}
				}
			}
		}
	}
	return out
}

func compiledPattern(pass *analysis.Pass, e ast.Expr) (string, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "regexp" || (fn.Name() != "MustCompile" && fn.Name() != "Compile") {
		return "", false
	}
	return constString(pass, call.Args[0])
}

func patternOf(pass *analysis.Pass, x ast.Expr, patterns map[types.Object]string) (string, bool) {
	if pat, ok := compiledPattern(pass, x); ok {
		return pat, true
	}
	if id, ok := ast.Unparen(x).(*ast.Ident); ok {
		pat, ok := patterns[pass.TypesInfo.Uses[id]]
		return pat, ok
	}
	return "", false
}

// multiChar reports whether a match of pat can be more than one
// character long and is not the provably idempotent delimited form.
func multiChar(pat string) bool {
	re, err := syntax.Parse(pat, syntax.Perl)
	if err != nil {
		return true
	}
	re = strip(re.Simplify())
	if singleChar(re) {
		return false
	}
	if delimited(re) {
		return false
	}
	return true
}

// strip removes capture groups.
func strip(re *syntax.Regexp) *syntax.Regexp {
	for re.Op == syntax.OpCapture && len(re.Sub) == 1 {
		re = re.Sub[0]
	}
	return re
}

// singleChar: every match is one character, or a run of characters of
// one class.
func singleChar(re *syntax.Regexp) bool {
	re = strip(re)
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune) == 1
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true
	case syntax.OpPlus, syntax.OpStar, syntax.OpQuest, syntax.OpRepeat:
		return len(re.Sub) == 1 && atom(strip(re.Sub[0]))
	}
	return false
}

func atom(re *syntax.Regexp) bool {
	switch re.Op {
	case syntax.OpLiteral:
		return len(re.Rune) == 1
	case syntax.OpCharClass, syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		return true
	}
	return false
}

// delimited: L[^R]*R or L[^R]+R, single-rune L != R, case-sensitive,
// the class excluding R and not excluding L.
func delimited(re *syntax.Regexp) bool {
	if re.Op != syntax.OpConcat || len(re.Sub) != 3 {
		return false
	}
	l, mid, r := strip(re.Sub[0]), strip(re.Sub[1]), strip(re.Sub[2])
	if l.Op != syntax.OpLiteral || len(l.Rune) != 1 || r.Op != syntax.OpLiteral || len(r.Rune) != 1 {
		return false
	}
	if l.Flags&syntax.FoldCase != 0 || r.Flags&syntax.FoldCase != 0 || l.Rune[0] == r.Rune[0] {
		return false
	}
	if mid.Op != syntax.OpStar && mid.Op != syntax.OpPlus || len(mid.Sub) != 1 {
		return false
	}
	class := strip(mid.Sub[0])
	if class.Op != syntax.OpCharClass {
		return false
	}
	return !classHas(class, r.Rune[0]) && classHas(class, l.Rune[0])
}

func classHas(class *syntax.Regexp, c rune) bool {
	for i := 0; i+1 < len(class.Rune); i += 2 {
		if class.Rune[i] <= c && c <= class.Rune[i+1] {
			return true
		}
	}
	return false
}

// inFixpointLoop reports whether call sits in a for loop that tests for
// convergence: a ==/!= between two string values in its condition or
// body, or a condition that asks whether a match remains
// (strings.Contains, bytes.Contains, regexp MatchString/Match).
func inFixpointLoop(pass *analysis.Pass, call ast.Node, parents map[ast.Node]ast.Node, body ast.Node) bool {
	for p := parents[call]; p != nil; p = parents[p] {
		var loop ast.Node
		var cond ast.Expr
		switch l := p.(type) {
		case *ast.ForStmt:
			loop, cond = l, l.Cond
		case *ast.RangeStmt:
			loop = l
		case *ast.FuncLit:
			return false
		}
		if loop != nil && converges(pass, loop, cond) {
			return true
		}
		if p == body {
			break
		}
	}
	return false
}

func converges(pass *analysis.Pass, loop ast.Node, cond ast.Expr) bool {
	if cond != nil {
		found := false
		ast.Inspect(cond, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
					switch sel.Sel.Name {
					case "Contains", "ContainsAny", "Index", "MatchString", "Match":
						found = true
					}
				}
			}
			return !found
		})
		if found {
			return true
		}
	}
	found := false
	ast.Inspect(loop, func(n ast.Node) bool {
		b, ok := n.(*ast.BinaryExpr)
		if !ok || found || (b.Op != token.EQL && b.Op != token.NEQ) {
			return !found
		}
		if stringValue(pass, b.X) && stringValue(pass, b.Y) {
			found = true
		}
		return !found
	})
	return found
}

// stringValue: a non-constant string (or []byte via bytes.Equal is not
// a BinaryExpr; string comparison is the spelling this rule credits).
func stringValue(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value != nil {
		return false
	}
	b, ok := tv.Type.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

// site is one same-package call of a function.
type site struct {
	call    *ast.CallExpr
	parents map[ast.Node]ast.Node
	body    *ast.BlockStmt
}

// callSites indexes every same-package (non-test) call by callee.
func callSites(pass *analysis.Pass, decls []*ast.FuncDecl, byObj map[types.Object]*ast.FuncDecl) map[types.Object][]site {
	out := map[types.Object][]site{}
	for _, fd := range decls {
		var parents map[ast.Node]ast.Node
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var obj types.Object
			switch f := ast.Unparen(call.Fun).(type) {
			case *ast.Ident:
				obj = pass.TypesInfo.Uses[f]
			case *ast.SelectorExpr:
				obj = pass.TypesInfo.Uses[f.Sel]
			}
			if obj == nil || byObj[obj] == nil {
				return true
			}
			if parents == nil {
				parents = dominance.Parents(fd.Body)
			}
			out[obj] = append(out[obj], site{call, parents, fd.Body})
			return true
		})
	}
	return out
}

func allInFixpoint(pass *analysis.Pass, sites []site) bool {
	for _, s := range sites {
		if !inFixpointLoop(pass, s.call, s.parents, s.body) {
			return false
		}
	}
	return true
}
