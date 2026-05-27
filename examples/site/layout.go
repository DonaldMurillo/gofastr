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
)

// HeaderComponent renders the sticky top bar:
//   [GoFastr ver]   Docs   Get started   Examples   Components   Kiln       ⌘K   ⌥
type HeaderComponent struct{}

func (h *HeaderComponent) Render() render.HTML {
	// Brand — uses LinkHTML because the visible content is a span trio
	// (mark + wordmark + version pill) rather than plain text.
	brand := html.LinkHTML(html.LinkHTMLConfig{
		Href:  "/",
		Class: "nav__brand",
		Content: render.Join(
			html.Span(html.TextConfig{Class: "mark"}),
			render.Text(" GoFastr "),
			html.Span(html.TextConfig{Class: "ver"}, render.Text("v0.0.4")),
		),
	})

	// Primary nav. data-fui-match-prefix opts into the runtime's prefix-
	// active-route rule (so "Docs" lights up on /docs/foo too).
	navLink := func(href, text string) render.HTML {
		return html.Link(html.LinkConfig{
			Href:       href,
			Text:       text,
			ExtraAttrs: html.Attrs{"data-fui-match-prefix": ""},
		})
	}
	primary := render.Tag("nav",
		map[string]string{"class": "nav__links", "aria-label": "Primary"},
		navLink("/docs/", "Docs"),
		navLink("/get-started", "Get started"),
		navLink("/examples", "Examples"),
		navLink("/components", "Components"),
		navLink("/kiln", "Kiln"),
	)

	// Right side: search command (palette wiring deferred to a follow-up
	// commit once we register a CommandPalette island), plus GitHub icon.
	// The GitHub mark stays as inline SVG so it survives strict CSP (no
	// external img-src) — pkg.go.dev links don't carry our brand pixel.
	searchCmd := render.Tag("button",
		map[string]string{"class": "nav__cmd", "type": "button", "aria-label": "Open search"},
		html.Span(html.TextConfig{}, render.Text("Search")),
		render.Tag("kbd", nil, render.Text("⌘K")),
	)
	githubIcon := html.LinkHTML(html.LinkHTMLConfig{
		Href:  "https://github.com/DonaldMurillo/gofastr",
		Class: "nav__icon",
		ExtraAttrs: html.Attrs{
			"aria-label": "GitHub",
			"rel":        "external",
		},
		Content: render.Raw(`<svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M12 0a12 12 0 0 0-3.8 23.4c.6.1.8-.3.8-.6v-2.1c-3.3.7-4-1.6-4-1.6-.5-1.4-1.3-1.7-1.3-1.7-1.1-.7.1-.7.1-.7 1.2.1 1.8 1.2 1.8 1.2 1.1 1.9 2.9 1.3 3.6 1 .1-.8.4-1.3.8-1.6-2.7-.3-5.5-1.3-5.5-6 0-1.3.5-2.4 1.2-3.2-.1-.3-.5-1.5.1-3.2 0 0 1-.3 3.3 1.2a11.5 11.5 0 0 1 6 0c2.3-1.5 3.3-1.2 3.3-1.2.6 1.7.2 2.9.1 3.2.8.8 1.2 1.9 1.2 3.2 0 4.6-2.8 5.6-5.5 5.9.4.4.8 1.1.8 2.2v3.3c0 .3.2.7.8.6A12 12 0 0 0 12 0z"/></svg>`),
	})
	right := html.Div(html.DivConfig{Class: "nav__right"}, searchCmd, githubIcon)

	// Wrapping element is a <div> (not <header>) — framework Layout already
	// wraps this component's output in <header role="banner">. Doubling up
	// would emit nested <header> tags. The .nav class styles the chrome
	// regardless of tag.
	return html.Div(html.DivConfig{Class: "nav"}, brand, primary, right)
}

// FooterComponent — five-column credits grid + bottom strip.
type FooterComponent struct{}

func (f *FooterComponent) Render() render.HTML {
	col := func(title string, items ...render.HTML) render.HTML {
		return html.Div(html.DivConfig{},
			render.Tag("h6", nil, render.Text(title)),
			html.UnorderedList(html.ListConfig{}, items...),
		)
	}
	li := func(href, text string) render.HTML {
		return html.ListItem(html.ListItemConfig{},
			html.Link(html.LinkConfig{Href: href, Text: text}),
		)
	}

	brandCol := html.Div(html.DivConfig{},
		html.Div(html.DivConfig{Class: "foot__brand"},
			html.Span(html.TextConfig{Class: "mark"}),
			render.Text(" GoFastr "),
			html.Span(html.TextConfig{Class: "ver"}, render.Text("v0.0.4")),
		),
		render.Tag("p", map[string]string{"class": "foot__copy"},
			render.Text("A Go full-stack framework where agents are first-class authors. Pre-alpha. Built in public."),
		),
	)

	grid := html.Div(html.DivConfig{Class: "foot__grid"},
		brandCol,
		col("Read",
			li("/get-started", "Get started"),
			li("/docs/", "Docs"),
			li("/philosophy", "Philosophy"),
			li("https://github.com/DonaldMurillo/gofastr/commits/main", "Journal"),
		),
		col("Use",
			li("/examples", "Examples"),
			li("/kiln", "Kiln"),
			li("https://pkg.go.dev/github.com/DonaldMurillo/gofastr/cmd/gofastr", "CLI"),
		),
		col("Make",
			li("https://github.com/DonaldMurillo/gofastr/blob/main/CONTRIBUTING.md", "Contribute"),
			li("https://github.com/DonaldMurillo/gofastr/tree/main/docs", "RFCs"),
			li("https://github.com/DonaldMurillo/gofastr/releases", "Releases"),
		),
		col("Elsewhere",
			li("https://github.com/DonaldMurillo/gofastr", "GitHub"),
			li("https://pkg.go.dev/github.com/DonaldMurillo/gofastr", "pkg.go.dev"),
			li("https://github.com/DonaldMurillo/gofastr/discussions", "Discussions"),
		),
	)

	bottom := html.Div(html.DivConfig{Class: "foot__bottom"},
		html.Span(html.TextConfig{}, render.Text("© 2026 — a research project, not a company.")),
		html.Span(html.TextConfig{}, render.Text("Set in system sans + mono.")),
	)

	// Like the header — a <div>, not a <footer>, because the framework Layout
	// already wraps this in <footer role="contentinfo">.
	return html.Div(html.DivConfig{Class: "foot"}, grid, bottom)
}
