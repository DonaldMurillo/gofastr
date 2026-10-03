// Package docpage is the app's help/docs page: the article nav on
// the left, the article in the middle, the table of contents on the
// right, and a previous/next pager under the article. It is built the
// way siteheader and sitefooter are: plain html elements composed
// here, the look in docpage.style.css (an owned style), and every
// dimension a theme token. The grid sits on the page measure the
// header and footer use.
//
// The regions are whatever the app passes in, so a layout can hand
// it outlets and a route area: the TOC column collapses when its
// region renders empty (an article with no headings), and below the
// lg breakpoint it goes. Below md the page is one column and the nav
// is the Sidebar's own phone drawer.
//
// This package is the app's own code: edit it freely.
package docpage

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Config holds the page's regions.
type Config struct {
	// Nav is the left rail: the article list.
	Nav render.HTML
	// Crumbs sits above the article body.
	Crumbs render.HTML
	// Body is the article.
	Body render.HTML
	// Pager sits under the article (see Pager).
	Pager render.HTML
	// Toc is the right rail. When it renders an empty element (an
	// outlet with no fill) the column collapses.
	Toc render.HTML
}

// Render builds the page. The article element is what a table of
// contents watches for the active heading (Target: "article").
func Render(cfg Config) render.HTML {
	return Style.Scope(html.Div(html.DivConfig{},
		html.Div(html.DivConfig{Class: Style.Nav()}, cfg.Nav),
		html.Article(html.ArticleConfig{Class: Style.Article()},
			html.Div(html.DivConfig{Class: Style.Crumbs()}, cfg.Crumbs),
			cfg.Body,
			cfg.Pager,
		),
		html.Div(html.DivConfig{Class: Style.Toc()}, cfg.Toc),
	))
}

// PagerConfig names the neighbouring pages. An empty href draws no
// card on that side.
type PagerConfig struct {
	PrevHref, PrevLabel string
	NextHref, NextLabel string
	// PrevDir and NextDir are the small direction labels; they default
	// to "Previous" and "Next".
	PrevDir, NextDir string
}

// Pager builds the previous/next cards under an article.
func Pager(cfg PagerConfig) render.HTML {
	if cfg.PrevHref == "" && cfg.NextHref == "" {
		return render.HTML("")
	}
	if cfg.PrevDir == "" {
		cfg.PrevDir = "Previous"
	}
	if cfg.NextDir == "" {
		cfg.NextDir = "Next"
	}
	card := func(href, dir, label string, next bool) render.HTML {
		return html.LinkHTML(html.LinkHTMLConfig{
			Href:  href,
			Class: Style.PageWith(PageVariants{Next: next}),
			Content: render.Join(
				html.Span(html.TextConfig{Class: Style.Dir()}, render.Text(dir)),
				html.Span(html.TextConfig{Class: Style.PageTitle()}, render.Text(label)),
			),
		})
	}
	var prev, next render.HTML
	if cfg.PrevHref != "" {
		prev = card(cfg.PrevHref, cfg.PrevDir, cfg.PrevLabel, false)
	}
	if cfg.NextHref != "" {
		next = card(cfg.NextHref, cfg.NextDir, cfg.NextLabel, true)
	}
	return html.Nav(html.NavConfig{Class: Style.Pager(), Label: "More articles"}, prev, next)
}
