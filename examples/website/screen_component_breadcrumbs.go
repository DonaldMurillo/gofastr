package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/breadcrumbs"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type BreadcrumbsScreen struct{}

func (s *BreadcrumbsScreen) ScreenTitle() string        { return "Breadcrumbs" }
func (s *BreadcrumbsScreen) ScreenDescription() string  { return "ARIA-correct trail navigation." }
func (s *BreadcrumbsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *BreadcrumbsScreen) Render() render.HTML {
	demo := breadcrumbs.New(breadcrumbs.Config{},
		breadcrumbs.Crumb{Text: "Home", Href: "/"},
		breadcrumbs.Crumb{Text: "Components", Href: "/components/"},
		breadcrumbs.Crumb{Text: "Breadcrumbs"},
	)
	source := `breadcrumbs.New(breadcrumbs.Config{},
    breadcrumbs.Crumb{Text: "Home",       Href: "/"},
    breadcrumbs.Crumb{Text: "Components", Href: "/components/"},
    breadcrumbs.Crumb{Text: "Breadcrumbs"}, // current — no Href
)`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Breadcrumbs")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"A nav element with an ordered list. The last crumb (with no Href) renders as aria-current=\"page\".")),
		demoFrame(demo, source),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Separators")),
		render.Tag("p", nil, render.Text(
			"The slash between crumbs is a CSS pseudo-element (li + li::before). No extra DOM. Restyle the separator by overriding .breadcrumbs > li + li::before in your theme.")),
	)
}
