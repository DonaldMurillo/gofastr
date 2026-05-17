package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// HeaderComponent is the site nav. Re-rendered on every page so the
// active-link logic in runtime.js can apply aria-current correctly.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	// Two nav copies — only one is visible per viewport. Avoids the
	// Chrome quirk where a <details>-wrapped nav with display:contents
	// collapses to zero inline-size at desktop sizes. Both copies emit
	// aria-label="Main"; CSS hides the unused one (no duplicate
	// landmarks in the a11y tree of any one viewport).
	// data-fui-match-prefix opts links into the runtime's prefix
	// active-route rule: "/docs/" lights up on /docs/foo,
	// "/components/" on /components/accordion, etc. Without this
	// attribute the runtime only does exact-href matching — which is
	// what we want for breadcrumbs and sidebars (where many links share
	// prefixes and the server picks the canonical "active" one).
	prefix := html.Attrs{"data-fui-match-prefix": ""}
	links := []render.HTML{
		html.Link(html.LinkConfig{Href: "/", Text: "Home"}),
		html.Link(html.LinkConfig{Href: "/docs/", Text: "Docs", Attrs: prefix}),
		html.Link(html.LinkConfig{Href: "/components/", Text: "Components", Attrs: prefix}),
		html.Link(html.LinkConfig{Href: "/framework-ui/", Text: "Framework UI", Attrs: prefix}),
		html.Link(html.LinkConfig{Href: "/customers", Text: "Customers (CRUD)", Attrs: prefix}),
		html.Link(html.LinkConfig{Href: "/examples/", Text: "Examples", Attrs: prefix}),
		html.Link(html.LinkConfig{Href: "/about", Text: "About"}),
		html.Link(html.LinkConfig{
			Href:  "https://github.com/DonaldMurillo/gofastr",
			Text:  "GitHub",
			Attrs: html.Attrs{"rel": "external"},
		}),
	}
	desktopNav := render.Tag("nav", map[string]string{
		"class":      "site-nav-desktop",
		"aria-label": "Main",
		"role":       "navigation",
	}, links...)
	mobileNav := render.Tag("nav", map[string]string{
		"aria-label": "Main",
		"role":       "navigation",
	}, links...)
	return render.Tag("div", map[string]string{"class": "site-header"},
		html.Link(html.LinkConfig{Href: "/", Text: "GoFastr", Class: "brand"}),
		desktopNav,
		render.Tag("details", map[string]string{
			"class":               "site-nav",
			"data-fui-disclosure": "",
		},
			render.Tag("summary", map[string]string{
				"class":      "site-nav__toggle",
				"aria-label": "Toggle navigation menu",
			}, render.Text("☰ Menu")),
			mobileNav,
		),
	)
}

// FooterComponent — single-line attribution.
//
// Returns inner content only; Layout wraps in <footer role=contentinfo>.
type FooterComponent struct{}

func (f *FooterComponent) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "site-footer"},
		render.Text("Built with GoFastr — pre-alpha research, no license yet."),
	)
}
