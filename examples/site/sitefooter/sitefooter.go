// Package sitefooter is the GoFastr site's colophon, built the way
// siteheader builds the top bar: plain html elements composed here, the
// look in docsite-footer.style.css (an owned style; `gofastr gen styles`
// writes the class methods in docsite-footer_style.gen.go), and every
// dimension a theme token — the wider spacing step (64px) this page's
// rhythm needs is docsite-footer.tokens.css's --spacing-section, and the grid
// sits at the site's wide cap (--size-wide-width, 1240px), the same
// measure the header and page sections align to.
//
// The footer is the page's contentinfo landmark.
package sitefooter

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Link is one destination.
type Link struct {
	Label, Href string
	// External opens in a new tab (the repo, pkg.go.dev, …).
	External bool
}

// Column is a titled list of links.
type Column struct {
	Title string
	Links []Link
}

// Config is what the site passes in.
type Config struct {
	// Version is the release label beside the name ("dev", "v0.8.0").
	Version string
	// Tagline is the line under the name.
	Tagline string
	// Columns are the titled link lists beside the lead.
	Columns []Column
	// Bottom is the quiet strip under the grid (copyright, colophon).
	Bottom []render.HTML
}

// Render builds the footer.
func Render(cfg Config) render.HTML {
	lead := html.Div(html.DivConfig{},
		html.Div(html.DivConfig{Class: Style.Brand()},
			html.Span(html.TextConfig{Class: Style.BrandMark()}),
			render.Text(" GoFastr "),
			html.Span(html.TextConfig{Class: Style.BrandVer()}, render.Text(cfg.Version)),
		),
		html.Paragraph(html.TextConfig{Class: Style.Copy()}, render.Text(cfg.Tagline)),
	)

	cols := make([]render.HTML, 0, len(cfg.Columns))
	for _, c := range cfg.Columns {
		items := make([]render.HTML, 0, len(c.Links))
		for _, l := range c.Links {
			attrs := html.Attrs{}
			if l.External {
				attrs["rel"] = "external"
				attrs["target"] = "_blank"
			}
			items = append(items, html.ListItem(html.ListItemConfig{},
				html.Link(html.LinkConfig{Href: l.Href, Text: l.Label, ExtraAttrs: attrs})))
		}
		cols = append(cols, html.Div(html.DivConfig{},
			html.Paragraph(html.TextConfig{Class: Style.Title()}, render.Text(c.Title)),
			html.UnorderedList(html.ListConfig{Class: Style.Links()}, items...),
		))
	}

	return Style.Scope(html.Footer(html.FooterConfig{ContentInfo: true},
		html.Div(html.DivConfig{Class: Style.Grid()},
			append([]render.HTML{lead}, cols...)...,
		),
		html.Div(html.DivConfig{Class: Style.Bottom()}, cfg.Bottom...),
	))
}
