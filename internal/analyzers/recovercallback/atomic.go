package recovercallback

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/internal/pathflow"
	"golang.org/x/tools/go/analysis"
)

func hostAtomicSlots(pass *analysis.Pass) map[types.Object]bool {
	slots := map[types.Object]bool{}
	for _, file := range pass.Files {
		if pathflow.IsTestFile(pass, file) {
			continue
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || !function.Name.IsExported() {
				continue
			}
			registration := false
			for _, prefix := range []string{"Set", "On", "Register", "With", "Use", "Add"} {
				registration = registration || strings.HasPrefix(function.Name.Name, prefix)
			}
			if !registration {
				continue
			}
			parameters := paramObjects(pass.TypesInfo.Defs[function.Name].Type())
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Store" {
					return true
				}
				value := ast.Unparen(call.Args[0])
				if address, ok := value.(*ast.UnaryExpr); ok {
					value = ast.Unparen(address.X)
				}
				identifier, ok := value.(*ast.Ident)
				if !ok || !parameters[pass.TypesInfo.ObjectOf(identifier)] {
					return true
				}
				if slot := atomicCallbackSlot(pass, selector.X); slot != nil {
					slots[slot] = true
				}
				return true
			})
		}
	}
	return slots
}

func (graph *graph) atomicSlotLoad(call *ast.CallExpr) bool {
	if len(call.Args) != 0 {
		return false
	}
	if identifier, ok := ast.Unparen(call.Fun).(*ast.Ident); ok {
		return graph.slotLoaders[graph.pass.TypesInfo.ObjectOf(identifier)]
	}
	selector, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Load" && graph.hostSlots[atomicCallbackSlot(graph.pass, selector.X)]
}

// hostSlotLoaders finds locals bound to a host slot's Load method value,
// `load := registry.observer.Load` or a later `load = ...Load`: calling
// one reads the slot exactly like the direct Load.
func hostSlotLoaders(pass *analysis.Pass, slots map[types.Object]bool) map[types.Object]bool {
	loaders := map[types.Object]bool{}
	if len(slots) == 0 {
		return loaders
	}
	isLoader := func(expression ast.Expr) bool {
		selector, ok := ast.Unparen(expression).(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Load" {
			return false
		}
		selection := pass.TypesInfo.Selections[selector]
		return selection != nil && selection.Kind() == types.MethodVal && slots[atomicCallbackSlot(pass, selector.X)]
	}
	for _, file := range pass.Files {
		if pathflow.IsTestFile(pass, file) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				if len(node.Lhs) != len(node.Rhs) {
					return true
				}
				for index, value := range node.Rhs {
					if identifier, ok := node.Lhs[index].(*ast.Ident); ok && isLoader(value) {
						if object := pass.TypesInfo.ObjectOf(identifier); object != nil {
							loaders[object] = true
						}
					}
				}
			case *ast.ValueSpec:
				for index, value := range node.Values {
					if index < len(node.Names) && isLoader(value) {
						if object := pass.TypesInfo.Defs[node.Names[index]]; object != nil {
							loaders[object] = true
						}
					}
				}
			}
			return true
		})
	}
	return loaders
}

func atomicCallbackSlot(pass *analysis.Pass, receiver ast.Expr) types.Object {
	receiverType := pass.TypesInfo.TypeOf(receiver)
	if receiverType == nil {
		return nil
	}
	receiverType = types.Unalias(receiverType)
	if pointer, ok := receiverType.(*types.Pointer); ok {
		receiverType = types.Unalias(pointer.Elem())
	}
	named, ok := receiverType.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "sync/atomic" || named.Obj().Name() != "Pointer" || named.TypeArgs().Len() != 1 || !isCallbackType(named.TypeArgs().At(0)) {
		return nil
	}
	switch receiver := ast.Unparen(receiver).(type) {
	case *ast.Ident:
		object := pass.TypesInfo.ObjectOf(receiver)
		if object != nil && object.Parent() == pass.Pkg.Scope() && !isCancelNamed(object.Name()) {
			return object
		}
	case *ast.SelectorExpr:
		if selection := pass.TypesInfo.Selections[receiver]; selection != nil && selection.Kind() == types.FieldVal && !isCancelNamed(selection.Obj().Name()) {
			return selection.Obj()
		}
	}
	return nil
}
