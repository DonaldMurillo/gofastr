package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Themed wraps children in a <div> carrying ref's class, so the registered
// theme override applies to them.
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
