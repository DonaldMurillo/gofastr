package main

import (
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

// HeaderComponent is the site nav. Re-rendered on every page so the
// active-link logic in runtime.js can apply aria-current correctly.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	return render.Tag("header", map[string]string{"class": "site-header"},
		elements.Link(elements.LinkConfig{Href: "/", Text: "GoFastr", Class: "brand"}),
		elements.Nav(elements.NavConfig{Label: "Main"},
			elements.Link(elements.LinkConfig{Href: "/", Text: "Home"}),
			elements.Link(elements.LinkConfig{Href: "/docs/", Text: "Docs"}),
			elements.Link(elements.LinkConfig{Href: "/examples/", Text: "Examples"}),
			elements.Link(elements.LinkConfig{Href: "/about", Text: "About"}),
			elements.Link(elements.LinkConfig{
				Href:  "https://github.com/DonaldMurillo/gofastr",
				Text:  "GitHub",
				Attrs: elements.Attrs{"rel": "external"},
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
