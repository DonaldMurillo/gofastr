package embedskip

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The three 2026-11 arms share one scope: a reflect field walk the
// embedskip loop detector recognizes. The pointer-gate and
// TextUnmarshaler arms take the tag-keyed walk and every same-package
// function that reaches one (Bind reaches collectTagged through
// bindQuery); the cycle arm takes any function whose own body holds a
// field walk, tag-keyed or not, because descending is exactly what this
// analyzer pushes code toward and a descending walk is the one that can
// loop.

const (
	msgPointerGate = "struct kind gate on reflect.ValueOf(%s).Elem() binds nothing for a pointer-typed input (Handler[*T, O]): follow the pointer levels down first (a derefType loop) before testing for reflect.Struct"
	msgTextBinder  = "tag binder sets fields by kind with no encoding.TextUnmarshaler check: a time.Time, UUID or validated enum tagged field fails or binds raw; call UnmarshalText first when the field's address implements it"
	msgPtrCycle    = "reflect walk recurses through a pointer with no visited set: a value that points back into itself recurses until the stack runs out; remember the pointers walked (map[uintptr]bool on v.Pointer())"
)

// funcInfo is one package function the arms judge.
type funcInfo struct {
	decl    *ast.FuncDecl
	callees map[*types.Func]bool
}

func runArms(pass *analysis.Pass) {
	funcs := map[*types.Func]*funcInfo{}
	var order []*types.Func
	tagWalk := map[*types.Func]bool{}
	anyWalk := map[*types.Func]bool{}
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			obj, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			fi := &funcInfo{decl: fd, callees: map[*types.Func]bool{}}
			funcs[obj] = fi
			order = append(order, obj)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					if callee := staticCallee(pass, x.Fun); callee != nil && callee.Pkg() == pass.Pkg {
						fi.callees[callee] = true
					}
				case *ast.ForStmt, *ast.RangeStmt:
					bound, body := loopBound(pass, x)
					if body != nil && isNumFieldCall(pass, bound) {
						keyed := keyedByTag(pass, body)
						if keyed {
							tagWalk[obj] = true
						}
						// The cycle arm's scope: a tag-keyed walk, or one
						// that descends (the posture this analyzer asks
						// for, and the one that can loop).
						if keyed || descends(pass, body) {
							anyWalk[obj] = true
						}
					}
				}
				return true
			})
		}
	}
	if len(funcs) == 0 {
		return
	}

	// reach: tag walkers and every function that calls into one.
	reach := map[*types.Func]bool{}
	for fn := range tagWalk {
		reach[fn] = true
	}
	for changed := true; changed; {
		changed = false
		for _, fn := range order {
			if reach[fn] {
				continue
			}
			for c := range funcs[fn].callees {
				if reach[c] {
					reach[fn] = true
					changed = true
					break
				}
			}
		}
	}

	callers := map[*types.Func][]*types.Func{}
	for _, fn := range order {
		for c := range funcs[fn].callees {
			callers[c] = append(callers[c], fn)
		}
	}
	mentionsTU := map[*types.Func]bool{}
	for _, fn := range order {
		if mentionsTextUnmarshaler(pass, funcs[fn].decl.Body) {
			mentionsTU[fn] = true
		}
	}
	handlesTU := func(fn *types.Func) bool {
		if mentionsTU[fn] {
			return true
		}
		for c := range funcs[fn].callees {
			if mentionsTU[c] {
				return true
			}
		}
		for _, c := range callers[fn] {
			if mentionsTU[c] {
				return true
			}
		}
		return false
	}

	for _, fn := range order {
		fi := funcs[fn]
		if reach[fn] {
			checkPointerGate(pass, fi.decl)
		}
		if len(tagWalk) > 0 && !handlesTU(fn) {
			if pos, ok := kindSetter(pass, fi.decl); ok {
				pass.Reportf(pos, msgTextBinder)
			}
		}
		if anyWalk[fn] {
			checkPointerCycle(pass, fn, fi.decl)
		}
	}
}

func loopBound(pass *analysis.Pass, n ast.Node) (ast.Expr, *ast.BlockStmt) {
	switch s := n.(type) {
	case *ast.ForStmt:
		var bound ast.Expr
		if b, ok := s.Cond.(*ast.BinaryExpr); ok {
			bound = b.Y
			if isNumFieldCall(pass, b.X) {
				bound = b.X
			}
		}
		return bound, s.Body
	case *ast.RangeStmt:
		return s.X, s.Body
	}
	return nil, nil
}

