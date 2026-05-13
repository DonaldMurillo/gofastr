package main

import (
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// HeaderComponent is the site nav. Re-rendered on every page so the
// active-link logic in runtime.js can apply aria-current correctly.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	nav := html.Nav(html.NavConfig{Label: "Main"},
		html.Link(html.LinkConfig{Href: "/", Text: "Home"}),
		html.Link(html.LinkConfig{Href: "/docs/", Text: "Docs"}),
		html.Link(html.LinkConfig{Href: "/components/", Text: "Components"}),
		html.Link(html.LinkConfig{Href: "/framework-ui/", Text: "Framework UI"}),
		html.Link(html.LinkConfig{Href: "/customers", Text: "Customers (CRUD)"}),
		html.Link(html.LinkConfig{Href: "/examples/", Text: "Examples"}),
		html.Link(html.LinkConfig{Href: "/about", Text: "About"}),
		html.Link(html.LinkConfig{
			Href:  "https://github.com/DonaldMurillo/gofastr",
			Text:  "GitHub",
			Attrs: html.Attrs{"rel": "external"},
		}),
	)
	// Inner content only — Layout wraps the result in <header role=banner>.
	// We add the .site-header class to a wrapper div so the existing CSS
	// rules (positioning, padding, etc.) keep working without changes.
	// <details>/<summary>: JS-free disclosure that closes on SPA-nav + Esc.
	return render.Tag("div", map[string]string{"class": "site-header"},
		html.Link(html.LinkConfig{Href: "/", Text: "GoFastr", Class: "brand"}),
		render.Tag("details", map[string]string{
			"class":               "site-nav",
			"data-fui-disclosure": "",
		},
			render.Tag("summary", map[string]string{
				"class":      "site-nav__toggle",
				"aria-label": "Toggle navigation menu",
			}, render.Text("☰ Menu")),
			nav,
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
