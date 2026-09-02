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
// strings.ToUpper result (directly or through a local), and an
// EqualFold against an ASCII constant whose branch performs a lookup.
//
// The rule is deliberately silent on:
//   - maps of plain values (string/int/bool): word counts and alias
//     tables have no entry to impersonate.
//   - deny-list membership tests (`_, sensitive := m[fold]`): folding
//     look-alikes onto entries only makes a deny list stricter.
//   - maps populated with folded keys (both sides fold: Postgres
//     identifier semantics in the schema differ), and EqualFold whose
//     branch does not key a lookup on the folded value itself.
//   - folding that never indexes: ToLower for display, sorting, or
//     substring search.
//   - keys pinned ASCII first: an if in the same function that rejects
//     the value on a >= 0x80 comparison (or an *ascii*-named check)
//     before the lookup — the fixed lookupRuleLocked posture.
//   - user-facing text comparison: EqualFold on human strings is
//     correct; only EqualFold guarding a lookup/grant is reported.
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
	Name: "gofastrasciifold",
	Doc:  "forbids registry lookups keyed by strings.ToLower/ToUpper or guarded by EqualFold with an ASCII constant: Unicode case folding maps homoglyphs onto ASCII (ſ → S), so fold ASCII only or refuse non-ASCII keys first",
	Run:  run,
}

const maxDepth = 6

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		bound := boundExprs(pass, f)
		pinned := asciiPinned(pass, f)
		// Fold-populated maps: written somewhere in this file with a
		// folded key. Both sides then fold (liveLower[ToLower(col)] =
		// ...; byName[ToLower(f.Name)] = f), the symmetric comparison
		// Postgres identifier semantics ask for — not a fixed registry
		// an external input is folded ONTO.
		foldPopulated := foldWrittenMaps(pass, f, bound)
		// Discard lookups: `if _, sensitive := m[fold]; sensitive` — a
		// deny-list membership test. Folding extra look-alikes onto
		// entries only makes a deny list stricter; the value that would
		// be impersonated is thrown away.
		discards := discardLookups(pass, f)
		var ifStack []*ast.IfStmt
		ast.Inspect(f, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.IfStmt:
				ifStack = append(ifStack, e)
			case *ast.AssignStmt:
				// A folded WRITE into a map is population, not lookup.
				return e.Tok.String() != "=" || !isFoldWrite(pass, e, bound)
			case *ast.IndexExpr:
				if !isRegistry(pass, e.X) || discards[e.Pos()] {
					return true
				}
				if mapObj(pass, e.X); foldPopulated[mapObj(pass, e.X)] {
					return true
				}
				if inner := foldArg(pass, e.Index, bound, 0); inner != nil {
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
				// Only when the branch looks the FOLDED VALUE up (the
				// same variable keys a registry inside the branch): a
				// folded comparison over human text, or beside lookups
				// keyed by other variables, is fine.
				if len(ifStack) == 0 {
					return true
				}
				if obj := foldedOperand(pass, e.Args, bound); obj != nil && branchKeysOn(pass, ifStack[len(ifStack)-1], obj, bound) {
					pass.Reportf(e.Pos(), "EqualFold against an ASCII constant guards a lookup: Unicode case folding maps look-alikes onto ASCII (ſ → S), so a homoglyph passes the guard; compare ASCII only or refuse non-ASCII keys first")
				}
			}
			return true
		})
	}
	return nil, nil
}

// mapObj is the variable object behind a map expression, when it has one.
func mapObj(pass *analysis.Pass, x ast.Expr) types.Object {
	if id, ok := x.(*ast.Ident); ok {
		return pass.TypesInfo.ObjectOf(id)
	}
	return nil
}