func staticCallee(pass *analysis.Pass, fun ast.Expr) *types.Func {
	switch f := fun.(type) {
	case *ast.IndexExpr: // generic instantiation f[T](...)
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	id := calleeIdent(fun)
	if id == nil {
		return nil
	}
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok {
		return nil
	}
	return fn.Origin()
}

// ---- arm 1: the entry gate with no pointer-level loop (8708ddd5) ----

// checkPointerGate reports `E.Kind() != reflect.Struct` (or ==) where E
// is one Elem() below reflect.ValueOf/TypeOf of an interface-typed
// parameter: the destination pointer's pointee, which is itself a
// pointer for a Handler[*T, O] input. A pointer-kind test on E's
// variable after that Elem and before the gate (a deref loop, an
// explicit second level) silences it.
func checkPointerGate(pass *analysis.Pass, fd *ast.FuncDecl) {
	params := map[types.Object]bool{}
	for _, fl := range fd.Type.Params.List {
		for _, n := range fl.Names {
			if obj := pass.TypesInfo.Defs[n]; obj != nil && types.IsInterface(obj.Type()) {
				params[obj] = true
			}
		}
	}
	if len(params) == 0 {
		return
	}
	assigns := localAssigns(pass, fd.Body)
	ptrTests := kindTests(pass, fd.Body, "Pointer", "Ptr")

	// A != gate whose branch returns a built error refuses the input out
	// loud (config.Load, di.Inject: "expected pointer to struct"); the
	// bug is the silent skip, a branch that binds nothing and goes on.
	refused := map[*ast.BinaryExpr]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || !returnsBuiltError(pass, ifs.Body) {
			return true
		}
		ast.Inspect(ifs.Cond, func(m ast.Node) bool {
			if be, ok := m.(*ast.BinaryExpr); ok && be.Op == token.NEQ {
				refused[be] = true
			}
			return true
		})
		return true
	})
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || (be.Op != token.NEQ && be.Op != token.EQL) || refused[be] {
			return true
		}
		var operand ast.Expr
		switch {
		case isReflectKindConst(pass, be.Y, "Struct"):
			operand = be.X
		case isReflectKindConst(pass, be.X, "Struct"):
			operand = be.Y
		default:
			return true
		}
		e, ok := kindReceiver(pass, operand)
		if !ok {
			return true
		}
		derefPos, param, ok := singleDeref(pass, e, be.Pos(), assigns, params, 0)
		if !ok {
			return true
		}
		if root := rootObj(pass, e); root != nil {
			for _, t := range ptrTests {
				if t.obj == root && t.pos > derefPos && t.pos < be.Pos() {
					return true
				}
			}
		}
		pass.Reportf(be.Pos(), msgPointerGate, param)
		return true
	})
}

// returnsBuiltError reports whether body ends in a return whose last
// result is an errors.New / fmt.Errorf call.
func returnsBuiltError(pass *analysis.Pass, body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	r, ok := body.List[len(body.List)-1].(*ast.ReturnStmt)
	if !ok || len(r.Results) == 0 {
		return false
	}
	call, ok := ast.Unparen(r.Results[len(r.Results)-1]).(*ast.CallExpr)
	if !ok {
		return false
	}
	fn := staticCallee(pass, call.Fun)
	if fn == nil || fn.Pkg() == nil {
		return false
	}
	switch fn.Pkg().Path() + "." + fn.Name() {
	case "errors.New", "fmt.Errorf":
		return true
	}
	return false
}

type assign struct {
	pos token.Pos
	rhs ast.Expr
}

// localAssigns lists, per local object, every single-value assignment
// in source order.
func localAssigns(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object][]assign {
	out := map[types.Object][]assign{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if len(s.Lhs) != len(s.Rhs) {
				return true
			}
			for i, l := range s.Lhs {
				id, ok := l.(*ast.Ident)
				if !ok {
					continue
				}
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					out[obj] = append(out[obj], assign{s.Pos(), s.Rhs[i]})
				}
			}
		case *ast.ValueSpec:
			if len(s.Names) != len(s.Values) {
				return true
			}
			for i, id := range s.Names {
				if obj := pass.TypesInfo.Defs[id]; obj != nil {
					out[obj] = append(out[obj], assign{s.Pos(), s.Values[i]})
				}
			}
		}
		return true
	})
	return out
}

// lastBefore returns the latest assignment to obj that starts before pos.
func lastBefore(as []assign, pos token.Pos) (assign, bool) {
	var best assign
	found := false
	for _, a := range as {
		if a.pos < pos && (!found || a.pos > best.pos) {
			best, found = a, true
		}
	}
	return best, found
}

