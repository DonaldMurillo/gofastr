package compositekey

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The print-key arm (2026-11, bug 5ade7a35 in battery/auth). A cache
// keyed by fmt.Sprintf("%T|%+v", h, h) of an interface value names a
// configuration by how fmt prints it, and fmt prints what the dynamic
// value holds: a pointer field prints its address, so two equal configs
// print differently and one hasher holding a pointer or a mutable field
// minted a new key per call — a full KDF run each time and a dummy-hash
// cache that grew without bound.
//
// It fires where a map index or a sync.Map key argument
// (Load/Store/LoadOrStore/LoadAndDelete/Delete/Swap/CompareAndSwap/
// CompareAndDelete) is a fmt.Sprintf/Sprint built from a %v, %+v or %#v
// of:
//   - a value of a non-empty interface type that declares neither
//     String() nor Error() (fmt then prints the dynamic value's fields);
//   - a pointer whose pointee is not a struct of plain values (a pointer
//     to a struct holding a pointer, map, slice, chan, func or interface
//     field prints an address somewhere in the key).
//
// The key is followed through a local bound from the Sprintf and through
// a package helper with a return of that shape (the hasherKey spelling).
//
// Silent:
//   - `any`/interface{} operands: an `any` key is usually a scalar id
//     read from a decoded row, and nothing about its static type says
//     otherwise;
//   - interfaces declaring String() or Error(): fmt calls the method;
//   - the Sprintf inside a type-switch clause on the same value that
//     lists concrete types: the fix spelling keys the built-in hashers,
//     whose fields are plain values, by %+v and everything else by %T;
//   - %T, %s, %d and the other verbs: only the v family prints fields.
//
// Fix spelling: key by reflect.Type (or %T) or by a comparable struct of
// the fields that name the configuration.
const msgPrintKey = "map key built from %%%s of %s (%s): fmt prints a pointer's address and a dynamic value's fields, so equal configurations print differently and the map grows per call; key by reflect.Type or a comparable struct"

var syncMapKeyMethods = map[string]bool{
	"Load": true, "Store": true, "LoadOrStore": true, "LoadAndDelete": true,
	"Delete": true, "Swap": true, "CompareAndSwap": true, "CompareAndDelete": true,
}

type printKey struct {
	verb    string
	operand ast.Expr
	why     string
}

func runPrintKey(pass *analysis.Pass) {
	helpers := printKeyHelpers(pass)
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		bound := boundExprs(pass, f)
		reported := map[token.Pos]bool{}
		check := func(key ast.Expr) {
			pk, at := printKeyOrigin(pass, key, bound, helpers, 0)
			if pk == nil || reported[at] {
				return
			}
			reported[at] = true
			pass.Reportf(key.Pos(), msgPrintKey, pk.verb, types.ExprString(pk.operand), pk.why)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch e := n.(type) {
			case *ast.IndexExpr:
				if t := pass.TypesInfo.TypeOf(e.X); t != nil {
					if _, ok := t.Underlying().(*types.Map); ok {
						check(e.Index)
					}
				}
			case *ast.CallExpr:
				sel, ok := e.Fun.(*ast.SelectorExpr)
				if !ok || !syncMapKeyMethods[sel.Sel.Name] || len(e.Args) == 0 || !isSyncMap(pass.TypesInfo.TypeOf(sel.X)) {
					return true
				}
				check(e.Args[0])
			}
			return true
		})
	}
}

func isSyncMap(t types.Type) bool {
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "sync" && n.Obj().Name() == "Map"
}

// printKeyOrigin follows key to a flagged Sprintf: directly, through a
// local bound from one, or through a helper. The returned position
// dedups reports per origin.
func printKeyOrigin(pass *analysis.Pass, key ast.Expr, bound map[types.Object]ast.Expr, helpers map[*types.Func]*printKey, depth int) (*printKey, token.Pos) {
	if depth > maxDepth {
		return nil, 0
	}
	key = ast.Unparen(key)
	switch x := key.(type) {
	case *ast.Ident:
		obj := pass.TypesInfo.ObjectOf(x)
		if e, ok := bound[obj]; ok {
			pk, _ := printKeyOrigin(pass, e, bound, helpers, depth+1)
			if pk != nil {
				return pk, obj.Pos()
			}
		}
	case *ast.CallExpr:
		if pk := flaggedSprintf(pass, x); pk != nil {
			return pk, x.Pos()
		}
		var id *ast.Ident
		switch f := x.Fun.(type) {
		case *ast.Ident:
			id = f
		case *ast.SelectorExpr:
			id = f.Sel
		}
		if id != nil {
			if fn, ok := pass.TypesInfo.Uses[id].(*types.Func); ok {
				if pk := helpers[fn]; pk != nil {
					return pk, x.Pos()
				}
			}
		}
	}
	return nil, 0
}

// printKeyHelpers collects package functions with one string result and a
// return whose value is a flagged Sprintf outside a type-switch clause
// that pins the operand's type.
func printKeyHelpers(pass *analysis.Pass) map[*types.Func]*printKey {
	out := map[*types.Func]*printKey{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type.Results == nil || fd.Type.Results.NumFields() != 1 {
				continue
			}
			fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				r, ok := n.(*ast.ReturnStmt)
				if !ok || len(r.Results) != 1 || out[fn] != nil {
					return true
				}
				if call, ok := ast.Unparen(r.Results[0]).(*ast.CallExpr); ok {
					if pk := flaggedSprintf(pass, call); pk != nil && !pinnedByTypeSwitch(pass, fd.Body, r, pk.operand) {
						out[fn] = pk
					}
				}
				return true
			})
		}
	}
	return out
}

