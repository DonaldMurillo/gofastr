// Package docpage is the app's help/docs page. This package is the app's
// own code: edit it freely.
package docpage

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Config holds the page's regions.
type Config struct {
	// Nav is the left rail; unset or holding an empty element, it collapses.
	Nav render.HTML
	// Crumbs sits above the article body.
	Crumbs render.HTML
	// Body is the article.
	Body render.HTML
	// Pager sits under the article.
	Pager render.HTML
	// Toc is the right rail; unset or holding an empty element, it collapses.
	Toc render.HTML
}

// Render builds the page; Body sits in its <article>, which a table of contents watches.
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

// PagerConfig names the neighbouring pages; an empty href draws no card.
type PagerConfig struct {
	// Ctx resolves the pager's own words through i18nui; pass the request's.
	Ctx                 context.Context
	PrevHref, PrevLabel string
	NextHref, NextLabel string
	// PrevDir and NextDir default to "Previous" and "Next" in Ctx's language.
	PrevDir, NextDir string
}

// Pager builds the previous/next cards under an article.
func Pager(cfg PagerConfig) render.HTML {
	if cfg.PrevHref == "" && cfg.NextHref == "" {
		return render.HTML("")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg.PrevDir == "" {
		cfg.PrevDir = i18nui.PaginationPrevious.Text(ctx)
	}
	if cfg.NextDir == "" {
		cfg.NextDir = i18nui.PaginationNext.Text(ctx)
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
	return html.Nav(html.NavConfig{Class: Style.Pager(), Label: i18nui.NavMoreArticles.Text(ctx)}, prev, next)
}