// singleDeref reports whether e is exactly one Elem() below
// reflect.ValueOf(p) / reflect.TypeOf(p) for an interface-typed
// parameter p, resolving locals through their latest assignment before
// pos. It returns the position of the Elem step and p's name.
func singleDeref(pass *analysis.Pass, e ast.Expr, pos token.Pos, assigns map[types.Object][]assign, params map[types.Object]bool, depth int) (token.Pos, string, bool) {
	if depth > 8 {
		return 0, "", false
	}
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		a, ok := lastBefore(assigns[pass.TypesInfo.ObjectOf(id)], pos)
		if !ok {
			return 0, "", false
		}
		return singleDeref(pass, a.rhs, a.pos, assigns, params, depth+1)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return 0, "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !isReflect(pass.TypesInfo.TypeOf(sel.X), "Value", "Type") {
		return 0, "", false
	}
	switch sel.Sel.Name {
	case "Type":
		// rv.Type() of a single-deref value names the same level.
		return singleDeref(pass, sel.X, pos, assigns, params, depth+1)
	case "Elem":
		name, ok := reflectOfParam(pass, sel.X, pos, assigns, params, depth+1)
		if !ok {
			return 0, "", false
		}
		return call.Pos(), name, true
	}
	return 0, "", false
}

// reflectOfParam reports whether e is reflect.ValueOf(p) or
// reflect.TypeOf(p) for an interface-typed parameter p.
func reflectOfParam(pass *analysis.Pass, e ast.Expr, pos token.Pos, assigns map[types.Object][]assign, params map[types.Object]bool, depth int) (string, bool) {
	if depth > 8 {
		return "", false
	}
	e = ast.Unparen(e)
	if id, ok := e.(*ast.Ident); ok {
		a, ok := lastBefore(assigns[pass.TypesInfo.ObjectOf(id)], pos)
		if !ok {
			return "", false
		}
		return reflectOfParam(pass, a.rhs, a.pos, assigns, params, depth+1)
	}
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	fn := staticCallee(pass, call.Fun)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "reflect" || (fn.Name() != "ValueOf" && fn.Name() != "TypeOf") {
		return "", false
	}
	id, ok := ast.Unparen(call.Args[0]).(*ast.Ident)
	if !ok || !params[pass.TypesInfo.ObjectOf(id)] {
		return "", false
	}
	return id.Name, true
}

// kindReceiver returns E from E.Kind().
func kindReceiver(pass *analysis.Pass, e ast.Expr) (ast.Expr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Kind" || !isReflect(pass.TypesInfo.TypeOf(sel.X), "Value", "Type") {
		return nil, false
	}
	return sel.X, true
}

type kindTest struct {
	obj types.Object
	pos token.Pos
}

// kindTests lists every `X.Kind() ==/!= reflect.<names>` comparison by
// X's root variable, and every `case reflect.<names>` of a switch on
// X.Kind().
func kindTests(pass *analysis.Pass, body ast.Node, names ...string) []kindTest {
	var out []kindTest
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BinaryExpr:
			if x.Op != token.EQL && x.Op != token.NEQ {
				return true
			}
			var operand ast.Expr
			switch {
			case isReflectKindConst(pass, x.Y, names...):
				operand = x.X
			case isReflectKindConst(pass, x.X, names...):
				operand = x.Y
			default:
				return true
			}
			if e, ok := kindReceiver(pass, operand); ok {
				if root := rootObj(pass, e); root != nil {
					out = append(out, kindTest{root, x.Pos()})
				}
			}
		case *ast.SwitchStmt:
			if x.Tag == nil {
				return true
			}
			e, ok := kindReceiver(pass, x.Tag)
			if !ok {
				return true
			}
			root := rootObj(pass, e)
			if root == nil {
				return true
			}
			for _, s := range x.Body.List {
				cc := s.(*ast.CaseClause)
				for _, v := range cc.List {
					if isReflectKindConst(pass, v, names...) {
						out = append(out, kindTest{root, cc.Pos()})
					}
				}
			}
		}
		return true
	})
	return out
}

// rootObj returns the variable at the base of a selector/call chain:
// rv for rv.Type(), st for st.
func rootObj(pass *analysis.Pass, e ast.Expr) types.Object {
	for {
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			if v, ok := pass.TypesInfo.ObjectOf(x).(*types.Var); ok {
				return v
			}
			return nil
		case *ast.CallExpr:
			e = x.Fun
		case *ast.SelectorExpr:
			e = x.X
		default:
			return nil
		}
	}
}

