package main

// =============================================================================
// Header (.nav) + Footer (.foot) components, mounted on the default Layout so
// they wrap every screen the site registers. SSR-first — the prototype's
// markup is reproduced byte-for-byte (minus the search palette + mobile
// drawer, both intentionally deferred to follow-up commits).
//
// Built with core-ui/html primitives — html.Link, html.UnorderedList,
// html.LinkHTML — so attribute escaping + landmark roles are handled by
// typed builders rather than ad-hoc render.Tag calls. The two framework
// chrome elements (header role="banner", footer role="contentinfo") are
// supplied by the Layout wrapper; this component returns the inside.
//
// Porting target if these patterns turn out to be reusable: lift the nav
// into framework/ui/SiteNav and the footer into framework/ui/SiteFooter so
// other apps consuming the framework can drop them in with the same v2
// tokens. Until a second consumer exists, they live here.
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// HeaderComponent renders the sticky top bar:
//   [GoFastr ver]   Docs   Get started   Examples   Components   Kiln       ⌘K   ⌥
//
// The CommandPalette widget is mounted globally in main.go; the header's
// own search button opens it (data-fui-open) and binds ⌘K
// (data-fui-shortcut-click), so the header needs no injected trigger.
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	// Brand stays site-local — the λ mark, lowercase wordmark, and the
	// amber pre-alpha status pulse are GoFastr's identity. The framework's
	// SiteHeader takes Brand as a slot so each consuming site renders
	// whatever brand it wants. See ui.SiteHeader docs for the contract.
	brand := html.LinkHTML(html.LinkHTMLConfig{
		Href:  "/",
		Class: "site-brand",
		ExtraAttrs: html.Attrs{
			"aria-label": "gofastr — pre-alpha v0.0.4",
		},
		Content: render.Join(
			html.Span(html.TextConfig{Class: "site-brand__mark"}, render.Text("λ")),
			html.Span(html.TextConfig{Class: "site-brand__name"}, render.Text("gofastr")),
			html.Span(html.TextConfig{
				Class:      "site-brand__status",
				ExtraAttrs: html.Attrs{"title": "Pre-alpha — APIs change between commits."},
			},
				html.Span(html.TextConfig{Class: "site-brand__pulse"}),
				html.Span(html.TextConfig{Class: "site-brand__tag"}, render.Text("pre-alpha")),
				html.Span(html.TextConfig{Class: "site-brand__ver"}, render.Text("0.0.4")),
			),
		),
	})

	// Action cluster: a single search trigger (theme toggle, GitHub icon
	// follow). It opens the CommandPalette on click (data-fui-open) AND
	// binds ⌘K (data-fui-shortcut-click) directly, so there's exactly one
	// "open search" control. Previously a second, visually-hidden trigger
	// from ui.CommandPalette carried the shortcut, which left screen-reader
	// users hearing two identical "open search" buttons.
	searchCmd := render.Tag("button",
		map[string]string{
			"class":                   "site-cmd",
			"type":                    "button",
			"aria-label":              "Open search — find a doc, component, or example",
			"data-fui-open":           "site-command-palette",
			"data-fui-shortcut-click": "Meta+K",
		},
		// Magnifier glyph — shown only on phones (CSS swap below).
		// Desktop: hidden; the placeholder text + ⌘K hint carries
		// the affordance. Mobile: visible; replaces the pill.
		render.Raw(`<svg class="site-cmd__glyph" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>`),
		html.Span(html.TextConfig{Class: "site-cmd__placeholder"}, render.Text("Find docs, components, examples…")),
		render.Tag("kbd", nil,
			render.Tag("kbd", nil, render.Text("⌘")),
			render.Tag("kbd", nil, render.Text("K")),
		),
	)
	themeBtn := ui.ThemeToggle(ui.ThemeToggleConfig{
		Variant: ui.ThemeToggleIcon,
		Class:   "site-icon",
	})
	githubIcon := html.LinkHTML(html.LinkHTMLConfig{
		Href:  "https://github.com/DonaldMurillo/gofastr",
		Class: "site-icon",
		ExtraAttrs: html.Attrs{
			"aria-label": "GitHub",
			"rel":        "external",
		},
		Content: render.Raw(`<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 0a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2.1c-3.3.7-4-1.6-4-1.6-.5-1.4-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1.1 1.9 2.9 1.3 3.6 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.3.5-2.4 1.2-3.2-.1-.3-.5-1.5.1-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.7.2 2.9.1 3.2.8.8 1.2 1.9 1.2 3.2 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6A12 12 0 0 0 12 0z"/></svg>`),
	})
	actionChildren := []render.HTML{searchCmd, themeBtn, githubIcon}

	return ui.SiteHeader(ui.SiteHeaderConfig{
		Brand: brand,
		NavItems: []ui.SiteHeaderLink{
			{Label: "Docs", Href: "/docs/", MatchPrefix: true},
			{Label: "Get started", Href: "/get-started", MatchPrefix: true},
			{Label: "Examples", Href: "/examples", MatchPrefix: true},
			{Label: "Components", Href: "/components/", MatchPrefix: true},
			{Label: "Kiln", Href: "/kiln", MatchPrefix: true},
		},
		MobileExtraLinks: []ui.SiteHeaderLink{
			{Label: "Home", Href: "/"},
			{Label: "Philosophy", Href: "/philosophy", MatchPrefix: true},
			{Label: "GitHub ↗", Href: "https://github.com/DonaldMurillo/gofastr", External: true},
		},
		Actions: render.Join(actionChildren...),
		Class:   "site-header",
	})
}

