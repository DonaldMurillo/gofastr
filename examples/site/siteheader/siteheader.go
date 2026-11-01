// Package siteheader is the GoFastr site's top bar, built the way an
// app builds any piece of chrome the framework does not ship:
//
//   - the markup is plain html elements and framework parts composed
//     here (html.Header, html.Nav, headless.Disclosure, ui.ThemeToggle);
//   - the look is docsite-header.style.css, an owned style; Style.Scope
//     stamps the owner on the root, and `gofastr gen styles` writes the
//     class methods in docsite-header_style.gen.go;
//   - every dimension is a theme token: the bar is --size-header-height
//     tall (the theme carries the site's 60px), and the one ramp values
//     the built-in text scale doesn't have (11/13/15px) plus the faint
//     hairline under the bar are docsite-header.tokens.css tokens, generated
//     as Tokens for the site to Extend its theme with;
//   - the behaviour is the framework's: headless.Disclosure (Trap) is
//     the phone menu, a light-dismiss popup (Dismiss) — the
//     headless-disclosure module closes it on Escape, on a link tap, on
//     a tap outside it and on navigation, and traps focus while it is
//     open — and the runtime's active-link pass sets aria-current on
//     the nav links (data-cui-match-prefix keeps a section link lit).
//
// The bar is the page's banner landmark and a direct child of the
// page-tall ui.Stack, which pins it for the whole page when the layout
// sets StickyHeader (the stack also tells sticky parts below where the
// bar ends), so content scrolls under it.
package siteheader

import (
	"context"

	brandassets "github.com/DonaldMurillo/gofastr/assets/brand"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

// Link is one destination.
type Link struct {
	Label, Href string
	// Section keeps the link current on every page under Href (Framework
	// stays lit on /framework/getting-started), through the runtime's
	// data-cui-match-prefix.
	Section bool
	// External opens in a new tab; only honored in the phone menu.
	External bool
}

// Config is what the site passes in.
type Config struct {
	// Ctx resolves the nav and toggle labels through i18nui.
	Ctx context.Context
	// Version is the release label beside the brand ("dev", "v0.8.0").
	Version string
	// Links renders both the desktop nav and the phone menu list.
	Links []Link
	// MenuLead renders only in the phone menu, above Links: the entry
	// points the desktop bar reaches through its search (Get started,
	// Docs).
	MenuLead []Link
}

// navLink renders one Link; data-cui-match-prefix and rel=external ride
// the typed fields, never caller markup.
func navLink(l Link) render.HTML {
	attrs := html.Attrs{}
	if l.Section {
		attrs["data-cui-match-prefix"] = ""
	}
	if l.External {
		attrs["rel"] = "external"
		attrs["target"] = "_blank"
	}
	return html.Link(html.LinkConfig{Href: l.Href, Text: l.Label, ExtraAttrs: attrs})
}

// searchAttrs are the contract both search controls share: one trigger
// that opens the CommandPalette on click (data-cui-open) and binds ⌘K
// directly (data-hui-shortcut-click), so there is exactly one "open
// search" affordance per surface. Each is a link to the search page,
// where a reader without script lands instead.
func searchAttrs() map[string]string {
	return map[string]string{
		"href":                    "/search",
		"aria-label":              "Open search to find a doc, component, or example",
		"data-cui-open":           "site-command-palette",
		"data-hui-shortcut-click": "Meta+K",
	}
}

// searchButton builds the "Find docs, components, examples…" pill for
// the bar: mono placeholder on the left, ⌘K keycaps on the right. The
// magnifier glyph shows only when the pill collapses (the phone menu
// renders the icon button instead).
func searchButton() render.HTML {
	attrs := searchAttrs()
	attrs["class"] = Style.Cmd()
	return render.Tag("a", attrs,
		ui.Icon(icons.Search, ui.IconConfig{Size: "16", Class: Style.CmdGlyph()}),
		html.Span(html.TextConfig{Class: Style.CmdPlaceholder()}, render.Text("Find docs, components, examples…")),
		html.Kbd(html.TextConfig{},
			html.Kbd(html.TextConfig{}, render.Text("⌘")),
			html.Kbd(html.TextConfig{}, render.Text("K")),
		),
	)
}

// panelSearch builds the phone menu's copy of the trigger: a
// full-width field at the top of the menu, glyph and placeholder, no
// keycaps (a phone has no ⌘K).
func panelSearch() render.HTML {
	attrs := searchAttrs()
	attrs["class"] = Style.PanelSearch()
	return render.Tag("a", attrs,
		ui.Icon(icons.Search, ui.IconConfig{Size: "16"}),
		html.Span(html.TextConfig{}, render.Text("Find docs, components, examples…")),
	)
}

// githubLink is the GitHub mark, a 44px icon anchor.
func githubLink() render.HTML {
	return html.LinkHTML(html.LinkHTMLConfig{
		Href:  "https://github.com/DonaldMurillo/gofastr",
		Class: Style.Icon(),
		ExtraAttrs: html.Attrs{
			"aria-label": "GitHub",
			"rel":        "external",
		},
		Content: ui.Icon(icons.Github, ui.IconConfig{Size: "16"}),
	})
}

// actions is the right-hand cluster: the search pill, the theme toggle
// (the framework's own ghost icon button) and the GitHub mark.
func actions() render.HTML {
	return html.Div(html.DivConfig{Class: Style.BarActions()},
		searchButton(),
		ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),
		githubLink(),
	)
}

