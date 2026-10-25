// Package helperbuiltslice is the second spelling of the helper-indirection
// shape: Render() ranges over the []component.Component returned by a
// same-package helper that BUILDS a foreign-type child.
//
// This spelling is silent one filter earlier than helperbuiltxpkg: the range
// and call expressions type as an unnamed slice and its interface element,
// and namedOf returns nil for the slice, so componentsInRenderBodies filters
// them out before the interface carve-out is even reached. Same root cause —
// the helper's body is never inspected — same silent outcome for the class
// the package docs define as Blocking.
package helperbuiltslice

import (
	"checkembedtestdata/src/xpkgchild"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct{}

func (c *rootComp) kids() []component.Component {
	return []component.Component{&xpkgchild.Panel{}}
}

func (c *rootComp) Render() render.HTML {
	var out render.HTML
	for _, k := range c.kids() {
		out += k.Render()
	}
	return out
}

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "helperbuiltslice",
		Screen:  app.NewScreen("/helperbuiltslice", &rootComp{}),
		Origins: []string{"https://acme.example"},
	}}
}
