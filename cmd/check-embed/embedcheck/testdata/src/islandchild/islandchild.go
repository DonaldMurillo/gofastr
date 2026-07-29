// Package islandchild is a check-embed fixture for a child held inside an
// island.Island wrapper.
//
// island.Island is a one-field wrapper around the component it renders and is
// the framework's main composition primitive, so a child behind one is an
// ordinary rendered child, not a host back-reference.
package islandchild

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type childComp struct{}

func (*childComp) Render() render.HTML { return render.HTML("<button>save</button>") }

func (*childComp) Actions() {
	component.On("islandchild-save", func(_ *component.ComponentContext) {}, // want `embed surface "islandchild".*server action "islandchild-save"`
		component.WithClientJS(`G.serverAction("islandchild-save")`))
}

type rootComp struct{ isl *island.Island }

func (c *rootComp) Render() render.HTML { return c.isl.Render() }

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "islandchild",
		Screen:  app.NewScreen("/islandchild", &rootComp{isl: island.NewIsland("panel", &childComp{})}),
		Origins: []string{"https://acme.example"},
	}}
}
