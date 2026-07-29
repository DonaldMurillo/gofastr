// Package unresolved is a check-embed fixture for the give-up path.
//
// The embeddable root reaches its child through an interface-typed field, so
// nothing static can prove which component renders there. The gate must SAY so.
// Silence would be indistinguishable from "checked, and clean" — which is how a
// rendered child's server action shipped to a customer's page unnoticed.
package unresolved

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type panelComp struct{}

func (*panelComp) Render() render.HTML { return render.HTML("<p>panel</p>") }

type rootComp struct {
	child component.Component // want `is interface-typed, so the component it holds is chosen at runtime`
}

func (c *rootComp) Render() render.HTML { return c.child.Render() }

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "unresolved",
		Screen:  app.NewScreen("/unresolved", &rootComp{child: &panelComp{}}),
		Origins: []string{"https://acme.example"},
	}}
}
