// Package siteheader is the app's top bar. This package is the app's own
// code: edit it freely.
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
	// Section keeps the link current on every page under Href.
	Section bool
}

// Config is what the app passes in.
type Config struct {
	// Ctx resolves the nav labels through i18nui.
	Ctx context.Context
	// Name is the wordmark; it links home.
	Name string
	// Mark is the logo before Name, in the theme's primary colour; zero draws none.
	Mark  render.HTML
	Links []Link
	// CTA is the call to action, in the bar and in the phone menu; zero draws none.
	CTA Link
	// Actions stay in the bar at every width.
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
				attrs = html.Attrs{"data-cui-match-prefix": ""}
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
		primary = html.Nav(html.NavConfig{Class: Style.Links(), Label: i18nui.NavPrimary.Text(ctx)}, links()...)
	}
	if cfg.CTA.Href != "" {
		barCTA = html.Div(html.DivConfig{Class: Style.BarCta()},
			ui.LinkButton(ui.LinkButtonConfig{Label: cfg.CTA.Label, Href: cfg.CTA.Href, Size: ui.ButtonSizeSmall}))
	}
	if len(cfg.Links) > 0 || cfg.CTA.Href != "" {
		panel := []render.HTML{
			html.Nav(html.NavConfig{Class: Style.PanelLinks(), Label: i18nui.NavMobilePrimary.Text(ctx)}, links()...),
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
				headless.PartSummary: {"aria-label": i18nui.NavToggle.Text(ctx)},
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
