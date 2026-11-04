package memberspelling

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type panel struct{}

func (*panel) Render() render.HTML { return "" }
func (*panel) Actions() {
	component.On("optional", func(*component.ComponentContext) {}, // want `embed surface "member".*server action "optional"`
		component.WithClientJS(`G?.serverAction("save")`))
	component.On("spaced", func(*component.ComponentContext) {}, // want `embed surface "member".*server action "spaced"`
		component.WithClientJS(`G . serverAction("save")`))
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{Name: "member", Screen: app.NewScreen("/member", &panel{})}}
}
