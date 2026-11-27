// Package lookalike holds same-named stand-ins for embed.Surface,
// app.NewScreen, component.On and component.WithClientJS. None of them is the
// framework's, so check-embed reports no finding here.
package lookalike

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type Surface struct {
	Name   string
	Screen *app.Screen
}

type realActionComp struct{}

func (realActionComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*realActionComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {}, component.WithClientJS(`G.serverAction("save")`))
}

type localOnComp struct{}

func (localOnComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*localOnComp) Actions() {
	On("save", nil, WithClientJS(`G.serverAction("save")`))
}

func On(string, any, ...string) {}

func WithClientJS(js string) string { return js }

type screens struct{}

func (screens) NewScreen(path string, comp any) *app.Screen { return nil }

var fake screens

func Local() []Surface {
	return []Surface{{Name: "local", Screen: app.NewScreen("/local", &realActionComp{})}}
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{
		{Name: "fake-screen", Screen: fake.NewScreen("/fake", &realActionComp{})},
		{Name: "local-on", Screen: app.NewScreen("/local-on", &localOnComp{})},
	}
}
