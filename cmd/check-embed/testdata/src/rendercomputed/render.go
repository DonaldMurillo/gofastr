package rendercomputed

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct{}
type childComp struct{}

func (*rootComp) Render() render.HTML  { return (&childComp{}).Render() }
func (*childComp) Render() render.HTML { return "" }
func script() string                   { return `G.serverAction("save")` }
func (*childComp) Actions() {
	component.On("save", func(*component.ComponentContext) {}, component.WithClientJS(script())) // want `embed surface "render-computed".*not.*knowable`
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{Name: "render-computed", Screen: app.NewScreen("/render", &rootComp{})}}
}
