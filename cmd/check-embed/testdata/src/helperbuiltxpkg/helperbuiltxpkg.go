// Package helperbuiltxpkg is a check-embed fixture for the helper-indirection
// spelling of the shape neither gate can resolve: Render() delegates to a
// same-package helper whose declared return type is the component.Component
// interface, and the helper BUILDS a child from ANOTHER package.
//
// The child executes into existence only when Render runs (the helper is
// called from Render), so the boot-time value walk in framework/uihost cannot
// see it — the mechanism TestBootWalkCannotSeeRenderBuiltChild pins. The
// analyzer types the `c.build()` call as component.Component, an interface,
// so componentsInRenderBodies' interface carve-out returns without a note,
// and the helper's body, the one place the child exists statically, is
// inspected by nothing. Silence from both gates is indistinguishable from
// "checked, and clean".
package helperbuiltxpkg

import (
	"checkembedtestdata/src/xpkgchild"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct{}

// build is a same-package helper. Its body is the only place the child exists
// statically, and it is not a Render/RenderCtx body, so no walk reads it.
func (c *rootComp) build() component.Component {
	return &xpkgchild.Panel{}
}

func (c *rootComp) Render() render.HTML {
	return c.build().Render()
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "helperbuiltxpkg",
		Screen:  app.NewScreen("/helperbuiltxpkg", &rootComp{}),
		Origins: []string{"https://acme.example"},
	}}
}
