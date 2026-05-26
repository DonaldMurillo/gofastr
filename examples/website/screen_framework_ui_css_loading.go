package main

import (
	"net/http"
	"sort"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ─── Demo-only registered components ────────────────────────────────────
//
// Two styled components registered solely for the css-loading demo so the
// page has live examples of each load mode without polluting the rest of
// the website. `demo-fancy-card` is LoadAuto — its CSS is not in the SSR
// bundle until a button reveals it; the runtime loads the sheet on first
// appearance via the marker scan. `demo-command-palette` is LoadPrewarm —
// the runtime fetches it on an idle callback after first paint, so it's
// already linked when the user clicks "show palette" later.

var fancyCardStyle = registry.RegisterStyle("demo-fancy-card", fancyCardCSS)

func fancyCardCSS(t style.Theme) string {
	return style.NewComponentSheet("demo-fancy-card", t).
		Rule("&").Set(
		"display", "block",
		"padding", "{spacing.xl}",
		"border-radius", "{radii.lg}",
		"background", "linear-gradient(135deg, {colors.primary} 0%, {colors.info} 100%)",
		"color", "white",
		"box-shadow", "0 8px 24px rgba(79,70,229,0.25)",
	).End().
		Rule(".title").Set(
		"margin", "0 0 {spacing.sm} 0",
		"font-size", "1.25rem",
		"font-weight", "700",
	).End().
		Rule(".body").Set(
		"margin", "0",
		"opacity", "0.92",
		"font-size", "0.95rem",
		"line-height", "1.45",
	).End().
		MustBuild()
}

var commandPaletteStyle = registry.RegisterStyle("demo-command-palette", commandPaletteCSS,
	registry.WithLoad(registry.LoadPrewarm))

func commandPaletteCSS(t style.Theme) string {
	return style.NewComponentSheet("demo-command-palette", t).
		Rule("&").Set(
		"display", "grid",
		"gap", "{spacing.sm}",
		"padding", "{spacing.lg}",
		"border", "2px dashed {colors.primary}",
		"border-radius", "{radii.md}",
		"background", "{colors.background}",
		"font-family", "{fonts.mono}",
	).End().
		Rule(".row").Set(
		"display", "grid",
		"grid-template-columns", "1fr auto",
		"gap", "{spacing.md}",
		"padding", "{spacing.sm} {spacing.md}",
		"border-radius", "{radii.sm}",
		"background", "{colors.surface}",
		"font-size", "0.9rem",
	).End().
		Rule(".key").Set(
		"font-weight", "600",
		"color", "{colors.text-muted}",
	).End().
		MustBuild()
}

// renderFancyCard / renderPalette emit the demo components. Both go
// through the registry handle so the data-fui-comp marker is injected
// onto the outermost tag — that's what the runtime scans for.
func renderFancyCard() render.HTML {
	return fancyCardStyle.WrapHTML(render.HTML(
		`<div>` +
			`<p class="title">Fancy Card (LoadAuto)</p>` +
			`<p class="body">This card's CSS was not in the SSR bundle. The runtime saw the marker, ` +
			`fetched <code>/__gofastr/comp/demo-fancy-card.css</code>, and applied it. Check the ` +
			`Network tab — it fires exactly once per session.</p>` +
			`</div>`))
}

func renderCommandPalette() render.HTML {
	return commandPaletteStyle.WrapHTML(render.HTML(
		`<div>` +
			`<div class="row"><span>Open recent file</span><span class="key">⌘ P</span></div>` +
			`<div class="row"><span>Run task</span><span class="key">⌘ ⇧ P</span></div>` +
			`<div class="row"><span>Toggle terminal</span><span class="key">⌃ \</span></div>` +
			`</div>`))
}

// ─── Screen ──────────────────────────────────────────────────────────────

type CSSLoadingDemoScreen struct{}

func (s *CSSLoadingDemoScreen) ScreenTitle() string        { return "Component CSS Loading" }
func (s *CSSLoadingDemoScreen) ScreenDescription() string  { return "How per-component stylesheets ship, load, and dedup." }
func (s *CSSLoadingDemoScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *CSSLoadingDemoScreen) Render() render.HTML {
	header := ui.PageHeader(ui.PageHeaderConfig{
		Eyebrow: "framework/ui",
		Title:   "Component CSS loading",
		Subtitle: "Components ship CSS the way they ship HTML — per-render, deduped, content-addressed. " +
			"No global stylesheet, no FOUC, no manual imports.",
		Actions: html.LinkHTML(html.LinkHTMLConfig{
			Href:    "/framework-ui/",
			Class:   "ui-button",
			Content: render.Text("← Framework UI"),
		}),
	})

	how := ui.Section(ui.SectionConfig{
		Heading:     "How it works",
		Description: `The framework injects data-fui-comp="<name>" on a registered component's outermost tag. On SSR, it scans the rendered HTML and emits one bundled <link> for the page's components. After hydration, the runtime scans any newly-inserted DOM and lazy-loads sheets it hasn't seen — dedup'd by <link data-fui-style="<name>">.`,
		ID:          "how-it-works",
	},
		ui.Callout(ui.CalloutConfig{Title: "Want to see it work? Open DevTools first.", Variant: ui.StatusInfo},
			render.Text("DevTools → Network → filter ‘comp’. Reload this page: you'll see one /__gofastr/comp-bundle.css?names=… on first paint, plus a single /__gofastr/comp/demo-command-palette.css fired on idle (LoadPrewarm). The live demos below add one more request on the first click of each — and zero on the second."),
		),
	)

	modes := ui.Section(ui.SectionConfig{
		Heading:     "Three load modes",
		Description: "Picked per component at registration time. All three share a sync dedup guard — a component can't double-load even if the modes overlap.",
		ID:          "load-modes",
	},
		renderLoadModesTable(),
	)

	revealCardBtn := html.Button(html.ButtonConfig{
		Label: "Reveal fancy card",
		Class: "ui-button",
		ExtraAttrs: html.Attrs{
			"data-fui-rpc":                "/islands/css-demo/reveal-card",
			"data-fui-rpc-method":         "POST",
			"data-fui-rpc-signal":         "css-demo-card-slot",
			"data-fui-rpc-after-text":     "Revealed ✓ (dedup short-circuit on re-click)",
			"data-fui-rpc-after-disable":  "",
			"data-fui-rpc-scroll-to":      "#live-load-auto .demo-css-card-slot",
		},
	})

	live := ui.Section(ui.SectionConfig{
		Heading:     "Live: reveal a LoadAuto component",
		Description: "demo-fancy-card is LoadAuto and is NOT rendered server-side on this page, so its CSS isn't in the SSR bundle. Click → island RPC returns HTML with the marker → runtime fetches /__gofastr/comp/demo-fancy-card.css → card paints. Click again — silent no-op because the dedup guard short-circuits.",
		ID:          "live-load-auto",
	},
		render.Tag("p", map[string]string{"class": "demo-watch-hint"},
			render.Text("Watch for: "),
			render.Tag("code", nil, render.Text("/__gofastr/comp/demo-fancy-card.css")),
		),
		revealCardBtn,
		render.Tag("div", map[string]string{
			"data-fui-signal":      "css-demo-card-slot",
			"data-fui-signal-mode": "html",
			"class":                "demo-css-card-slot",
		}),
	)

	revealPaletteBtn := html.Button(html.ButtonConfig{
		Label: "Reveal command palette",
		Class: "ui-button",
		ExtraAttrs: html.Attrs{
			"data-fui-rpc":                "/islands/css-demo/reveal-palette",
			"data-fui-rpc-method":         "POST",
			"data-fui-rpc-signal":         "css-demo-palette-slot",
			"data-fui-rpc-after-text":     "Revealed ✓ (palette CSS was already loaded)",
			"data-fui-rpc-after-disable":  "",
			"data-fui-rpc-scroll-to":      "#live-load-prewarm .demo-css-card-slot",
		},
	})

	palette := ui.Section(ui.SectionConfig{
		Heading:     "Live: LoadPrewarm — already loaded",
		Description: "demo-command-palette is LoadPrewarm: its CSS was idle-fetched right after first paint, even though the palette is NOT rendered on this page. Click reveal → styles apply instantly. No new network request — the link is already attached.",
		ID:          "live-load-prewarm",
	},
		render.Tag("p", map[string]string{"class": "demo-watch-hint"},
			render.Text("Already loaded: "),
			render.Tag("code", nil, render.Text("/__gofastr/comp/demo-command-palette.css")),
			render.Text(" (fired on idle, before you got here)"),
		),
		revealPaletteBtn,
		render.Tag("div", map[string]string{
			"data-fui-signal":      "css-demo-palette-slot",
			"data-fui-signal-mode": "html",
			"class":                "demo-css-card-slot",
		}),
	)

	catalog := ui.Section(ui.SectionConfig{
		Heading:     "Registered components on this server",
		Description: "Every registry.RegisterStyle call surfaces here. Names, load modes, and the content-addressed version pulled from the live catalog. (Scroll horizontally on narrow screens — content stays readable.)",
		ID:          "catalog",
	},
		render.Tag("div", map[string]string{"class": "demo-table-scroll"},
			renderCatalogTable(),
		),
	)

	howToRegister := ui.Section(ui.SectionConfig{
		Heading:     "Adding your own styled component",
		Description: "RegisterStyle returns a *Style handle; keep it in a package var and call .WrapHTML at every render site. The framework injects the marker, scopes the CSS, hashes the bytes for cache-busting, and the runtime takes over.",
		ID:          "how-to-register",
	},
		ui.CodeBlock(ui.CodeBlockConfig{
			Language: "go",
			Code: `// modal/modal.go — register at package init
var Style = registry.RegisterStyle(
    "ui-modal", modalCSS,
    registry.WithLoad(registry.LoadAuto),
)

func modalCSS(t style.Theme) string {
    return style.NewComponentSheet("ui-modal", t).
        Rule("&").Set("display", "flex").End().
        Rule(".header").Set("font-weight", "700").End().
        Rule(".body").Set("padding", "{spacing.lg}").End().
        MustBuild()
}

// at any render site
func (s *Screen) Render() render.HTML {
    return Style.WrapHTML(html.Div(html.DivConfig{Class: "modal"}, ...))
}`,
		}),
	)

	return render.Join(header, how, modes, live, palette, catalog, howToRegister)
}

// renderLoadModesTable replaces the three-callout stack with a
// comparison table — the trade-offs land in one glance instead of
// three paragraphs.
func renderLoadModesTable() render.HTML {
	return ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{
			{Key: "mode", Header: "Mode"},
			{Key: "when", Header: "When loaded"},
			{Key: "use", Header: "Use for"},
			{Key: "cost", Header: "First-paint cost"},
		},
		Rows: []ui.Row{
			{Cells: map[string]render.HTML{
				"mode": render.HTML(`<code>LoadAuto</code> <small>(default)</small>`),
				"when": render.Text("Marker first hits the DOM (SSR-on-page or post-hydration scan)."),
				"use":  render.Text("Anything used on some pages but not all — the typical case."),
				"cost": render.Text("Zero on pages that don't render it."),
			}},
			{Cells: map[string]render.HTML{
				"mode": render.HTML(`<code>LoadPrewarm</code>`),
				"when": render.Text("LoadAuto + idle prefetch right after first paint."),
				"use":  render.Text("Likely-soon components: hotkey palette, confirm modal."),
				"cost": render.Text("Zero on first paint; one idle-time HTTP request after."),
			}},
			{Cells: map[string]render.HTML{
				"mode": render.HTML(`<code>LoadAlways</code>`),
				"when": render.Text("SSR emits <link> in <head> on every page, used or not."),
				"use":  render.Text("Chrome on essentially every screen (PageHeader)."),
				"cost": render.Text("One render-blocking link on every page."),
			}},
		},
	})
}

