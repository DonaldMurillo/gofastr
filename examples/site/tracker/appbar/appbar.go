// Package appbar is the tracker's application bar, built the way an
// app builds any piece of chrome the framework does not ship:
//
//   - the markup is plain html elements and framework parts composed
//     here (html.Header as the banner landmark, the brand link, the
//     caller's search field, actions, and the sidebar's drawer
//     trigger);
//   - the look is appbar.style.css, an owned style; Style.Scope
//     stamps the owner on the root, and `gofastr gen styles` writes
//     the class methods in appbar_style.gen.go;
//   - every dimension is a theme token: the bar is --size-header-height
//     tall (the token ui.ContentRow's viewport mode subtracts), and the
//     one value only this bar needs, the desktop search field's width,
//     is its own token in appbar.tokens.css, generated as Tokens for
//     the app to Extend its theme with;
//   - the behaviour is the framework's: the sidebar's own stylesheet
//     hides the drawer trigger at widths where the inline column
//     shows, and the search field folds away below the md breakpoint.
//
// The bar is the app band above the content row: full-bleed (its
// border too), not the page measure a marketing site's header sits on.
package appbar

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Config is what the tracker's shell passes in.
type Config struct {
	// Name is the brand wordmark; Href is where it links (usually "/").
	Name, Href string
	// Search is the desktop search field (ui.SearchInput). It hides
	// below the md breakpoint; the sidebar drawer is the phone's way
	// around the app.
	Search render.HTML
	// Actions stay in the bar at every viewport width (the bell, the
	// theme toggle).
	Actions render.HTML
	// MobileTrigger is the phone navigation trigger
	// (ui.SidebarDrawerTrigger). The sidebar's own stylesheet hides it
	// at widths where the inline column shows, so the bar renders it
	// unconditionally.
	MobileTrigger render.HTML
}

// Render builds the bar: the banner landmark, --size-header-height
// tall on the surface colour with a bottom rule, brand left, search
// beside it, actions and the phone trigger at the right edge.
func Render(cfg Config) render.HTML {
	brand := html.LinkHTML(html.LinkHTMLConfig{
		Href:    cfg.Href,
		Class:   Style.Brand(),
		Content: render.Text(cfg.Name),
	})
	var search render.HTML
	if cfg.Search != "" {
		search = html.Div(html.DivConfig{Class: Style.Search()}, cfg.Search)
	}
	var actions render.HTML
	if cfg.Actions != "" || cfg.MobileTrigger != "" {
		actions = html.Div(html.DivConfig{Class: Style.Actions()}, cfg.Actions, cfg.MobileTrigger)
	}
	return Style.Scope(html.Header(html.HeaderConfig{Banner: true}, brand, search, actions))
}
