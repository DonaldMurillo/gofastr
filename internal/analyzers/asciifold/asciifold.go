// Package asciifold catches registry lookups that fold Unicode case.
//
// Bug class: strings.ToLower/ToUpper (and EqualFold) use Unicode simple
// case mapping, which folds look-alikes ONTO ASCII — ſ uppercases to S —
// so a homoglyph spelling resolves a real catalog entry while reading
// like a typo in review. Found by the 2026-09-01 adversarial probes
// TestLookupRuleRejectsFoldHomoglyphs and
// TestSuppressionHomoglyphRefStaysUnknown (a rule reference "gofaſtr1003"
// resolved and silently suppressed GOFASTR1003 through
// rulesByID[strings.ToUpper(key)] in framework/contracts/rule.go). Fixed
// in 77fdbaf4 by refusing non-ASCII keys before folding.
//
// The rule reports an index into a REGISTRY — a map whose values are
// structs, struct pointers, or funcs — keyed by a strings.ToLower /
// strings.ToUpper result (directly, through a local, through a
// one-line fold helper, or through a fold variable
// `var lower = strings.ToUpper`), and an EqualFold against an ASCII
// constant whose branch performs a lookup.
//
// The rule is deliberately silent on:
//   - maps of plain values (string/int/bool): word counts and alias
//     tables have no entry to impersonate.
//   - membership tests that discard the value (`_, ok := m[fold]`)
//     only when the posture reads as denial: the map or the enclosing
//     function is named deny/block/sensitive/reserved/forbidden/
//     reject/strip, or the ok result is used negated — folding
//     look-alikes onto entries then only makes the list stricter. The
//     same syntax over an allow list, where folding makes the grant
//     weaker (a homoglyph inherits the entry's permission), is
//     reported. `_ = m[fold]` discards presence too: dead code, silent
//     unconditionally.
//   - maps populated with folded keys (both sides fold, local or
//     struct-held: put/get pairs behind methods fold identically), and
//     EqualFold whose branch does not key a REGISTRY on the folded
//     value itself.
//   - folding that never indexes: ToLower for display, sorting, or
//     substring search.
//   - keys pinned ASCII first: an if in the same function rejecting
//     the value on a comparison of a byte or rune VIEW of it — an
//     index expression (`name[i]`), a rune/byte conversion of one
//     (`rune(name[0])`), or a ContainsFunc/IndexFunc callback
//     parameter — against 127/128/0x7f/0x80, or an *ascii*-named
//     check taking the value. A len(...) bound shares no view with
//     the bytes ("ſ" is two bytes long and passes it) and pins
//     nothing.
//   - user-facing text comparison: EqualFold on human strings is
//     correct; only EqualFold guarding a registry lookup is reported.
package asciifold

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "asciifold",
	Doc:  "forbids registry lookups keyed by strings.ToLower/ToUpper or guarded by EqualFold with an ASCII constant: Unicode case folding maps homoglyphs onto ASCII (ſ → S), so fold ASCII only or refuse non-ASCII keys first",
	Run:  run,
}

const maxDepth = 6

