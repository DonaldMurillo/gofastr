// Package cleanisland is a ROUND-2 REVIEW fixture (delete with
// wrapper_not_noted_test.go).
//
// It is the ordinary, safe, fully-analysable app shape: an embeddable surface
// whose root component wraps a SAME-PACKAGE, action-free child in an
// island.Island inside Render(). This is the shape the blueprint emits for
// every island block.
//
// Everything here is statically resolvable: the child's type is in this
// package, its Actions() body is in this syntax tree, and it registers no
// server action. The gate must stay silent — no finding AND no unresolved
// note — because a note now FAILS `gofastr build`.
package cleanisland

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type childComp struct{}

func (*childComp) Render() render.HTML { return render.HTML("<p>ok</p>") }

func (*childComp) Actions() {
	component.On("zzclean-click", func(_ *component.ComponentContext) {},
		component.WithClientJS("G.setState('count', 1)"))
}

type rootComp struct{}

func (c *rootComp) Render() render.HTML {
	return island.NewIsland("panel", &childComp{}).Render()
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "cleanisland",
		Screen:  app.NewScreen("/cleanisland", &rootComp{}),
		Origins: []string{"https://acme.example"},
	}}
}
