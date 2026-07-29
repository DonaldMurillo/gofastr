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
		// A registration inside a function literal is not a violation — the body
		// does not execute merely by being declared — but the walk cannot tell an
		// unused closure from an immediately-invoked one, so it says so instead of
		// passing in silence.
		component.On("dead", func(_ *component.ComponentContext) {}, // want `inside a function literal`
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
