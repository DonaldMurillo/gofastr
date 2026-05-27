package main

// =============================================================================
// Page-local CSS — additions beyond styles.go's site chrome. One function per
// page, registered from createStyleSheet so token resolution flows uniformly
// through the typed pipeline. Each block's class names come straight from the
// design prototype at /tmp/gofastr-design/gofastr/project/pages/*-v2.html so
// the markup in screens_pages.go reads the same as the prototype source.
// =============================================================================

import "github.com/DonaldMurillo/gofastr/core-ui/style"

// -----------------------------------------------------------------------------
// /get-started — six-step onboarding with sticky step-rail.
// -----------------------------------------------------------------------------

func pageGetStarted(ss *style.StyleSheet) {
	ss.Rule(".gs-hero").
		Set("padding", "var(--s-9) 0 {spacing.xxl}",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".gs-hero__grid").
		Set("display", "grid",
			"grid-template-columns", "minmax(0, 1fr) minmax(0, 1fr)",
			"gap", "{spacing.xxxl}",
			"align-items", "start").End()
	ss.Rule(".gs-hero h1").
		Set("font-size", "clamp(40px, 5vw, 64px)",
			"line-height", "1.05",
			"letter-spacing", "-0.03em",
			"margin-bottom", "{spacing.xl}",
			"max-width", "16ch").End()
	ss.Rule(".gs-hero h1 .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".gs-hero .lede").
		Set("color", "{colors.text-muted}",
			"font-size", "var(--t-lg)",
			"max-width", "48ch").End()

	ss.Rule(".gs-facts").
		Set("display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "{spacing.md}").End()
	ss.Rule(".fact").
		Set("padding", "{spacing.lg}",
			"background", "{colors.surface}",
			"border", "1px solid var(--line-faint)",
			"border-radius", "{radii.md}").End()
	ss.Rule(".fact.full").Set("grid-column", "1 / -1").End()
	ss.Rule(".fact .l").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"display", "block",
			"margin-bottom", "6px").End()
	ss.Rule(".fact .v").
		Set("font-size", "var(--t-md)", "color", "{colors.text}", "font-weight", "500").End()

	ss.Rule(".gs-body").
		Set("display", "grid",
			"grid-template-columns", "220px minmax(0, 1fr)",
			"gap", "{spacing.xxxl}",
			"padding", "var(--s-9) 0").End()
	ss.Rule(".step-rail").
		Set("position", "sticky",
			"top", "calc(var(--nav-h) + {spacing.lg})",
			"align-self", "start",
			"font-size", "var(--t-sm)").End()
	ss.Rule(".step-rail h6").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.md}",
			"font-weight", "400").End()
	ss.Rule(".step-rail ol").Set("display", "grid", "gap", "2px").End()
	ss.Rule(".step-rail li a").
		Set("display", "grid",
			"grid-template-columns", "28px 1fr",
			"gap", "8px",
			"padding", "6px 0",
			"color", "{colors.text-muted}").End()
	ss.Rule(".step-rail li a .n").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".step-rail li a.active").Set("color", "{colors.text}").End()
	ss.Rule(".step-rail li a.active .n").Set("color", "{colors.primary}").End()
	ss.Rule(".step-rail .meta").
		Set("margin-top", "{spacing.lg}",
			"padding-top", "{spacing.md}",
			"border-top", "1px solid var(--line-faint)",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "var(--fg-4)").End()

	ss.Rule(".step").
		Set("padding-bottom", "var(--s-8)",
			"border-bottom", "1px solid var(--line-faint)",
			"margin-bottom", "var(--s-8)").End()
	ss.Rule(".step:last-child").Set("border-bottom", "0").End()
	ss.Rule(".step__head").
		Set("display", "flex",
			"align-items", "baseline",
			"gap", "{spacing.md}",
			"margin-bottom", "{spacing.lg}").End()
	ss.Rule(".step__num").
		Set("font-family", "{fonts.mono}",
			"font-size", "12px",
			"color", "{colors.primary}").End()
	ss.Rule(".step__title").
		Set("font-size", "var(--t-2xl)",
			"font-weight", "500",
			"letter-spacing", "-0.02em",
			"flex", "1").End()
	ss.Rule(".step__time").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.text-subtle}").End()
	ss.Rule(".step__body p").
		Set("color", "{colors.text-muted}",
			"font-size", "var(--t-md)",
			"max-width", "62ch",
			"margin-bottom", "{spacing.lg}").End()

	ss.Rule(".callout").
		Set("padding", "{spacing.lg}",
			"background", "color-mix(in oklch, {colors.primary} 6%, {colors.surface})",
			"border-left", "3px solid {colors.primary}",
			"border-radius", "{radii.sm}",
			"margin", "{spacing.lg} 0").End()
	ss.Rule(".callout h5").
		Set("font-size", "var(--t-md)",
			"margin-bottom", "6px",
			"color", "{colors.text}").End()
	ss.Rule(".callout p").
		Set("color", "{colors.text-muted}", "font-size", "var(--t-sm)", "margin", "0").End()

	ss.Rule(".result").
		Set("padding", "{spacing.xl}",
			"background", "color-mix(in oklch, var(--tk-str) 8%, {colors.surface})",
			"border", "1px solid color-mix(in oklch, var(--tk-str) 30%, {colors.border})",
			"border-radius", "{radii.md}",
			"margin", "{spacing.lg} 0").End()
	ss.Rule(".result h5").
		Set("color", "{colors.text}", "margin-bottom", "{spacing.md}", "font-size", "var(--t-md)").End()
	ss.Rule(".result ul").
		Set("display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "8px",
			"margin-top", "{spacing.md}").End()
	ss.Rule(".result ul li").
		Set("color", "{colors.text}",
			"font-size", "var(--t-sm)",
			"padding-left", "20px",
			"position", "relative").End()
	ss.Rule(".result ul li::before").
		Set("content", `"✓"`,
			"position", "absolute",
			"left", "2px",
			"color", "var(--tk-str)").End()

	ss.Rule(".next").
		Set("padding", "var(--s-9) 0",
			"border-top", "1px solid {colors.border}").End()
	ss.Rule(".next h2").
		Set("font-size", "var(--t-3xl)",
			"margin-bottom", "{spacing.xl}",
			"letter-spacing", "-0.025em").End()
	ss.Rule(".next__grid").
		Set("display", "grid",
			"grid-template-columns", "repeat(3, 1fr)",
			"gap", "{spacing.lg}").End()
}

