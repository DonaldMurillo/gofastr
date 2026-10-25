package interfacecomponent

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type interfaceComp struct{}

func (interfaceComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*interfaceComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {}, // want `embed surface "interface".*server action "save"`
		component.WithClientJS(`G.serverAction("save")`))
}

func Surfaces() []fembed.Surface {
	var comp component.Component = &interfaceComp{}
	scr := app.NewScreen("/interface", comp)
	return []fembed.Surface{{Name: "interface", Screen: scr}}
}
