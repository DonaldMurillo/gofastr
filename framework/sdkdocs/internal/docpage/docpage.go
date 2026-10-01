// Package docpage is the SDK reference's docs page: the section menu
// rail, the article column, and an optional contents rail, collapsing
// to one column on phones. The look lives in
// sdkdocs-docpage.style.css, an owned style; `gofastr gen styles`
// writes the class methods in sdkdocs-docpage_style.gen.go.
//
// sdkdocs is framework code and cannot import an example, so it keeps
// its own copy of the docs page an app draws for itself
// (examples/acme-site/helpdocs). The package is internal so the
// generated Style handle is not framework API.
package docpage

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Config holds the page's regions.
type Config struct {
	// Nav is the left rail (interactive.SectionMenu).
	Nav render.HTML
	// Toc is the optional right rail (an in-page table of contents).
	// An empty value releases the column, including after a soft
	// navigation.
	Toc render.HTML
	// Crumbs is the trail above the article body.
	Crumbs []ui.Crumb
}

// Render draws the page around body.
func Render(cfg Config, body ...render.HTML) render.HTML {
	article := make([]render.HTML, 0, len(body)+1)
	if len(cfg.Crumbs) > 0 {
		article = append(article, html.Div(html.DivConfig{Class: Style.Crumbs()},
			ui.Breadcrumbs(ui.BreadcrumbsConfig{Items: cfg.Crumbs})))
	}
	article = append(article, body...)
	return Style.Scope(html.Div(html.DivConfig{},
		html.Div(html.DivConfig{Class: Style.Nav()}, cfg.Nav),
		html.Article(html.ArticleConfig{Class: Style.Article()}, article...),
		html.Div(html.DivConfig{Class: Style.Toc()}, cfg.Toc),
	))
}
