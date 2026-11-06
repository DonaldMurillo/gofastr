package rawtext

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// numberText reports the call that turns e into Go's digits:
// strconv.Itoa, FormatInt, FormatUint or FormatFloat, or fmt.Sprint or
// Sprintf whose every value argument is a plain number. A concatenation
// is number text when one of its operands is, and a local variable when
// the function assigns it number text (localNumber): the two-line
// n := strconv.Itoa(size) hoist reaches the phrase just the same.
func numberText(pass *analysis.Pass, e ast.Expr) (string, bool) {
	e = ast.Unparen(e)
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		if s, ok := numberText(pass, bin.X); ok {
			return s, true
		}
		return numberText(pass, bin.Y)
	}
	if id, ok := e.(*ast.Ident); ok {
		if v, ok := pass.TypesInfo.Uses[id].(*types.Var); ok {
			if s, ok := localNumber(pass)[v]; ok {
				return s, true
			}
		}
	}
	return numberCall(pass, e)
}

// numberCall is numberText with no variables followed.
func numberCall(pass *analysis.Pass, e ast.Expr) (string, bool) {
	e = ast.Unparen(e)
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		if s, ok := numberCall(pass, bin.X); ok {
			return s, true
		}
		return numberCall(pass, bin.Y)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	pkg, name := callee(pass, call)
	switch {
	case pkg == "strconv" && (name == "Itoa" || name == "FormatInt" || name == "FormatUint" || name == "FormatFloat"):
		return "strconv." + name, true
	case pkg == "fmt" && (name == "Sprint" || name == "Sprintf"):
		args := call.Args
		if name == "Sprintf" && len(args) > 0 {
			args = args[1:]
		}
		if len(args) == 0 {
			return "", false
		}
		for _, a := range args {
			if !plainNumber(pass, a) {
				return "", false
			}
		}
		return "fmt." + name, true
	}
	return "", false
}

// plainNumber reports whether e is an integer or float that prints as
// digits: not a Stringer (a time.Duration prints "1s") or an error.
func plainNumber(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil || types.Implements(t, errorType) {
		return false
	}
	if obj, _, _ := types.LookupFieldOrMethod(t, true, nil, "String"); obj != nil {
		if _, isFunc := obj.(*types.Func); isFunc {
			return false
		}
	}
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&(types.IsInteger|types.IsFloat) != 0
}

// phraseSink reports whether call fills a phrase or template from a
// params map: Phrase.Text, Translator.T, and i18n's T, Fill,
// FillContext, NewError and Wrap.
func phraseSink(pass *analysis.Pass, call *ast.CallExpr) bool {
	if pkg, name := callee(pass, call); pkg == module+"core/i18n" {
		switch name {
		case "T", "Fill", "FillContext", "NewError", "Wrap":
			return true
		}
		return false
	}
	fn := calleeFunc(pass, call)
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	recv := sig.Recv().Type()
	if p, isPtr := recv.(*types.Pointer); isPtr {
		recv = p.Elem()
	}
	named, ok := types.Unalias(recv).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != module+"core/i18n" {
		return false
	}
	switch named.Obj().Name() + "." + fn.Name() {
	case "Phrase.Text", "Translator.T":
		return true
	}
	return false
}

// phraseForwarders lists the package's functions and methods that take
// a variadic or map parameter and call a phrase sink: a helper such as
// headless's fill(tpl, kv...), which builds the params map itself. Its
// call sites are sinks too, or a strconv.Itoa passed to it would reach
// the phrase unseen.
func phraseForwarders(pass *analysis.Pass) map[*types.Func]bool {
	out := map[*types.Func]bool{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok || !takesParams(fn.Type().(*types.Signature)) {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && phraseSink(pass, call) {
					out[fn] = true
				}
				return !out[fn]
			})
		}
	}
	return out
}

// takesParams reports whether sig ends in a variadic parameter or takes
// a map: the two shapes a params-forwarding helper has.
func takesParams(sig *types.Signature) bool {
	ps := sig.Params()
	if sig.Variadic() {
		return true
	}
	for i := range ps.Len() {
		if _, ok := ps.At(i).Type().Underlying().(*types.Map); ok {
			return true
		}
	}
	return false
}

// calleeFunc is the function or method a call names statically, or nil.
func calleeFunc(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = fun.Sel
	case *ast.Ident:
		id = fun
	default:
		return nil
	}
	fn, _ := pass.TypesInfo.Uses[id].(*types.Func)
	return fn
}

