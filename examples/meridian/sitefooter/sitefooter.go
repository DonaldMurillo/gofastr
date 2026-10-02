// Package sitefooter is Meridian's colophon, built the way siteheader
// builds the top bar: plain html elements composed here, the look in
// meridian-sitefooter.style.css (an owned style; `gofastr gen styles`
// writes the class methods in meridian-sitefooter_style.gen.go), and
// every dimension a theme token. Its content sits on the page measure
// the header and main share, so the three line up at every width.
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
}

// Column is a titled list of links.
type Column struct {
	Title string
	Links []Link
}

// Config is what the site passes in.
type Config struct {
	// Name is the product name; it links home.
	Name    string
	Columns []Column
}

// Render builds the footer.
func Render(cfg Config) render.HTML {
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

	return Style.Scope(html.Footer(html.FooterConfig{ContentInfo: true},
		html.Div(html.DivConfig{Class: Style.Inner()},
			html.Div(html.DivConfig{Class: Style.Top()},
				html.Div(html.DivConfig{},
					html.Link(html.LinkConfig{Href: "/", Text: cfg.Name, Class: Style.Brand()})),
				html.Div(html.DivConfig{Class: Style.Columns()}, cols...),
			),
		)))
}
