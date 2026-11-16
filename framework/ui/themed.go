package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Themed wraps children in a <div class="cui-theme-<hash>"> so the
// CSS variable cascade applies a section-level theme override to
// every descendant. Components inside Themed read var(--color-…)
// as usual: the browser dereferences them against the override
// block, not the canonical :root.
//
// Use it for dark sections, branded callouts, multi-tenant
// re-skinning of one subtree without touching surrounding chrome.
//
//	var Dark = style.RegisterThemeOverride(darkTheme)
//
//	ui.Themed(Dark,
//	    ui.Section(ui.SectionConfig{Heading: "Settings"},
//	        ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary}),
//	    ),
//	)
//
// The override class block lives in /__gofastr/app.css; registering
// the same theme twice (same content) returns the same handle, so
// the CSS only ships once.
func Themed(ref style.ThemeRef, children ...render.HTML) render.HTML {
	return render.Tag("div", map[string]string{"class": ref.Class()}, children...)
}

// Scheme names a color scheme a subtree is pinned to with [ColorScheme].
type Scheme string

const (
	// SchemeDark keeps the subtree dark whatever the page shows.
	SchemeDark Scheme = "dark"
	// SchemeLight keeps the subtree light whatever the page shows.
	SchemeLight Scheme = "light"
)

// ColorScheme wraps children in a <div data-color-scheme="dark|light">:
// the theme's palette for that scheme, its native-control scheme and its
// paint, re-declared on the wrapper, so a dark band stays dark on a
// light page and a light card stays light on a dark one.
//
//	ui.ColorScheme(ui.SchemeDark, ui.Section(ui.SectionConfig{Heading: "Pricing"}, ...))
//
// The theme needs a dark palette (DarkColors); a light-only theme has
// no dark tokens to pin. An unknown scheme renders the children in a
// plain <div> that follows the page.
func ColorScheme(scheme Scheme, children ...render.HTML) render.HTML {
	attrs := map[string]string{}
	if scheme == SchemeDark || scheme == SchemeLight {
		attrs["data-color-scheme"] = string(scheme)
	}
	return render.Tag("div", attrs, children...)
}
