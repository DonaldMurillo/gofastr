// Package renderbuiltxpkg is a check-embed fixture for the shape neither gate can
// resolve: a child BUILT inside Render() (invisible to the boot-time value
// walk) whose type lives in ANOTHER package (so its Actions() body is not in
// this syntax tree).
//
// The analyzer cannot prove whether this child registers a server action. It
// must say so with an unresolved note rather than pass silently — silence here
// is indistinguishable from "checked, and clean".
package renderbuiltxpkg

import (
	"checkembedtestdata/src/xpkgchild"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct{}

func (c *rootComp) Render() render.HTML {
	child := &xpkgchild.Panel{}
	return child.Render()
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "renderbuiltxpkg",
		Screen:  app.NewScreen("/renderbuiltxpkg", &rootComp{}),
		Origins: []string{"https://acme.example"},
	}}
}
