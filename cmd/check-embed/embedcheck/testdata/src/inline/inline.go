package inline

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type inlineComp struct{}

func (inlineComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*inlineComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {}, // want `embed surface "inline".*server action "save"`
		component.WithClientJS(`G.serverAction("save")`))
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:   "inline",
		Screen: app.NewScreen("/inline", &inlineComp{}),
	}}
}
