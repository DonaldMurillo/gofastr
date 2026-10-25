// Package clean is a check-embed fixture: an embeddable surface whose screen's
// component registers an ordinary client-only action (G.setState). The gate
// MUST stay silent here — it targets the G.serverAction property, not action
// registration in general. A gate that flags everything is worse than no gate.
package clean

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// plainComp renders a panel and registers a setState action — the client-only
// handler an embed is meant to use.
type plainComp struct{}

func (plainComp) Render() render.HTML { return render.HTML("<p>ok</p>") }

func (c *plainComp) Actions() {
	component.On("click", func(_ *component.ComponentContext) {},
		component.WithClientJS("G.setState('count', 1)"))
}

// Surfaces declares an embeddable surface rendering plainComp. No server action
// is reachable, so check-embed must report nothing.
func Surfaces() []fembed.Surface {
	scr := app.NewScreen("/clean", &plainComp{})
	return []fembed.Surface{{
		Name:    "clean",
		Screen:  scr,
		Origins: []string{"https://acme.example"},
	}}
}
