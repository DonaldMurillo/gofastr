package chained

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type chainedComp struct{}

func (chainedComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*chainedComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {}, // want `embed surface "chained".*server action "save"`
		component.WithClientJS(`G.serverAction("save")`))
}

func Surfaces() []fembed.Surface {
	scr := app.NewScreen("/chained", &chainedComp{}).WithTitle("Chained")
	return []fembed.Surface{{Name: "chained", Screen: scr}}
}
