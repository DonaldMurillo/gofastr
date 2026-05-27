package main

// =============================================================================
// Site stylesheet, built via the typed style.StyleSheet DSL so every value
// flows through theme-token resolution. The previous draft of this file was
// a raw CSS string — corrected here so:
//
//   - {colors.primary}, {spacing.md}, {radii.md} etc. resolve to var(--…)
//     names from the typed Theme rather than being inline literals
//   - extra page-local tokens (line-faint, accent-2, the syntax-highlight
//     palette, the higher spacing steps that don't fit XS..XXXL, the v2
//     font-size scale) are declared once at :root and re-used everywhere
//   - the typed pipeline catches typos (Set odd-count panics) and gives
//     the catalog endpoint visibility into what's defined
//
// The CSS produced still matches pages/home-v2.html in the design bundle
// 1:1 — the difference is HOW it's built, not what it renders.
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// createStyleSheet returns the full v2 site CSS. The caller passes the
// theme produced by createTheme so the StyleSheet inherits the typed token
// dictionary used by token-substitution like {colors.surface}.
func createStyleSheet(t style.Theme) string {
	ss := style.NewStyleSheet(t)

	rootTokens(ss)
	resetAndType(ss)
	tagsAndButtons(ss)
	siteNav(ss)
	siteFooter(ss)
	codeBlockStyles(ss)
	sectionFraming(ss)
	heroLayout(ss)
	generatesLayout(ss)
	architectureLayout(ss)
	agentsLayout(ss)
	examplesLayout(ss)
	alphaLayout(ss)
	pageGetStarted(ss)
	pageConceptsIndex(ss)
	pageConceptsDoc(ss)
	pageExamples(ss)
	pageKiln(ss)
	pagePhilosophy(ss)
	pageNotFound(ss)
	responsive(ss)

	return ss.CSS()
}

// -----------------------------------------------------------------------------
// :root — extra v2 tokens that don't map to canonical theme slots.
// Typed slots (colors.primary, spacing.md, etc.) are written for free by the
// framework's app.css emit; we only add the deltas.
// -----------------------------------------------------------------------------

func rootTokens(ss *style.StyleSheet) {
	ss.Rule(":root").
		Set(
			// Surface ladder — bg-4 (hover) has no typed slot.
			"--bg-4", "oklch(0.255 0.008 75)",

			// Lines — faint hairline + the framework strong border are
			// elsewhere; we add the muted hairline used between gen-rows.
			"--line-faint", "oklch(0.22 0.005 75)",

			// Text — fg-4 is the faintest "footnote" shade, no typed slot.
			"--fg-4", "oklch(0.42 0.010 65)",

			// Accent variants — hover (-2) and low-emphasis backgrounds.
			"--accent-2", "oklch(0.74 0.165 75)",
			"--accent-dim", "oklch(0.42 0.110 75)",

			// Restrained syntax palette (no rainbow). Used by the hero
			// code block's hand-tokenized spans. If we lift the highlight
			// into framework/ui/CodeBlock, these become the canonical
			// tk-* tokens on the typed Theme.
			"--tk-kw", "oklch(0.78 0.07 230)",
			"--tk-fn", "oklch(0.85 0.12 78)",
			"--tk-str", "oklch(0.74 0.10 145)",
			"--tk-num", "oklch(0.78 0.09 30)",
			"--tk-com", "oklch(0.48 0.008 75)",
			"--tk-pn", "oklch(0.65 0.008 75)",
			"--tk-type", "oklch(0.84 0.06 220)",

			// Spacing steps above XXXL (the typed scale stops at 48). The
			// home page needs 64/96 for section padding and the hero gap;
			// 128 is here for the larger gallery pages we'll add later.
			"--s-8", "64px",
			"--s-9", "96px",
			"--s-10", "128px",

			// Font-size scale — v2 uses a custom ramp. The framework's
			// Typography set has different names (Sm/Md/Lg/…); we expose
			// the v2 ramp directly so the design's clamp() ladders read
			// the same as the prototype.
			"--t-xs", "12px",
			"--t-sm", "13px",
			"--t-md", "15px",
			"--t-lg", "17px",
			"--t-xl", "22px",
			"--t-2xl", "28px",
			"--t-3xl", "36px",
			"--t-4xl", "52px",
			"--t-5xl", "72px",

			// Layout caps.
			"--col-max", "1240px",
			"--nav-h", "60px",
		).End()
}