// -----------------------------------------------------------------------------
// /docs/ (concepts index) — IA-grouped 53 docs.
// -----------------------------------------------------------------------------

func pageConceptsIndex(ss *style.StyleSheet) {
	ss.Rule(".cx-hero").
		Set("padding", "var(--s-9) 0 {spacing.xxl}",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".cx-hero__grid").
		Set("display", "grid",
			"grid-template-columns", "minmax(0, 1.4fr) minmax(0, 1fr)",
			"gap", "{spacing.xxxl}",
			"align-items", "start").End()
	ss.Rule(".cx-hero h1").
		Set("font-size", "clamp(40px, 5vw, 64px)",
			"line-height", "1.05",
			"letter-spacing", "-0.03em",
			"margin-bottom", "{spacing.xl}",
			"max-width", "16ch").End()
	ss.Rule(".cx-hero h1 .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".cx-hero .lede").
		Set("color", "{colors.text-muted}",
			"font-size", "var(--t-lg)",
			"max-width", "50ch").End()

	ss.Rule(".cx-stats").
		Set("display", "grid",
			"grid-template-columns", "repeat(3, 1fr)",
			"gap", "{spacing.md}",
			"padding", "{spacing.lg}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}").End()
	ss.Rule(".cx-stats .v").
		Set("font-size", "var(--t-2xl)",
			"font-weight", "500",
			"color", "{colors.text}",
			"letter-spacing", "-0.02em",
			"display", "block",
			"margin-bottom", "4px").End()
	ss.Rule(".cx-stats .l").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.text-subtle}").End()

	ss.Rule(".cx-body").
		Set("display", "grid",
			"grid-template-columns", "220px minmax(0, 1fr)",
			"gap", "{spacing.xxxl}",
			"padding", "{spacing.xxl} 0 var(--s-9)").End()
	ss.Rule(".intent-rail").
		Set("position", "sticky",
			"top", "calc(var(--nav-h) + {spacing.lg})",
			"align-self", "start",
			"font-size", "var(--t-sm)").End()
	ss.Rule(".intent-rail h6").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.md}",
			"font-weight", "400").End()
	ss.Rule(".intent-rail li a").
		Set("display", "grid",
			"grid-template-columns", "28px 1fr 28px",
			"gap", "8px",
			"padding", "6px 0",
			"color", "{colors.text-muted}").End()
	ss.Rule(".intent-rail .n").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".intent-rail .ct").
		Set("font-family", "{fonts.mono}",
			"font-size", "10px",
			"color", "var(--fg-4)",
			"text-align", "right").End()
	ss.Rule(".intent-rail li a.active").Set("color", "{colors.text}").End()
	ss.Rule(".intent-rail li a.active .n").Set("color", "{colors.primary}").End()

	ss.Rule(".intent").Set("margin-bottom", "var(--s-9)").End()
	ss.Rule(".intent__head").
		Set("display", "flex",
			"align-items", "baseline",
			"gap", "{spacing.md}",
			"margin-bottom", "{spacing.md}").End()
	ss.Rule(".intent__num").
		Set("font-family", "{fonts.mono}", "font-size", "12px", "color", "{colors.primary}").End()
	ss.Rule(".intent__title").
		Set("font-size", "var(--t-2xl)",
			"font-weight", "500",
			"letter-spacing", "-0.02em",
			"flex", "1").End()
	ss.Rule(".intent__meta").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.text-subtle}").End()
	ss.Rule(".intent__lede").
		Set("color", "{colors.text-muted}",
			"max-width", "62ch",
			"margin-bottom", "{spacing.xl}").End()

	ss.Rule(".docs").
		Set("display", "grid",
			"grid-template-columns", "repeat(3, 1fr)",
			"gap", "1px",
			"background", "var(--line-faint)",
			"border", "1px solid var(--line-faint)",
			"border-radius", "{radii.md}",
			"overflow", "hidden").End()
	ss.Rule(".doc").
		Set("padding", "{spacing.lg}",
			"background", "{colors.background}",
			"display", "flex",
			"flex-direction", "column",
			"gap", "8px").End()
	ss.Rule(".doc:hover").Set("background", "{colors.surface}").End()
	ss.Rule(".doc__head").
		Set("display", "flex", "align-items", "center", "gap", "8px").End()
	ss.Rule(".doc__head .pill").
		Set("font-family", "{fonts.mono}",
			"font-size", "10px",
			"padding", "2px 6px",
			"border-radius", "{radii.sm}",
			"text-transform", "lowercase").End()
	ss.Rule(".pill.frame").
		Set("background", "color-mix(in oklch, var(--tk-fn) 18%, transparent)",
			"color", "var(--tk-fn)").End()
	ss.Rule(".pill.core").
		Set("background", "color-mix(in oklch, var(--tk-kw) 18%, transparent)",
			"color", "var(--tk-kw)").End()
	ss.Rule(".pill.battery").
		Set("background", "color-mix(in oklch, var(--tk-str) 18%, transparent)",
			"color", "var(--tk-str)").End()
	ss.Rule(".pill.ui").
		Set("background", "color-mix(in oklch, var(--tk-type) 18%, transparent)",
			"color", "var(--tk-type)").End()
	ss.Rule(".doc__title").
		Set("font-size", "var(--t-md)",
			"font-weight", "500",
			"color", "{colors.text}",
			"letter-spacing", "-0.005em").End()
	ss.Rule(".doc__desc").
		Set("font-size", "var(--t-sm)",
			"color", "{colors.text-muted}",
			"line-height", "1.5").End()
	ss.Rule(".doc__meta").
		Set("font-family", "{fonts.mono}",
			"font-size", "10px",
			"color", "var(--fg-4)",
			"margin-top", "auto").End()

	ss.Rule(".path-strip").
		Set("display", "flex",
			"align-items", "center",
			"gap", "8px",
			"padding", "{spacing.md} {spacing.lg}",
			"margin-top", "{spacing.md}",
			"border", "1px dashed var(--line-faint)",
			"border-radius", "{radii.sm}",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"flex-wrap", "wrap").End()
	ss.Rule(".path-strip .l").Set("color", "{colors.text-subtle}").End()
	ss.Rule(".path-strip .s").Set("color", "{colors.text}").End()
	ss.Rule(".path-strip .arrow").Set("color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// /docs/{slug} (concepts doc) — 3-col article shell.
// -----------------------------------------------------------------------------

func pageConceptsDoc(ss *style.StyleSheet) {
	ss.Rule(".doc-shell").
		Set("display", "grid",
			"grid-template-columns", "220px minmax(0, 1fr) 220px",
			"gap", "{spacing.xxxl}",
			"max-width", "1360px",
			"margin", "0 auto",
			"padding", "{spacing.xxl} {spacing.xxl} var(--s-9)").End()

	ss.Rule(".docnav").
		Set("position", "sticky",
			"top", "calc(var(--nav-h) + {spacing.lg})",
			"align-self", "start",
			"max-height", "calc(100vh - var(--nav-h) - {spacing.xl})",
			"overflow-y", "auto",
			"font-size", "var(--t-sm)").End()
	ss.Rule(".docnav__group").Set("margin-bottom", "{spacing.lg}").End()
	ss.Rule(".docnav__group > .label").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"display", "flex",
			"gap", "6px",
			"margin-bottom", "6px",
			"font-weight", "400").End()
	ss.Rule(".docnav__group > .label .n").Set("color", "var(--fg-4)").End()
	ss.Rule(".docnav li a").
		Set("display", "block",
			"padding", "3px 0 3px 22px",
			"color", "{colors.text-muted}").End()
	ss.Rule(".docnav li a.active").
		Set("color", "{colors.text}",
			"border-left", "2px solid {colors.primary}",
			"margin-left", "-2px",
			"padding-left", "20px").End()

	ss.Rule(".doc-content").Set("max-width", "720px").End()
	ss.Rule(".doc-crumbs").
		Set("display", "flex", "gap", "6px",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.lg}").End()
	ss.Rule(".doc-crumbs .sep").Set("color", "var(--fg-4)").End()
	ss.Rule(".doc-crumbs .current").Set("color", "{colors.text-muted}").End()

	ss.Rule(".doc-head").Set("margin-bottom", "{spacing.xl}").End()
	ss.Rule(".doc-head h1").
		Set("font-size", "clamp(32px, 4.4vw, 56px)",
			"line-height", "1.05",
			"letter-spacing", "-0.03em",
			"margin-bottom", "{spacing.md}",
			"text-wrap", "balance").End()
	ss.Rule(".doc-head h1 .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".doc-head__meta").
		Set("display", "flex", "gap", "{spacing.md}",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.lg}",
			"align-items", "center",
			"flex-wrap", "wrap").End()
	ss.Rule(".doc-head__meta .sep").Set("color", "var(--fg-4)").End()
	ss.Rule(".doc-head__lede").
		Set("font-size", "var(--t-lg)",
			"line-height", "1.55",
			"color", "{colors.text-muted}",
			"max-width", "60ch",
			"text-wrap", "balance").End()

	ss.Rule(".prose").Set("color", "{colors.text}", "font-size", "var(--t-md)").End()
	ss.Rule(".prose p").
		Set("color", "{colors.text-muted}",
			"line-height", "1.7",
			"margin-bottom", "{spacing.lg}").End()
	ss.Rule(".prose h2").
		Set("font-size", "var(--t-2xl)",
			"margin-top", "{spacing.xxxl}",
			"margin-bottom", "{spacing.md}",
			"letter-spacing", "-0.02em").End()
	ss.Rule(".prose h3").
		Set("font-size", "var(--t-xl)",
			"margin-top", "{spacing.xxl}",
			"margin-bottom", "{spacing.md}",
			"letter-spacing", "-0.015em").End()
	ss.Rule(".prose ul, .prose ol").
		Set("margin", "0 0 {spacing.lg} 0",
			"padding-left", "{spacing.lg}",
			"color", "{colors.text-muted}",
			"display", "grid",
			"gap", "8px").End()
	ss.Rule(".prose ul li").Set("list-style", "none", "padding-left", "16px", "position", "relative").End()
	ss.Rule(".prose ul li::before").
		Set("content", `"•"`, "position", "absolute", "left", "0",
			"color", "{colors.primary}").End()
	ss.Rule(".prose ol").Set("list-style", "decimal").End()
	ss.Rule(".prose blockquote").
		Set("border-left", "2px solid {colors.primary}",
			"padding", "{spacing.md} {spacing.lg}",
			"color", "{colors.text}",
			"margin", "{spacing.lg} 0",
			"background", "color-mix(in oklch, {colors.primary} 5%, transparent)").End()
	ss.Rule(".prose figure").Set("margin", "{spacing.xl} 0").End()
	ss.Rule(".prose figcaption").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-top", "8px",
			"text-align", "center").End()
	ss.Rule(".prose .note").
		Set("padding", "{spacing.lg}",
			"background", "color-mix(in oklch, {colors.primary} 6%, {colors.surface})",
			"border-left", "3px solid {colors.primary}",
			"border-radius", "{radii.sm}",
			"margin", "{spacing.lg} 0").End()
	ss.Rule(".prose .note h4").
		Set("font-size", "var(--t-md)",
			"margin-bottom", "6px",
			"color", "{colors.text}").End()
	ss.Rule(".prose .note p").Set("margin", "0").End()

	ss.Rule(".doc-foot").
		Set("margin-top", "var(--s-8)",
			"padding-top", "{spacing.xl}",
			"border-top", "1px solid {colors.border}").End()
	ss.Rule(".doc-foot__nav").
		Set("display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "{spacing.lg}",
			"margin-bottom", "{spacing.xl}").End()
	ss.Rule(".prev-card, .next-card").
		Set("padding", "{spacing.lg}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"display", "flex",
			"flex-direction", "column",
			"gap", "4px").End()
	ss.Rule(".prev-card:hover, .next-card:hover").Set("border-color", "{colors.border-strong}").End()
	ss.Rule(".next-card").Set("text-align", "right").End()
	ss.Rule(".prev-card .dir, .next-card .dir").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.text-subtle}").End()
	ss.Rule(".prev-card .ttl, .next-card .ttl").
		Set("color", "{colors.text}", "font-weight", "500").End()
	ss.Rule(".doc-foot__chrome").
		Set("display", "flex",
			"align-items", "center",
			"gap", "{spacing.md}",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"flex-wrap", "wrap").End()
	ss.Rule(".doc-foot__chrome .sep").Set("color", "var(--fg-4)").End()
	ss.Rule(".feedback").
		Set("margin-left", "auto",
			"display", "flex",
			"align-items", "center",
			"gap", "6px").End()
	ss.Rule(".feedback button").
		Set("padding", "4px 10px",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.sm}",
			"background", "{colors.surface}",
			"color", "{colors.text-muted}",
			"font-family", "{fonts.mono}",
			"font-size", "11px").End()
	ss.Rule(".feedback button:hover").
		Set("color", "{colors.text}", "border-color", "{colors.border-strong}").End()

	ss.Rule(".toc").
		Set("position", "sticky",
			"top", "calc(var(--nav-h) + {spacing.lg})",
			"align-self", "start",
			"font-size", "var(--t-sm)").End()
	ss.Rule(".toc h6").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.md}",
			"font-weight", "400").End()
	ss.Rule(".toc ol").
		Set("counter-reset", "toc",
			"display", "grid", "gap", "4px").End()
	ss.Rule(".toc ol li").
		Set("counter-increment", "toc",
			"padding-left", "22px",
			"position", "relative").End()
	ss.Rule(".toc ol li::before").
		Set("content", `counter(toc, decimal-leading-zero)`,
			"position", "absolute",
			"left", "0",
			"font-family", "{fonts.mono}",
			"font-size", "10px",
			"color", "var(--fg-4)").End()
	ss.Rule(".toc ol li a").Set("color", "{colors.text-muted}").End()
	ss.Rule(".toc ol li a.active").Set("color", "{colors.text}").End()
	ss.Rule(".toc__foot").
		Set("margin-top", "{spacing.lg}",
			"padding-top", "{spacing.md}",
			"border-top", "1px solid var(--line-faint)",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "var(--fg-4)").End()
}

