package gallery

import "github.com/DonaldMurillo/gofastr/core-ui/style"

// ContributeCSS adds the layout classes the catalog's own Demo closures emit.
//
// These are the gallery's contract with itself: a demo that shows three button
// variants wraps them in `.demo-row`, and one that stacks examples uses
// `.demo-stack`. The markup lives here, so the rules that make it lay out have
// to live here too. They were previously written out by hand in
// examples/site/styles_pages.go, which meant the docs site owned CSS for markup
// it does not emit — and the moment a second consumer appeared (the theme
// configurator) the rules were duplicated rather than shared.
//
// This is layout only: flex direction and gap, every value a theme token. No
// colour, no border, no component styling — a demo's appearance comes from the
// components it renders, which is the whole point of a gallery.
func ContributeCSS(ss *style.StyleSheet) {
	ss.Rule(".demo-row").
		Set("display", "flex",
			"flex-wrap", "wrap",
			"gap", "var(--spacing-md)",
			"align-items", "center").End()
	ss.Rule(".demo-stack").
		Set("display", "flex",
			"flex-direction", "column",
			"gap", "var(--spacing-md)").End()
	ss.Rule(".demo-stack-lg").
		Set("display", "flex",
			"flex-direction", "column",
			"gap", "var(--spacing-xl)").End()
	// A few components are viewport-height by design (Workbench). Inside a
	// catalog card that would eat the page, so the demo gets a bounded box to
	// live in. Height only — the component still owns everything about how it
	// looks.
	ss.Rule(".demo-viewport").
		Set("block-size", "320px",
			"overflow", "hidden").End()
}

// BaseCSS renders [ContributeCSS] against a theme. For consumers that need a
// CSS string rather than a StyleSheet to build into — uihost.WithCustomCSS,
// for instance.
func BaseCSS(t style.Theme) string {
	ss := style.NewStyleSheet(t)
	ContributeCSS(ss)
	return ss.CSS()
}