// -----------------------------------------------------------------------------
// Reset + type. Body owns the v2 surface; the framework also writes body
// styles, so we set the same properties later in the cascade.
// -----------------------------------------------------------------------------

func resetAndType(ss *style.StyleSheet) {
	ss.Rule("*, *::before, *::after").Set("box-sizing", "border-box").End()
	ss.Rule("html, body, p, h1, h2, h3, h4, h5, h6, figure, blockquote, dl, dd, ul, ol").
		Set("margin", "0", "padding", "0").End()
	ss.Rule("ul, ol").Set("list-style", "none").End()
	ss.Rule("img, svg, video").Set("display", "block", "max-width", "100%").End()
	ss.Rule("button, input, select, textarea").
		Set("font", "inherit", "color", "inherit", "background", "none", "border", "0").End()
	ss.Rule("button").Set("cursor", "pointer").End()
	ss.Rule("a").Set("color", "inherit", "text-decoration", "none").End()

	ss.Rule("html").
		Set("-webkit-font-smoothing", "antialiased",
			"-moz-osx-font-smoothing", "grayscale").End()

	// Body — every value sourced from the typed theme. The framework's
	// default body rule sets font-family from its own token; we re-set
	// to win the cascade (last rule wins on equal specificity).
	ss.Rule("body").
		Set("font-family", "{fonts.body}",
			"font-size", "var(--t-md)",
			"line-height", "1.55",
			"color", "{colors.text}",
			"background", "{colors.background}").End()

	ss.Rule("::selection").
		Set("background", "var(--accent-dim)", "color", "{colors.text}").End()

	// Headings — semibold, tight tracking, sourced from the heading font.
	ss.Rule("h1, h2, h3, h4, h5").
		Set("font-family", "{fonts.heading}",
			"font-weight", "500",
			"letter-spacing", "-0.02em",
			"line-height", "1.1").End()
	ss.Rule("h1").Set("font-size", "var(--t-5xl)").End()
	ss.Rule("h2").Set("font-size", "var(--t-4xl)").End()
	ss.Rule("h3").Set("font-size", "var(--t-2xl)").End()
	ss.Rule("h4").Set("font-size", "var(--t-xl)").End()
	ss.Rule("h5").Set("font-size", "var(--t-lg)").End()
	ss.Rule("p").Set("line-height", "1.65", "color", "{colors.text-muted}").End()

	ss.Rule("code, kbd, samp").
		Set("font-family", "{fonts.mono}", "font-size", "0.9em").End()
	ss.Rule(":not(pre) > code").
		Set("background", "{colors.surface-soft}",
			"border", "1px solid var(--line-faint)",
			"border-radius", "{radii.sm}",
			"padding", "1px 6px",
			"font-size", "0.86em",
			"color", "{colors.text}").End()

	// Links pick up the primary token by default — page-local sections
	// can override (e.g. nav links are muted).
	ss.Rule("a").
		Set("color", "{colors.primary}").
		Transition("opacity {durations.fast} {easings.ease-out}").End()
	ss.Rule("a:hover").Set("opacity", "0.78").End()

	// Prose links get an underline accent so they stand apart in running text.
	ss.Rule(".prose a").
		Set("text-decoration", "underline",
			"text-decoration-color", "var(--accent-dim)",
			"text-underline-offset", "3px").End()
	ss.Rule(".prose a:hover").
		Set("text-decoration-color", "{colors.primary}", "opacity", "1").End()

	// Layout helpers used inline.
	ss.Rule(".container-site").
		Set("max-width", "var(--col-max)",
			"margin", "0 auto",
			"padding", "0 {spacing.xxl}").End()
	ss.Rule(".muted").Set("color", "{colors.text-subtle}").End()
	ss.Rule(".faint").Set("color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// .tag (small mono label, not uppercase) and .btn (primary/ghost/lg).
// -----------------------------------------------------------------------------

func tagsAndButtons(ss *style.StyleSheet) {
	ss.Rule(".tag").
		Set("display", "inline-flex",
			"align-items", "center",
			"gap", "6px",
			"padding", "3px 10px",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-muted}",
			"background", "{colors.surface}",
			"border", "1px solid var(--line-faint)",
			"border-radius", "{radii.full}",
			"white-space", "nowrap").End()
	ss.Rule(".tag.accent").
		Set("color", "{colors.primary}",
			"border-color", "var(--accent-dim)",
			"background", "color-mix(in oklch, {colors.primary} 8%, {colors.surface})").End()
	ss.Rule(".tag .dot").
		Set("width", "6px", "height", "6px",
			"border-radius", "999px",
			"background", "{colors.text-subtle}").End()
	ss.Rule(".tag.accent .dot").
		Set("background", "{colors.primary}",
			"box-shadow", "0 0 6px {colors.primary}").End()

	// Spacing utilities used in place of inline style="margin-bottom:…"
	// attributes — strict CSP blocks inline styles, so every spacing tweak
	// has to live in a class. Add more here if you need another step.
	ss.Rule(".mb-md").Set("margin-bottom", "{spacing.md}").End()
	ss.Rule(".mb-lg").Set("margin-bottom", "{spacing.lg}").End()

	// .code__head .alive — the green status dot in the code-block chrome.
	// Was inline style on a <span>; class-ified for CSP compliance.
	ss.Rule(".code__head .alive").
		Set("width", "6px", "height", "6px",
			"border-radius", "999px",
			"background", "var(--tk-str)",
			"display", "inline-block").End()

	// Buttons — primary fills with amber, ghost is a transparent outline.
	ss.Rule(".btn").
		Set("display", "inline-flex",
			"align-items", "center",
			"gap", "8px",
			"padding", "9px 14px",
			"border-radius", "{radii.md}",
			"font-size", "var(--t-sm)",
			"font-weight", "500",
			"letter-spacing", "-0.005em",
			"border", "1px solid transparent",
			"white-space", "nowrap").
		Transition("background {durations.fast} {easings.ease-out}, border-color {durations.fast} {easings.ease-out}, opacity {durations.fast} {easings.ease-out}").End()
	ss.Rule(".btn--primary").
		Set("background", "{colors.primary}",
			"color", "{colors.primary-fg}",
			"border-color", "{colors.primary}").End()
	ss.Rule(".btn--primary:hover").
		Set("background", "var(--accent-2)",
			"border-color", "var(--accent-2)",
			"opacity", "1").End()
	ss.Rule(".btn--ghost").
		Set("color", "{colors.text}",
			"border-color", "{colors.border}",
			"background", "transparent").End()
	ss.Rule(".btn--ghost:hover").
		Set("background", "{colors.surface-soft}",
			"border-color", "{colors.border-strong}",
			"opacity", "1").End()
	ss.Rule(".btn--lg").Set("padding", "11px 18px", "font-size", "var(--t-md)").End()

	// Focus ring — visible for keyboard nav, suppressed for pointer.
	ss.Rule(":focus-visible").
		Set("outline", "2px solid {colors.primary}",
			"outline-offset", "2px",
			"border-radius", "{radii.sm}").End()
}

// -----------------------------------------------------------------------------
// .nav — sticky top bar. Layout-only styling; structure is in layout.go.
// -----------------------------------------------------------------------------

func siteNav(ss *style.StyleSheet) {
	ss.Rule(".nav").
		Set("position", "sticky", "top", "0", "z-index", "{z-index.sticky}",
			"height", "var(--nav-h)",
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.xxl}",
			"padding", "0 {spacing.xxl}",
			"background", "color-mix(in oklch, {colors.background} 88%, transparent)",
			"backdrop-filter", "blur(12px)",
			"-webkit-backdrop-filter", "blur(12px)",
			"border-bottom", "1px solid var(--line-faint)",
			"font-size", "var(--t-sm)").End()

	ss.Rule(".nav__brand").
		Set("display", "flex", "align-items", "baseline", "gap", "8px",
			"color", "{colors.text}",
			"font-weight", "500",
			"font-size", "15px",
			"letter-spacing", "-0.01em").End()
	ss.Rule(".nav__brand .mark").
		Set("display", "inline-block",
			"width", "9px", "height", "9px",
			"border-radius", "2px",
			"background", "{colors.primary}",
			"margin-right", "2px",
			"transform", "translateY(1px)").End()
	ss.Rule(".nav__brand .ver").
		Set("color", "{colors.text-subtle}",
			"font-size", "12px",
			"font-family", "{fonts.mono}",
			"font-weight", "400").End()

	ss.Rule(".nav__links").
		Set("display", "flex", "gap", "{spacing.xl}", "margin-left", "{spacing.xl}").End()
	ss.Rule(".nav__links a").
		Set("color", "{colors.text-muted}",
			"font-weight", "400",
			"white-space", "nowrap").End()
	ss.Rule(".nav__links a:hover").Set("color", "{colors.text}", "opacity", "1").End()
	ss.Rule(`.nav__links a[aria-current="page"]`).Set("color", "{colors.text}").End()

	ss.Rule(".nav__right").
		Set("margin-left", "auto",
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.md}").End()
	ss.Rule(".nav__cmd").
		Set("display", "inline-flex", "align-items", "center", "gap", "6px",
			"padding", "5px 10px",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"color", "{colors.text-subtle}",
			"font-size", "var(--t-xs)",
			"font-family", "{fonts.mono}",
			"background", "{colors.surface}").End()
	ss.Rule(".nav__cmd kbd").
		Set("font-size", "10px",
			"padding", "1px 5px",
			"border", "1px solid {colors.border}",
			"border-radius", "3px",
			"background", "{colors.surface-soft}",
			"color", "{colors.text-muted}").End()
	ss.Rule(".nav__icon").
		Set("width", "30px", "height", "30px",
			"display", "grid", "place-items", "center",
			"border-radius", "{radii.md}",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".nav__icon:hover").
		Set("background", "{colors.surface-soft}", "color", "{colors.text}", "opacity", "1").End()
}