// pinnedByTypeSwitch reports whether node sits in a case clause, listing
// concrete types, of a type switch on operand's variable.
func pinnedByTypeSwitch(pass *analysis.Pass, body *ast.BlockStmt, node ast.Node, operand ast.Expr) bool {
	id, ok := ast.Unparen(operand).(*ast.Ident)
	if !ok {
		return false
	}
	obj := pass.TypesInfo.Uses[id]
	pinned := false
	ast.Inspect(body, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSwitchStmt)
		if !ok || pinned {
			return !pinned
		}
		var x ast.Expr
		switch a := ts.Assign.(type) {
		case *ast.AssignStmt:
			x = a.Rhs[0]
		case *ast.ExprStmt:
			x = a.X
		}
		ta, ok := x.(*ast.TypeAssertExpr)
		if !ok {
			return true
		}
		sw, ok := ast.Unparen(ta.X).(*ast.Ident)
		if !ok {
			return true
		}
		for _, s := range ts.Body.List {
			cc := s.(*ast.CaseClause)
			if cc.List == nil || node.Pos() < cc.Pos() || node.End() > cc.End() {
				continue
			}
			// The switch subject, or the clause's own binding of it.
			if pass.TypesInfo.Uses[sw] == obj || pass.TypesInfo.Implicits[cc] == obj {
				concrete := true
				for _, t := range cc.List {
					if tt := pass.TypesInfo.TypeOf(t); tt == nil || types.IsInterface(tt) {
						concrete = false
					}
				}
				pinned = concrete
			}
		}
		return true
	})
	return pinned
}

// flaggedSprintf returns the first %v-family verb of a fmt.Sprintf (or
// any argument of fmt.Sprint) whose operand prints unstably.
func flaggedSprintf(pass *analysis.Pass, call *ast.CallExpr) *printKey {
	name := qualifiedCallee(pass, call.Fun)
	switch name {
	case "fmt.Sprint":
		for _, a := range call.Args {
			if why := unstablePrint(pass.TypesInfo.TypeOf(a)); why != "" {
				return &printKey{verb: "v", operand: a, why: why}
			}
		}
	case "fmt.Sprintf":
		if len(call.Args) == 0 {
			return nil
		}
		tv, ok := pass.TypesInfo.Types[call.Args[0]]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return nil
		}
		verbs, ok := formatVerbs(constant.StringVal(tv.Value))
		if !ok {
			return nil
		}
		for i, v := range verbs {
			if i+1 >= len(call.Args) || v.verb != 'v' {
				continue
			}
			a := call.Args[i+1]
			if why := unstablePrint(pass.TypesInfo.TypeOf(a)); why != "" {
				return &printKey{verb: v.flags + "v", operand: a, why: why}
			}
		}
	}
	return nil
}

type fmtVerb struct {
	flags string
	verb  rune
}

// formatVerbs lists the verbs of a format string in argument order. It
// gives up (ok=false) on explicit argument indexes and * widths, which
// break the one-verb-one-argument mapping.
func formatVerbs(format string) ([]fmtVerb, bool) {
	var out []fmtVerb
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i < len(format) && format[i] == '%' {
			continue
		}
		flags := ""
		for i < len(format) && strings.ContainsRune("+-# 0", rune(format[i])) {
			if format[i] == '+' || format[i] == '#' {
				flags += string(format[i])
			}
			i++
		}
		for i < len(format) && (format[i] >= '0' && format[i] <= '9' || format[i] == '.') {
			i++
		}
		if i >= len(format) {
			return nil, false
		}
		if format[i] == '[' || format[i] == '*' {
			return nil, false
		}
		out = append(out, fmtVerb{flags: flags, verb: rune(format[i])})
	}
	return out, true
}

// unstablePrint explains why %v of a value of static type t is not a
// stable name for it, or returns "".
func unstablePrint(t types.Type) string {
	if t == nil || printsByMethod(t) {
		return ""
	}
	switch u := t.Underlying().(type) {
	case *types.Interface:
		if u.NumMethods() == 0 {
			return ""
		}
		return "an interface value prints its dynamic value's fields"
	case *types.Pointer:
		if st, ok := u.Elem().Underlying().(*types.Struct); ok && plainStruct(st, 0) {
			return ""
		}
		return "a pointer prints an address"
	}
	return ""
}

// printsByMethod reports whether fmt would call String() or Error() on a
// value of type t.
func printsByMethod(t types.Type) bool {
	ms := types.NewMethodSet(t)
	for i := range ms.Len() {
		switch ms.At(i).Obj().Name() {
		case "String", "Error":
			return true
		}
	}
	return false
}

// plainStruct reports whether every field of st prints as a value: no
// pointer, map, slice, chan, func or interface anywhere inside.
func plainStruct(st *types.Struct, depth int) bool {
	if depth > 4 {
		return false
	}
	for f := range st.Fields() {
		switch u := f.Type().Underlying().(type) {
		case *types.Basic:
			if u.Kind() == types.UnsafePointer {
				return false
			}
		case *types.Array:
			if _, ok := u.Elem().Underlying().(*types.Basic); !ok {
				return false
			}
		case *types.Struct:
			if !plainStruct(u, depth+1) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
