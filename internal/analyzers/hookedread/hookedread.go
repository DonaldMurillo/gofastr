// Package hookedread keeps the entity screens on the hooked read.
//
// The invariant: a BeforeList/BeforeGet hook hides a row and an
// AfterGet/AfterList hook masks a field on every surface that hands a
// caller a row. The crud read API (GetOne, ListAll, CountAll, SumAll,
// GroupCountAll, TypedQuery Find/First/Count) returns STORED values
// unless its context carries crud.WithReadHooks: right for trusted Go
// code that reads to write back, wrong for a screen. The invariant was
// fixed one surface at a time five times across two bug bashes
// (6f14a1ed crud record writes, 692b957e _events, cascades, belongs_to
// and state refusals, a91ca1d63 trash restore/purge, 4a45581eb and
// 4d4fe53c7 the Activity tab and the admin audit pages), each on a
// surface that read rows its own way. The screen packages now read
// through named readers (framework/entityui meta: getRow, listRows,
// countRows, ... hooked; getRowUnhooked, listRowsUnhooked,
// listByIDsUnhooked stored), and this rule holds that seam.
//
// Fires, in framework/entityui and battery/admin only (the packages
// that draw entity rows to an end user), on a call to one of the read
// methods above on a *crud.CrudHandler or *crud.TypedQuery whose
// context argument is not a crud.WithReadHooks(...) call, nor a local
// bound once to one.
//
// Silent postures, deliberately:
//   - inside a function whose name ends in "Unhooked": the named
//     stored-value readers, whose doc says the three jobs they may do
//     (prefill an input that round-trips, diff against the hooked read
//     to find masked fields, ask whether a row the hooked read missed is
//     stored);
//   - a context argument that is crud.WithReadHooks(...), or an
//     identifier assigned exactly once from one;
//   - every other package: crud's own internal reads (write pre-images,
//     preflights, media objects, scope probes) are scoped one by one and
//     guarded by TestReadHookParityCrud, and in-process Go callers
//     (seeds, typed repos, aggregates) want stored values by design;
//   - _test.go files.
//
// The fix: read through the hooked reader (getRow, listRows, ...), or,
// for one of the three stored-value jobs, through an *Unhooked reader.
package hookedread

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

var Analyzer = &analysis.Analyzer{
	Name: "hookedread",
	Doc:  "report an entity screen reading crud rows without crud.WithReadHooks outside a named *Unhooked reader",
	Run:  run,
}

// screenPackages are the import path suffixes the rule holds.
var screenPackages = []string{"/framework/entityui", "/battery/admin"}

const crudPath = "/framework/crud"

// readMethods are the read API methods, by receiver type name.
var readMethods = map[string]map[string]bool{
	"CrudHandler": {"GetOne": true, "ListAll": true, "CountAll": true, "SumAll": true, "GroupCountAll": true},
	"TypedQuery":  {"Find": true, "First": true, "Count": true},
}

func inScope(path string) bool {
	for _, s := range screenPackages {
		if strings.HasSuffix(path, s) {
			return true
		}
	}
	return false
}

func run(pass *analysis.Pass) (any, error) {
	if !inScope(pass.Pkg.Path()) {
		return nil, nil
	}
	for _, file := range pass.Files {
		if strings.HasSuffix(pass.Fset.Position(file.Pos()).Filename, "_test.go") {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || strings.HasSuffix(fn.Name.Name, "Unhooked") {
				continue
			}
			hooked := hookedLocals(pass, fn.Body)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 {
					return true
				}
				name, ok := readMethod(pass, call)
				if !ok {
					return true
				}
				if isWithReadHooks(pass, call.Args[0]) {
					return true
				}
				if id, ok := ast.Unparen(call.Args[0]).(*ast.Ident); ok && hooked[pass.TypesInfo.Uses[id]] {
					return true
				}
				pass.Reportf(call.Pos(), "%s reads stored values: a screen reads through the hooked reader (crud.WithReadHooks), "+
					"or through a function named *Unhooked for a prefill, a mask diff or an existence check", name)
				return true
			})
		}
	}
	return nil, nil
}

// readMethod reports whether call is a read-API method of the crud
// package, and its name.
func readMethod(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok {
		return "", false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return "", false
	}
	t := sig.Recv().Type()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return "", false
	}
	obj := named.Origin().Obj()
	if obj.Pkg() == nil || !strings.HasSuffix(obj.Pkg().Path(), crudPath) {
		return "", false
	}
	if readMethods[obj.Name()][fn.Name()] {
		return obj.Name() + "." + fn.Name(), true
	}
	return "", false
}

// isWithReadHooks reports whether e is a call of crud.WithReadHooks.
func isWithReadHooks(pass *analysis.Pass, e ast.Expr) bool {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	var id *ast.Ident
	switch f := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return false
	}
	fn, ok := pass.TypesInfo.Uses[id].(*types.Func)
	return ok && fn.Name() == "WithReadHooks" && fn.Pkg() != nil && strings.HasSuffix(fn.Pkg().Path(), crudPath)
}

// hookedLocals are the locals of body assigned exactly once, from a
// crud.WithReadHooks call.
func hookedLocals(pass *analysis.Pass, body *ast.BlockStmt) map[types.Object]bool {
	writes := map[types.Object]int{}
	hooked := map[types.Object]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range as.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			obj := pass.TypesInfo.ObjectOf(id)
			if obj == nil {
				continue
			}
			writes[obj]++
			if len(as.Rhs) == len(as.Lhs) && isWithReadHooks(pass, as.Rhs[i]) {
				hooked[obj] = true
			}
		}
		return true
	})
	for obj := range hooked {
		if writes[obj] != 1 {
			delete(hooked, obj)
		}
	}
	return hooked
}
