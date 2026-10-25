// Package childaction is a check-embed fixture for the rendered-child hole.
//
// The embeddable ROOT registers no action at all. Its child does, and that is
// enough: AutoCompileActions compiles the child under its own id and the whole
// compiled bundle ships into the frame, so the button 401s in the customer's
// page. A gate that inspected only the root's own Actions() passed this.
package childaction

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type childComp struct{}

func (*childComp) Render() render.HTML { return render.HTML("<button>save</button>") }

func (*childComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {}, // want `embed surface "childaction".*server action "save"`
		component.WithClientJS(`G.serverAction("save")`))
}

type rootComp struct{ child *childComp }

func (c *rootComp) Render() render.HTML { return c.child.Render() }

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "childaction",
		Screen:  app.NewScreen("/childaction", &rootComp{child: &childComp{}}),
		Origins: []string{"https://acme.example"},
	}}
}