// -----------------------------------------------------------------------------
// /examples — six reference apps as stacked card rows.
// -----------------------------------------------------------------------------

func pageExamples(ss *style.StyleSheet) {
	ss.Rule(".ex-hero").
		Set("padding", "var(--s-9) 0 {spacing.xxl}",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".ex-hero h1").
		Set("font-size", "clamp(40px, 5vw, 64px)",
			"line-height", "1.05",
			"letter-spacing", "-0.03em",
			"margin-bottom", "{spacing.lg}",
			"max-width", "16ch").End()
	ss.Rule(".ex-hero h1 .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".ex-hero .lede").
		Set("color", "{colors.text-muted}", "font-size", "var(--t-lg)", "max-width", "52ch").End()

	ss.Rule(".ex-row").
		Set("padding", "var(--s-8) 0",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".ex-row__grid").
		Set("display", "grid",
			"grid-template-columns", "60px minmax(0, 1fr) minmax(0, 1fr)",
			"gap", "{spacing.xl}",
			"align-items", "start").End()
	ss.Rule(".ex-row__num").
		Set("font-family", "{fonts.mono}",
			"font-size", "12px",
			"color", "{colors.text-subtle}",
			"padding-top", "6px").End()
	ss.Rule(".ex-row__body").Set("display", "grid", "gap", "{spacing.md}").End()
	ss.Rule(".ex-row__meta").
		Set("display", "flex", "gap", "{spacing.md}", "align-items", "center", "flex-wrap", "wrap").End()
	ss.Rule(".ex-row__meta .lc").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.text-subtle}").End()
	ss.Rule(".ex-row__title").
		Set("font-size", "var(--t-2xl)",
			"font-weight", "500",
			"letter-spacing", "-0.02em").End()
	ss.Rule(".ex-row__title .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".ex-row__desc").
		Set("color", "{colors.text-muted}", "max-width", "52ch").End()
	ss.Rule(".ex-row__points li").
		Set("padding-left", "20px",
			"position", "relative",
			"color", "{colors.text-muted}",
			"font-size", "var(--t-sm)",
			"line-height", "1.6").End()
	ss.Rule(".ex-row__points li::before").
		Set("content", `"→"`,
			"position", "absolute",
			"left", "0",
			"color", "{colors.primary}").End()
	ss.Rule(".ex-row__cli").
		Set("padding", "8px 12px",
			"background", "{colors.surface-soft}",
			"border", "1px solid var(--line-faint)",
			"border-radius", "{radii.sm}",
			"font-family", "{fonts.mono}",
			"font-size", "12px",
			"color", "{colors.text}",
			"display", "inline-flex",
			"gap", "8px",
			"align-self", "flex-start").End()
	ss.Rule(".ex-row__cli .p").Set("color", "{colors.text-subtle}").End()
	ss.Rule(".ex-row__right").
		Set("display", "flex", "flex-direction", "column", "gap", "{spacing.md}").End()
	ss.Rule(".ex-shot").
		Set("padding", "{spacing.lg}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"display", "grid", "gap", "8px").End()
	ss.Rule(".ex-shot .bar").
		Set("height", "12px", "background", "{colors.surface-soft}",
			"border-radius", "4px").End()
	ss.Rule(".ex-shot .bar.accent").
		Set("background", "color-mix(in oklch, {colors.primary} 40%, {colors.surface-soft})",
			"width", "30%").End()
	ss.Rule(".ex-shot .bar.kw").
		Set("background", "color-mix(in oklch, var(--tk-kw) 30%, {colors.surface-soft})",
			"width", "50%").End()
	ss.Rule(".ex-shot .row").
		Set("display", "flex", "gap", "8px").End()
	ss.Rule(".ex-shot .square").
		Set("width", "60px", "height", "60px",
			"background", "{colors.surface-soft}",
			"border-radius", "4px",
			"flex", "0 0 60px").End()
}

// -----------------------------------------------------------------------------
// /kiln — agent build mode, louder amber wash.
// -----------------------------------------------------------------------------

func pageKiln(ss *style.StyleSheet) {
	// body wash on the kiln page only — scoped via a body-level class.
	ss.Rule(".kiln-page").
		Set("background-image",
			"radial-gradient(ellipse at top left, color-mix(in oklch, {colors.primary} 8%, transparent), transparent 50%), radial-gradient(ellipse at bottom right, color-mix(in oklch, {colors.primary} 5%, transparent), transparent 60%)").End()

	ss.Rule(".k-hero").Set("padding", "var(--s-9) 0").End()
	ss.Rule(".k-hero__lockup").
		Set("display", "inline-flex",
			"align-items", "center",
			"gap", "12px",
			"margin-bottom", "{spacing.lg}").End()
	ss.Rule(".k-hero__lockup .mark").
		Set("display", "grid",
			"place-items", "center",
			"width", "28px", "height", "28px",
			"background", "{colors.primary}",
			"color", "{colors.primary-fg}",
			"border-radius", "{radii.sm}",
			"font-weight", "600",
			"font-family", "{fonts.body}").End()
	ss.Rule(".k-hero h1").
		Set("font-size", "clamp(44px, 6vw, 88px)",
			"line-height", "1.0",
			"letter-spacing", "-0.035em",
			"margin-bottom", "{spacing.xl}",
			"max-width", "14ch",
			"text-wrap", "balance").End()
	ss.Rule(".k-hero h1 .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".k-hero .lede").
		Set("color", "{colors.text-muted}", "font-size", "var(--t-lg)", "max-width", "56ch",
			"margin-bottom", "{spacing.md}").End()
	ss.Rule(".k-hero__ctas").
		Set("display", "flex", "gap", "{spacing.md}", "align-items", "center", "flex-wrap", "wrap",
			"margin-top", "{spacing.xl}").End()
	ss.Rule(".k-hero__cli").
		Set("padding", "10px 14px",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"font-family", "{fonts.mono}",
			"font-size", "var(--t-sm)",
			"color", "{colors.text}").End()
	ss.Rule(".k-hero__cli .p").Set("color", "{colors.primary}", "margin-right", "8px").End()

	ss.Rule(".k-demo").Set("padding", "{spacing.xxl} 0").End()
	ss.Rule(".k-demo__frame").
		Set("border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}",
			"overflow", "hidden",
			"background", "{colors.background}").End()
	ss.Rule(".k-demo__chrome").
		Set("display", "flex", "align-items", "center", "gap", "{spacing.md}",
			"padding", "10px 14px",
			"background", "{colors.surface}",
			"border-bottom", "1px solid {colors.border}").End()
	ss.Rule(".k-demo__chrome .dots").
		Set("display", "flex", "gap", "6px").End()
	ss.Rule(".k-demo__chrome .dots span").
		Set("width", "9px", "height", "9px",
			"border-radius", "999px",
			"background", "{colors.surface-soft}").End()
	ss.Rule(".k-demo__chrome .url").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"padding", "2px 10px",
			"background", "{colors.surface-soft}",
			"border-radius", "{radii.sm}").End()
	ss.Rule(".k-demo__body").
		Set("display", "grid",
			"grid-template-columns", "1fr 380px",
			"min-height", "440px").End()

	ss.Rule(".ghost").Set("padding", "{spacing.xl}").End()
	ss.Rule(".ghost h3").
		Set("margin-bottom", "{spacing.md}", "color", "{colors.text-muted}", "font-weight", "500").End()
	ss.Rule(".ghost-row").
		Set("height", "20px",
			"margin-bottom", "10px",
			"background", "{colors.surface}",
			"border-radius", "4px").End()
	ss.Rule(".ghost-row.s").Set("width", "60%").End()
	ss.Rule(".ghost-row.m").Set("width", "80%").End()

	ss.Rule(".kpanel").
		Set("background", "{colors.surface}",
			"border-left", "1px solid {colors.border}",
			"display", "flex", "flex-direction", "column").End()
	ss.Rule(".kpanel__head").
		Set("display", "flex", "align-items", "center", "gap", "8px",
			"padding", "10px 14px",
			"border-bottom", "1px solid var(--line-faint)",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".kpanel__head .dot").
		Set("width", "6px", "height", "6px",
			"border-radius", "999px",
			"background", "{colors.primary}",
			"box-shadow", "0 0 6px {colors.primary}").End()
	ss.Rule(".kpanel__head .session").Set("margin-left", "auto").End()
	ss.Rule(".kpanel__chat").
		Set("flex", "1",
			"padding", "{spacing.md} {spacing.lg}",
			"display", "flex",
			"flex-direction", "column",
			"gap", "{spacing.md}",
			"overflow-y", "auto").End()
	ss.Rule(".km").Set("display", "flex", "flex-direction", "column", "gap", "4px").End()
	ss.Rule(".km__who").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".km__who.agent").Set("color", "{colors.primary}").End()
	ss.Rule(".km__body").
		Set("font-size", "var(--t-sm)",
			"color", "{colors.text}",
			"line-height", "1.5").End()
	ss.Rule(".km__tool").
		Set("display", "inline-flex",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"padding", "2px 8px",
			"background", "color-mix(in oklch, {colors.primary} 12%, transparent)",
			"color", "{colors.primary}",
			"border-radius", "{radii.sm}",
			"margin-top", "4px").End()
	ss.Rule(".kpanel__plan").
		Set("padding", "{spacing.md} {spacing.lg}",
			"border-top", "1px solid var(--line-faint)",
			"background", "{colors.background}").End()
	ss.Rule(".kpanel__plan .lbl").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "6px").End()
	ss.Rule(".kpanel__plan .op").
		Set("font-family", "{fonts.mono}",
			"font-size", "12px",
			"padding", "3px 8px",
			"display", "block",
			"color", "{colors.text-muted}").End()
	ss.Rule(".kpanel__plan .op.add").Set("color", "var(--tk-str)").End()
	ss.Rule(".kpanel__plan .actions").
		Set("display", "flex", "gap", "8px", "margin-top", "{spacing.md}").End()
	ss.Rule(".kpanel__plan .actions button").
		Set("padding", "6px 14px",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.sm}",
			"font-family", "{fonts.body}",
			"font-size", "12px",
			"font-weight", "500").End()
	ss.Rule(".kpanel__plan .actions button.approve").
		Set("background", "{colors.primary}",
			"color", "{colors.primary-fg}",
			"border-color", "{colors.primary}").End()
	ss.Rule(".kpanel__plan .actions button.reject").
		Set("background", "transparent", "color", "{colors.text-muted}").End()
	ss.Rule(".kpanel__input").
		Set("padding", "{spacing.md}",
			"border-top", "1px solid var(--line-faint)",
			"display", "flex", "gap", "8px").End()
	ss.Rule(".kpanel__input input").
		Set("flex", "1",
			"padding", "8px 12px",
			"background", "{colors.background}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.sm}",
			"color", "{colors.text}",
			"font-family", "{fonts.body}",
			"font-size", "var(--t-sm)").End()

	// timeline + capabilities + cli sections
	ss.Rule(".timeline").Set("padding", "var(--s-9) 0",
		"border-top", "1px solid var(--line-faint)").End()
	ss.Rule(".timeline h2").
		Set("font-size", "var(--t-3xl)", "margin-bottom", "{spacing.xl}",
			"letter-spacing", "-0.025em", "max-width", "20ch").End()
	ss.Rule(".tl-rail").Set("display", "grid", "gap", "{spacing.md}").End()
	ss.Rule(".tl-evt").
		Set("display", "grid",
			"grid-template-columns", "70px 24px 1fr",
			"gap", "{spacing.md}",
			"padding", "{spacing.md} 0",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".tl-evt__t").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.text-subtle}").End()
	ss.Rule(".tl-evt__dot").Set("display", "grid", "place-items", "center", "padding-top", "4px").End()
	ss.Rule(".tl-evt__dot span").
		Set("width", "8px", "height", "8px",
			"border-radius", "999px",
			"background", "{colors.text-subtle}").End()
	ss.Rule(".tl-evt.tool .tl-evt__dot span").Set("background", "{colors.primary}").End()
	ss.Rule(".tl-evt.approve .tl-evt__dot span").Set("background", "var(--tk-str)").End()
	ss.Rule(".tl-evt p").
		Set("color", "{colors.text-muted}",
			"font-size", "13px",
			"margin-top", "2px").End()

	ss.Rule(".caps").
		Set("padding", "var(--s-9) 0", "border-top", "1px solid var(--line-faint)").End()
	ss.Rule(".caps__grid").
		Set("display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "{spacing.lg}",
			"margin-top", "{spacing.xl}").End()
	ss.Rule(".cap").
		Set("padding", "{spacing.xl}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.lg}").End()
	ss.Rule(".cap.can").Set("border-color", "color-mix(in oklch, var(--tk-str) 35%, {colors.border})").End()
	ss.Rule(".cap.cant").Set("border-color", "color-mix(in oklch, {colors.danger} 35%, {colors.border})").End()
	ss.Rule(".cap h3").Set("font-size", "var(--t-xl)", "margin-bottom", "{spacing.md}").End()
	ss.Rule(".cap.can h3").Set("color", "var(--tk-str)").End()
	ss.Rule(".cap.cant h3").Set("color", "{colors.danger}").End()
	ss.Rule(".cap ul li").
		Set("padding", "6px 0 6px 22px", "position", "relative",
			"color", "{colors.text-muted}", "font-size", "var(--t-sm)").End()
	ss.Rule(".cap.can ul li::before").
		Set("content", `"✓"`, "position", "absolute", "left", "0", "color", "var(--tk-str)").End()
	ss.Rule(".cap.cant ul li::before").
		Set("content", `"✗"`, "position", "absolute", "left", "0", "color", "{colors.danger}").End()

	ss.Rule(".cli-sect").
		Set("padding", "var(--s-9) 0", "border-top", "1px solid var(--line-faint)").End()
	ss.Rule(".cli-block").
		Set("display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "{spacing.lg}",
			"margin-top", "{spacing.xl}").End()
	ss.Rule(".cli-cmd").
		Set("border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"overflow", "hidden",
			"background", "{colors.background}").End()
	ss.Rule(".cli-cmd__head").
		Set("padding", "8px 14px",
			"background", "{colors.surface}",
			"border-bottom", "1px solid {colors.border}",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}").End()
	ss.Rule(".cli-cmd__body").
		Set("padding", "12px 14px",
			"font-family", "{fonts.mono}",
			"font-size", "12px",
			"line-height", "1.7",
			"white-space", "pre-wrap").End()
	ss.Rule(".cli-cmd__body .p").Set("color", "{colors.primary}").End()
	ss.Rule(".cli-cmd__body .o").Set("color", "{colors.text-subtle}").End()
	ss.Rule(".cli-cmd__body .ok").Set("color", "var(--tk-str)").End()
}