// foldWrittenMaps collects map objects written with a folded key in this
// file.
func foldWrittenMaps(pass *analysis.Pass, f *ast.File, bound map[types.Object]ast.Expr) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range st.Lhs {
			if idx, ok := lhs.(*ast.IndexExpr); ok {
				if foldArg(pass, idx.Index, bound, 0) != nil {
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

// discardLookups collects the positions of folded lookups whose value is
// discarded (`_, ok := m[fold]`).
func discardLookups(pass *analysis.Pass, f *ast.File) map[token.Pos]bool {
	out := map[token.Pos]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		// Tuple (`_, v = m[k]`) and comma-ok (`_, ok := m[k]`) both.
		pairs := make([][2]ast.Expr, 0, len(st.Lhs))
		if len(st.Lhs) == len(st.Rhs) {
			for i := range st.Lhs {
				pairs = append(pairs, [2]ast.Expr{st.Lhs[i], st.Rhs[i]})
			}
		} else if len(st.Lhs) == 2 && len(st.Rhs) == 1 {
			pairs = append(pairs, [2]ast.Expr{st.Lhs[0], st.Rhs[0]})
		}
		for _, p := range pairs {
			if id, ok := p[0].(*ast.Ident); ok && id.Name == "_" {
				if idx, ok := p[1].(*ast.IndexExpr); ok {
					out[idx.Pos()] = true
				}
			}
		}
		return true
	})
	return out
}

// isFoldWrite reports whether an assignment writes a folded key into a
// map (`liveLower[ToLower(col)] = ...`).
func isFoldWrite(pass *analysis.Pass, st *ast.AssignStmt, bound map[types.Object]ast.Expr) bool {
	for _, lhs := range st.Lhs {
		if idx, ok := lhs.(*ast.IndexExpr); ok {
			if foldArg(pass, idx.Index, bound, 0) != nil {
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

// branchKeysOn reports whether the if's body indexes a map with the
// object itself (or a fold of it).
func branchKeysOn(pass *analysis.Pass, st *ast.IfStmt, obj types.Object, bound map[types.Object]ast.Expr) bool {
	found := false
	ast.Inspect(st.Body, func(n ast.Node) bool {
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return true
		}
		key := idx.Index
		if foldArg(pass, key, bound, 0) != nil {
			key = foldArg(pass, key, bound, 0)
		}
		if id, ok := key.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(id) == obj {
			if _, isMap := mapUnderlying(pass, idx.X); isMap {
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

// foldArg reports the argument of the strings.ToLower / strings.ToUpper
// call x is (or resolves through locals to), or nil when x is not a fold.
func foldArg(pass *analysis.Pass, x ast.Expr, bound map[types.Object]ast.Expr, depth int) ast.Expr {
	if depth > maxDepth {
		return nil
	}
	switch e := x.(type) {
	case *ast.ParenExpr:
		return foldArg(pass, e.X, bound, depth+1)
	case *ast.CallExpr:
		switch qualifiedCallee(pass, e.Fun) {
		case "strings.ToLower", "strings.ToUpper":
			if len(e.Args) == 1 {
				return e.Args[0]
			}
		}
		return nil
	case *ast.Ident:
		if obj := pass.TypesInfo.ObjectOf(e); obj != nil {
			if src, ok := bound[obj]; ok {
				return foldArg(pass, src, bound, depth+1)
			}
		}
		return nil
	}
	return nil
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

// asciiPinned collects objects an if-statement has pinned to ASCII: the
// condition both references the object and rejects on a >= 0x80 / 0x7f /
// 127 comparison or an *ascii*-named check — the
// strings.ContainsFunc(key, func(r rune) bool { return r >= 0x80 })
// posture the rule.go fix shipped.
func asciiPinned(pass *analysis.Pass, f *ast.File) map[types.Object]bool {
	pinned := map[types.Object]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.IfStmt)
		if !ok || st.Cond == nil {
			return true
		}
		marker := false
		ast.Inspect(st.Cond, func(c ast.Node) bool {
			switch e := c.(type) {
			case *ast.BasicLit:
				marker = marker || isASCIIBound(e)
			case *ast.CallExpr:
				name := ""
				switch fun := e.Fun.(type) {
				case *ast.Ident:
					name = fun.Name
				case *ast.SelectorExpr:
					name = fun.Sel.Name
				}
				marker = marker || containsFoldASCII(name)
			}
			return true
		})
		if !marker {
			return true
		}
		ast.Inspect(st.Cond, func(c ast.Node) bool {
			if id, ok := c.(*ast.Ident); ok {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					pinned[obj] = true
				}
			}
			return true
		})
		return true
	})
	return pinned
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
