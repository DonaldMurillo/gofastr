package main

// The site's chrome, wired from the site's own owned-style packages:
// siteheader renders the banner landmark (and owns its sticky bar,
// brand lockup, search pill and phone menu), sitefooter renders the
// colophon. Both are plain packages of this site, not framework
// components — the composition lives here so the layout below reads as
// the page frame it is.

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/site/sitefooter"
	"github.com/DonaldMurillo/gofastr/examples/site/siteheader"
)

// siteHeader renders the top bar around the request's context (i18n for
// the nav labels). The CommandPalette widget is mounted globally in
// main.go; the header's own search controls open it (data-cui-open) and
// bind ⌘K, so the header needs no injected trigger.
func siteHeader(ctx context.Context) render.HTML {
	return siteheader.Render(siteheader.Config{
		Ctx:        ctx,
		Version:    versionLabel(),
		Links:      siteNavLinks(),
		ExtraLinks: siteNavExtraLinks(),
	})
}

// siteFooter renders the colophon.
func siteFooter() render.HTML {
	return sitefooter.Render(sitefooter.Config{
		Version: versionLabel(),
		Tagline: "The full-stack Go framework that doesn't get in the way of you or your agents. Early (v0.x). Built in public.",
		Columns: siteFooterColumns(),
		Bottom: []render.HTML{
			html.Span(html.TextConfig{}, render.Text("© 2026. A research project, not a company.")),
			html.Span(html.TextConfig{}, render.Text("Set in system sans + mono.")),
		},
	})
}