// renderCatalogTable produces a server-side snapshot of every
// registered component, sorted by name. This is intentionally
// rendered through framework/ui components so the page also dogfoods
// the very system it's documenting (DataTable, StatusBadge).
func renderCatalogTable() render.HTML {
	entries := registry.All()
	if len(entries) == 0 {
		return ui.EmptyState(ui.EmptyStateConfig{
			Title:       "No components registered",
			Description: "Surprising — at least PageHeader should be here.",
		})
	}
	rows := make([]ui.Row, 0, len(entries))
	for _, e := range entries {
		mode := loadModeLabel(e.Load)
		variant := loadModeVariant(e.Load)
		rows = append(rows, ui.Row{
			Cells: map[string]render.HTML{
				"name": render.HTML(`<code>` + e.Name + `</code>`),
				"mode": ui.StatusBadge(ui.StatusBadgeConfig{Label: mode, Variant: variant}),
				"url":  render.HTML(`<code>/__gofastr/comp/` + e.Name + `.css</code>`),
			},
		})
	}
	return ui.DataTable(ui.DataTableConfig{
		Caption: "Catalog (registry.All)",
		Columns: []ui.Column{
			{Key: "name", Header: "Name"},
			{Key: "mode", Header: "Load mode"},
			{Key: "url", Header: "URL"},
		},
		Rows: rows,
	})
}