// -----------------------------------------------------------------------------
// .foot — 5-col footer + bottom strip.
// -----------------------------------------------------------------------------

func siteFooter(ss *style.StyleSheet) {
	ss.Rule(".foot").
		Set("padding", "var(--s-8) 0 {spacing.xxl}",
			"border-top", "1px solid {colors.border}").End()
	ss.Rule(".foot__grid").
		Set("display", "grid",
			"grid-template-columns", "1.4fr 1fr 1fr 1fr 1fr",
			"gap", "var(--s-8)",
			"max-width", "var(--col-max)",
			"margin", "0 auto",
			"padding", "0 {spacing.xxl}").End()

	ss.Rule(".foot__brand").
		Set("display", "flex", "align-items", "baseline", "gap", "8px",
			"margin-bottom", "{spacing.md}",
			"color", "{colors.text}", "font-weight", "500").End()
	ss.Rule(".foot__brand .mark").
		Set("display", "inline-block",
			"width", "9px", "height", "9px",
			"border-radius", "2px",
			"background", "{colors.primary}",
			"transform", "translateY(1px)").End()
	ss.Rule(".foot__brand .ver").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"font-weight", "400").End()
	ss.Rule(".foot__copy").
		Set("font-size", "var(--t-sm)",
			"color", "{colors.text-subtle}",
			"line-height", "1.6",
			"max-width", "30ch").End()
	ss.Rule(".foot h6").
		Set("font-family", "{fonts.body}",
			"font-weight", "500",
			"font-size", "13px",
			"color", "{colors.text}",
			"margin-bottom", "{spacing.md}",
			"letter-spacing", "-0.005em",
			"text-transform", "none").End()
	ss.Rule(".foot ul li").Set("padding", "3px 0").End()
	ss.Rule(".foot ul li a").
		Set("color", "{colors.text-subtle}", "font-size", "var(--t-sm)").End()
	ss.Rule(".foot ul li a:hover").Set("color", "{colors.text}", "opacity", "1").End()
	ss.Rule(".foot__bottom").
		Set("display", "flex",
			"justify-content", "space-between",
			"padding-top", "{spacing.xl}",
			"margin-top", "var(--s-8)",
			"border-top", "1px solid var(--line-faint)",
			"color", "var(--fg-4)",
			"font-size", "12px",
			"font-family", "{fonts.mono}",
			"max-width", "var(--col-max)",
			"margin-left", "auto",
			"margin-right", "auto",
			"padding-left", "{spacing.xxl}",
			"padding-right", "{spacing.xxl}").End()
}

