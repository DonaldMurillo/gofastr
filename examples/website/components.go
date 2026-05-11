package main

import (
	"github.com/gofastr/gofastr/core-ui/html"
	"github.com/gofastr/gofastr/core/render"
)

// HeaderComponent is the site nav. Re-rendered on every page so the
// active-link logic in runtime.js can apply aria-current correctly.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	return render.Tag("header", map[string]string{"class": "site-header"},
		html.Link(html.LinkConfig{Href: "/", Text: "GoFastr", Class: "brand"}),
		html.Nav(html.NavConfig{Label: "Main"},
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
		),
	)
}

// FooterComponent — single-line attribution.
type FooterComponent struct{}

func (f *FooterComponent) Render() render.HTML {
	return render.Tag("footer", map[string]string{"class": "site-footer"},
		render.Text("Built with GoFastr — pre-alpha research, no license yet."),
	)
}
