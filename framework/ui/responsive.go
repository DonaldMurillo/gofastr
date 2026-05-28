package ui

// Responsive — viewport-swap primitive. Renders BOTH a desktop and a
// mobile variant of a region, then hides one via the registered
// stylesheet's @media query. Use it when a CSS-only collapse of the
// desktop tree produces poor mobile UX (think: a multi-level sidebar
// that has no good "stack vertically" form, or a complex toolbar
// that's much better as a single picker on small screens).
//
// Why both render in SSR (not a runtime swap):
//   - Same HTML lands on every viewport — no FOUC, no JS dependency
//   - Search engines + AT only walk the visible branch (display:none
//     removes a subtree from the accessibility tree and tab order)
//   - SPA navigation doesn't have to re-fetch a separate "mobile page"
//
// Trade-off: a duplicate subtree in the markup. Worth it when the two
// variants render genuinely different elements (a vertical nestedlist
// vs. a <select> jump menu). NOT worth it when CSS alone could
// reflow the desktop tree (use plain @media for that case).
//
//	ui.Responsive(ui.ResponsiveConfig{Breakpoint: 1024},
//	    desktopSidebar,   // shown when viewport >= 1024
//	    mobilePicker)     // shown when viewport < 1024
//
// The primitive wraps each variant in `<div data-fui-viewport="…">`
// and registers a stylesheet that toggles their display: above the
// breakpoint the desktop variant shows, below it the mobile variant.

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ResponsiveConfig configures the swap.
type ResponsiveConfig struct {
	// Breakpoint in pixels — viewport >= Breakpoint renders the
	// desktop variant; < Breakpoint renders the mobile variant.
	// Defaults to 1024 when zero.
	Breakpoint int

	// Class is appended to the wrapping <div>'s class list.
	Class string
}

// Responsive emits both variants wrapped in viewport-toggled divs.
func Responsive(cfg ResponsiveConfig, desktop, mobile render.HTML) render.HTML {
	bp := cfg.Breakpoint
	if bp <= 0 {
		bp = 1024
	}
	// Register a per-breakpoint stylesheet so multiple Responsive
	// instances on the same page that pick the same breakpoint share
	// one bundled CSS asset.
	style := getOrRegisterResponsiveStyle(bp)

	cls := "ui-responsive"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return style.WrapHTML(html.Div(html.DivConfig{Class: cls},
		html.Div(html.DivConfig{
			Class:      "ui-responsive__desktop",
			ExtraAttrs: html.Attrs{"data-fui-viewport": "desktop"},
		}, desktop),
		html.Div(html.DivConfig{
			Class:      "ui-responsive__mobile",
			ExtraAttrs: html.Attrs{"data-fui-viewport": "mobile"},
		}, mobile),
	))
}

// responsiveStyleCache holds one registered Style per breakpoint. The
// registry dedupes per Name, so registering "ui-responsive-1024" twice
// returns the same handle and emits one CSS rule in the bundle.
var responsiveStyleCache = make(map[int]*registry.Style)

func getOrRegisterResponsiveStyle(bp int) *registry.Style {
	if s, ok := responsiveStyleCache[bp]; ok {
		return s
	}
	name := "ui-responsive-" + strconv.Itoa(bp)
	bpCopy := bp // capture for closure
	s := registry.RegisterStyle(name, func(_ style.Theme) string {
		return responsiveCSS(name, bpCopy)
	})
	responsiveStyleCache[bp] = s
	return s
}

// responsiveCSS generates the breakpoint-specific show/hide rules.
// Two queries (above and below the breakpoint) so neither variant
// flashes on initial paint before media queries evaluate.
func responsiveCSS(name string, bp int) string {
	bps := strconv.Itoa(bp)
	// Use the registered Name as a data-fui-comp scope so multiple
	// breakpoints don't collide on .ui-responsive__desktop class.
	scope := `[data-fui-comp="` + name + `"]`
	return `` +
		`@media (min-width: ` + bps + `px) {` +
		`  ` + scope + ` .ui-responsive__mobile { display: none !important; }` +
		`}` +
		`@media (max-width: ` + strconv.Itoa(bp-1) + `px) {` +
		`  ` + scope + ` .ui-responsive__desktop { display: none !important; }` +
		`}`
}