func isReflectKindConst(pass *analysis.Pass, e ast.Expr, names ...string) bool {
	var id *ast.Ident
	switch x := ast.Unparen(e).(type) {
	case *ast.SelectorExpr:
		id = x.Sel
	case *ast.Ident:
		id = x
	default:
		return false
	}
	c, ok := pass.TypesInfo.Uses[id].(*types.Const)
	if !ok || c.Pkg() == nil || c.Pkg().Path() != "reflect" {
		return false
	}
	for _, n := range names {
		if c.Name() == n {
			return true
		}
	}
	return false
}

// ---- arm 2: a kind setter with no TextUnmarshaler path (4cd4d974) ----

// kindSetter reports whether fd is a text-to-field setter: it takes a
// string parameter and switches on V.Kind() with a case that calls
// V.SetString and another that calls V.SetInt/SetUint/SetFloat/SetBool.
func kindSetter(pass *analysis.Pass, fd *ast.FuncDecl) (token.Pos, bool) {
	hasString := false
	for _, fl := range fd.Type.Params.List {
		if t := pass.TypesInfo.TypeOf(fl.Type); t != nil {
			if b, ok := t.Underlying().(*types.Basic); ok && b.Kind() == types.String {
				hasString = true
			}
		}
	}
	if !hasString {
		return 0, false
	}
	var pos token.Pos
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || sw.Tag == nil || pos.IsValid() {
			return pos == token.NoPos
		}
		if _, ok := kindReceiver(pass, sw.Tag); !ok {
			return true
		}
		str, num := false, false
		ast.Inspect(sw.Body, func(m ast.Node) bool {
			call, ok := m.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !isReflect(pass.TypesInfo.TypeOf(sel.X), "Value") {
				return true
			}
			switch sel.Sel.Name {
			case "SetString":
				str = true
			case "SetInt", "SetUint", "SetFloat", "SetBool":
				num = true
			}
			return true
		})
		if str && num {
			pos = sw.Pos()
		}
		return true
	})
	return pos, pos.IsValid()
}

// mentionsTextUnmarshaler reports whether body names
// encoding.TextUnmarshaler, a package variable initialized from it, or
// an UnmarshalText method.
func mentionsTextUnmarshaler(pass *analysis.Pass, body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		switch x := n.(type) {
		case *ast.Ident:
			obj := pass.TypesInfo.Uses[x]
			if obj == nil {
				return true
			}
			if isTextUnmarshalerName(obj) {
				found = true
			}
			if v, ok := obj.(*types.Var); ok && v.Parent() == pass.Pkg.Scope() && pkgVarNamesTU(pass, v) {
				found = true
			}
			if x.Name == "UnmarshalText" {
				if _, ok := obj.(*types.Func); ok {
					found = true
				}
			}
		}
		return true
	})
	return found
}

func isTextUnmarshalerName(obj types.Object) bool {
	tn, ok := obj.(*types.TypeName)
	return ok && tn.Pkg() != nil && tn.Pkg().Path() == "encoding" && tn.Name() == "TextUnmarshaler"
}

// pkgVarNamesTU reports whether package variable v is initialized from an
// expression naming encoding.TextUnmarshaler (the
// reflect.TypeFor[encoding.TextUnmarshaler]() spelling).
func pkgVarNamesTU(pass *analysis.Pass, v *types.Var) bool {
	for _, init := range pass.TypesInfo.InitOrder {
		for _, lhs := range init.Lhs {
			if lhs != v {
				continue
			}
			found := false
			ast.Inspect(init.Rhs, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && isTextUnmarshalerName(pass.TypesInfo.Uses[id]) {
					found = true
				}
				return !found
			})
			return found
		}
	}
	return false
}

// ---- arm 3: pointer recursion with no visited set (cdf2cb1b) ----