func run(pass *analysis.Pass) (any, error) {
	helpers := foldHelpers(pass)
	foldVars := foldVariables(pass)
	for _, f := range pass.Files {
		bound := boundExprs(pass, f)
		pinned := asciiPinned(pass, f)
		// Fold-populated maps: written somewhere in this file with a
		// folded key. Both sides then fold (liveLower[ToLower(col)] =
		// ...; r.byName[ToLower(f.Name)] = f), the symmetric comparison
		// Postgres identifier semantics ask for — not a fixed registry
		// an external input is folded ONTO. Maps held in struct fields
		// count: put/get pairs behind methods are where they live.
		foldPopulated := foldWrittenMaps(pass, f, bound, helpers, foldVars)
		// Discard lookups: `if _, sensitive := m[fold]; sensitive` — a
		// membership test. Folding extra look-alikes onto entries only
		// makes a deny list stricter, so the silence is kept only for
		// denial-shaped postsures (see discardLookups).
		discards := discardLookups(pass, f)
		var ifStack []*ast.IfStmt
		var nodeStack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				// Inspect signals "children done" with a nil visit:
				// pop the node (and any if it closed).
				if len(nodeStack) > 0 {
					done := nodeStack[len(nodeStack)-1]
					nodeStack = nodeStack[:len(nodeStack)-1]
					if _, ok := done.(*ast.IfStmt); ok && len(ifStack) > 0 {
						ifStack = ifStack[:len(ifStack)-1]
					}
				}
				return true
			}
			nodeStack = append(nodeStack, n)
			pop := func() { nodeStack = nodeStack[:len(nodeStack)-1] }
			switch e := n.(type) {
			case *ast.IfStmt:
				ifStack = append(ifStack, e)
			case *ast.AssignStmt:
				// A folded WRITE into a map is population, not lookup.
				// Inspect calls f(nil) only after a visit that returned
				// true, so the children of this assignment will never
				// produce their "children done" visits: pop the node here
				// or the stack stays misaligned for the rest of the file
				// and a later close pops a stale node instead of the one
				// that actually ended — an IfStmt miss leaves ifStack
				// pointing at an if that no longer encloses the call.
				if e.Tok.String() == "=" && isFoldWrite(pass, e, bound, helpers, foldVars) {
					pop()
					return false
				}
			case *ast.IndexExpr:
				if !isRegistry(pass, e.X) || discards[e.Pos()] {
					return true
				}
				if foldPopulated[mapObj(pass, e.X)] {
					return true
				}
				if inner := foldArg(pass, e.Index, bound, helpers, foldVars, 0); inner != nil {
					if id, ok := inner.(*ast.Ident); !ok || !pinned[pass.TypesInfo.ObjectOf(id)] {
						pass.Reportf(e.Pos(), "registry lookup folds Unicode case: ToLower/ToUpper map look-alikes onto ASCII (ſ → S), so a homoglyph resolves a real entry; fold ASCII only or refuse non-ASCII keys first")
					}
				}
			case *ast.CallExpr:
				if qualifiedCallee(pass, e.Fun) != "strings.EqualFold" || len(e.Args) != 2 {
					return true
				}
				if !hasASCIIConstantArg(pass, e.Args) {
					return true
				}
				// Only when an ENCLOSING branch looks the FOLDED VALUE
				// up in a registry (the same variable keys one inside
				// the branch): a folded comparison over human text, or
				// beside lookups keyed by other variables, is fine.
				if len(ifStack) == 0 {
					return true
				}
				if obj := foldedOperand(pass, e.Args, bound); obj != nil && branchKeysOn(pass, ifStack[len(ifStack)-1], obj, bound, helpers, foldVars) {
					pass.Reportf(e.Pos(), "EqualFold against an ASCII constant guards a lookup: Unicode case folding maps look-alikes onto ASCII (ſ → S), so a homoglyph passes the guard; compare ASCII only or refuse non-ASCII keys first")
				}
			}
			return true
		})
	}
	return nil, nil
}

// mapObj is the object behind a map expression when it has one: the
// variable for an identifier, the field for a selector (`r.byName`) —
// both sides of a put/get pair behind methods key on the same field.
func mapObj(pass *analysis.Pass, x ast.Expr) types.Object {
	switch e := x.(type) {
	case *ast.Ident:
		return pass.TypesInfo.ObjectOf(e)
	case *ast.SelectorExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			if _, isPkg := pass.TypesInfo.Uses[id].(*types.PkgName); isPkg {
				return nil
			}
		}
		if obj := pass.TypesInfo.ObjectOf(e.Sel); obj != nil {
			return obj
		}
	}
	return nil
}

