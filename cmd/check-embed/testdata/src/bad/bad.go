// Package bad is a check-embed fixture: an embeddable surface whose screen's
// component registers a G.serverAction — the one thing that does not work
// inside a frame. The gate MUST flag it, naming the surface, component and
// action.
package bad

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
	component.On("save", func(_ *component.ComponentContext) {}, component.WithClientJS(`G.serverAction("save")`)) // want `embed surface "bad".*server action "save".*island RPC, a form POST, or polling`
}

// Surfaces declares an embeddable surface rendering serverActionComp. The
// surface→screen→component→action link is statically resolvable, so check-embed
// must report exactly one finding.
func Surfaces() []fembed.Surface {
	scr := app.NewScreen("/bad", &serverActionComp{})
	return []fembed.Surface{{
		Name:    "bad",
		Screen:  scr,
		Origins: []string{"https://acme.example"},
	}}
}
