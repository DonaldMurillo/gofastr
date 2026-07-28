package falsepositives

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type cleanComp struct{}

func (cleanComp) Render() render.HTML { return render.HTML("<p>clean</p>") }

func (*cleanComp) Actions() {
	component.On(`G.serverAction("event-name")`, func(_ *component.ComponentContext) {
		_ = `G.serverAction("server-handler-string")`
	}, component.WithClientJS(`
		// G.serverAction("line-comment")
		/* G.serverAction ("block-comment") */
		G.setState("saved", true)
	`))

	unused := func() {
		component.On("dead", func(_ *component.ComponentContext) {},
			component.WithClientJS(`G.serverAction("dead")`))
	}
	_ = unused
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:   "clean",
		Screen: app.NewScreen("/clean", &cleanComp{}),
	}}
}