// foldWrittenMaps collects map objects written with a folded key in this
// file.
func foldWrittenMaps(pass *analysis.Pass, f *ast.File, bound map[types.Object]ast.Expr, helpers map[*types.Func]int, foldVars map[types.Object]bool) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range st.Lhs {
			if idx, ok := lhs.(*ast.IndexExpr); ok {
				if foldArg(pass, idx.Index, bound, helpers, foldVars, 0) != nil {
					if obj := mapObj(pass, idx.X); obj != nil {
						out[obj] = true
					}
				}
			}
		}
		return true
	})
	return out
}

// discardLookups collects the positions of discard-shaped lookups the
// rule stays silent on. `_ = m[k]` discards presence too — dead code,
// silent unconditionally. The comma-ok shape (`_, ok := m[fold]`) is
// silent only when the posture reads as denial: the map or the
// enclosing function is named deny/block/sensitive/reserved/forbidden/
// reject/strip, or the ok result is used negated. The syntax alone
// cannot tell a deny list from an allow list, and folding makes an
// allow list weaker — a homoglyph inherits the entry's permission —
// so the undecided spellings are reported, not silenced.
func discardLookups(pass *analysis.Pass, f *ast.File) map[token.Pos]bool {
	out := map[token.Pos]bool{}
	var funcs []*ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			funcs = append(funcs, fd)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		// Plain discard of value and presence alike: dead code.
		if len(st.Lhs) == 1 && len(st.Rhs) == 1 {
			if id, ok := st.Lhs[0].(*ast.Ident); ok && id.Name == "_" {
				if idx, ok := st.Rhs[0].(*ast.IndexExpr); ok {
					out[idx.Pos()] = true
				}
			}
			return true
		}
		// Comma-ok: `_, ok := m[k]`.
		if len(st.Lhs) != 2 || len(st.Rhs) != 1 {
			return true
		}
		if id, ok := st.Lhs[0].(*ast.Ident); !ok || id.Name != "_" {
			return true
		}
		idx, ok := st.Rhs[0].(*ast.IndexExpr)
		if !ok {
			return true
		}
		name := ""
		if obj := mapObj(pass, idx.X); obj != nil {
			name = obj.Name()
		}
		for _, fd := range funcs {
			if fd.Pos() <= idx.Pos() && idx.Pos() <= fd.End() {
				name += " " + fd.Name.Name
			}
		}
		denial := false
		for _, kw := range []string{"deny", "block", "sensitive", "reserved", "forbidden", "reject", "strip"} {
			if strings.Contains(strings.ToLower(name), kw) {
				denial = true
				break
			}
		}
		if !denial {
			if okID, ok := st.Lhs[1].(*ast.Ident); ok && okID.Name != "_" {
				if obj := pass.TypesInfo.ObjectOf(okID); obj != nil && usedNegated(pass, f, obj) {
					denial = true
				}
			}
		}
		if denial {
			out[idx.Pos()] = true
		}
		return true
	})
	return out
}