func loadModeLabel(m registry.LoadMode) string {
	switch m {
	case registry.LoadAlways:
		return "LoadAlways"
	case registry.LoadPrewarm:
		return "LoadPrewarm"
	default:
		return "LoadAuto"
	}
}

func loadModeVariant(m registry.LoadMode) ui.StatusVariant {
	switch m {
	case registry.LoadAlways:
		return ui.StatusWarning
	case registry.LoadPrewarm:
		return ui.StatusInfo
	default:
		return ui.StatusNeutral
	}
}

// ─── Island RPC handlers ────────────────────────────────────────────────
//
// Each handler returns the rendered HTML for the demo component as the
// body. The runtime treats it as the new value of the named signal,
// which has data-fui-signal-mode="html" → innerHTML swap → scanAndLoadCSS
// fires automatically because of the marker.

func CSSLoadingRevealCardHandler(w http.ResponseWriter, _ *http.Request) {
	render.RespondHTML(w, renderFancyCard())
}

func CSSLoadingRevealPaletteHandler(w http.ResponseWriter, _ *http.Request) {
	render.RespondHTML(w, renderCommandPalette())
}

// catalogSortedNames is used by tests to assert the table renders
// every registered entry in stable order.
func catalogSortedNames() []string {
	entries := registry.All()
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

