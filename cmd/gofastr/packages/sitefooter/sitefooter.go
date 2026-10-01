// Package sitefooter is the app's colophon, built the way siteheader
// builds the top bar: plain html elements composed here, the look in
// sitefooter.style.css (an owned style; `gofastr gen styles` writes the
// class methods in sitefooter_style.gen.go), and every dimension a
// theme token. Its content sits on the page measure the header and
// main share, so the three line up at every width.
//
// The footer is the page's contentinfo landmark. This package is the
// app's own code: edit it freely.
package sitefooter

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Link is one destination.
type Link struct {
	Label, Href string
}

// Column is a titled list of links.
type Column struct {
	Title string
	Links []Link
}

// Config is what the app passes in.
type Config struct {
	// Name is the app's name; it links home.
	Name string
	// Tagline is one line under the name. Empty draws none.
	Tagline string
	Columns []Column
	// Note is the bottom line (the copyright). Empty draws none.
	Note string
}

// Render builds the footer.
func Render(cfg Config) render.HTML {
	lead := []render.HTML{
		html.Link(html.LinkConfig{Href: "/", Text: cfg.Name, Class: Style.Brand()}),
	}
	if cfg.Tagline != "" {
		lead = append(lead, html.Paragraph(html.TextConfig{Class: Style.Tagline()}, render.Text(cfg.Tagline)))
	}

	cols := make([]render.HTML, 0, len(cfg.Columns))
	for _, c := range cfg.Columns {
		items := make([]render.HTML, 0, len(c.Links))
		for _, l := range c.Links {
			items = append(items, html.ListItem(html.ListItemConfig{},
				html.Link(html.LinkConfig{Href: l.Href, Text: l.Label})))
		}
		cols = append(cols, html.Div(html.DivConfig{},
			html.Heading(html.HeadingConfig{Level: 2, Class: Style.Title()}, render.Text(c.Title)),
			html.UnorderedList(html.ListConfig{Class: Style.Links()}, items...),
		))
	}

	var note render.HTML
	if cfg.Note != "" {
		note = html.Paragraph(html.TextConfig{Class: Style.Note()}, render.Text(cfg.Note))
	}

	return Style.Scope(html.Footer(html.FooterConfig{ContentInfo: true},
		html.Div(html.DivConfig{Class: Style.Inner()},
			html.Div(html.DivConfig{Class: Style.Top()},
				html.Div(html.DivConfig{}, lead...),
				html.Div(html.DivConfig{Class: Style.Columns()}, cols...),
			),
			note,
		)))
}