// usedNegated reports whether the object is read under a ! anywhere in
// the file. Objects are function-scoped, so a file scan cannot cross
// contaminate.
func usedNegated(pass *analysis.Pass, f *ast.File, obj types.Object) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if u, ok := n.(*ast.UnaryExpr); ok && u.Op == token.NOT {
			if id, ok := u.X.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// isFoldWrite reports whether an assignment writes a folded key into a
// map (`liveLower[ToLower(col)] = ...`).
func isFoldWrite(pass *analysis.Pass, st *ast.AssignStmt, bound map[types.Object]ast.Expr, helpers map[*types.Func]int, foldVars map[types.Object]bool) bool {
	for _, lhs := range st.Lhs {
		if idx, ok := lhs.(*ast.IndexExpr); ok {
			if foldArg(pass, idx.Index, bound, helpers, foldVars, 0) != nil {
				return true
			}
		}
	}
	return false
}

// foldedOperand resolves the non-constant EqualFold argument to its
// variable (TrimSpace and friends peeled).
func foldedOperand(pass *analysis.Pass, args []ast.Expr, bound map[types.Object]ast.Expr) types.Object {
	for _, a := range args {
		tv, ok := pass.TypesInfo.Types[a]
		if ok && tv.Value != nil {
			continue // the constant side
		}
		x := a
		for i := 0; i < 4; i++ {
			switch e := x.(type) {
			case *ast.ParenExpr:
				x = e.X
			case *ast.CallExpr:
				if qualifiedCallee(pass, e.Fun) == "strings.TrimSpace" && len(e.Args) == 1 {
					x = e.Args[0]
				} else {
					return nil
				}
			case *ast.Ident:
				return pass.TypesInfo.ObjectOf(e)
			default:
				return nil
			}
		}
	}
	return nil
}

// branchKeysOn reports whether the if's body indexes a REGISTRY with
// the object itself (or a fold of it).
func branchKeysOn(pass *analysis.Pass, st *ast.IfStmt, obj types.Object, bound map[types.Object]ast.Expr, helpers map[*types.Func]int, foldVars map[types.Object]bool) bool {
	found := false
	ast.Inspect(st.Body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		key := idx.Index
		if foldArg(pass, key, bound, helpers, foldVars, 0) != nil {
			key = foldArg(pass, key, bound, helpers, foldVars, 0)
		}
		if id, ok := key.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
			if isRegistry(pass, idx.X) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// isRegistry reports whether x is a map whose values are structs, struct
// pointers, or funcs — something with entries to impersonate, not a
// word count.
func isRegistry(pass *analysis.Pass, x ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[x]
	if !ok || tv.Type == nil {
		return false
	}
	m, ok := tv.Type.Underlying().(*types.Map)
	if !ok {
		return false
	}
	switch v := m.Elem().Underlying().(type) {
	case *types.Struct:
		return true
	case *types.Pointer:
		_, isStruct := v.Elem().Underlying().(*types.Struct)
		return isStruct
	case *types.Signature:
		return true
	}
	return false
}

// foldArg reports the argument whose bytes the strings.ToLower /
// strings.ToUpper call x folds (or resolves through locals, one-line
// fold helpers, fold variables, and a TrimSpace around the fold to),
// or nil when x is not a fold.
func foldArg(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr, helpers map[*types.Func]int, foldVars map[types.Object]bool, depth int) ast.Expr {
	if depth > maxDepth {
		return nil
	}
	switch e := x.(type) {
	case *ast.ParenExpr:
		return foldArg(pass, e.X, bound, helpers, foldVars, depth+1)
	case *ast.CallExpr:
		switch qualifiedCallee(pass, e.Fun) {
		case "strings.ToLower", "strings.ToUpper":
			if len(e.Args) == 1 {
				return e.Args[0]
			}
		case "strings.TrimSpace":
			// A trim around the fold is still a folded key.
			if len(e.Args) == 1 {
				if inner := foldArg(pass, e.Args[0], bound, helpers, foldVars, depth+1); inner != nil {
					return inner
				}
			}
		}
		// A one-line fold helper: `func norm(s string) string {
		// return strings.ToLower(s) }` keyed as gadgets[norm(name)].
		if fn, ok := calleeFunc(pass, e.Fun); ok {
			if pi, ok := helpers[fn]; ok && pi < len(e.Args) {
				return e.Args[pi]
			}
		}
		// A fold variable: `var lower = strings.ToUpper`.
		if id, ok := e.Fun.(*ast.Ident); ok && len(e.Args) == 1 {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil && foldVars[obj] {
				return e.Args[0]
			}
		}
		return nil
	case *ast.Ident:
		if obj := pass.TypesInfo.ObjectOf(e); obj != nil {
			if src, ok := bound[obj]; ok {
				return foldArg(pass, src, bound, helpers, foldVars, depth+1)
			}
		}
		return nil
	}
	return nil
}

// foldHelpers maps one-result functions whose body is a single return
// of a fold (through trims) to the index of the parameter the fold
// wraps: `func norm(s string) string { return strings.ToLower(s) }`.
func foldHelpers(pass *analysis.Pass) map[*types.Func]int {
	out := map[*types.Func]int{}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type.Results == nil {
				continue
			}
			if len(fd.Type.Results.List) != 1 || len(fd.Body.List) != 1 {
				continue
			}
			ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			if pi, ok := foldParamIndex(pass, ret.Results[0], fn); ok {
				out[fn] = pi
			}
		}
	}
	return out
}

// foldParamIndex peels folds and trims down to a parameter of fn and
// returns that parameter's index.
func foldParamIndex(pass *analysis.Pass, e ast.Expr, fn *types.Func) (int, bool) {
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return 0, false
	}
	params := sig.Params()
	for range maxDepth {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.CallExpr:
			switch qualifiedCallee(pass, x.Fun) {
			case "strings.ToLower", "strings.ToUpper", "strings.TrimSpace":
				if len(x.Args) != 1 {
					return 0, false
				}
				e = x.Args[0]
			default:
				return 0, false
			}
		case *ast.Ident:
			if obj := pass.TypesInfo.ObjectOf(x); obj != nil {
				for i := range params.Len() {
					if params.At(i) == obj {
						return i, true
					}
				}
			}
			return 0, false
		default:
			return 0, false
		}
	}
	return 0, false
}

