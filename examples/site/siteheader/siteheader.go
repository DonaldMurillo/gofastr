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
//     the phone menu — the headless-disclosure module closes it on
//     Escape, on a link tap and on navigation, and traps focus while it
//     is open — and the runtime's active-link pass sets aria-current on
//     the nav links (data-cui-match-prefix keeps a section link lit).
//
// The bar is the page's banner landmark and a direct child of the
// page-tall ui.Stack, which is what lets it stay pinned for the whole
// page while content scrolls under it.
package siteheader

import (
	"context"
	"slices"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
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
	// ExtraLinks render only in the phone menu (Home, GitHub ↗, …).
	ExtraLinks []Link
}

// githubMark is the GitHub logo glyph; the anchor's aria-label carries
// the meaning, the drawing stays decorative.
const githubMark = `<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 0a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2.1c-3.3.7-4-1.6-4-1.6-.5-1.4-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1.1 1.9 2.9 1.3 3.6 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.3.5-2.4 1.2-3.2-.1-.3-.5-1.5.1-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.7.2 2.9.1 3.2.8.8 1.2 1.9 1.2 3.2 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6A12 12 0 0 0 12 0z"/></svg>`

// searchGlyph is the magnifier drawn inside the phone menu's icon-only
// search trigger.
const searchGlyph = `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>`

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
// search" affordance per surface.
func searchAttrs() map[string]string {
	return map[string]string{
		"type":                    "button",
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
	return render.Tag("button", attrs,
		render.Raw(`<svg class="`+Style.CmdGlyph()+`" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>`),
		html.Span(html.TextConfig{Class: Style.CmdPlaceholder()}, render.Text("Find docs, components, examples…")),
		html.Kbd(html.TextConfig{},
			html.Kbd(html.TextConfig{}, render.Text("⌘")),
			html.Kbd(html.TextConfig{}, render.Text("K")),
		),
	)
}

// searchIconButton builds the phone-menu's copy of the trigger: the
// 44px icon shape of the pill, same palette, same ⌘K binding.
func searchIconButton() render.HTML {
	attrs := searchAttrs()
	attrs["class"] = Style.CmdIcon()
	return render.Tag("button", attrs, render.Raw(searchGlyph))
}

// actions is the right-hand cluster: the search pill, the theme toggle
// (the framework's own ghost icon button) and the GitHub mark.

func actions() render.HTML {
	return html.Div(html.DivConfig{Class: Style.BarActions()},
		searchButton(),
		ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),
		html.LinkHTML(html.LinkHTMLConfig{
			Href:  "https://github.com/DonaldMurillo/gofastr",
			Class: Style.Icon(),
			ExtraAttrs: html.Attrs{
				"aria-label": "GitHub",
				"rel":        "external",
			},
			Content: render.Raw(githubMark),
		}),
	)
}

// brand is the λ mark, the lowercase wordmark and the live version
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
			html.Span(html.TextConfig{Class: Style.BrandMark()}, render.Text("λ")),
			html.Span(html.TextConfig{Class: Style.BrandName()}, render.Text("gofastr")),
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
	menuLinks := slices.Clone(links)
	for _, l := range cfg.ExtraLinks {
		menuLinks = append(menuLinks, navLink(l))
	}

	var primary render.HTML
	if len(links) > 0 {
		primary = html.Nav(html.NavConfig{Class: Style.Links(), Label: i18nui.T(ctx, i18nui.KeyNavPrimary)}, links...)
	}

	var menu render.HTML
	if len(menuLinks) > 0 {
		menu = headless.Disclosure(headless.DisclosureProps{
			Summary: render.Join(
				html.Span(html.TextConfig{Class: Style.GlyphOpen()}, ui.Icon("menu", ui.IconConfig{})),
				html.Span(html.TextConfig{Class: Style.GlyphClose()}, ui.Icon("close", ui.IconConfig{})),
			),
			Content: render.Join(
				html.Nav(html.NavConfig{Class: Style.PanelLinks(), Label: i18nui.T(ctx, i18nui.KeyNavMobilePrimary)}, menuLinks...),
				// The drawer's own action cluster: the icon-shaped
				// search trigger, the theme toggle and the GitHub mark.
				html.Div(html.DivConfig{Class: Style.PanelActions()},
					searchIconButton(),
					ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),
					html.LinkHTML(html.LinkHTMLConfig{
						Href:  "https://github.com/DonaldMurillo/gofastr",
						Class: Style.Icon(),
						ExtraAttrs: html.Attrs{
							"aria-label": "GitHub",
							"rel":        "external",
						},
						Content: render.Raw(githubMark),
					}),
				),
			),
			Trap: true,
			Parts: headless.Parts{Attrs: headless.PartAttrs{
				headless.PartSummary: {"aria-label": i18nui.T(ctx, i18nui.KeyNavToggle)},
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
