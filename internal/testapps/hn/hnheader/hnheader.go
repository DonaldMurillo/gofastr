// Package hnheader is the reader's top bar: the name and the section
// links on one strip in the theme's primary colour. The framework ships
// no news-site masthead, so the app draws its own the sanctioned way:
// plain html elements composed here, the look in hnheader.style.css (an
// owned style; `gofastr gen styles` writes hnheader_style.gen.go), every
// value a theme token.
//
// The server marks the current section for first paint; every link
// hands its current-state to the runtime's activelink module
// (data-cui-activelink), which moves the mark after a client
// navigation, since the bar lives in the layout and is not re-rendered.
package hnheader

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Link is one section.
type Link struct {
	Label, Href string
	// Also lists other paths that show this section (/news is the top
	// list too).
	Also []string
}

// Config is what the app passes in.
type Config struct {
	// Ctx carries the request, whose path picks the current section.
	Ctx context.Context
	// Name is the site name; Mark is the one or two letters in the box
	// beside it.
	Name, Mark string
	Links      []Link
}

// Render builds the bar.
func Render(cfg Config) render.HTML {
	path := ""
	if r := app.RequestFromContext(cfg.Ctx); r != nil {
		path = r.URL.Path
	}
	links := make([]render.HTML, 0, len(cfg.Links))
	for _, l := range cfg.Links {
		attrs := html.Attrs{"data-cui-activelink": ""}
		if current(path, l) {
			attrs["aria-current"] = "page"
		}
		links = append(links, html.Link(html.LinkConfig{Href: l.Href, Text: l.Label, ExtraAttrs: attrs}))
	}
	brand := html.LinkHTML(html.LinkHTMLConfig{
		Href:  "/",
		Class: Style.Brand(),
		Content: render.Join(
			html.Span(html.TextConfig{Class: Style.Mark(), ExtraAttrs: html.Attrs{"aria-hidden": "true"}}, render.Text(cfg.Mark)),
			render.Text(cfg.Name),
		),
	})
	var nav render.HTML
	if len(links) > 0 {
		nav = html.Nav(html.NavConfig{Class: Style.Links(), Label: "Sections"}, links...)
	}
	return Style.Scope(html.Header(html.HeaderConfig{Banner: true},
		html.Div(html.DivConfig{Class: Style.Bar()}, brand, nav)))
}

func current(path string, l Link) bool {
	path = strings.TrimSuffix(path, "/")
	if path == "" {
		path = "/"
	}
	if path == l.Href {
		return true
	}
	for _, a := range l.Also {
		if path == a {
			return true
		}
	}
	return false
}
