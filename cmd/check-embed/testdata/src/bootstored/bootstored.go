package bootstored

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct {
	child *childComp
	box   holder
}

type holder struct{ children []*childComp }
type childComp struct{}

func (*rootComp) Render() render.HTML  { return (&childComp{}).Render() }
func (*childComp) Render() render.HTML { return "" }
func script() string                   { return `G.serverAction("save")` }
func (*childComp) Actions() {
	component.On("save", func(*component.ComponentContext) {}, component.WithClientJS(script()))
}

func Surfaces() []fembed.Surface {
	child := &childComp{}
	box := holder{children: []*childComp{new(childComp)}}
	var absent *childComp
	return []fembed.Surface{
		{Name: "local-child", Screen: app.NewScreen("/local", &rootComp{child: child})},
		{Name: "stored-holder", Screen: app.NewScreen("/holder", &rootComp{box: box})},
		{Name: "absent-local", Screen: app.NewScreen("/absent", &rootComp{child: absent})},
	}
}