// foldVariables collects package-level `var f = strings.ToLower` (or
// ToUpper) bindings: a fold reached through a function variable.
func foldVariables(pass *analysis.Pass) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				switch qualifiedCallee(pass, vs.Values[0]) {
				case "strings.ToLower", "strings.ToUpper":
					if obj := pass.TypesInfo.ObjectOf(vs.Names[0]); obj != nil {
						out[obj] = true
					}
				}
			}
		}
	}
	return out
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

// hasASCIIConstantArg reports whether one EqualFold argument is a
// constant that is ASCII-only and non-empty.
func hasASCIIConstantArg(pass *analysis.Pass, args []ast.Expr) bool {
	for _, a := range args {
		tv, ok := pass.TypesInfo.Types[a]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			continue
		}
		s := constant.StringVal(tv.Value)
		if s == "" {
			continue
		}
		ascii := true
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				ascii = false
				break
			}
		}
		if ascii {
			return true
		}
	}
	return false
}

// asciiPinned collects objects an if-statement has pinned to ASCII: a
// comparison of a byte or rune VIEW of the value against the ASCII
// boundary (127/128/0x7f/0x80) — an index expression (`name[i]`), a
// rune/byte conversion of one (`rune(name[0])`), or a
// ContainsFunc/IndexFunc callback parameter ranging over it, the
// strings.ContainsFunc(key, func(r rune) bool { return r >= 0x80 })
// posture the rule.go fix shipped — or an *ascii*-named check taking
// the value directly. A bare len(...) bound shares no view with the
// value's bytes — "ſ" is two bytes long and passes it — so it pins
// nothing, and neither do the other identifiers that merely share the
// condition.
func asciiPinned(pass *analysis.Pass, f *ast.File) map[types.Object]bool {
	pinned := map[types.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.IfStmt)
		if !ok || st.Cond == nil {
			return true
		}
		// Callback parameters of ContainsFunc/IndexFunc over a
		// variable stand for that variable's runes.
		cb := map[types.Object]types.Object{}
		ast.Inspect(st.Cond, func(c ast.Node) bool {
			call, ok := c.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 {
				return true
			}
			switch calleeName(call.Fun) {
			case "ContainsFunc", "IndexFunc":
			default:
				return true
			}
			src, ok := call.Args[0].(*ast.Ident)
			if !ok {
				return true
			}
			lit, ok := call.Args[1].(*ast.FuncLit)
			if !ok || len(lit.Type.Params.List) != 1 {
				return true
			}
			srcObj := pass.TypesInfo.ObjectOf(src)
			for _, name := range lit.Type.Params.List[0].Names {
				if obj := pass.TypesInfo.ObjectOf(name); obj != nil {
					cb[obj] = srcObj
				}
			}
			return true
		})
		ast.Inspect(st.Cond, func(c ast.Node) bool {
			switch e := c.(type) {
			case *ast.BinaryExpr:
				switch e.Op {
				case token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ:
				default:
					return true
				}
				for _, side := range [2]struct{ view, lit ast.Expr }{{e.X, e.Y}, {e.Y, e.X}} {
					lit, ok := side.lit.(*ast.BasicLit)
					if !ok || !isASCIIBound(lit) {
						continue
					}
					if obj := byteRuneView(pass, side.view, cb); obj != nil {
						pinned[obj] = true
					}
				}
			case *ast.CallExpr:
				if containsFoldASCII(calleeName(e.Fun)) {
					for _, a := range e.Args {
						if id, ok := a.(*ast.Ident); ok {
							if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
								pinned[obj] = true
							}
						}
					}
				}
			}
			return true
		})
		return true
	})
	return pinned
}

