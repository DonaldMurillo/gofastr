// Package docpage is the GoFastr site's article shell: the section nav
// on the left, the article in the middle, with breadcrumbs above the
// body and a previous/next pager under it. It is built the way
// siteheader and sitefooter are: plain html elements composed here
// (ui.Breadcrumbs for the trail), the look in docsite-docpage.style.css
// (an owned style), and every dimension a theme token — the 96px of air
// under an article is this package's token. The grid sits at the same 1360px measure the framework's doc
// layout used, and collapses to one column in lockstep with
// interactive.SectionMenu's 900px swap.
//
// Copy the package and change it.
package docpage

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Config holds the page's regions.
type Config struct {
	// Ctx resolves the breadcrumb strings through i18nui.
	Ctx context.Context
	// Nav is the left rail (interactive.SectionMenu). Empty renders the
	// narrow single-column shape with the article centered.
	Nav render.HTML
	// Crumbs is the trail above the article body (see ui.Crumb).
	Crumbs []ui.Crumb
	// CrumbsLabel overrides the breadcrumb nav's aria-label.
	CrumbsLabel string
	// Pager, when set, renders the previous/next cards under the body.
	Pager *PagerConfig
}

// Render builds the page around the body.
func Render(cfg Config, body ...render.HTML) render.HTML {
	children := make([]render.HTML, 0, 2)
	if cfg.Nav != "" {
		children = append(children, html.Div(html.DivConfig{Class: Style.Nav()}, cfg.Nav))
	}

	article := make([]render.HTML, 0, len(body)+2)
	if len(cfg.Crumbs) > 0 {
		article = append(article, html.Div(html.DivConfig{Class: Style.Crumbs()},
			ui.Breadcrumbs(ui.BreadcrumbsConfig{
				Items: cfg.Crumbs,
				Label: cfg.CrumbsLabel,
				Ctx:   cfg.Ctx,
				// Phones show the last two steps, so the trail fits one
				// line instead of wrapping with a separator leading the
				// second.
				CompactMobile: true,
			})))
	}
	article = append(article, body...)
	if cfg.Pager != nil {
		article = append(article, Pager(*cfg.Pager))
	}
	children = append(children, html.Article(html.ArticleConfig{Class: Style.Content()}, article...))

	return Style.Scope(html.Div(html.DivConfig{}, children...))
}

// PagerConfig names the neighbouring pages. An empty NextHref draws no
// next card; the previous card always renders (callers point it at an
// index fallback).
type PagerConfig struct {
	PrevHref, PrevLabel string
	NextHref, NextLabel string
	// PrevDir and NextDir are the small direction labels; they default
	// to "← Previous" and "Next →".
	PrevDir, NextDir string
}

// Pager builds the previous/next cards under an article.
func Pager(cfg PagerConfig) render.HTML {
	prevDir := cfg.PrevDir
	if prevDir == "" {
		prevDir = "← Previous"
	}
	nextDir := cfg.NextDir
	if nextDir == "" {
		nextDir = "Next →"
	}
	prev := html.LinkHTML(html.LinkHTMLConfig{
		Href:    cfg.PrevHref,
		Class:   Style.Page(),
		Content: pagerCard(prevDir, cfg.PrevLabel),
	})
	var next render.HTML
	if cfg.NextHref != "" {
		next = html.LinkHTML(html.LinkHTMLConfig{
			Href:    cfg.NextHref,
			Class:   Style.PageWith(PageVariants{Next: true}),
			Content: pagerCard(nextDir, cfg.NextLabel),
		})
	}
	return html.Div(html.DivConfig{Class: Style.PagerFoot()},
		html.Nav(html.NavConfig{Class: Style.Pager(), Label: "Docs pages"}, prev, next))
}

func pagerCard(dir, label string) render.HTML {
	return render.Join(
		html.Span(html.TextConfig{Class: Style.Dir()}, render.Text(dir)),
		html.Span(html.TextConfig{Class: Style.PageTitle()}, render.Text(label)),
	)
}
