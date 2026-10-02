// Package siteheader is rtc-call's top bar, built the way an app
// builds any piece of chrome the framework does not ship:
//
//   - the markup is plain html elements and framework parts composed
//     here (html.Header as the banner landmark, the brand link, the
//     nav of links);
//   - the look is rtc-siteheader.style.css, an owned style; Style.Scope
//     stamps the owner on the root, and `gofastr gen styles` writes
//     the class methods in rtc-siteheader_style.gen.go;
//   - every dimension is a theme token: the bar's content sits on the
//     page measure (--size-page-width, with --size-page-gutter
//     outside it), the column ui.Container's page width gives main;
//   - the behaviour is the framework's: the runtime's active-link pass
//     sets aria-current on the nav links (data-fui-match-prefix keeps
//     a section link lit).
//
// The two links fit beside the brand at phone widths, so the bar ships
// no phone menu: nothing folds away.
package siteheader

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Link is one destination.
type Link struct {
	Label, Href string
	// Section keeps the link current on every page under Href, through
	// the runtime's data-fui-match-prefix.
	Section bool
}

// Config is what the app passes in.
type Config struct {
	// Name is the product name; it links home.
	Name  string
	Links []Link
}

// Render builds the bar.
func Render(cfg Config) render.HTML {
	links := func() []render.HTML {
		out := make([]render.HTML, 0, len(cfg.Links))
		for _, l := range cfg.Links {
			var attrs html.Attrs
			if l.Section {
				attrs = html.Attrs{"data-fui-match-prefix": ""}
			}
			out = append(out, html.Link(html.LinkConfig{Href: l.Href, Text: l.Label, ExtraAttrs: attrs}))
		}
		return out
	}()

	brand := html.LinkHTML(html.LinkHTMLConfig{
		Href:    "/",
		Class:   Style.Brand(),
		Content: render.Text(cfg.Name),
	})

	var primary render.HTML
	if len(cfg.Links) > 0 {
		primary = html.Nav(html.NavConfig{
			Class: Style.Links(),
			Label: i18nui.T(context.Background(), i18nui.KeyNavPrimary),
		}, links...)
	}
	return Style.Scope(html.Header(html.HeaderConfig{Banner: true},
		html.Div(html.DivConfig{Class: Style.Bar()}, brand, primary)))
}
