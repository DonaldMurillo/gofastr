// Package siteheader is the app's top bar, built the way an app
// builds any piece of chrome the framework does not ship:
//
//   - the markup is plain html elements and framework parts composed
//     here (html.Header, html.Nav, headless.Disclosure, ui.Icon,
//     ui.LinkButton);
//   - the look is siteheader.style.css, an owned style; Style.Scope
//     stamps the owner on the root, and `gofastr gen styles` writes the
//     class methods in siteheader_style.gen.go;
//   - every dimension is a theme token: the page measure and the bar
//     height are built-ins, and the one value only this bar needs, the
//     phone menu's stagger, is its own token in siteheader.tokens.css,
//     generated as Tokens for the app to Extend its theme with;
//   - the behaviour is the framework's: headless.Disclosure's module
//     closes the phone menu on Escape, on a link tap and on navigation,
//     and traps focus while it is open; the runtime's active-link pass
//     sets aria-current on the nav links.
//
// The bar is the page's banner landmark and a direct child of the
// page-tall ui.Stack, which is what lets it stay pinned for the whole
// page. This package is the app's own code: edit it freely.
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
	// Section keeps the link current on every page under Href (Help
	// stays lit on /help/billing), through the runtime's
	// data-fui-match-prefix.
	Section bool
}

// Config is what the app passes in.
type Config struct {
	// Ctx resolves the nav labels through i18nui.
	Ctx context.Context
	// Name is the wordmark; it links home.
	Name string
	// Mark is the logo drawn before Name inside the brand link. It
	// takes the theme's primary colour (.mark colours with
	// --color-primary, so the mark inherits it); a monochrome currentColor
	// SVG is the shape that needs nothing else. Zero draws none.
	Mark  render.HTML
	Links []Link
	// CTA is the call to action: a small button at the end of the bar
	// on wide screens, a full-width one at the foot of the phone menu.
	// A zero CTA draws none.
	CTA Link
	// Actions stay in the bar at every width (the theme toggle).
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

	brand := []render.HTML{}
	if cfg.Mark != "" {
		brand = append(brand, html.Span(html.TextConfig{Class: Style.Mark()}, cfg.Mark))
	}
	brand = append(brand, render.Text(cfg.Name))
	brandLink := html.LinkHTML(html.LinkHTMLConfig{
		Href:    "/",
		Class:   Style.Brand(),
		Content: render.Join(brand...),
	})

	var primary, barCTA, menu render.HTML
	if len(cfg.Links) > 0 {
		primary = html.Nav(html.NavConfig{Class: Style.Links(), Label: i18nui.T(ctx, i18nui.KeyNavPrimary)}, links()...)
	}
	if cfg.CTA.Href != "" {
		barCTA = html.Div(html.DivConfig{Class: Style.BarCta()},
			ui.LinkButton(ui.LinkButtonConfig{Label: cfg.CTA.Label, Href: cfg.CTA.Href, Size: ui.ButtonSizeSmall}))
	}
	if len(cfg.Links) > 0 || cfg.CTA.Href != "" {
		panel := []render.HTML{
			html.Nav(html.NavConfig{Class: Style.PanelLinks(), Label: i18nui.T(ctx, i18nui.KeyNavMobilePrimary)}, links()...),
		}
		if cfg.CTA.Href != "" {
			panel = append(panel, html.Div(html.DivConfig{Class: Style.PanelCta()},
				ui.LinkButton(ui.LinkButtonConfig{Label: cfg.CTA.Label, Href: cfg.CTA.Href})))
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

	end := html.Div(html.DivConfig{Class: Style.End()}, cfg.Actions, barCTA, menu)
	return Style.Scope(html.Header(html.HeaderConfig{Banner: true},
		html.Div(html.DivConfig{Class: Style.Bar()}, brandLink, primary, end)))
}