// brand is the go-arrow lockup and the live version
// capsule; the capsule is GoFastr's identity, so it renders from
// Config.Version, never caller markup.
func brand(version string) render.HTML {
	return html.LinkHTML(html.LinkHTMLConfig{
		Href:  "/",
		Class: Style.Brand(),
		ExtraAttrs: html.Attrs{
			"aria-label": "gofastr, " + version + " (v0.x, APIs may change)",
		},
		Content: render.Join(
			html.Span(html.TextConfig{Class: Style.BrandMark()}, render.HTML(brandassets.SVG())),
			html.Span(html.TextConfig{
				Class:      Style.BrandStatus(),
				ExtraAttrs: html.Attrs{"title": "v0.x. Pin a version; APIs may change between releases."},
			},
				html.Span(html.TextConfig{Class: Style.BrandPulse()}),
				html.Span(html.TextConfig{Class: Style.BrandVer()}, render.Text(version)),
			),
		),
	})
}

// Render builds the bar. Below the xl breakpoint the links (and the
// search + toggle cluster, below md) move into a native <details> menu,
// so the bar works without JavaScript.
func Render(cfg Config) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	links := make([]render.HTML, 0, len(cfg.Links))
	for _, l := range cfg.Links {
		links = append(links, navLink(l))
	}
	var menuLinks []render.HTML
	if len(cfg.MenuLead) > 0 {
		lead := make([]render.HTML, 0, len(cfg.MenuLead))
		for _, l := range cfg.MenuLead {
			lead = append(lead, navLink(l))
		}
		menuLinks = append(menuLinks, html.Div(html.DivConfig{Class: Style.PanelLead()}, lead...))
	}
	menuLinks = append(menuLinks, links...)

	var primary render.HTML
	if len(links) > 0 {
		primary = html.Nav(html.NavConfig{Class: Style.Links(), Label: i18nui.NavPrimary.Text(ctx)}, links...)
	}

	var menu render.HTML
	if len(menuLinks) > 0 {
		menu = headless.Disclosure(headless.DisclosureProps{
			Summary: render.Join(
				html.Span(html.TextConfig{Class: Style.GlyphOpen()}, ui.Icon(icons.Menu, ui.IconConfig{})),
				html.Span(html.TextConfig{Class: Style.GlyphClose()}, ui.Icon(icons.Close, ui.IconConfig{})),
			),
			// Below md the bar's search, theme toggle and GitHub mark
			// move in here: search on top, the two icons in one row
			// at the bottom. From md they stay in the bar, so the
			// menu holds links only.
			Content: render.Join(
				panelSearch(),
				html.Nav(html.NavConfig{Class: Style.PanelLinks(), Label: i18nui.NavMobilePrimary.Text(ctx)}, menuLinks...),
				html.Div(html.DivConfig{Class: Style.PanelActions()},
					ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),
					githubLink(),
				),
			),
			Trap:    true,
			Dismiss: true,
			Parts: headless.Parts{Attrs: headless.PartAttrs{
				headless.PartSummary: {"aria-label": i18nui.NavToggle.Text(ctx)},
			}},
		}, headless.Classes{
			headless.PartRoot:    Style.Menu(),
			headless.PartSummary: Style.Toggle(),
			headless.PartPanel:   Style.Panel(),
		})
	}

	return Style.Scope(html.Header(html.HeaderConfig{Banner: true},
		html.Div(html.DivConfig{Class: Style.Bar()},
			brand(cfg.Version),
			primary,
			html.Div(html.DivConfig{Class: Style.End()},
				actions(),
				menu,
			),
		)))
}