// -----------------------------------------------------------------------------
// /philosophy — long-form essay with magazine masthead.
// -----------------------------------------------------------------------------

func pagePhilosophy(ss *style.StyleSheet) {
	ss.Rule(".ph-hero").
		Set("padding", "var(--s-9) 0", "border-bottom", "1px solid {colors.border}").End()
	ss.Rule(".ph-hero__grid").
		Set("display", "grid",
			"grid-template-columns", "120px minmax(0, 1fr) 180px",
			"gap", "{spacing.xxxl}",
			"align-items", "start").End()
	ss.Rule(".ph-hero .meta").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"line-height", "1.7").End()
	ss.Rule(".ph-hero h1").
		Set("font-size", "clamp(40px, 5.4vw, 72px)",
			"line-height", "1.02",
			"letter-spacing", "-0.035em",
			"text-wrap", "balance").End()
	ss.Rule(".ph-hero h1 .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".ph-hero .by").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"line-height", "1.7",
			"text-align", "right").End()

	ss.Rule(".ph-body").
		Set("display", "grid",
			"grid-template-columns", "220px minmax(0, 1fr)",
			"gap", "{spacing.xxxl}",
			"padding", "var(--s-9) 0").End()
	ss.Rule(".ph-toc").
		Set("position", "sticky",
			"top", "calc(var(--nav-h) + {spacing.lg})",
			"align-self", "start",
			"font-size", "var(--t-sm)").End()
	ss.Rule(".ph-toc h6").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.md}",
			"font-weight", "400").End()
	ss.Rule(".ph-toc ol").Set("counter-reset", "phtoc", "display", "grid", "gap", "4px").End()
	ss.Rule(".ph-toc ol li").
		Set("counter-increment", "phtoc",
			"padding-left", "28px",
			"position", "relative").End()
	ss.Rule(".ph-toc ol li::before").
		Set("content", `counter(phtoc, decimal-leading-zero)`,
			"position", "absolute", "left", "0",
			"font-family", "{fonts.mono}", "font-size", "10px",
			"color", "var(--fg-4)").End()
	ss.Rule(".ph-toc ol li a").Set("color", "{colors.text-muted}").End()

	ss.Rule(".ph-article").Set("max-width", "720px").End()
	ss.Rule(".ph-article p").
		Set("color", "{colors.text-muted}",
			"font-size", "var(--t-md)",
			"line-height", "1.75",
			"margin-bottom", "{spacing.lg}").End()
	ss.Rule(".ph-article p.lede").
		Set("font-size", "var(--t-lg)",
			"color", "{colors.text}",
			"text-wrap", "balance",
			"margin-bottom", "{spacing.xl}").End()
	ss.Rule(".ph-article h2").
		Set("font-size", "var(--t-2xl)",
			"margin-top", "var(--s-8)",
			"margin-bottom", "{spacing.md}",
			"letter-spacing", "-0.02em").End()

	ss.Rule(".pullquote").
		Set("border-top", "1px solid {colors.primary}",
			"border-bottom", "1px solid {colors.primary}",
			"padding", "{spacing.xl} 0",
			"margin", "{spacing.xxl} 0",
			"font-size", "var(--t-xl)",
			"color", "{colors.text}",
			"line-height", "1.4",
			"position", "relative").End()
	ss.Rule(".pullquote::before").
		Set("content", `"“"`,
			"font-size", "60px",
			"color", "{colors.primary}",
			"position", "absolute",
			"left", "-32px", "top", "0",
			"line-height", "1").End()

	ss.Rule(".conv-list").
		Set("display", "grid", "gap", "0", "margin", "{spacing.lg} 0").End()
	ss.Rule(".conv").
		Set("display", "grid",
			"grid-template-columns", "44px 1fr",
			"gap", "{spacing.md}",
			"padding", "{spacing.md} 0",
			"border-top", "1px solid var(--line-faint)").End()
	ss.Rule(".conv .num").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.primary}").End()
	ss.Rule(".conv .title").Set("font-weight", "500", "color", "{colors.text}", "margin-bottom", "4px").End()
	ss.Rule(".conv .desc").Set("color", "{colors.text-muted}", "font-size", "var(--t-sm)").End()

	ss.Rule(".roadmap").
		Set("padding", "{spacing.xl}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"margin-top", "{spacing.lg}").End()
	ss.Rule(".roadmap h6").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.md}",
			"font-weight", "400").End()
	ss.Rule(".roadmap__row").
		Set("display", "grid",
			"grid-template-columns", "100px 1fr 80px",
			"gap", "{spacing.md}",
			"padding", "10px 0",
			"border-bottom", "1px dashed var(--line-faint)",
			"align-items", "center").End()
	ss.Rule(".roadmap__when").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.text-subtle}").End()
	ss.Rule(".roadmap__what").Set("color", "{colors.text}", "font-size", "var(--t-sm)").End()
	ss.Rule(".roadmap__status").
		Set("font-family", "{fonts.mono}", "font-size", "10px",
			"padding", "2px 8px", "border-radius", "999px",
			"text-align", "center").End()
	ss.Rule(".roadmap__status.shipped").
		Set("background", "color-mix(in oklch, var(--tk-str) 18%, transparent)",
			"color", "var(--tk-str)").End()
	ss.Rule(".roadmap__status.next").
		Set("background", "color-mix(in oklch, {colors.primary} 18%, transparent)",
			"color", "{colors.primary}").End()
	ss.Rule(".roadmap__status.later").
		Set("background", "{colors.surface-soft}", "color", "{colors.text-subtle}").End()

	ss.Rule(".biblio").
		Set("margin-top", "{spacing.xxl}",
			"padding-top", "{spacing.lg}",
			"border-top", "1px solid var(--line-faint)").End()
	ss.Rule(".biblio h6").
		Set("font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"margin-bottom", "{spacing.md}",
			"font-weight", "400").End()
	ss.Rule(".biblio dl").Set("display", "grid", "gap", "8px").End()
	ss.Rule(".biblio dt").
		Set("font-family", "{fonts.mono}", "font-size", "11px", "color", "{colors.primary}",
			"display", "inline-block", "margin-right", "8px").End()
	ss.Rule(".biblio dd").
		Set("color", "{colors.text-muted}", "font-size", "var(--t-sm)").End()
}