// byteRuneView resolves an expression that exposes the bytes or runes
// of a variable to that variable's object: a ContainsFunc/IndexFunc
// callback parameter mapped to it, an index expression on it
// (`name[i]`), or a rune/byte conversion of such an index
// (`rune(name[0])`). Anything else computed over the variable — len
// above all — is not a view and pins nothing.
func byteRuneView(pass *analysis.Pass, x ast.Expr, cb map[types.Object]types.Object) types.Object {
	switch e := x.(type) {
	case *ast.ParenExpr:
		return byteRuneView(pass, e.X, cb)
	case *ast.Ident:
		if obj := pass.TypesInfo.ObjectOf(e); obj != nil {
			if src, ok := cb[obj]; ok && src != nil {
				return src
			}
		}
		return nil
	case *ast.IndexExpr:
		if id, ok := e.X.(*ast.Ident); ok {
			return pass.TypesInfo.ObjectOf(id)
		}
	case *ast.CallExpr:
		if id, ok := e.Fun.(*ast.Ident); ok && (id.Name == "rune" || id.Name == "byte") && len(e.Args) == 1 {
			return byteRuneView(pass, e.Args[0], cb)
		}
	}
	return nil
}

func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// isASCIIBound reports whether a literal is the ASCII boundary (0x80,
// 0x7f, 128, 127) or a string starting past it.
func isASCIIBound(lit *ast.BasicLit) bool {
	if lit.Kind == token.INT {
		switch lit.Value {
		case "128", "127", "0x80", "0x7f", "0x7F", "0X80", "0X7f", "0X7F":
			return true
		}
		return false
	}
	if lit.Kind == token.STRING && len(lit.Value) >= 2 {
		v := lit.Value[1 : len(lit.Value)-1]
		return v == "\\x80" || v == "\\u017f" || v == "ſ"
	}
	return false
}

func containsFoldASCII(name string) bool {
	return strings.Contains(strings.ToLower(name), "ascii")
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
			if st.Tok.String() != ":=" && st.Tok.String() != "=" {
				return true
			}
			if len(st.Lhs) == len(st.Rhs) {
				for idx, lhs := range st.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
						if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
							m[obj] = st.Rhs[idx]
						}
					}
				}
			} else if len(st.Lhs) == 2 && len(st.Rhs) == 1 {
				if id, ok := st.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
					if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
						m[obj] = st.Rhs[0]
					}
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

func qualifiedCallee(pass *analysis.Pass, fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			if pkg, ok := pass.TypesInfo.Uses[id].(*types.PkgName); ok {
				return pkg.Imported().Name() + "." + f.Sel.Name
			}
		}
	}
	return ""
}
