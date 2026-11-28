// Package reach has one surface per route the walk takes from a root to a
// child, and each child is reachable by that route alone.
package reach

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type ctorChild struct{}

func (ctorChild) Render() render.HTML { return "" }
func (*ctorChild) Actions() {
	component.On("ctor", nil, component.WithClientJS(`G.serverAction("ctor")`))
}

type ctorRoot struct{ child component.Component }

func (ctorRoot) Render() render.HTML { return "" }

func newCtorRoot(child component.Component) *ctorRoot { return &ctorRoot{child: child} }

type ptrChild struct{}

func (ptrChild) Render() render.HTML { return "" }
func (*ptrChild) Actions()           { component.On("ptr", nil, component.WithClientJS(`G.serverAction("ptr")`)) }

type ptrRoot struct{ c *ptrChild }

func (ptrRoot) Render() render.HTML { return "" }

type sliceChild struct{}

func (sliceChild) Render() render.HTML { return "" }
func (*sliceChild) Actions() {
	component.On("slice", nil, component.WithClientJS(`G.serverAction("slice")`))
}

type sliceRoot struct{ cs []sliceChild }

func (sliceRoot) Render() render.HTML { return "" }

type arrayChild struct{}

func (arrayChild) Render() render.HTML { return "" }
func (*arrayChild) Actions() {
	component.On("array", nil, component.WithClientJS(`G.serverAction("array")`))
}

type arrayRoot struct{ cs [1]arrayChild }

func (arrayRoot) Render() render.HTML { return "" }

type mapChild struct{}

func (mapChild) Render() render.HTML { return "" }
func (*mapChild) Actions()           { component.On("map", nil, component.WithClientJS(`G.serverAction("map")`)) }

type mapRoot struct{ m map[string]mapChild }

func (mapRoot) Render() render.HTML { return "" }

type nestedChild struct{}

func (nestedChild) Render() render.HTML { return "" }
func (*nestedChild) Actions() {
	component.On("nested", nil, component.WithClientJS(`G.serverAction("nested")`))
}

type holder struct{ c nestedChild }

type nestedRoot struct{ h holder }

func (nestedRoot) Render() render.HTML { return "" }

type bodyChild struct{}

func (bodyChild) Render() render.HTML { return "" }
func (*bodyChild) Actions() {
	component.On("body", nil, component.WithClientJS(`G.serverAction("body")`))
}

type bodyRoot struct{}

func (bodyRoot) Render() render.HTML { return bodyChild{}.Render() }

type helperChild struct{}

func (helperChild) Render() render.HTML { return "" }
func (*helperChild) Actions() {
	component.On("helper", nil, component.WithClientJS(`G.serverAction("helper")`))
}

type helperRoot struct{}

func (helperRoot) Render() render.HTML { return build().Render() }

func build() component.Component { return helperChild{} }

type treeChild struct{}

func (treeChild) Render() render.HTML { return "" }
func (*treeChild) Actions() {
	component.On("tree", nil, component.WithClientJS(`G.serverAction("tree")`))
}

type tree struct {
	kids []tree
	leaf *treeChild
}

type treeRoot struct{ t tree }

func (treeRoot) Render() render.HTML { return "" }

func Surfaces() []fembed.Surface {
	return []fembed.Surface{
		{Name: "ctor", Screen: app.NewScreen("/ctor", newCtorRoot(&ctorChild{}))},
		{Name: "ptr", Screen: app.NewScreen("/ptr", &ptrRoot{})},
		{Name: "slice", Screen: app.NewScreen("/slice", &sliceRoot{})},
		{Name: "array", Screen: app.NewScreen("/array", &arrayRoot{})},
		{Name: "map", Screen: app.NewScreen("/map", &mapRoot{})},
		{Name: "nested", Screen: app.NewScreen("/nested", &nestedRoot{})},
		{Name: "body", Screen: app.NewScreen("/body", &bodyRoot{})},
		{Name: "helper", Screen: app.NewScreen("/helper", &helperRoot{})},
		{Name: "tree", Screen: app.NewScreen("/tree", &treeRoot{})},
	}
}
