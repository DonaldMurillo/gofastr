package whitespace

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type whitespaceComp struct{}

func (whitespaceComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*whitespaceComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {}, // want `embed surface "whitespace".*G.serverAction\(`
		component.WithClientJS(`G.serverAction ("save")`))
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:   "whitespace",
		Screen: app.NewScreen("/whitespace", &whitespaceComp{}),
	}}
}
