// Package islandxpkg is a check-embed fixture combining both blind
// spots: an island wrapper AND a child type from another package.
package islandxpkg

import (
	"checkembedtestdata/src/xpkgchild"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/island"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type rootComp struct{ isl *island.Island }

func (c *rootComp) Render() render.HTML { return c.isl.Render() }

func Surfaces() []fembed.Surface {
	return []fembed.Surface{{
		Name:    "islandxpkg",
		Screen:  app.NewScreen("/islandxpkg", &rootComp{isl: island.NewIsland("panel", &xpkgchild.Panel{})}),
		Origins: []string{"https://acme.example"},
	}}
}
