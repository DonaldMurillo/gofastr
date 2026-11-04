package rendernilfield

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct {
	unused *childComp
	stored *childComp
	later  func() any
}

type childComp struct{}

func (*rootComp) Render() render.HTML  { return (&childComp{}).Render() }
func (*childComp) Render() render.HTML { return "" }
func script() string                   { return `G.serverAction("save")` }
func (*childComp) Actions() {
	component.On("save", func(*component.ComponentContext) {}, component.WithClientJS(script()))
}

func discardChild(*childComp) *rootComp { return &rootComp{} }

func Surfaces() []fembed.Surface {
	return []fembed.Surface{
		{Name: "nil-field", Screen: app.NewScreen("/nil", &rootComp{})},
		{Name: "nil-cast", Screen: app.NewScreen("/cast", &rootComp{unused: (*childComp)(nil)})},
		{Name: "boot-value", Screen: app.NewScreen("/boot", &rootComp{stored: &childComp{}})},
		{Name: "boot-new", Screen: app.NewScreen("/new", &rootComp{stored: new(childComp)})},
		{Name: "deferred-value", Screen: app.NewScreen("/deferred", &rootComp{later: func() any { return &childComp{} }})},
		{Name: "discarded-value", Screen: app.NewScreen("/discarded", discardChild(&childComp{}))},
	}
}
