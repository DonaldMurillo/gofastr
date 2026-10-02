// Package siteheader is Meridian's top bar, built the way an app
// builds any piece of chrome the framework does not ship:
//
//   - the markup is plain html elements and framework parts composed
//     here (html.Header, html.Nav, headless.Disclosure, ui.Icon,
//     ui.LinkButton);
//   - the look is meridian-siteheader.style.css, an owned style;
//     Style.Scope stamps the owner on the root, and `gofastr gen
//     styles` writes the class methods in
//     meridian-siteheader_style.gen.go;
//   - every dimension is a theme token: the page measure and the bar
//     height are built-ins, and the one value only this bar needs, the
//     phone menu's stagger, is its own token in
//     meridian-siteheader.tokens.css, generated as Tokens for the site
//     to Extend its theme with;
//   - the behaviour is the framework's: headless.Disclosure's module
//     closes the phone menu on Escape, on a link tap and on navigation,
//     and traps focus while it is open; the runtime's active-link pass
//     sets aria-current on the nav links.
//
// The bar keeps two action slots beside the nav: Persistent rides the
// bar at every width (the sign-in CTA a visitor needs to reach), while
// Actions sit at the bar's end on wide screens and fold into the phone
// menu below md (the theme toggle, sign out).
//
// The bar is the page's banner landmark and a direct child of the
// page-tall ui.Stack, which is what lets it stay pinned for the whole
// page.
package siteheader

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Link is one destination.
type Link struct {
	Label, Href string
	// Section keeps the link current on every page under Href,
	// through the runtime's data-fui-match-prefix.
	Section bool
}

// Config is what the site passes in.
type Config struct {
	// Ctx resolves the nav labels through i18nui.
	Ctx context.Context
	// Name is the wordmark, linked home.
	Name  string
	Links []Link
	// Persistent stays in the bar at every width: the one control the
	// page's journey depends on (the sign-in CTA). It never appears in
	// the phone menu. Zero draws none.
	Persistent render.HTML
	// Actions sit at the end of the bar on wide screens and move into
	// the phone menu below md (the theme toggle, sign out). Zero
	// draws none.
	Actions render.HTML
}

// Render builds the bar.
func Render(cfg Config) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
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
	}

	brand := html.Link(html.LinkConfig{Href: "/", Text: cfg.Name, Class: Style.Brand()})

	var primary, barActions, menu render.HTML
	if len(cfg.Links) > 0 {
		primary = html.Nav(html.NavConfig{Class: Style.Links(), Label: i18nui.T(ctx, i18nui.KeyNavPrimary)}, links()...)
	}
	if cfg.Actions != "" {
		// display: contents on wide screens, so the cluster joins the
		// bar's end row; the sheet folds it into the phone menu.
		barActions = html.Div(html.DivConfig{Class: Style.BarActions()}, cfg.Actions)
	}
	if len(cfg.Links) > 0 || cfg.Actions != "" {
		panel := []render.HTML{
			html.Nav(html.NavConfig{Class: Style.PanelLinks(), Label: i18nui.T(ctx, i18nui.KeyNavMobilePrimary)}, links()...),
		}
		if cfg.Actions != "" {
			panel = append(panel, html.Div(html.DivConfig{Class: Style.PanelActions()}, cfg.Actions))
		}
		menu = headless.Disclosure(headless.DisclosureProps{
			Summary: render.Join(
				html.Span(html.TextConfig{Class: Style.IconOpen()}, ui.Icon("menu", ui.IconConfig{})),
				html.Span(html.TextConfig{Class: Style.IconClose()}, ui.Icon("close", ui.IconConfig{})),
			),
			Content: render.Join(panel...),
			Trap:    true,
			Parts: headless.Parts{Attrs: headless.PartAttrs{
				headless.PartSummary: {"aria-label": i18nui.T(ctx, i18nui.KeyNavToggle)},
			}},
		}, headless.Classes{
			headless.PartRoot:    Style.Menu(),
			headless.PartSummary: Style.Toggle(),
			headless.PartPanel:   Style.Panel(),
		})
	}

	end := html.Div(html.DivConfig{Class: Style.End()}, barActions, cfg.Persistent, menu)
	return Style.Scope(html.Header(html.HeaderConfig{Banner: true},
		html.Div(html.DivConfig{Class: Style.Bar()}, brand, primary, end)))
}
