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
	pageComponents(ss)
	demoModalStyles(ss)
	responsive(ss)

	// Fan in any co-located styles registered via style.Contribute(...) at
	// package-init time. Applied AFTER the host base rules so a package can
	// override base styling by re-declaring the same selector.
	style.Apply(ss)

	return ss.CSS()
}

// demoModalStyles backs the "RPC → Open Widget" demo modal so its markup uses
// class names instead of inline style="…" attrs (strict-CSP strips inline
// styles — see core-ui/check TestLintNoInlineStyles_RepoIsClean).
func demoModalStyles(ss *style.StyleSheet) {
	ss.Rule(".demo-modal-body").
		Set("text-align", "center",
			"padding", "var(--s-8, 32px) 0").End()
	ss.Rule(".demo-modal-emoji").
		Set("font-size", "24px",
			"margin", "0 0 8px").End()
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
			// Bumped 0.42->0.62 so it clears WCAG AA (4.5:1) on the page's
			// near-black surfaces — it backs doc-card meta, the site +
			// section-menu eyebrows, and gen-row numbers, all of which axe
			// flagged at ~2.35:1. 0.62 reads ~4.9:1 on --bg, ~5.5:1 on the
			// darkest hero panel.
			"--fg-4", "oklch(0.62 0.010 65)",

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
			// Comments — bumped 0.48->0.62 so the italic // spans clear AA
			// (4.5:1) on the code-block surface; axe flagged them at 2.7:1.
			"--tk-com", "oklch(0.62 0.008 75)",
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

			// Framework component theming. core-ui/interactive + framework/ui
			// components (Counter, Toggle, Collapsible, Tabs, Dropdown) are
			// styled with --fui-* custom properties and fall back to LIGHT
			// hardcoded colors when a host leaves them unset — which is why
			// the dropdown menu rendered white on this dark theme. Aliasing
			// them to the site's --color-* tokens (which flip with
			// data-color-scheme) themes every framework component correctly
			// in both modes from one place.
			"--fui-surface", "var(--color-surface)",
			"--fui-foreground", "var(--color-text)",
			"--fui-border", "var(--color-border)",
			"--fui-primary", "var(--color-primary)",
			"--fui-muted", "var(--color-text-muted)",
			"--fui-muted-bg", "var(--color-surface-soft)",
			// Several framework components (SegmentedControl track,
			// ShortcutHint key, AvatarGroup overflow chip) read
			// --color-muted as a low-emphasis FILL and fall back to a LIGHT
			// hardcoded grey when a host leaves it unset — which painted a
			// near-white chip on this dark theme (the dark --color-text on
			// top then dropped to ~1:1). Alias it to the soft surface so the
			// fill flips with the scheme and the muted/foreground text on it
			// reads at AA.
			"--color-muted", "var(--color-surface-soft)",

			// Semantic status hues. The framework defaults (Success #15803D,
			// Danger #DC2626, Warning #A16207, Info #2563EB) are tuned to hit
			// AA on a WHITE surface; on this dark-default theme they read
			// ~3.3–4.0:1 as the label colour on their own 15%-tinted dark
			// backgrounds (Badge/Tag/StatCard trend/JSONViewer key+value).
			// Re-tone to brighter oklch variants of the SAME hue so they
			// clear 4.5:1 (~7:1 actual) on the tinted dark chips. The light
			// scheme below restores the original AA-on-white values.
			"--color-success", "oklch(0.74 0.15 150)",
			"--color-danger", "oklch(0.72 0.16 25)",
			"--color-warning", "oklch(0.74 0.13 80)",
			"--color-info", "oklch(0.72 0.13 250)",

			// Layout caps.
			"--col-max", "1240px",
			"--nav-h", "60px",
			// Override framework ui.Container's "wide" cap to match
			// the v2 1240px target. ui.Container reads
			// --ui-container-{narrow,default,wide}; setting wide here
			// lets us use ui.Container(ContainerWide) site-wide
			// instead of a local .container-site helper.
			"--ui-container-wide", "1240px",
		).End()

	// ─── Light theme branch ──────────────────────────────────────────
	// ui.ThemeToggle flips data-color-scheme on <html>. This block
	// inverts the v2 surface ladder + text colors so the same UI
	// works light-mode.
	//
	// Contrast targets (WCAG AA): 4.5:1 for normal text, 3:1 for
	// large/UI. The amber accent must be retoned for text uses —
	// oklch(0.82 0.155 78) reads cleanly on a warm-near-black surface
	// (≈ 9:1) but collapses to ~1.45:1 on a warm-near-white surface.
	// We define a darker amber-text token (`--color-primary-text`)
	// for foreground accents in light mode and keep `--color-primary`
	// as the amber-fill color (CTAs / pulse / pill bg).
	ss.Rule(`:root[data-color-scheme="light"]`).
		Set(
			// Surface ladder — invert oklch L from ~0.135–0.255 to ~0.99–0.93.
			"--color-background", "oklch(0.99 0.004 75)",
			"--color-surface", "oklch(0.97 0.005 75)",
			"--color-surface-soft", "oklch(0.94 0.006 75)",
			"--bg-4", "oklch(0.90 0.007 75)",
			// Borders — softer hairlines on the warm-white surfaces.
			"--color-border", "oklch(0.88 0.008 75)",
			"--color-border-strong", "oklch(0.74 0.010 75)",
			"--line-faint", "oklch(0.92 0.005 75)",
			// Text ladder — warm-near-black at the top, two muted shades.
			// All three pass WCAG AA against the warm-white surfaces:
			//   text        L=0.18 vs bg L=0.99 → ≈ 14:1
			//   text-muted  L=0.36 vs bg L=0.99 → ≈ 7.1:1
			//   text-subtle L=0.44 vs bg L=0.99 → ≈ 5.0:1 (was 0.52 ≈ 3.9:1 — failed)
			"--color-text", "oklch(0.18 0.005 75)",
			"--color-text-muted", "oklch(0.36 0.008 75)",
			"--color-text-subtle", "oklch(0.44 0.010 70)",
			// fg-4 is the faintest body shade — kept at AA-Large floor
			// (was 0.62 ≈ 2.8:1, FAILED even 3:1). 0.50 ≈ 3.9:1 — passes
			// AA-Large; do not use for body copy in this mode.
			"--fg-4", "oklch(0.50 0.010 65)",
			// Code surface — slightly elevated soft-warm panel; text
			// stays near-black for legibility on the lighter chip.
			"--color-code-surface", "oklch(0.95 0.006 75)",
			"--color-code-text", "oklch(0.18 0.005 75)",
			"--color-code-border", "oklch(0.88 0.008 75)",
			// Syntax palette — desaturate/shift L down on light so
			// tokens read at AA against a near-white background.
			//   was tk-fn 0.46 0.18 50 (neon-orange) → retoned olive.
			//   was tk-str 0.44 0.15 145 (neon-green) → forest.
			"--tk-kw", "oklch(0.38 0.13 245)",
			"--tk-fn", "oklch(0.42 0.10 50)",
			"--tk-str", "oklch(0.42 0.10 145)",
			"--tk-num", "oklch(0.44 0.15 30)",
			// Comments — 0.55 read 4.2:1 on the light code surface (axe
			// color-contrast fail on Linux CI, which defaults to the light
			// scheme). 0.50 reads ≈5.2:1 — AA with margin.
			"--tk-com", "oklch(0.50 0.008 75)",
			"--tk-pn", "oklch(0.42 0.008 75)",
			"--tk-type", "oklch(0.38 0.10 220)",
			// Amber accent retoned for light mode. The dark-mode amber
			// (oklch 0.82 0.155 78) reads at ~9:1 on the warm-near-
			// black surface but collapses to ~1.45:1 on warm-near-
			// white, so it's invisible as text and weak as a CTA fill.
			// The accent is used as SMALL text everywhere (brand tag,
			// .pkg/.path mono labels, dt eyebrows, links, card h4s), so
			// AA-Large (3:1) is not enough — every use must clear 4.5:1.
			// The previous oklch(0.62 0.18 60) → #d16400 read 3.2–3.7:1
			// on the warm-white surfaces and failed axe on Linux CI
			// (which defaults to the light scheme). Re-tone to
			// oklch(0.51 0.115 60) — a deeper amber in the same family
			// that is INSIDE the sRGB gamut (no browser gamut-mapping
			// skew) and clears AA with margin: ≈5.8:1 on bg, ≈5.5:1 on
			// surface, ≈4.9:1 on the StatusPill accent chip (8% mix),
			// and ≈5.8:1 for the near-white CTA label on the amber fill.
			"--color-primary", "oklch(0.51 0.115 60)",
			"--color-primary-fg", "oklch(0.99 0.004 75)",
			// Accent mirrors primary on this single-accent site. The
			// typed theme sets it to the DARK amber; without this
			// override .ex-row__src links etc. kept the bright
			// oklch(0.82 0.155 78) in light mode — 1.7:1 on warm-white.
			"--color-accent", "oklch(0.51 0.115 60)",
			// Accent-dim (subtle underlines, low-emphasis bg) needs to
			// stay visible on warm-white. Bump alpha + lower L slightly.
			"--accent-dim", "oklch(0.65 0.12 70)",
			// Restore the framework's light-mode semantic hues. The :root
			// block above re-tones these BRIGHTER for the dark default (so
			// status text reads on dark tinted chips); on the warm-white
			// surfaces those bright variants would fail, so here we put
			// back the framework defaults — which are tuned to clear
			// 4.6:1 as label text on the components' own 15%-tinted
			// chips (Badge/Tag/StatCard/ValidationSummary), the binding
			// constraint axe checks. Keep in sync with
			// core-ui/style.DefaultTheme.
			"--color-success", "#166534",
			"--color-danger", "#B91C1C",
			"--color-warning", "#854D0E",
			"--color-info", "#1D4ED8",
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
	// In-content prose links must be distinguishable without colour (WCAG
	// link-in-text-block / axe). The base rule above strips underlines for
	// nav + card chrome; restore them for links inside body copy.
	ss.Rule(".ph-article p a, .ph-article dd a, .ph-article li a, .doc-page p a, .doc-page li a, .doc-head__lede a, .alpha__list dd a").
		Set("text-decoration", "underline").End()

	ss.Rule("html").
		Set("-webkit-font-smoothing", "antialiased",
			"-moz-osx-font-smoothing", "grayscale",
			// Belt-and-braces: html-level clipping in case any
			// overflow:hidden ancestor in the body chain gets bypassed
			// by a sticky/fixed positioned descendant. Prevents the
			// rubber-band scroll on iOS Safari when content runs wide.
			"overflow-x", "hidden").End()

	// Body — every value sourced from the typed theme. The framework's
	// default body rule sets font-family from its own token; we re-set
	// to win the cascade (last rule wins on equal specificity).
	ss.Rule("body").
		Set("font-family", "{fonts.body}",
			"font-size", "var(--t-md)",
			"line-height", "1.55",
			"color", "{colors.text}",
			"background", "{colors.background}",
			// Safety net: any child that intrinsically wants to be wider
			// than the viewport (code blocks, install lines, embedded
			// terminals) must not extend the document. Each such surface
			// owns its own overflow-x: auto so the user can pan inside
			// it; the page itself stays viewport-bound.
			"overflow-x", "hidden").End()

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

	// Layout helpers used inline. The .container-site class is gone —
	// callers use ui.Container(ContainerWide) and the wide cap is
	// themed via --ui-container-wide in tokens(). The framework
	// component owns its own responsive padding.
	ss.Rule(".muted").Set("color", "{colors.text-subtle}").End()
	// Command palette result rows: path meta is dimmed against the title.
	ss.Rule(".pal-meta").Set("opacity", "0.5").End()
	ss.Rule(".faint").Set("color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// .tag (small mono label, not uppercase) and .btn (primary/ghost/lg).
// -----------------------------------------------------------------------------

func tagsAndButtons(ss *style.StyleSheet) {
	// The status pill (.tag / .tag.accent / .dot) is now ui.StatusPill, which
	// ships its own CSS. The site only retunes the accent border/bg to the v2
	// tokens via the component's --ui-status-pill-* vars.
	ss.Rule(`[data-fui-comp="ui-status-pill"]`).
		Set("border-color", "var(--line-faint)").End()
	ss.Rule(`[data-fui-comp="ui-status-pill"].ui-status-pill--accent`).
		Set("--ui-status-pill-accent-border", "var(--accent-dim)").End()

	// Spacing utilities used in place of inline style="margin-bottom:…"
	// attributes — strict CSP blocks inline styles, so every spacing tweak
	// has to live in a class. Add more here if you need another step.
	ss.Rule(".mb-md").Set("margin-bottom", "{spacing.md}").End()
	ss.Rule(".mb-lg").Set("margin-bottom", "{spacing.lg}").End()

	// (The site's old .btn / .btn--primary / .btn--ghost / .btn--lg rules were
	// dead — every button in the markup is a framework ui.Button / ui.LinkButton,
	// which ship their own CSS. Removed.)

	// Focus ring — visible for keyboard nav, suppressed for pointer.
	ss.Rule(":focus-visible").
		Set("outline", "2px solid {colors.primary}",
			"outline-offset", "2px",
			"border-radius", "{radii.sm}").End()
}

// -----------------------------------------------------------------------------
// .nav — fixed top bar. Layout-only styling; structure is in layout.go.
//
// Position: fixed on the OUTER <header role="banner"> (the framework's
// layout wrapper), not on .nav itself. position:sticky on .nav was a
// no-op because .nav's parent <header> isn't a scrolling container —
// sticky needs an ancestor that scrolls, and the scrolling viewport
// is the document body, not the immediate parent. Fixed on the outer
// <header> pins it across every screen + survives screen-group
// sibling-nav (which only swaps the inner content cell).
// -----------------------------------------------------------------------------

func siteNav(ss *style.StyleSheet) {
	// Outer banner — the framework's layout wraps HeaderComponent in this.
	// z-index hardcoded because the {z-index.sticky} token wasn't
	// resolving (left the literal in the CSS → invalid → auto → content
	// scrolled over the header). 100 sits above the framework's default
	// sticky tier (10) and below modal/toast tiers (1000+).
	ss.Rule(`header[role="banner"]`).
		Set("position", "fixed",
			"top", "0",
			"left", "0",
			"right", "0",
			"z-index", "100",
			// Opaque background — the previous 88%/12% mix produced a
			// see-through bar even with backdrop-filter:blur because
			// nothing on this site has the visual density behind it
			// for a blur to register against. Solid surface keeps the
			// header legible against any scroll position.
			"background", "{colors.background}",
			"border-bottom", "1px solid var(--line-faint)").End()
	// Body needs to push down so the first viewport pixel of content
	// isn't hidden under the fixed banner.
	ss.Rule("body").Set("padding-top", "var(--nav-h)").End()
	ss.Rule(".ui-site-header").
		Set("height", "var(--nav-h)",
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.xxl}",
			"padding", "0 {spacing.xxl}",
			"font-size", "var(--t-sm)").End()

	// Brand — λ mark + lowercase wordmark + status capsule. Each part
	// has its own selector so the rhythm is tunable: mark a touch
	// brighter, wordmark heavier, status quieter.
	ss.Rule(".site-brand").
		Set("display", "inline-flex",
			"align-items", "center",
			"gap", "10px",
			"color", "{colors.text}",
			"font-family", "{fonts.body}",
			"font-weight", "500",
			"font-size", "15px",
			"letter-spacing", "-0.01em").End()
	// The mark is a typographic λ — sized + colored to feel like a
	// confident chip without resembling a logo placeholder. Mono
	// fixes its width so it lines up with vertical text neighbours.
	ss.Rule(".site-brand__mark").
		Set("display", "inline-grid",
			"place-items", "center",
			"width", "22px",
			"height", "22px",
			"border-radius", "5px",
			"background", "color-mix(in oklch, {colors.primary} 14%, {colors.surface})",
			"color", "{colors.primary}",
			"font-family", "{fonts.mono}",
			"font-size", "14px",
			"line-height", "1",
			"font-weight", "500").End()
	ss.Rule(".site-brand__name").
		Set("font-weight", "500",
			"color", "{colors.text}",
			"letter-spacing", "-0.012em").End()
	// Status capsule — three child spans separated by 6px gap, mono.
	// Visually subordinate to the wordmark, but together they read as
	// the project's live tag.
	ss.Rule(".site-brand__status").
		Set("display", "inline-flex",
			"align-items", "center",
			"gap", "6px",
			"padding", "2px 8px 2px 6px",
			"margin-left", "4px",
			"border", "1px solid color-mix(in oklch, {colors.primary} 30%, transparent)",
			"border-radius", "999px",
			"color", "{colors.text-muted}",
			"font-family", "{fonts.mono}",
			"font-size", "10px",
			"line-height", "1",
			"text-transform", "lowercase",
			"font-weight", "400").End()
	ss.Rule(".site-brand__pulse").
		Set("display", "inline-block",
			"width", "6px",
			"height", "6px",
			"border-radius", "50%",
			"background", "{colors.primary}",
			"box-shadow", "0 0 0 0 color-mix(in oklch, {colors.primary} 60%, transparent)",
			"animation", "nav-pulse 2.4s ease-out infinite").End()
	ss.Rule(".site-brand__tag").Set("color", "{colors.primary}").End()
	ss.Rule(".site-brand__ver").Set("color", "{colors.text-subtle}").End()
	// Respect reduced motion preference — pulse becomes a static dot
	// when the user has prefers-reduced-motion: reduce.
	ss.Media("(prefers-reduced-motion: reduce)", func(inner *style.StyleSheet) {
		inner.Rule(".site-brand__pulse").
			Set("animation", "none",
				"box-shadow", "0 0 6px color-mix(in oklch, {colors.primary} 50%, transparent)").End()
	})
	// The keyframes live at the top level so the at-rule is emitted
	// once and reused.
	ss.Keyframes("nav-pulse",
		style.Step("0%", "box-shadow", "0 0 0 0 color-mix(in oklch, {colors.primary} 50%, transparent)"),
		style.Step("70%", "box-shadow", "0 0 0 8px color-mix(in oklch, {colors.primary} 0%, transparent)"),
		style.Step("100%", "box-shadow", "0 0 0 0 color-mix(in oklch, {colors.primary} 0%, transparent)"),
	)

	// Nav links — minimal, with a subtle left-to-right underline reveal
	// on hover/active so the relationship between hover and active is
	// visible to keyboard users too.
	ss.Rule(".ui-site-header__links").
		Set("display", "flex", "gap", "{spacing.xl}", "margin-left", "{spacing.xl}").End()
	ss.Rule(".ui-site-header__links a").
		Set("position", "relative",
			"display", "inline-flex",
			"align-items", "center",
			"height", "var(--nav-h)",
			"color", "{colors.text-muted}",
			"font-weight", "400",
			"font-size", "var(--t-sm)",
			"letter-spacing", "-0.005em",
			"white-space", "nowrap",
			"transition", "color 120ms ease").End()
	// The animated underline-reveal is now ui.SiteHeader's NavUnderline
	// variant; the site only positions it to clear the bar's baseline and
	// tunes the active text colour via the component's vars.
	ss.Rule(`[data-fui-comp="ui-site-header"].ui-site-header--nav-underline`).
		Set("--ui-site-header-nav-underline-bottom", "14px",
			"--ui-site-header-nav-active-color", "{colors.text}").End()

	ss.Rule(".ui-site-header__right").
		Set("margin-left", "auto",
			"display", "flex",
			"align-items", "center",
			"gap", "{spacing.md}").End()
	// Search pill — replaces the old "Search ⌘K" wireframe with a real
	// dual-affordance control: left-aligned mono placeholder text +
	// right-aligned kbd group. Width is fluid so the pill grows on
	// hover (subtle invitation), and the placeholder rotates through
	// real catalog hints to advertise what the palette knows.
	ss.Rule(".site-cmd").
		Set("display", "inline-flex",
			"align-items", "center",
			"gap", "10px",
			"min-width", "200px",
			"padding", "6px 8px 6px 12px",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"color", "{colors.text-subtle}",
			"font-size", "var(--t-xs)",
			"font-family", "{fonts.body}",
			"background", "{colors.surface}",
			"transition", "border-color 160ms ease, color 160ms ease, background 160ms ease").End()
	ss.Rule(".site-cmd__glyph").Set("display", "none").End()
	ss.Rule(".site-cmd:hover").
		Set("border-color", "color-mix(in oklch, {colors.primary} 35%, {colors.border})",
			"color", "{colors.text}",
			"background", "color-mix(in oklch, {colors.primary} 4%, {colors.surface})").End()
	ss.Rule(".site-cmd > span").
		Set("flex", "1",
			"text-align", "left",
			"color", "{colors.text}",
			"letter-spacing", "-0.005em").End()
	ss.Rule(".site-cmd kbd").
		Set("display", "inline-flex",
			"align-items", "center",
			"gap", "2px",
			"font-family", "{fonts.mono}",
			"font-size", "10px",
			"line-height", "1",
			"padding", "3px 6px",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.sm}",
			"background", "{colors.background}",
			"color", "{colors.text-muted}",
			"font-weight", "500").End()
	ss.Rule(".site-icon").
		Set("width", "30px", "height", "30px",
			"display", "grid", "place-items", "center",
			"border-radius", "{radii.md}",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".site-icon:hover").
		Set("background", "{colors.surface-soft}", "color", "{colors.text}", "opacity", "1").End()

	// Mobile drawer — hidden by default; the @media (max-width: 640px)
	// block in responsive() flips display:block. Native <details>; the
	// framework's runtime auto-closes on cross-page nav via
	// data-fui-disclosure.
	//
	// Trigger is the trigram glyph: three stacked 1.5px bars built with
	// CSS box-shadows on a single 22px square. When [open], the middle
	// bar fades out and the outer two rotate into an ×. No SVG, no JS —
	// state flips via the parent's open attribute.
	// .ui-site-header__mobile sits inside .ui-site-header__right at every viewport; no auto
	// margin needed (the parent cluster owns the right-edge alignment).
	ss.Rule(".ui-site-header__mobile").Set("display", "none").End()
	ss.Rule(".ui-site-header__mobile > summary").
		Set("list-style", "none",
			"cursor", "pointer",
			"display", "inline-grid",
			"place-items", "center",
			"width", "36px",
			"height", "36px",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"background", "{colors.surface}",
			"color", "{colors.text-muted}",
			"transition", "border-color 160ms ease, background 160ms ease").End()
	ss.Rule(".ui-site-header__mobile > summary::-webkit-details-marker").Set("display", "none").End()
	ss.Rule(".ui-site-header__mobile > summary:hover").
		Set("border-color", "{colors.border-strong}",
			"background", "{colors.surface-soft}").End()
	// Open-state: brighten the color so the X reads cleanly. We
	// deliberately do NOT change the border color — the X icon swap
	// IS the state cue; a border tint added an outline that looked
	// like a leftover focus ring.
	ss.Rule(".ui-site-header__mobile[open] > summary").
		Set("color", "{colors.text}").End()
	// Hamburger ↔ X icon swap is handled entirely by ui.SiteHeader
	// (SVG menu / SVG close, display-swapped by details[open]). No
	// site-level overrides needed.
	// Convert the framework's trigger-anchored popover into a
	// viewport-anchored sheet — v2 prefers full-width drawers on
	// phones so the right edge can't clip on narrow viewports.
	// Done entirely via CSS vars exposed by ui.SiteHeader; no
	// selector-stacking overrides needed.
	ss.Rule(`[data-fui-comp="ui-site-header"]`).
		Set("--ui-site-header-drawer-position", "fixed",
			"--ui-site-header-drawer-top", "calc(var(--nav-h) + 8px)",
			"--ui-site-header-drawer-right", "{spacing.md}",
			"--ui-site-header-drawer-left", "{spacing.md}",
			"--ui-site-header-drawer-min-width", "0",
			"--ui-site-header-drawer-shadow", "0 10px 30px rgba(0,0,0,0.35)").End()
}

