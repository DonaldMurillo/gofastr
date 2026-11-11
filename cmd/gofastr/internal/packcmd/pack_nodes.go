package packcmd

import (
	"go/ast"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// reverseNodeBlock reads the three spellings appgen's
// renderAppSpecBlockForScreen gives a node block:
//
//	noderender.RenderTrustedNode(uinode.Node{…})
//	island.NewIsland("name", nodeComponent{node: uinode.Node{…}}).Render()
//	component.NewWidget("name", nodeComponent{node: uinode.Node{…}}).Render()
//
// The node literal carries the block's kind, props and children as the
// generator wrote them, so the block reads straight back. Without this
// pack dropped every plain section, heading, div and island or widget
// wrapper, and the recovered spec regenerated the screen without them.
func reverseNodeBlock(call *ast.CallExpr) (appspec.AppSpecBlock, bool) {
	if callSel(call) == "noderender.RenderTrustedNode" && len(call.Args) == 1 {
		return reverseNodeLit(call.Args[0])
	}
	render, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || render.Sel.Name != "Render" || len(call.Args) != 0 {
		return appspec.AppSpecBlock{}, false
	}
	wrap, ok := render.X.(*ast.CallExpr)
	if !ok || len(wrap.Args) != 2 {
		return appspec.AppSpecBlock{}, false
	}
	name := appspec.AstString(wrap.Args[0])
	comp, ok := wrap.Args[1].(*ast.CompositeLit)
	if !ok || name == "" || appgen.IdentName(comp.Type) != "nodeComponent" {
		return appspec.AppSpecBlock{}, false
	}
	b, ok := reverseNodeLit(appspec.FieldVals(comp)["node"])
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	switch callSel(wrap) {
	case "island.NewIsland":
		b.Island = name
	case "component.NewWidget":
		b.Widget = name
	default:
		return appspec.AppSpecBlock{}, false
	}
	return b, true
}

// reverseNodeLit reads one uinode.Node{Kind, Props, Children} literal.
// A child written with its type elided (gofmt -s) reads the same.
func reverseNodeLit(e ast.Expr) (appspec.AppSpecBlock, bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return appspec.AppSpecBlock{}, false
	}
	if lit.Type != nil {
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Node" {
			return appspec.AppSpecBlock{}, false
		}
	}
	fields := appspec.FieldVals(lit)
	kind := appspec.AstString(fields["Kind"])
	if kind == "" {
		return appspec.AppSpecBlock{}, false
	}
	var b appspec.AppSpecBlock
	// `kind: section` is the catalog's ui.Section; the node renderer's
	// plain <section> is the `type: section` spelling (typeNamesCatalogKind).
	if strings.EqualFold(kind, "section") {
		b.Type = kind
	} else {
		b.Kind = kind
	}
	if props, ok := appspec.AstAny(fields["Props"]).(map[string]any); ok && len(props) > 0 {
		b.Props = props
	}
	if children, ok := fields["Children"].(*ast.CompositeLit); ok {
		for _, el := range children.Elts {
			child, ok := reverseNodeLit(el)
			if !ok {
				return appspec.AppSpecBlock{}, false
			}
			b.Children = append(b.Children, child)
		}
	}
	return b, true
}

// packScreenAction is one component.On registration in a screen's
// Actions method, in the order the generator wrote it.
type packScreenAction struct {
	name, clientJS string
}

// packScreenActions reads each screen type's Actions method:
// component.On(name, handler, component.WithClientJS(js)) per action,
// keyed by receiver type name.
func packScreenActions(file *ast.File) map[string][]packScreenAction {
	out := map[string][]packScreenAction{}
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != "Actions" || fn.Body == nil {
			continue
		}
		recv := appgen.RecvTypeName(fn.Recv.List[0].Type)
		for _, stmt := range fn.Body.List {
			es, ok := stmt.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok || callSel(call) != "component.On" || len(call.Args) < 2 {
				continue
			}
			a := packScreenAction{name: appspec.AstString(call.Args[0])}
			for _, opt := range call.Args[2:] {
				if oc, ok := opt.(*ast.CallExpr); ok && callSel(oc) == "component.WithClientJS" && len(oc.Args) == 1 {
					a.clientJS = appspec.AstString(oc.Args[0])
				}
			}
			if a.name != "" {
				out[recv] = append(out[recv], a)
			}
		}
	}
	return out
}

// packAttachActions turns the action markers the node renderer writes
// into a block's props back into its actions: data-action names the
// first action (data-action-type its input/change/submit event), and
// data-action-<event> names each later one. A marker counts only when
// the screen registers an action by that name, so an author's own
// data-action-* prop stays a prop.
func packAttachActions(blocks []appspec.AppSpecBlock, actions []packScreenAction) {
	if len(actions) == 0 {
		return
	}
	index := func(name string) int {
		return slices.IndexFunc(actions, func(a packScreenAction) bool { return a.name == name })
	}
	for i := range blocks {
		b := &blocks[i]
		packAttachActions(b.Children, actions)
		first, _ := b.Props["data-action"].(string)
		if first == "" || index(first) < 0 {
			continue
		}
		act := appspec.AppSpecAction{Name: first, ClientJS: actions[index(first)].clientJS}
		delete(b.Props, "data-action")
		if ev, _ := b.Props["data-action-type"].(string); ev == "input" || ev == "change" || ev == "submit" {
			act.Event = ev
			delete(b.Props, "data-action-type")
		}
		b.Actions = append(b.Actions, act)
		var later []appspec.AppSpecAction
		for key, v := range b.Props {
			name, _ := v.(string)
			ev, ok := strings.CutPrefix(key, "data-action-")
			if !ok || ev == "type" || name == "" || index(name) < 0 {
				continue
			}
			later = append(later, appspec.AppSpecAction{Name: name, Event: ev, ClientJS: actions[index(name)].clientJS})
			delete(b.Props, key)
		}
		slices.SortStableFunc(later, func(x, y appspec.AppSpecAction) int {
			if d := index(x.Name) - index(y.Name); d != 0 {
				return d
			}
			return strings.Compare(x.Event, y.Event)
		})
		b.Actions = append(b.Actions, later...)
		if len(b.Props) == 0 {
			b.Props = nil
		}
	}
}
