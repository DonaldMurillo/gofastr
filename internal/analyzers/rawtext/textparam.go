package rawtext

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// textParams maps each of the package's functions and methods to the
// string parameters it writes into the page: a parameter passed straight
// to render.Text, set as a label attribute (in a map literal or by
// index, attrs["aria-label"] = p), set on a reader-facing field of a UI
// component, or handed on at such a position of another function in the
// map. Its call sites are uitext sinks too: chartEmpty(..., "No data
// yet", ...) shipped English to every empty chart because the literal
// sat one call away from the attribute it filled.
func textParams(pass *analysis.Pass) map[*types.Func]map[int]bool {
	type decl struct {
		fn     *types.Func
		body   *ast.BlockStmt
		params map[*types.Var]int
	}
	var decls []decl
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			sig := fn.Type().(*types.Signature)
			params := map[*types.Var]int{}
			for i := range sig.Params().Len() {
				p := sig.Params().At(i)
				if b, ok := p.Type().Underlying().(*types.Basic); ok && b.Kind() == types.String && !(sig.Variadic() && i == sig.Params().Len()-1) {
					params[p] = i
				}
			}
			if len(params) > 0 {
				decls = append(decls, decl{fn, fd.Body, params})
			}
		}
	}
	out := map[*types.Func]map[int]bool{}
	mark := func(d decl, e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return false
		}
		v, _ := pass.TypesInfo.Uses[id].(*types.Var)
		i, ok := d.params[v]
		if !ok || out[d.fn][i] {
			return false
		}
		if out[d.fn] == nil {
			out[d.fn] = map[int]bool{}
		}
		out[d.fn][i] = true
		return true
	}
	// Iterate to a fixpoint: a helper that forwards to another helper
	// becomes a sink once the inner one is known to be.
	for changed := true; changed; {
		changed = false
		for _, d := range decls {
			ast.Inspect(d.body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if pkg, name := callee(pass, n); pkg == module+"core/render" && name == "Text" && len(n.Args) == 1 {
						changed = mark(d, n.Args[0]) || changed
					}
					if sinks := out[calleeFunc(pass, n)]; sinks != nil {
						for i, arg := range n.Args {
							if sinks[i] {
								changed = mark(d, arg) || changed
							}
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range n.Lhs {
						if i < len(n.Rhs) && labelIndex(pass, lhs) {
							changed = mark(d, n.Rhs[i]) || changed
						}
					}
				case *ast.CompositeLit:
					for _, val := range textValues(pass, n) {
						changed = mark(d, val) || changed
					}
				}
				return true
			})
		}
	}
	return out
}

// labelIndex reports whether e is m["aria-label"] (or another label
// attribute) on a string-keyed map.
func labelIndex(pass *analysis.Pass, e ast.Expr) bool {
	ix, ok := e.(*ast.IndexExpr)
	if !ok {
		return false
	}
	if _, ok := pass.TypesInfo.TypeOf(ix.X).Underlying().(*types.Map); !ok {
		return false
	}
	k := pass.TypesInfo.Types[ix.Index]
	return k.Value != nil && k.Value.Kind() == constant.String && labelAttrs[constant.StringVal(k.Value)]
}

// textValues are the values a composite literal writes into the page:
// label attributes of a string-keyed map, and reader-facing string
// fields of a UI component.
func textValues(pass *analysis.Pass, lit *ast.CompositeLit) []ast.Expr {
	t := pass.TypesInfo.TypeOf(lit)
	if t == nil {
		return nil
	}
	var out []ast.Expr
	if named, ok := types.Unalias(t).(*types.Named); ok {
		if st, ok := named.Underlying().(*types.Struct); ok && named.Obj().Pkg() != nil && uiPackage(named.Obj().Pkg().Path()) {
			for i, val := range litFields(lit, st) {
				if f := st.Field(i); uiField(f.Name()) {
					out = append(out, val)
				}
			}
		}
	}
	if _, ok := t.Underlying().(*types.Map); ok {
		for _, e := range lit.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				if k := pass.TypesInfo.Types[kv.Key]; k.Value != nil && k.Value.Kind() == constant.String && labelAttrs[constant.StringVal(k.Value)] {
					out = append(out, kv.Value)
				}
			}
		}
	}
	return out
}

// checkTextArgs reports English passed to a parameter that textParams
// found writing into the page.
func checkTextArgs(pass *analysis.Pass, call *ast.CallExpr, ui uiReporter) {
	fn := calleeFunc(pass, call)
	sinks := ui.textParams[fn]
	for i, arg := range call.Args {
		if !sinks[i] {
			continue
		}
		if s, ok := uiProse(pass, arg); ok {
			ui.reportf(arg, "%s's %s parameter writes %q into the page in English: define it as an i18n.Phrase at package level and pass phrase.Text(ctx)", fn.Name(), fn.Type().(*types.Signature).Params().At(i).Name(), s)
		}
		reportNumberText(pass, arg, ui, "a number into "+fn.Name())
	}
}
