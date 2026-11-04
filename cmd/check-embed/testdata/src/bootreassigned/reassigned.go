package bootreassigned

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct{ child *childComp }
type childComp struct{}

func (*rootComp) Render() render.HTML  { return (&childComp{}).Render() }
func (*childComp) Render() render.HTML { return "" }
func script() string                   { return `G.serverAction("save")` }
func (*childComp) Actions() {
	component.On("save", func(*component.ComponentContext) {}, component.WithClientJS(script()))
}

func reassigned() fembed.Surface {
	child := &childComp{}
	child = nil
	return fembed.Surface{Name: "cleared-before", Screen: app.NewScreen("/cleared", &rootComp{child: child})}
}

func assignedAfter() fembed.Surface {
	child := &childComp{}
	surface := fembed.Surface{Name: "cleared-after", Screen: app.NewScreen("/later", &rootComp{child: child})}
	child = nil
	_ = child
	return surface
}

func shadowed() fembed.Surface {
	child := &childComp{}
	{
		child := (*childComp)(nil)
		_ = child
	}
	return fembed.Surface{Name: "shadowed", Screen: app.NewScreen("/shadowed", &rootComp{child: child})}
}

func ranged(candidates []*childComp) fembed.Surface {
	child := &childComp{}
	for _, child = range candidates {
		_ = child
	}
	return fembed.Surface{Name: "ranged", Screen: app.NewScreen("/ranged", &rootComp{child: child})}
}
