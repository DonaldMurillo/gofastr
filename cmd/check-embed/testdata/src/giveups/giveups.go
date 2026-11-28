// Package giveups has one surface per field shape the walk cannot follow;
// each must yield exactly one note.
package giveups

import (
	"checkembedtestdata/src/xpkgchild"
	"checkembedtestdata/src/xpkgiface"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type funcRoot struct{ make func() component.Component }

func (funcRoot) Render() render.HTML { return "" }

type ifaceRoot struct {
	c interface{ Render() render.HTML }
}

func (ifaceRoot) Render() render.HTML { return "" }

type namedIfaceRoot struct{ c component.Component }

func (r namedIfaceRoot) Render() render.HTML { return r.c.Render() }

type twiceRoot struct{}

func (twiceRoot) Render() render.HTML {
	a := &xpkgchild.Panel{}
	b := &xpkgchild.Panel{}
	return a.Render() + b.Render()
}

type xpkgIfaceRoot struct{ c xpkgiface.Renderer }

func (r xpkgIfaceRoot) Render() render.HTML { return r.c.Render() }

type mixedOptions struct{}

func (mixedOptions) Render() render.HTML { return "" }

func (*mixedOptions) Actions() {
	component.On("mixed", nil, extraOption(), component.WithClientJS(`G.setState("a", 1)`))
}

func extraOption() component.ActionOption { return component.WithClientJS(`G.serverAction("hidden")`) }

func pick() component.Component { return &funcRoot{} }

type node struct {
	a, b *node
	c    component.Component
}

type selfRefRoot struct{ n node }

func (selfRefRoot) Render() render.HTML { return "" }

func Surfaces() []fembed.Surface {
	return []fembed.Surface{
		{Name: "func-field", Screen: app.NewScreen("/func", &funcRoot{})},
		{Name: "iface-field", Screen: app.NewScreen("/iface", &ifaceRoot{})},
		{Name: "named-iface-render", Screen: app.NewScreen("/named", &namedIfaceRoot{})},
		{Name: "xpkg-iface-render", Screen: app.NewScreen("/xiface", &xpkgIfaceRoot{})},
		{Name: "mixed-options", Screen: app.NewScreen("/mixed", &mixedOptions{})},
		{Name: "xpkg-twice", Screen: app.NewScreen("/twice", &twiceRoot{})},
		{Name: "no-concrete", Screen: app.NewScreen("/n", pick())},
		{Name: "self-ref", Screen: app.NewScreen("/self", &selfRefRoot{})},
	}
}
