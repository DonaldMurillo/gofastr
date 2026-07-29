// Package renderbuilt is a check-embed fixture for a child the root
// BUILDS inside Render() rather than holding as a field.
//
// The boot-time walk in framework/uihost reads live component VALUES, and this
// child does not exist until Render runs — so the analyzer is the only gate
// that can see it. Same package, so it can be resolved exactly.
package renderbuilt

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type childComp struct{}

func (*childComp) Render() render.HTML { return render.HTML("<button>save</button>") }

func (*childComp) Actions() {
	component.On("renderbuilt-save", func(_ *component.ComponentContext) {}, // want `embed surface "renderbuilt".*server action "renderbuilt-save"`
		component.WithClientJS(`G.serverAction("renderbuilt-save")`))
}

type rootComp struct{}

func (c *rootComp) Render() render.HTML {
	child := &childComp{}
	return child.Render()
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "renderbuilt",
		Screen:  app.NewScreen("/renderbuilt", &rootComp{}),
		Origins: []string{"https://acme.example"},
	}}
}
