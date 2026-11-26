package testonly

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// serverActionComp registers a server action: its ClientJS calls G.serverAction,
// the literal the action compiler recognises (and rewrites to G._serverActionFor).
type serverActionComp struct{}

func (serverActionComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (c *serverActionComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {}, component.WithClientJS(`G.serverAction("save")`))
}

// Surfaces declares an embeddable surface rendering serverActionComp. The
// surface→screen→component→action link is statically resolvable, so check-embed
// must report exactly one finding.
func Surfaces() []fembed.Surface {
	scr := app.NewScreen("/testonly", &serverActionComp{})
	return []fembed.Surface{{
		Name:    "testonly",
		Screen:  scr,
		Origins: []string{"https://acme.example"},
	}}
}
