package ui

// Responsive is a viewport-swap primitive. Renders BOTH a desktop and a
// mobile variant of a region, then hides one via the registered
// stylesheet's @media query. Use it when a CSS-only collapse of the
// desktop tree produces poor mobile UX (think: a multi-level sidebar
// that has no good "stack vertically" form, or a complex toolbar
// that's much better as a single picker on small screens).
//
// Why both render in SSR (not a runtime swap):
//   - Same HTML lands on every viewport. No FOUC, no JS dependency
//   - Search engines + AT only walk the visible branch (display:none
//     removes a subtree from the accessibility tree and tab order)
//   - SPA navigation doesn't have to re-fetch a separate "mobile page"
//
// Trade-off: a duplicate subtree in the markup. Worth it when the two
// variants render genuinely different elements (a vertical nestedlist
// vs. a <select> jump menu). NOT worth it when CSS alone could
// reflow the desktop tree (use plain @media for that case).
//
//	ui.Responsive(ui.ResponsiveConfig{Below: ui.StackBelowLG},
//	    desktopSidebar,   // shown at 64rem and wider
//	    mobilePicker)     // shown below 64rem
//
// The primitive wraps each variant in a `<div class="fui-responsive__…">`
// and toggles their display from one stylesheet registered at package
// init: at and above the breakpoint the desktop variant shows, below it
// the mobile variant.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ResponsiveConfig configures the swap.
type ResponsiveConfig struct {
	// Below is the breakpoint the mobile variant shows below; the
	// desktop variant shows at and above it. The zero value is
	// StackBelowMD (48rem); StackBelowLG is 64rem. Other values panic.
	Below StackBreakpoint
	// Class is appended to the wrapping <div>'s class list.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the swap's root element.
	// Keys the component owns are dropped: class (use Class) and
	// data-cui-*.
	ExtraAttrs html.Attrs
}

// Responsive emits both variants wrapped in viewport-toggled divs.
func Responsive(cfg ResponsiveConfig, desktop, mobile render.HTML) render.HTML {
	checkStackBreakpoint(cfg.Below)
	cls := "fui-responsive"
	if cfg.Below == StackBelowLG {
		cls += " fui-responsive--stack-below-lg"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	desktopAttrs := html.Attrs{}
	if desktop == "" {
		desktopAttrs["data-cui-internal"] = ""
	}
	mobileAttrs := html.Attrs{}
	if mobile == "" {
		mobileAttrs["data-cui-internal"] = ""
	}
	return responsiveStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	},
		html.Div(html.DivConfig{
			Class:      "fui-responsive__desktop",
			ExtraAttrs: desktopAttrs,
		}, desktop),
		html.Div(html.DivConfig{
			Class:      "fui-responsive__mobile",
			ExtraAttrs: mobileAttrs,
		}, mobile),
	))
}

// responsiveStyle registers at package init, before any host builds
// its component catalog, so a page reached by client-side navigation
// can load it. A sheet registered on first render missed the catalog.
var responsiveStyle = registry.RegisterStyle("ui-responsive", responsiveCSS)

// responsiveCSS holds both postures. The child combinator keeps a
// nested Responsive's variants out of its parent's rules. Two queries
// per posture, so neither variant flashes before media queries apply.
func responsiveCSS(_ style.Theme) string {
	return `
@media (min-width: 48rem) { .fui-responsive:not(.fui-responsive--stack-below-lg) > .fui-responsive__mobile { display: none !important; } }
@media (max-width: 47.99rem) { .fui-responsive:not(.fui-responsive--stack-below-lg) > .fui-responsive__desktop { display: none !important; } }
@media (min-width: 64rem) { .fui-responsive--stack-below-lg > .fui-responsive__mobile { display: none !important; } }
@media (max-width: 63.99rem) { .fui-responsive--stack-below-lg > .fui-responsive__desktop { display: none !important; } }
`
}