// checkPhraseParams reports a stringified number in a params map
// literal handed straight to a phrase sink, in any argument of a
// helper that forwards to one, or in a map hoisted into a local first.
func checkPhraseParams(pass *analysis.Pass, call *ast.CallExpr, ui uiReporter) {
	forwarded := false
	if !phraseSink(pass, call) {
		if forwarded = ui.forwarders[calleeFunc(pass, call)]; !forwarded {
			return
		}
	}
	for _, arg := range call.Args {
		if forwarded {
			if fn, ok := numberText(pass, arg); ok {
				ui.reportf(arg, "%s turns a number into Go's digits before the phrase sees it: pass the number itself, which phrase params write in the reader's locale and read to pick the plural form", fn)
				continue
			}
		}
		lit, hoisted, ok := paramsMapArg(pass, arg)
		if !ok {
			continue
		}
		for _, e := range lit.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if fn, ok := numberText(pass, kv.Value); ok {
				// The hoisted map reports at the sink: the digits may
				// sit lines above the phrase that reads them.
				at := kv.Value
				if hoisted {
					at = arg
				}
				ui.reportf(at, "%s turns a number into Go's digits before the phrase sees it: pass the number itself, which phrase params write in the reader's locale and read to pick the plural form", fn)
			}
		}
	}
}

// paramsMapArg returns the params map literal an argument carries —
// the literal itself, or the map a local was declared with when no
// later assignment or index write changed it (params := map[string]
// any{…}; phrase.Text(ctx, params)) — and whether it was hoisted.
func paramsMapArg(pass *analysis.Pass, arg ast.Expr) (lit *ast.CompositeLit, hoisted, ok bool) {
	e := ast.Unparen(arg)
	if l, isLit := e.(*ast.CompositeLit); isLit {
		if _, isMap := pass.TypesInfo.TypeOf(l).Underlying().(*types.Map); isMap {
			return l, false, true
		}
		return nil, false, false
	}
	id, isIdent := e.(*ast.Ident)
	if !isIdent {
		return nil, false, false
	}
	v, isVar := pass.TypesInfo.Uses[id].(*types.Var)
	if !isVar {
		return nil, false, false
	}
	if l := localParams(pass)[v]; l != nil {
		return l, true, true
	}
	return nil, false, false
}

// localParamsCache holds each pass's localParams map while it runs.
var localParamsCache sync.Map // *analysis.Pass -> map[*types.Var]*ast.CompositeLit

// localParams maps each function-scoped variable declared with a map
// literal (params := map[string]any{…}) onto that literal, when no
// other assignment, and no index write into it, may have changed what
// it holds.
func localParams(pass *analysis.Pass) map[*types.Var]*ast.CompositeLit {
	if m, ok := localParamsCache.Load(pass); ok {
		return m.(map[*types.Var]*ast.CompositeLit)
	}
	out := map[*types.Var]*ast.CompositeLit{}
	poisoned := map[*types.Var]bool{}
	localVar := func(e ast.Expr) *types.Var {
		id, ok := ast.Unparen(e).(*ast.Ident)
		if !ok {
			return nil
		}
		v, _ := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if v == nil || v.IsField() || v.Parent() == nil || v.Parent() == pass.Pkg.Scope() {
			return nil
		}
		return v
	}
	record := func(lhs, rhs ast.Expr) {
		v := localVar(lhs)
		if v == nil {
			return
		}
		lit, isMap := ast.Unparen(rhs).(*ast.CompositeLit)
		if isMap {
			_, isMap = pass.TypesInfo.TypeOf(lit).Underlying().(*types.Map)
		}
		// Only a local bound to a map literal once and never touched
		// again is followed: any later binding may be the live one at
		// the sink.
		if !isMap || out[v] != nil || poisoned[v] {
			poisoned[v] = true
			return
		}
		out[v] = lit
	}
	poisonIndex := func(lhs ast.Expr) {
		idx, ok := lhs.(*ast.IndexExpr)
		if !ok {
			return
		}
		if v := localVar(idx.X); v != nil {
			poisoned[v] = true
		}
	}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch st := n.(type) {
			case *ast.AssignStmt:
				for _, l := range st.Lhs {
					poisonIndex(l)
				}
				if len(st.Lhs) != len(st.Rhs) {
					return true
				}
				if st.Tok != token.ASSIGN && st.Tok != token.DEFINE {
					return true // an op-assign keeps what the variable holds
				}
				for i, l := range st.Lhs {
					record(l, st.Rhs[i])
				}
			case *ast.GenDecl:
				if st.Tok != token.VAR {
					return true
				}
				for _, spec := range st.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for i, name := range vs.Names {
						if i < len(vs.Values) {
							record(name, vs.Values[i])
						}
					}
				}
			}
			return true
		})
	}
	for v := range poisoned {
		delete(out, v)
	}
	localParamsCache.Store(pass, out)
	return out
}

// reportNumberText reports number text written into the page, at a
// render.Text call, a label attribute or a reader-facing field.
func reportNumberText(pass *analysis.Pass, e ast.Expr, ui uiReporter, where string) {
	if fn, ok := numberText(pass, e); ok {
		ui.reportf(e, "%s writes %s in Go's digits: write the number with i18n.Number(ctx, n) (or i18n.Percent, i18n.Currency), which follows the reader's locale", fn, where)
	}
}