// -----------------------------------------------------------------------------
// /404 — router-trace miss page.
// -----------------------------------------------------------------------------

func pageNotFound(ss *style.StyleSheet) {
	ss.Rule(".nf-page main").
		Set("min-height", "calc(100vh - var(--nav-h))",
			"display", "flex",
			"align-items", "center",
			"justify-content", "center").End()
	ss.Rule(".nf").
		Set("display", "grid",
			"grid-template-columns", "1fr 1fr",
			"gap", "var(--s-8)",
			"max-width", "1120px",
			"padding", "0 {spacing.xxl}",
			"width", "100%").End()
	ss.Rule(".nf__num").
		Set("font-size", "clamp(120px, 18vw, 220px)",
			"line-height", "0.9",
			"color", "{colors.primary}",
			"letter-spacing", "-0.04em",
			"font-weight", "500").End()
	ss.Rule(".nf__num span").Set("color", "{colors.text}").End()
	ss.Rule(".nf__title").
		Set("font-size", "var(--t-2xl)",
			"margin", "{spacing.lg} 0 {spacing.md}").End()
	ss.Rule(".nf__title .amber").Set("color", "{colors.primary}").End()
	ss.Rule(".nf__lede").Set("color", "{colors.text-muted}", "max-width", "44ch").End()
	ss.Rule(".nf__path").
		Set("display", "inline-block",
			"margin-top", "{spacing.md}",
			"padding", "6px 12px",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.sm}",
			"font-family", "{fonts.mono}",
			"font-size", "var(--t-sm)").End()
	ss.Rule(".nf__path .u").Set("color", "{colors.primary}").End()

	ss.Rule(".nf__term").
		Set("border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"overflow", "hidden",
			"background", "{colors.surface}",
			"margin-bottom", "{spacing.md}").End()
	ss.Rule(".nf__term-head").
		Set("padding", "8px 12px",
			"border-bottom", "1px solid var(--line-faint)",
			"font-family", "{fonts.mono}",
			"font-size", "11px",
			"color", "{colors.text-subtle}",
			"display", "flex", "align-items", "center", "gap", "8px").End()
	ss.Rule(".nf__term-head .dot").
		Set("width", "6px", "height", "6px",
			"border-radius", "999px",
			"background", "{colors.primary}",
			"box-shadow", "0 0 8px {colors.primary}").End()
	ss.Rule(".nf__term-body").
		Set("padding", "12px",
			"font-family", "{fonts.mono}",
			"font-size", "12px",
			"line-height", "1.7",
			"white-space", "pre-wrap").End()
	ss.Rule(".nf__term-body .p").Set("color", "{colors.primary}").End()
	ss.Rule(".nf__term-body .e").Set("color", "{colors.danger}").End()
	ss.Rule(".nf__term-body .o").Set("color", "{colors.text-subtle}").End()
	ss.Rule(".nf__term-body .ok").Set("color", "var(--tk-str)").End()

	ss.Rule(".nf__suggest").
		Set("padding", "{spacing.lg}",
			"background", "{colors.surface}",
			"border", "1px solid {colors.border}",
			"border-radius", "{radii.md}",
			"margin-bottom", "{spacing.md}").End()
	ss.Rule(".nf__suggest h6").
		Set("font-family", "{fonts.mono}", "font-size", "11px",
			"color", "{colors.text-subtle}", "margin-bottom", "{spacing.md}", "font-weight", "400").End()
	ss.Rule(".nf__suggest li a").
		Set("display", "flex", "justify-content", "space-between",
			"padding", "8px 0",
			"color", "{colors.text}",
			"border-bottom", "1px solid var(--line-faint)").End()
	ss.Rule(".nf__suggest li:last-child a").Set("border-bottom", "0").End()
	ss.Rule(".nf__suggest .arrow").Set("color", "{colors.primary}").End()
}
