package rawtext

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
)

// checkCtxless reports a UI component literal that leaves its Ctx
// field unset inside fn while a request context is in scope there.
// The component says its words through Ctx, so without it a Spanish
// page shows them in English: Meridian's header ThemeToggle and
// SignOut did, on /es/, until this arm.
//
// A context is in scope when fn (or a function enclosing a func
// literal) has a context.Context parameter or local, a parameter or
// receiver whose struct carries a Ctx context.Context field (cfg.Ctx),
// or an *http.Request parameter (r.Context()). The report names the
// first of those it finds, in that order. A literal bound to a
// variable whose Ctx is assigned later in the function is set.
//
// fn is a declared function, or a func literal outside any (a handler
// bound to a package-level var); recv is nil for a literal.
func checkCtxless(pass *analysis.Pass, ft *ast.FuncType, body *ast.BlockStmt, recv *ast.FieldList, ui uiReporter) {
	if body == nil {
		return
	}
	var walk func(body ast.Node, expr string)
	walk = func(body ast.Node, expr string) {
		setLater := ctxAssigned(pass, body)
		bound := boundLiterals(pass, body)
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				if n != body {
					inner := ctxExpr(pass, n.Type, n.Body, nil)
					if inner == "" {
						inner = expr
					}
					walk(n.Body, inner)
					return false
				}
			case *ast.CompositeLit:
				if expr != "" {
					reportCtxless(pass, n, expr, setLater, bound, ui)
				}
			}
			return true
		})
	}
	walk(body, ctxExpr(pass, ft, body, recv))
}

func reportCtxless(pass *analysis.Pass, lit *ast.CompositeLit, expr string, setLater map[types.Object]bool, bound map[*ast.CompositeLit]types.Object, ui uiReporter) {
	named, ok := types.Unalias(pass.TypesInfo.TypeOf(lit)).(*types.Named)
	if !ok || named.Obj().Pkg() == nil || !uiPackage(named.Obj().Pkg().Path()) {
		return
	}
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return
	}
	keys := ctxKeys(st, 0)
	if len(keys) == 0 {
		return
	}
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return // positional: every field is written
		}
		if id, ok := kv.Key.(*ast.Ident); ok && slices.Contains(keys, id.Name) {
			return
		}
	}
	if obj := bound[lit]; obj != nil && setLater[obj] {
		return
	}
	ui.reportf(lit, "%s.%s leaves Ctx unset while a request context is in scope: the component says its words through Ctx, so they render in English in every other locale; set Ctx: %s", named.Obj().Pkg().Name(), named.Obj().Name(), expr)
}

// hasCtxField reports whether st has a field Ctx of type
// context.Context, its own or promoted from an embedded struct.
func hasCtxField(st *types.Struct) bool {
	return len(ctxKeys(st, 0)) > 0
}

// ctxKeys returns the keys a literal of st writes Ctx through: "Ctx"
// itself, or the embedded field that carries it (a literal cannot key
// a promoted field). depth stops a struct that embeds a pointer to
// itself.
func ctxKeys(st *types.Struct, depth int) []string {
	if depth > 4 {
		return nil
	}
	var out []string
	for i := range st.NumFields() {
		f := st.Field(i)
		switch {
		case f.Name() == "Ctx" && isContext(f.Type()):
			out = append(out, "Ctx")
		case f.Embedded():
			t := f.Type()
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			if inner, ok := t.Underlying().(*types.Struct); ok && len(ctxKeys(inner, depth+1)) > 0 {
				out = append(out, f.Name())
			}
		}
	}
	return out
}

func isContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

func isRequest(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Request"
}

// carriesCtx reports whether t is a struct, or a pointer to one, with
// a Ctx context.Context field.
func carriesCtx(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	return ok && hasCtxField(st)
}

// ctxExpr returns the expression a function with this signature,
// receiver and body reaches a request context through, or "" when it
// has none.
func ctxExpr(pass *analysis.Pass, ft *ast.FuncType, body *ast.BlockStmt, recv *ast.FieldList) string {
	params := func(pick func(types.Type) bool) string {
		if ft.Params == nil {
			return ""
		}
		for _, p := range ft.Params.List {
			if t := pass.TypesInfo.TypeOf(p.Type); t != nil && pick(t) {
				for _, n := range p.Names {
					if n.Name != "_" {
						return n.Name
					}
				}
			}
		}
		return ""
	}
	if name := params(isContext); name != "" {
		return name
	}
	local := ""
	ast.Inspect(body, func(n ast.Node) bool {
		if local != "" {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if id, ok := n.(*ast.Ident); ok && id.Name != "_" {
			if v, ok := pass.TypesInfo.Defs[id].(*types.Var); ok && isContext(v.Type()) {
				local = id.Name
			}
		}
		return true
	})
	if local != "" {
		return local
	}
	if name := params(carriesCtx); name != "" {
		return name + ".Ctx"
	}
	if recv != nil && len(recv.List) > 0 && len(recv.List[0].Names) > 0 {
		if t := pass.TypesInfo.TypeOf(recv.List[0].Type); t != nil && carriesCtx(t) {
			return recv.List[0].Names[0].Name + ".Ctx"
		}
	}
	if name := params(isRequest); name != "" {
		return name + ".Context()"
	}
	return ""
}

// ctxAssigned collects the variables whose Ctx field body assigns
// (cfg.Ctx = ctx).
func ctxAssigned(pass *analysis.Pass, body ast.Node) map[types.Object]bool {
	out := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, l := range as.Lhs {
			sel, ok := l.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Ctx" {
				continue
			}
			if id, ok := sel.X.(*ast.Ident); ok {
				if obj := pass.TypesInfo.Uses[id]; obj != nil {
					out[obj] = true
				}
			}
		}
		return true
	})
	return out
}

// boundLiterals maps each literal bound to a variable (x := T{},
// x = &T{}, var x = T{}) to that variable.
func boundLiterals(pass *analysis.Pass, body ast.Node) map[*ast.CompositeLit]types.Object {
	out := map[*ast.CompositeLit]types.Object{}
	lit := func(e ast.Expr) *ast.CompositeLit {
		if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
			e = u.X
		}
		c, _ := e.(*ast.CompositeLit)
		return c
	}
	obj := func(id *ast.Ident) types.Object {
		if o := pass.TypesInfo.Defs[id]; o != nil {
			return o
		}
		return pass.TypesInfo.Uses[id]
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) != len(n.Rhs) {
				return true
			}
			for i, r := range n.Rhs {
				if c := lit(r); c != nil {
					if id, ok := n.Lhs[i].(*ast.Ident); ok {
						out[c] = obj(id)
					}
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) != len(n.Values) {
				return true
			}
			for i, v := range n.Values {
				if c := lit(v); c != nil {
					out[c] = obj(n.Names[i])
				}
			}
		}
		return true
	})
	return out
}
