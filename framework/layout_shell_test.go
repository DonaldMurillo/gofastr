package framework

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout shell builders for this package's fixtures: the tree-layout
// spellings of the chrome the removed fixed-template API emitted.

// bareLayout is a layout whose body is the primary slot alone.
func bareLayout(name string) *app.Layout {
	return app.NewLayout(name, app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
}

// sidebarLayout renders a sidebar nav beside the primary slot.
func sidebarLayout(name string, sidebar component.Component) *app.Layout {
	return app.NewLayout(name, app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		inner, _ := component.SafeRenderCtx(ctx, sidebar)
		return html.Div(html.DivConfig{Class: "layout-body"},
			html.Nav(html.NavConfig{Label: "Sidebar"}, inner),
			l.Primary(),
		)
	})
}