// -----------------------------------------------------------------------------
// .code — chrome around the hand-tokenized Go highlighter in code_block.go.
// The framework's ui.CodeBlock gives us an unhighlighted version; we need the
// colored token spans for the hero so this stays hand-rolled. Lift into
// framework/ui/CodeBlock the first time another page needs it.
// -----------------------------------------------------------------------------

func codeBlockStyles(ss *style.StyleSheet) {
	ss.Rule(".code").
		Set("font-family", "{fonts.mono}",
			"font-size", "var(--t-sm)",
			"line-height", "1.65",
			"background", "{colors.code-surface}",
			"border", "1px solid {colors.code-border}",
			"border-radius", "{radii.lg}",
			"overflow", "hidden").End()
	ss.Rule(".code__head").
		Set("display", "flex", "align-items", "center", "gap", "10px",
			"padding", "8px 14px",
			"background", "{colors.surface}",
			"border-bottom", "1px solid {colors.code-border}",
			"font-family", "{fonts.mono}",
			"font-size", "12px",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".code__head .file").Set("color", "{colors.text}").End()
	ss.Rule(".code__head .right").
		Set("margin-left", "auto",
			"display", "flex",
			"gap", "10px",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".code__head .copy").
		Set("color", "{colors.text-subtle}", "cursor", "pointer").
		Transition("color {durations.fast}").End()
	ss.Rule(".code__head .copy:hover").Set("color", "{colors.text}").End()

	// IMPORTANT: display:block on each .ln so syntax-token spans flow as one
	// line. The prototype's first attempt used display:grid and broke layout.
	ss.Rule(".code__body").
		Set("padding", "14px 18px 14px 52px",
			"color", "{colors.code-text}",
			"white-space", "pre",
			"overflow-x", "auto",
			"counter-reset", "ln",
			"position", "relative").End()
	ss.Rule(".code__body .ln").Set("display", "block", "position", "relative").End()
	ss.Rule(".code__body .ln::before").
		Set("counter-increment", "ln",
			"content", "counter(ln)",
			"position", "absolute",
			"left", "-36px", "top", "0",
			"width", "28px",
			"text-align", "right",
			"color", "{colors.text-subtle}",
			"font-size", "11px",
			"user-select", "none").End()

	// Syntax tokens — color only, no italic except comments.
	ss.Rule(".tk-kw").Set("color", "var(--tk-kw)").End()
	ss.Rule(".tk-fn").Set("color", "var(--tk-fn)").End()
	ss.Rule(".tk-str").Set("color", "var(--tk-str)").End()
	ss.Rule(".tk-num").Set("color", "var(--tk-num)").End()
	ss.Rule(".tk-com").Set("color", "var(--tk-com)", "font-style", "italic").End()
	ss.Rule(".tk-pn").Set("color", "var(--tk-pn)").End()
	ss.Rule(".tk-type").Set("color", "var(--tk-type)").End()
}

