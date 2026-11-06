package optionindirection

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type optionComp struct{}

func (*optionComp) Render() render.HTML { return "" }

func (*optionComp) Actions() {
	option := component.WithClientJS(`G.serverAction("save")`)
	component.On("identifier", func(*component.ComponentContext) {}, option) // want `embed surface "options".*not.*knowable`
	options := []component.ActionOption{option}
	component.On("spread", func(*component.ComponentContext) {}, options...) // want `embed surface "options".*not.*knowable`
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{Name: "options", Screen: app.NewScreen("/options", &optionComp{})}}
}
