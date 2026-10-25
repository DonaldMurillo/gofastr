// Package notembeddable is a check-embed fixture: a component that registers a
// G.serverAction but is mounted on an ordinary app screen, NOT on any
// embeddable surface. The gate must stay silent — a server action is only a
// problem when it is reachable from an embed.Surface.
package notembeddable

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// internalComp registers a server action but is never attached to an embeddable
// surface — it is a normal app screen behind the app's own auth.
type internalComp struct{}

func (internalComp) Render() render.HTML { return render.HTML("<p>internal save</p>") }

func (c *internalComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {},
		component.WithClientJS(`G.serverAction("save")`))
}

// Register mounts internalComp on a normal app screen. No embed.Surface is
// involved, so check-embed must report nothing.
func Register(a *app.App) {
	a.RegisterScreen(app.NewScreen("/internal", &internalComp{}), nil)
}