// -----------------------------------------------------------------------------
// .section — vertical rhythm, the right-aligned section number, and the
// two-col head (h2 left, lede right).
// -----------------------------------------------------------------------------

func sectionFraming(ss *style.StyleSheet) {
	ss.Rule(".section-v2").
		Set("padding", "var(--s-9) 0",
			"border-bottom", "1px solid var(--line-faint)",
			"position", "relative").End()
	ss.Rule(".section__head").
		Set("display", "grid",
			"grid-template-columns", "minmax(0, 1fr) 360px",
			"gap", "{spacing.xxxl}",
			"margin-bottom", "{spacing.xxl}",
			"align-items", "end").End()
	ss.Rule(".section__head h2").
		Set("font-size", "clamp(var(--t-3xl), 4vw, var(--t-4xl))",
			"max-width", "18ch",
			"letter-spacing", "-0.025em").End()
	ss.Rule(".section__head p").
		Set("color", "{colors.text-muted}",
			"font-size", "var(--t-md)",
			"max-width", "50ch").End()
	ss.Rule(".section__num").
		Set("position", "absolute",
			"top", "{spacing.xxxl}",
			"right", "{spacing.xxl}",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// .hero — two-col grid with the headline left, the code block right.
// -----------------------------------------------------------------------------

func heroLayout(ss *style.StyleSheet) {
	ss.Rule(".hero").
		Set("padding", "var(--s-9) 0 var(--s-8)",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".hero__grid").
		Set("display", "grid",
			"grid-template-columns", "minmax(0, 1fr) minmax(0, 1.2fr)",
			"gap", "{spacing.xxxl}",
			"align-items", "start").End()
	ss.Rule(".hero__copy").Set("padding-top", "{spacing.md}").End()
	ss.Rule(".hero__title").
		Set("font-size", "clamp(40px, 5.4vw, 72px)",
			"line-height", "1.02",
			"letter-spacing", "-0.035em",
			"margin-bottom", "{spacing.xl}",
			"max-width", "14ch",
			"text-wrap", "balance").End()
	ss.Rule(".hero__title .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".hero__lede").
		Set("color", "{colors.text-muted}",
			"font-size", "var(--t-lg)",
			"line-height", "1.55",
			"max-width", "44ch",
			"margin-bottom", "{spacing.lg}").End()
	ss.Rule(".hero__lede + .hero__lede").Set("margin-bottom", "{spacing.xxl}").End()
	ss.Rule(".hero__lede strong").Set("color", "{colors.text}", "font-weight", "500").End()
	ss.Rule(".hero__ctas").
		Set("display", "flex", "gap", "{spacing.md}", "align-items", "center").End()
	ss.Rule(".hero__install").
		Set("margin-top", "{spacing.xl}",
			"padding", "8px 12px",
			"border", "1px dashed {colors.border}",
			"border-radius", "{radii.md}",
			"font-family", "{fonts.mono}",
			"font-size", "12px",
			"color", "{colors.text-muted}",
			"display", "inline-flex",
			"align-items", "center",
			"gap", "8px",
			"background", "transparent",
			"max-width", "100%",
			"overflow-x", "auto",
			"white-space", "nowrap").End()
	ss.Rule(".hero__install .p").Set("color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// .gen-list — release-notes-style row table.
// -----------------------------------------------------------------------------

func generatesLayout(ss *style.StyleSheet) {
	ss.Rule(".gen-list").
		Set("display", "grid", "grid-template-columns", "1fr",
			"border-top", "1px solid {colors.border}").End()
	ss.Rule(".gen-row").
		Set("display", "grid",
			"grid-template-columns", "32px 200px minmax(0, 1fr) 160px",
			"gap", "{spacing.xl}",
			"padding", "{spacing.lg} 0",
			"border-bottom", "1px solid var(--line-faint)",
			"align-items", "baseline").End()
	ss.Rule(".gen-row:hover").
		Set("background", "color-mix(in oklch, {colors.surface} 50%, transparent)").End()
	ss.Rule(".gen-row .n").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "var(--fg-4)").End()
	ss.Rule(".gen-row .name").
		Set("font-family", "{fonts.body}",
			"font-size", "var(--t-md)",
			"font-weight", "500",
			"color", "{colors.text}",
			"letter-spacing", "-0.01em").End()
	ss.Rule(".gen-row .desc").
		Set("color", "{colors.text-muted}", "font-size", "var(--t-sm)", "line-height", "1.55").End()
	ss.Rule(".gen-row .file").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"text-align", "right").End()
	ss.Rule(".gen-row .file code").
		Set("background", "transparent",
			"border", "0",
			"padding", "0",
			"color", "{colors.text-subtle}",
			"font-size", "11px").End()
}

// -----------------------------------------------------------------------------
// .arch__grid — four columns of arch-cards, becomes 2-col at mobile breakpoint.
// -----------------------------------------------------------------------------

func architectureLayout(ss *style.StyleSheet) {
	ss.Rule(".arch__grid").
		Set("display", "grid",
			"grid-template-columns", "repeat(4, 1fr)",
			"gap", "{spacing.lg}").End()
	ss.Rule(".arch-card").
		Set("padding", "{spacing.xl}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}",
			"display", "flex",
			"flex-direction", "column",
			"min-height", "220px").End()
	ss.Rule(".arch-card h4").
		Set("font-size", "var(--t-lg)",
			"font-weight", "500",
			"margin-bottom", "6px",
			"letter-spacing", "-0.01em").End()
	ss.Rule(".arch-card .pkg").
		Set("font-family", "{fonts.mono}",
			"font-size", "12px",
			"color", "{colors.primary}",
			"margin-bottom", "{spacing.md}").End()
	ss.Rule(".arch-card p").
		Set("font-size", "var(--t-sm)",
			"color", "{colors.text-muted}",
			"line-height", "1.55",
			"margin-bottom", "{spacing.md}").End()
	ss.Rule(".arch-card .members").
		Set("margin-top", "auto",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"line-height", "1.7").End()
	ss.Rule(".arch-card .members b").
		Set("color", "{colors.text-muted}", "font-weight", "400").End()
}

// -----------------------------------------------------------------------------
// .agents__split — two panes, framework left, Kiln right + terminal mock.
// -----------------------------------------------------------------------------

func agentsLayout(ss *style.StyleSheet) {
	ss.Rule(".agents__split").
		Set("display", "grid",
			"grid-template-columns", "minmax(0, 1fr) minmax(0, 1fr)",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}",
			"overflow", "hidden",
			"background", "{colors.surface}").End()
	ss.Rule(".pane").Set("padding", "{spacing.xl}").End()
	ss.Rule(".pane--left").Set("border-right", "1px solid {colors.border}").End()
	ss.Rule(".pane--right").Set("background", "{colors.surface-soft}").End()
	ss.Rule(".pane__lbl").
		Set("display", "inline-flex", "align-items", "center", "gap", "6px",
			"font-family", "{fonts.mono}", "font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.lg}").End()
	ss.Rule(".pane__lbl::before").
		Set("content", `""`,
			"width", "6px", "height", "6px",
			"border-radius", "999px",
			"background", "{colors.primary}").End()
	ss.Rule(".pane h4").Set("margin-bottom", "{spacing.md}").End()
	ss.Rule(".pane p").
		Set("font-size", "var(--t-sm)",
			"color", "{colors.text-muted}",
			"line-height", "1.6",
			"margin-bottom", "{spacing.md}").End()
	ss.Rule(".pane ul").Set("display", "grid", "gap", "8px").End()
	ss.Rule(".pane ul li").
		Set("font-size", "var(--t-sm)",
			"color", "{colors.text}",
			"padding-left", "16px",
			"position", "relative",
			"line-height", "1.5").End()
	ss.Rule(".pane ul li::before").
		Set("content", `"•"`,
			"position", "absolute",
			"left", "2px",
			"color", "{colors.primary}").End()

	// Terminal mock — replicated in agents pane.
	ss.Rule(".term").
		Set("border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"background", "{colors.background}",
			"overflow", "hidden",
			"font-family", "{fonts.mono}",
			"font-size", "12px",
			"margin-top", "{spacing.md}").End()
	ss.Rule(".term__head").
		Set("padding", "6px 12px",
			"border-bottom", "1px solid var(--line-faint)",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"display", "flex",
			"align-items", "center",
			"gap", "8px").End()
	ss.Rule(".term__head .dot").
		Set("width", "6px", "height", "6px",
			"border-radius", "999px",
			"background", "{colors.primary}",
			"box-shadow", "0 0 6px {colors.primary}").End()
	ss.Rule(".term__body").
		Set("padding", "10px 12px",
			"line-height", "1.7",
			"color", "{colors.text}",
			"white-space", "pre-wrap").End()
	ss.Rule(".term__body .o").Set("color", "{colors.text-subtle}").End()
	ss.Rule(".term__body .ok").Set("color", "var(--tk-str)").End()
}

// -----------------------------------------------------------------------------
// .ex__grid — three-col gallery of example cards.
// -----------------------------------------------------------------------------

func examplesLayout(ss *style.StyleSheet) {
	ss.Rule(".ex__grid").
		Set("display", "grid",
			"grid-template-columns", "repeat(3, 1fr)",
			"gap", "{spacing.lg}").End()
	ss.Rule(".ex-card").
		Set("display", "flex", "flex-direction", "column", "gap", "{spacing.md}",
			"padding", "{spacing.xl}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}").
		Transition("border-color {durations.fast}").End()
	ss.Rule(".ex-card:hover").Set("border-color", "{colors.border-strong}").End()
	ss.Rule(".ex-card .path").
		Set("font-family", "{fonts.mono}", "font-size", "12px", "color", "{colors.primary}").End()
	ss.Rule(".ex-card h4").
		Set("font-size", "var(--t-lg)", "font-weight", "500", "letter-spacing", "-0.01em").End()
	ss.Rule(".ex-card p").
		Set("font-size", "var(--t-sm)", "color", "{colors.text-muted}", "line-height", "1.55").End()
	ss.Rule(".ex-card .cmd").
		Set("margin-top", "auto",
			"padding", "6px 10px",
			"background", "{colors.surface-soft}",
			"border", "1px solid var(--line-faint)",
			"border-radius", "{radii.sm}",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text}").End()
	ss.Rule(".ex-card .cmd::before").Set("content", `"$ "`, "color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// .alpha__grid — two-col "state of the project" layout.
// -----------------------------------------------------------------------------

func alphaLayout(ss *style.StyleSheet) {
	ss.Rule(".alpha__grid").
		Set("display", "grid",
			"grid-template-columns", "minmax(0, 1.1fr) minmax(0, 1.4fr)",
			"gap", "{spacing.xxxl}").End()
	ss.Rule(".alpha__copy h2").
		Set("font-size", "var(--t-3xl)",
			"margin-bottom", "{spacing.lg}",
			"letter-spacing", "-0.025em",
			"max-width", "14ch").End()
	ss.Rule(".alpha__copy p").
		Set("color", "{colors.text-muted}", "font-size", "var(--t-md)").End()
	ss.Rule(".alpha__list dt").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.primary}",
			"margin-top", "{spacing.lg}",
			"padding-bottom", "4px",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".alpha__list dt:first-child").Set("margin-top", "0").End()
	ss.Rule(".alpha__list dd").
		Set("color", "{colors.text}",
			"padding", "8px 0",
			"font-size", "var(--t-sm)",
			"line-height", "1.5").End()
	ss.Rule(".alpha__list dd .when").
		Set("color", "{colors.text-subtle}",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"margin-left", "6px").End()
}

// -----------------------------------------------------------------------------
// Responsive — 980px breakpoint collapses every grid to single column.
// -----------------------------------------------------------------------------

func responsive(ss *style.StyleSheet) {
	ss.Media("(max-width: 980px)", func(inner *style.StyleSheet) {
		inner.Rule(".hero__grid").Set("grid-template-columns", "1fr").End()
		inner.Rule(".section__head").
			Set("grid-template-columns", "1fr", "gap", "{spacing.md}").End()
		inner.Rule(".gen-row").Set("grid-template-columns", "24px 1fr").End()
		inner.Rule(".gen-row .desc, .gen-row .file").Set("grid-column", "2").End()
		inner.Rule(".arch__grid").Set("grid-template-columns", "1fr 1fr").End()
		inner.Rule(".ex__grid").Set("grid-template-columns", "1fr").End()
		inner.Rule(".agents__split").Set("grid-template-columns", "1fr").End()
		inner.Rule(".pane--left").
			Set("border-right", "0", "border-bottom", "1px solid {colors.border}").End()
		inner.Rule(".alpha__grid").Set("grid-template-columns", "1fr").End()
		inner.Rule(".foot__grid").Set("grid-template-columns", "1fr 1fr").End()
		inner.Rule(".nav__links").Set("display", "none").End()
	})
}