// -----------------------------------------------------------------------------
// .foot — 5-col footer + bottom strip.
// -----------------------------------------------------------------------------

func siteFooter(ss *style.StyleSheet) {
	// ui.SiteFooter owns the grid (template/gap/max-width/centering) via its
	// --ui-site-footer-* vars; the site only supplies the 5-col template and
	// v2 measures. The root padding is dropped to 0 horizontal because the
	// centered grid carries its own inline padding.
	ss.Rule(".ui-site-footer").
		Set("padding", "var(--s-8) 0 {spacing.xxl}",
			"border-top", "1px solid {colors.border}").End()
	ss.Rule(`[data-fui-comp="ui-site-footer"]`).
		Set("--ui-site-footer-grid-template", "1.4fr 1fr 1fr 1fr 1fr",
			"--ui-site-footer-grid-gap", "var(--s-8)",
			"--ui-site-footer-max-width", "var(--col-max)").End()
	ss.Rule(".ui-site-footer__grid").Set("padding", "0 {spacing.xxl}").End()

	ss.Rule(".site-foot-brand").
		Set("display", "flex", "align-items", "baseline", "gap", "8px",
			"margin-bottom", "{spacing.md}",
			"color", "{colors.text}", "font-weight", "500").End()
	ss.Rule(".site-foot-brand__mark").
		Set("display", "inline-block",
			"width", "9px", "height", "9px",
			"border-radius", "2px",
			"background", "{colors.primary}",
			"transform", "translateY(1px)").End()
	ss.Rule(".site-foot-brand__ver").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"font-weight", "400").End()
	ss.Rule(".site-foot-brand__copy").
		Set("font-size", "var(--t-sm)",
			"color", "{colors.text-subtle}",
			"line-height", "1.6",
			"max-width", "30ch").End()
	ss.Rule(".ui-site-footer h6").
		Set("font-family", "{fonts.body}",
			"font-weight", "500",
			"font-size", "13px",
			"color", "{colors.text}",
			"margin-bottom", "{spacing.md}",
			"letter-spacing", "-0.005em",
			"text-transform", "none").End()
	ss.Rule(".ui-site-footer ul li").Set("padding", "3px 0").End()
	ss.Rule(".ui-site-footer ul li a").
		Set("color", "{colors.text-subtle}", "font-size", "var(--t-sm)").End()
	ss.Rule(".ui-site-footer ul li a:hover").Set("color", "{colors.text}", "opacity", "1").End()
	ss.Rule(".ui-site-footer__bottom").
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
	// The code block (.code / .code__head / .code__body / .ln chrome) is now
	// ui.CodeBlock, which ships its own framed CSS + the framework CopyButton.
	// The site only retunes the green status dot to the v2 string-token colour
	// and supplies the mono font scale via the component's vars.
	ss.Rule(`[data-fui-comp="ui-code-block"].ui-code-block--framed`).
		Set("font-family", "{fonts.mono}",
			"font-size", "var(--t-sm)",
			"line-height", "1.65",
			"--ui-code-block-status-color", "var(--tk-str)",
			"max-width", "100%",
			"min-width", "0").End()

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
	// .section-v2 is layered on the framework's ui.Section component, which
	// owns the <section> landmark, eyebrow, and scroll-margin. We reset the
	// component's default block margin and supply the v2 framing.
	ss.Rule(".section-v2").
		Set("margin", "0",
			"padding", "var(--s-9) 0",
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
	// The eyebrow markup is now ui.Section's .ui-section__eyebrow; the site
	// only pins it to the section's top-right corner.
	ss.Rule(".section-v2 .ui-section__eyebrow").
		Set("position", "absolute",
			"top", "{spacing.xxxl}",
			"right", "{spacing.xxl}",
			"color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// .hero — two-col grid with the headline left, the code block right.
// -----------------------------------------------------------------------------

func heroLayout(ss *style.StyleSheet) {
	ss.Rule(".hero").
		Set("padding", "var(--s-9) 0 var(--s-8)",
			"border-bottom", "1px solid var(--line-faint)").End()
	// .hero__grid is gone — ui.HeroSplit (framework) owns the 2-col
	// layout + mobile collapse. The home hero adds a small top
	// padding on the copy column to align with the code block.
	ss.Rule(`.hero-home .ui-hero-split__copy`).
		Set("padding-top", "{spacing.md}").End()
	// Grid item escape — without min-width:0 the code block intrinsically
	// pushes its column past the viewport on mobile and the page picks up
	// horizontal scroll.
	ss.Rule(".hero__code").Set("min-width", "0").End()
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
	// The terminal mock (.term / .term__head / .term__body) is now
	// ui.TerminalBlock, which ships its own CSS. The site only retunes the
	// header rule + success colour to the v2 tokens via the component vars.
	ss.Rule(`[data-fui-comp="ui-terminal-block"]`).
		Set("--ui-terminal-block-head-border", "var(--line-faint)",
			"--ui-terminal-block-ok-color", "var(--tk-str)").End()
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
// Responsive — three breakpoints:
//   < 640px   phone
//   640-1024  tablet
//   > 1024    desktop (default rules above)
// Mobile-first would be cleaner long-term, but the existing rules are
// desktop-first; the media queries here collapse layouts as the viewport
// shrinks. Each section's collapse rules live with that section in the
// styles_pages.go helpers; this file's responsive() handles the chrome.
// -----------------------------------------------------------------------------

func responsive(ss *style.StyleSheet) {
	// Sidebar layouts collapse at 900px — the same breakpoint at which
	// interactive.SectionMenu swaps its sticky rail for the mobile sheet, so
	// the rail never renders full-width above the content.
	ss.Media("(max-width: 900px)", func(inner *style.StyleSheet) {
		// /components/* — drop the sticky 260px rail column; SectionMenu
		// becomes a "Sections" trigger pill that opens the slide-in sheet.
		inner.Rule(".layout-components > .layout-body").
			Set("grid-template-columns", "1fr",
				"gap", "{spacing.lg}",
				"padding", "{spacing.lg}").End()
		// The in-page TOC un-sticks on mobile (ui.DocLayout self-collapses).
		inner.Rule(".toc").Set("position", "static").End()
	})

	// Existing 980px breakpoint for the home page sections.
	ss.Media("(max-width: 980px)", func(inner *style.StyleSheet) {
		// ui.HeroSplit collapses itself at <=980px; no override here.
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
		// Set the component's template var (not the property) so it wins
		// against ui.SiteFooter's own var-based grid rule.
		inner.Rule(`[data-fui-comp="ui-site-footer"]`).
			Set("--ui-site-footer-grid-template", "1fr 1fr").End()
	})

	// Phone — collapse everything to one column, hide horizontal nav links
	// (the mobile drawer in HeaderComponent's <details> takes over), shrink
	// hero headlines, drop multi-col card grids to single-col.
	ss.Media("(max-width: 640px)", func(inner *style.StyleSheet) {
		// Nav at phone width — brand (λ + gofastr) on the left, four
		// icon-sized controls tight on the right: search, trigram,
		// theme, GitHub. NOTHING gets removed (search + GitHub were
		// fully accessible on desktop, they stay so on phone) — they
		// just shed labels and tighten to a single 36px square apiece.
		// The status capsule and desktop link bar do collapse since
		// the links are mirrored in the drawer.
		inner.Rule(".ui-site-header__links").Set("display", "none").End()
		inner.Rule(".ui-site-header__mobile").Set("display", "block").End()
		inner.Rule(".site-brand__status").Set("display", "none").End()
		// Search pill morphs into an icon button on phones: the placeholder
		// text + ⌘K hint hide, the magnifier glyph shows. Touch targets are
		// 44×44 (WCAG 2.5.5) — the header controls were 30–36px before.
		inner.Rule(".site-cmd").
			Set("min-width", "44px",
				"width", "44px",
				"height", "44px",
				"padding", "0",
				"justify-content", "center",
				"gap", "0",
				"border-radius", "{radii.md}").End()
		inner.Rule(".site-cmd__placeholder, .site-cmd kbd").
			Set("display", "none").End()
		inner.Rule(".site-cmd__glyph").
			Set("display", "block").End()
		// Icon buttons (theme toggle, GitHub) to 44×44. The hamburger
		// (.ui-site-header__mobile-toggle) is sized by framework/ui's
		// SiteHeader CSS — bumped to 44px there.
		inner.Rule(".site-icon").Set("width", "44px", "height", "44px").End()
		inner.Rule(".ui-site-header").Set("padding", "0 {spacing.sm}", "gap", "{spacing.sm}").End()
		inner.Rule(".ui-site-header__right").Set("gap", "4px").End()
		// All card grids → 1 col.
		inner.Rule(".arch__grid").Set("grid-template-columns", "1fr").End()
		inner.Rule(".ex__grid").Set("grid-template-columns", "1fr").End()
		inner.Rule(".docs").Set("grid-template-columns", "1fr").End()
		inner.Rule(".next__grid").Set("grid-template-columns", "1fr").End()
		// Every page-local hero/body grid that's still multi-col below
		// the tablet breakpoint. These were missed in the original
		// responsive pass because each one's columns are page-specific.
		// ui.HeroSplit collapses gs-hero/cx-hero at <=980px on its own.
		inner.Rule(".gs-facts").Set("grid-template-columns", "1fr").End()
		inner.Rule(".cx-stats").Set("grid-template-columns", "1fr", "gap", "{spacing.md}").End()
		inner.Rule(".cx-body").Set("grid-template-columns", "1fr", "gap", "{spacing.lg}", "padding", "{spacing.lg} 0").End()
		inner.Rule(".intent-rail").Set("position", "static", "max-height", "none", "overflow", "visible").End()
		// Doc page chrome that the 1024px rule already collapses — but
		// the docnav max-height needs a tighter bound on small phones.
		inner.Rule(".docnav").Set("max-height", "240px", "overflow-y", "auto", "border-bottom", "1px solid var(--line-faint)", "padding-bottom", "{spacing.md}").End()
		inner.Rule(".toc").Set("display", "none").End() // hide in-page TOC on phone (drawer would be better; cheap fix for now)
		// Examples — three-column row → stacked.
		inner.Rule(".ex-row__grid").Set("grid-template-columns", "1fr", "gap", "{spacing.lg}").End()
		inner.Rule(".ex-row__num").Set("padding-top", "0").End()
		// Kiln demo + timeline + caps + cli.
		inner.Rule(".tl-evt").Set("grid-template-columns", "56px 16px 1fr").End()
		// Philosophy magazine masthead 3-col → stack.
		inner.Rule(".ph-hero__grid").Set("grid-template-columns", "1fr", "gap", "{spacing.lg}").End()
		inner.Rule(".ph-hero .by").Set("text-align", "left").End()
		inner.Rule(".ph-body").Set("grid-template-columns", "1fr", "gap", "{spacing.lg}", "padding", "{spacing.xl} 0").End()
		inner.Rule(".ph-toc").Set("position", "static").End()
		inner.Rule(".roadmap__row").Set("grid-template-columns", "1fr", "gap", "4px").End()
		// Pull quote pseudo-quote was overflowing — clip.
		inner.Rule(".pullquote::before").Set("left", "0").End()
		// Doc footer prev/next stacks.
		inner.Rule(".doc-foot__nav").Set("grid-template-columns", "1fr").End()
		inner.Rule(".components-overview__sections").Set("gap", "{spacing.xl}").End()
		inner.Rule(".cx-stats").Set("grid-template-columns", "repeat(3, 1fr)", "padding", "{spacing.md}").End()
		// Cap hero headline sizes.
		inner.Rule(".hero__title, .gs-hero h1, .cx-hero h1, .ex-hero h1, .k-hero h1, .ph-hero h1").
			Set("font-size", "clamp(32px, 10vw, 44px)").End()
		// Footer goes single column — drive ui.SiteFooter's vars so the
		// component's own grid rule picks the collapse up.
		inner.Rule(`[data-fui-comp="ui-site-footer"]`).
			Set("--ui-site-footer-grid-template", "1fr",
				"--ui-site-footer-grid-gap", "{spacing.xl}").End()
		inner.Rule(".ui-site-footer__bottom").Set("flex-direction", "column", "gap", "{spacing.md}", "align-items", "flex-start").End()
		// Step-rail (get-started) collapses too.
		inner.Rule(".gs-body").Set("grid-template-columns", "1fr", "gap", "{spacing.lg}", "padding", "{spacing.xl} 0").End()
		inner.Rule(".step-rail").Set("position", "static", "max-height", "200px", "overflow-y", "auto", "padding-bottom", "{spacing.md}", "border-bottom", "1px solid var(--line-faint)").End()
		// Install commands stay on one line and pan horizontally on
		// mobile. Wrapping a no-space command (go install …/cmd/gofastr)
		// breaks it mid-token ("gofast r/cmd"), which is illegible and
		// breaks copy-paste — horizontal scroll keeps the token intact.
		inner.Rule(".hero__install").Set("white-space", "nowrap", "overflow-x", "auto").End()
		inner.Rule(".k-hero__cli").Set("white-space", "nowrap", "overflow-x", "auto").End()
		// Code blocks inside paragraphs that have long URLs (install
		// commands, RPC paths) need to wrap rather than force a wide line.
		inner.Rule(":not(pre) > code").Set("word-break", "break-word").End()
		// ui.Container default padding (--spacing-md) is already the
		// phone-tight value below 720px — no override needed here.
		// Kiln demo body — single column.
		inner.Rule(".k-demo__body").Set("grid-template-columns", "1fr", "min-height", "0").End()
		inner.Rule(".caps__grid, .cli-block").Set("grid-template-columns", "1fr").End()
		// 404 page — single column.
		inner.Rule(".nf").Set("grid-template-columns", "1fr", "gap", "{spacing.xl}").End()
		inner.Rule(".nf__num").Set("font-size", "clamp(80px, 20vw, 140px)").End()
		// Components inner layout — sidebar is already stacked at tablet;
		// at phone its max-height tightens further.
		// (was: max-height:200px — caused the drawer body to overflow
		// past the constrained nav box, pushing main into the same
		// y-band as the drawer body. Removed; the drawer's own
		// max-height on its body handles internal scroll.)
	})
}
