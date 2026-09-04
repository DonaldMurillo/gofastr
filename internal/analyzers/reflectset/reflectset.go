// Package reflectset catches reflect.Value mutation of a struct field
// that never passed through CanSet: Set, SetString, SetInt, and the
// rest of the Set* family panic on values obtained from an unexported
// field, and the panic fires at injection time, not at declaration
// time, so a single lowercased tagged field takes down every request.
//
// The bug class: probe TestInjectUnexportedFieldErrors found it in
// core-ui/di di.go Inject (fixed in e936f791): Inject wrote
// ev.Field(i).Set(...) for every inject-tagged field, and Inject runs
// before the render pipeline's recover (app.RenderPageResult), so one
// unexported field panicked the page per request. The fix checks
// Field(i).CanSet() first and reports the wiring error.
//
// Silent postures, deliberately:
//   - any CanSet() call in the function on the same value (directly or
//     through the same local) — the fix posture;
//   - named-field access with the name WRITTEN HERE: FieldByName with
//     a string-literal name, or FieldByNameFunc with an inline
//     predicate func. Narrowed 2026-09-02, when the whole-repo run
//     measured all eight such sites (core-ui/style tokenmap.go)
//     walking the package's OWN token structs — a deliberate field
//     pick. A caller-supplied name (or predicate) is not written here:
//     it is the same one-unexported-field panic the index spelling
//     carries, and fires;
//   - receivers that do not come from Field()/FieldByIndex()/
//     FieldByName()/FieldByNameFunc(): a Set on an addressable Value
//     obtained another way (Elem, Index, a function result) has its
//     own rules and its own failures;
//   - non-reflect.Value receivers (a type with its own Set method);
//   - _test.go files.
package reflectset

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "reflectset",
	Doc:  "forbids reflect Set/SetString/… on an indexed Field value without a CanSet check; an unexported field panics at Set",
	Run:  run,
}

// setMethods is the Set* family that panics on a value obtained from an
// unexported field. SetLen/SetCap/SetMapIndex operate on slices and
// maps, whose settability does not hinge on CanSet of the field value
// itself, and are left out.
var setMethods = map[string]bool{
	"Set":        true,
	"SetString":  true,
	"SetInt":     true,
	"SetUint":    true,
	"SetBool":    true,
	"SetFloat":   true,
	"SetComplex": true,
	"SetPointer": true,
}

func run(pass *analysis.Pass) (any, error) {
	for _, f := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkFunc(pass, fn.Body)
		}
	}
	return nil, nil
}

func checkFunc(pass *analysis.Pass, body *ast.BlockStmt) {
	bound := bindings(pass, body)

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !setMethods[sel.Sel.Name] {
			return true
		}
		if !isReflectValue(pass, sel.X) {
			return true
		}
		origin := fieldOrigin(pass, sel.X, bound)
		if origin == "" {
			return true
		}
		if canSetChecked(pass, body, bound, origin) {
			return true
		}
		pass.Reportf(call.Pos(),
			"reflect %s on a field value without a CanSet check: an unexported field panics at Set; check CanSet and report the wiring error",
			sel.Sel.Name)
		return true
	})
}

// fieldOrigin returns the printed form of the receiver's field origin —
// Field(i) / FieldByIndex, or FieldByName / FieldByNameFunc with a
// non-literal name argument — or "" otherwise. One level of local
// binding is resolved.
func fieldOrigin(pass *analysis.Pass, recv ast.Expr, bound map[types.Object]ast.Expr) string {
	e := recv
	for range 8 {
		switch x := e.(type) {
		case *ast.Ident:
			b, ok := bound[pass.TypesInfo.ObjectOf(x)]
			if !ok {
				return ""
			}
			e = b
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if !ok {
				return ""
			}
			switch sel.Sel.Name {
			case "Field", "FieldByIndex":
				return types.ExprString(e)
			case "FieldByName":
				if len(x.Args) == 1 && isStringLiteral(x.Args[0]) {
					return "" // a deliberate named pick, written here
				}
				return types.ExprString(e)
			case "FieldByNameFunc":
				if len(x.Args) == 1 {
					if _, ok := x.Args[0].(*ast.FuncLit); ok {
						return "" // an inline predicate: the pick is written here
					}
				}
				return types.ExprString(e)
			}
			return ""
		default:
			return ""
		}
	}
	return ""
}

func isStringLiteral(e ast.Expr) bool {
	lit, ok := e.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// canSetChecked reports whether the function calls CanSet on the same
// field origin (directly or through its own local).
func canSetChecked(pass *analysis.Pass, body *ast.BlockStmt, bound map[types.Object]ast.Expr, origin string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "CanSet" || !isReflectValue(pass, sel.X) {
			return true
		}
		if types.ExprString(sel.X) == origin {
			found = true
			return false
		}
		// The CanSet receiver may be the local the field value was
		// bound to (`fv := ev.Field(i); fv.CanSet()`): resolve it the
		// same way and compare.
		if id, ok := sel.X.(*ast.Ident); ok {
			if b, ok := bound[pass.TypesInfo.ObjectOf(id)]; ok {
				if types.ExprString(b) == origin {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

// isReflectValue reports whether e's type is reflect.Value.
func isReflectValue(pass *analysis.Pass, e ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(e)
	if t == nil {
		return false
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "reflect" && obj.Name() == "Value"
}

// bindings maps each local to the expression it was last bound to.
func bindings(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]ast.Expr {
	bound := map[types.Object]ast.Expr{}
	ast.Inspect(body, func(n ast.Node) bool {
		st, ok := n.(*ast.AssignStmt)
		if !ok || len(st.Lhs) != 1 || len(st.Rhs) != 1 {
			return true
		}
		if id, ok := st.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
				bound[obj] = st.Rhs[0]
			}
		}
		return true
	})
	return bound
}
