// Package xpkgchild holds an action-bearing component used by the
// cross-package check-embed fixtures. It lives in its own package precisely so
// the analyzer cannot see its Actions() body from the surface's package.
package xpkgchild

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Panel registers a server action. Rendered inside an embed frame its button
// 401s, because the frame's grant is not the app's session.
type Panel struct{}

func (*Panel) Render() render.HTML { return render.HTML("<button>save</button>") }

func (*Panel) Actions() {
	component.On("xpkgchild-save", func(_ *component.ComponentContext) {},
		component.WithClientJS(`G.serverAction("xpkgchild-save")`))
}
