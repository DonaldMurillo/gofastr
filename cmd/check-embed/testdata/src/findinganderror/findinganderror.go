package findinganderror

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type badComp struct{}

func (badComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*badComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {},
		component.WithClientJS(`G.serverAction("save")`))
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:   "finding-and-error",
		Screen: app.NewScreen("/bad", &badComp{}),
	}}
}

var _ = doesNotExist
