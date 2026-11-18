// Package docpage is the SDK reference's three-column docs page frame.
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
	// Toc is the optional right rail; empty releases the column.
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