// FooterComponent — credits grid + bottom strip. Composition shipped
// by ui.SiteFooter; this consumer only fills in lead/columns/bottom.
type FooterComponent struct{}

func (f *FooterComponent) Render() render.HTML {
	lead := render.Join(
		html.Div(html.DivConfig{Class: "site-foot-brand"},
			html.Span(html.TextConfig{Class: "site-foot-brand__mark"}),
			render.Text(" GoFastr "),
			html.Span(html.TextConfig{Class: "site-foot-brand__ver"}, render.Text("v0.0.4")),
		),
		render.Tag("p", map[string]string{"class": "site-foot-brand__copy"},
			render.Text("A Go full-stack framework where agents are first-class authors. Pre-alpha. Built in public."),
		),
	)

	return ui.SiteFooter(ui.SiteFooterConfig{
		Lead: lead,
		Columns: []ui.SiteFooterColumn{
			{Title: "Read", Links: []ui.SiteFooterLink{
				{Label: "Get started", Href: "/get-started"},
				{Label: "Docs", Href: "/docs/"},
				{Label: "Philosophy", Href: "/philosophy"},
				{Label: "Journal", Href: "https://github.com/DonaldMurillo/gofastr/commits/main", External: true},
			}},
			{Title: "Use", Links: []ui.SiteFooterLink{
				{Label: "Examples", Href: "/examples"},
				{Label: "Kiln", Href: "/kiln"},
				{Label: "CLI", Href: "https://pkg.go.dev/github.com/DonaldMurillo/gofastr/cmd/gofastr", External: true},
			}},
			{Title: "Make", Links: []ui.SiteFooterLink{
				{Label: "Contribute", Href: "https://github.com/DonaldMurillo/gofastr/blob/main/CONTRIBUTING.md", External: true},
				{Label: "RFCs", Href: "https://github.com/DonaldMurillo/gofastr/tree/main/docs", External: true},
				{Label: "Releases", Href: "https://github.com/DonaldMurillo/gofastr/releases", External: true},
			}},
			{Title: "Elsewhere", Links: []ui.SiteFooterLink{
				{Label: "GitHub", Href: "https://github.com/DonaldMurillo/gofastr", External: true},
				{Label: "pkg.go.dev", Href: "https://pkg.go.dev/github.com/DonaldMurillo/gofastr", External: true},
				{Label: "Discussions", Href: "https://github.com/DonaldMurillo/gofastr/discussions", External: true},
			}},
		},
		Bottom: []render.HTML{
			html.Span(html.TextConfig{}, render.Text("© 2026 — a research project, not a company.")),
			html.Span(html.TextConfig{}, render.Text("Set in system sans + mono.")),
		},
		Class: "site-foot",
	})
}
