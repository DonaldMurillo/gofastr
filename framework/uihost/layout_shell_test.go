package uihost

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout shell builders for this package's fixtures: the tree-layout
// spellings of the header/sidebar/footer chrome the removed
// fixed-template API emitted, so tests read the way they always did.

// bareLayout is a layout whose body is the primary slot alone.
func bareLayout(name string) *app.Layout {
	return app.NewLayout(name, app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
}

// headerLayout renders a banner header above the primary slot.
func headerLayout(name string, header component.Component) *app.Layout {
	return app.NewLayout(name, app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		inner, _ := component.SafeRenderCtx(ctx, header)
		return render.Join(
			html.Header(html.HeaderConfig{Banner: true}, inner),
			l.Primary(),
		)
	})
}

// chromeTestLayout renders banner header, optional sidebar nav beside
// the primary, and contentinfo footer — the full fixed-template shape.
func chromeTestLayout(name string, header, sidebar, footer component.Component) *app.Layout {
	return app.NewLayout(name, app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		var out []render.HTML
		if header != nil {
			inner, _ := component.SafeRenderCtx(ctx, header)
			out = append(out, html.Header(html.HeaderConfig{Banner: true}, inner))
		}
		var body []render.HTML
		if sidebar != nil {
			inner, _ := component.SafeRenderCtx(ctx, sidebar)
			body = append(body, html.Nav(html.NavConfig{Label: "Sidebar"}, inner))
		}
		body = append(body, l.Primary())
		out = append(out, html.Div(html.DivConfig{Class: "layout-body"}, body...))
		if footer != nil {
			inner, _ := component.SafeRenderCtx(ctx, footer)
			out = append(out, html.Footer(html.FooterConfig{ContentInfo: true}, inner))
		}
		return render.Join(out...)
	})
}

// footerLayout renders a contentinfo footer below the primary slot,
// stacking every footer component given.
func footerLayout(name string, footers ...component.Component) *app.Layout {
	return app.NewLayout(name, app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		var parts []render.HTML
		parts = append(parts, l.Primary())
		for _, f := range footers {
			inner, _ := component.SafeRenderCtx(ctx, f)
			parts = append(parts, html.Footer(html.FooterConfig{ContentInfo: true}, inner))
		}
		return render.Join(parts...)
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
