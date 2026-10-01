package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// chromeShell builds a tree layout whose body is the classic chrome the
// removed fixed-template API emitted — a banner header, the sidebar nav
// beside the primary in a .layout-body row, a contentinfo footer — for
// test fixtures that assert on that observable structure. The shell
// components real apps compose (ui.ContentRow, app-owned markup) render the
// same landmarks their own way; this shorthand keeps the fixtures terse.
func chromeShell(name string, header, sidebar, footer component.Component) *Layout {
	return NewLayout(name, LayoutSpec{}, func(ctx context.Context, l *LayoutTree) render.HTML {
		var body []render.HTML
		if sidebar != nil {
			inner, _ := component.SafeRenderCtx(ctx, sidebar)
			body = append(body, html.Nav(html.NavConfig{Label: "Sidebar"}, inner))
		}
		body = append(body, l.Primary())
		var out []render.HTML
		if header != nil {
			inner, _ := component.SafeRenderCtx(ctx, header)
			out = append(out, html.Header(html.HeaderConfig{Banner: true}, inner))
		}
		out = append(out, html.Div(html.DivConfig{Class: "layout-body"}, body...))
		if footer != nil {
			inner, _ := component.SafeRenderCtx(ctx, footer)
			out = append(out, html.Footer(html.FooterConfig{ContentInfo: true}, inner))
		}
		return render.Join(out...)
	})
}

// bareShell is a layout whose body is the primary slot alone.
func bareShell(name string) *Layout {
	return NewLayout(name, LayoutSpec{}, func(ctx context.Context, l *LayoutTree) render.HTML {
		return l.Primary()
	})
}

// headerShell is chromeShell with a header alone.
func headerShell(name string, header component.Component) *Layout {
	return chromeShell(name, header, nil, nil)
}

// sidebarShell is chromeShell with a sidebar alone.
func sidebarShell(name string, sidebar component.Component) *Layout {
	return chromeShell(name, nil, sidebar, nil)
}