// checkPointerCycle reports a self-call with V.Elem() as an argument
// under a pointer-kind branch on V, in a function that walks struct
// fields, when the function carries no visited set and no depth bound.
func checkPointerCycle(pass *analysis.Pass, fn *types.Func, fd *ast.FuncDecl) {
	if carriesVisited(pass, fd) {
		return
	}
	var visit func(n ast.Node, ptrVars map[types.Object]bool)
	visit = func(n ast.Node, ptrVars map[types.Object]bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			switch x := m.(type) {
			case *ast.FuncLit:
				return false
			case *ast.SwitchStmt:
				if x.Tag == nil {
					return true
				}
				e, ok := kindReceiver(pass, x.Tag)
				if !ok {
					return true
				}
				root := rootObj(pass, e)
				for _, s := range x.Body.List {
					cc := s.(*ast.CaseClause)
					inner := ptrVars
					for _, v := range cc.List {
						if root != nil && isReflectKindConst(pass, v, "Pointer", "Ptr") {
							inner = with(ptrVars, root)
						}
					}
					for _, st := range cc.Body {
						visit(st, inner)
					}
				}
				return false
			case *ast.IfStmt:
				if root := pointerCond(pass, x.Cond); root != nil {
					if x.Init != nil {
						visit(x.Init, ptrVars)
					}
					visit(x.Body, with(ptrVars, root))
					if x.Else != nil {
						visit(x.Else, ptrVars)
					}
					return false
				}
			case *ast.CallExpr:
				if staticCallee(pass, x.Fun) != fn {
					return true
				}
				for _, a := range x.Args {
					if v, ok := elemOf(pass, a); ok && ptrVars[v] {
						pass.Reportf(x.Pos(), msgPtrCycle)
						return true
					}
				}
			}
			return true
		})
	}
	visit(fd.Body, map[types.Object]bool{})
}

func with(m map[types.Object]bool, o types.Object) map[types.Object]bool {
	out := make(map[types.Object]bool, len(m)+1)
	for k := range m {
		out[k] = true
	}
	out[o] = true
	return out
}

// pointerCond returns V's root for `V.Kind() == reflect.Pointer` (alone or
// as a conjunct).
func pointerCond(pass *analysis.Pass, cond ast.Expr) types.Object {
	switch c := ast.Unparen(cond).(type) {
	case *ast.BinaryExpr:
		if c.Op == token.LAND {
			if r := pointerCond(pass, c.X); r != nil {
				return r
			}
			return pointerCond(pass, c.Y)
		}
		if c.Op != token.EQL {
			return nil
		}
		var operand ast.Expr
		switch {
		case isReflectKindConst(pass, c.Y, "Pointer", "Ptr"):
			operand = c.X
		case isReflectKindConst(pass, c.X, "Pointer", "Ptr"):
			operand = c.Y
		default:
			return nil
		}
		if e, ok := kindReceiver(pass, operand); ok {
			return rootObj(pass, e)
		}
	}
	return nil
}

// elemOf returns V's root for an argument spelled V.Elem().
func elemOf(pass *analysis.Pass, e ast.Expr) (types.Object, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Elem" || !isReflect(pass.TypesInfo.TypeOf(sel.X), "Value", "Type") {
		return nil, false
	}
	root := rootObj(pass, sel.X)
	return root, root != nil
}

// carriesVisited reports whether fd threads a visited set or a depth
// bound through its recursion: a parameter or receiver that is a map (or
// a pointer to one, or to a struct holding one), or an integer parameter
// the body compares.
func carriesVisited(pass *analysis.Pass, fd *ast.FuncDecl) bool {
	var fields []*ast.Field
	if fd.Recv != nil {
		fields = append(fields, fd.Recv.List...)
	}
	fields = append(fields, fd.Type.Params.List...)
	for _, fl := range fields {
		t := pass.TypesInfo.TypeOf(fl.Type)
		if t == nil {
			continue
		}
		if holdsMap(t) {
			return true
		}
		if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger != 0 {
			for _, n := range fl.Names {
				if comparedInBody(pass, fd.Body, pass.TypesInfo.Defs[n]) {
					return true
				}
			}
		}
	}
	return false
}

func holdsMap(t types.Type) bool {
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	switch u := t.Underlying().(type) {
	case *types.Map:
		return true
	case *types.Struct:
		for f := range u.Fields() {
			if _, ok := f.Type().Underlying().(*types.Map); ok {
				return true
			}
		}
	}
	return false
}

func comparedInBody(pass *analysis.Pass, body ast.Node, obj types.Object) bool {
	if obj == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		be, ok := n.(*ast.BinaryExpr)
		if !ok || found {
			return !found
		}
		switch be.Op {
		case token.GTR, token.GEQ, token.LSS, token.LEQ:
		default:
			return true
		}
		for _, side := range []ast.Expr{be.X, be.Y} {
			if id, ok := ast.Unparen(side).(*ast.Ident); ok && pass.TypesInfo.Uses[id] == obj {
				found = true
			}
		}
		return true
	})
	return found
}
