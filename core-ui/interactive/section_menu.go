package interactive

import (
	"context"
	_ "embed"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

//go:embed section_menu.js
var sectionMenuJS string

var _ = registry.RegisterBehavior("sectionmenu", sectionMenuJS,
	registry.Markers(`[data-cui-comp="cui-section-menu"]`),
	registry.Requires("activelink"))

// SectionItem is one navigable link in a SectionMenu group.
type SectionItem struct {
	Label  string
	Href   string
	Active bool // marks the current page (aria-current="page" + .is-active)
}

// SectionGroup is a labelled, collapsible cluster of items.
type SectionGroup struct {
	Label   string
	Eyebrow string // optional ordinal/kicker shown before the label (e.g. "01")
	Items   []SectionItem
	// Collapsed starts the group closed in the sheet unless it holds the active item.
	Collapsed bool
}

// SectionMenuConfig configures a SectionMenu and its sheet.
type SectionMenuConfig struct {
	// Lead is an optional ungrouped item above the groups.
	Lead   *SectionItem
	Groups []SectionGroup
	// AriaLabel names the nav landmark (e.g. "Documentation sections").
	AriaLabel string
	// TriggerLabel names the pill and titles the sheet; empty is the localized "Menu".
	TriggerLabel string
	// DrawerName is the widget the pill opens and SectionMenuDrawer mounts; unique per menu.
	DrawerName string
	// Filter adds a box that filters the menu's links in the browser.
	Filter bool
	// FilterPlaceholder is the filter box's placeholder and name; empty is the localized "Filter".
	FilterPlaceholder string
	// FilterEmpty is shown when nothing matches; empty is the localized "No matches".
	FilterEmpty string
	// InlineTrigger keeps the pill in flow instead of floating it.
	InlineTrigger bool
	// FillColumn fills a column its host already pins, scrolling its overflow there.
	FillColumn bool
	// SwapAt is the width below which the rail gives way to the pill and sheet.
	SwapAt SwapWidth
	Class  string
	ID     string
	// Ctx is the request context the default words are written in; nil is English.
	Ctx context.Context
}

// SectionMenu renders the desktop rail plus the mobile trigger pill.
func SectionMenu(cfg SectionMenuConfig) render.HTML {
	aria := cfg.AriaLabel
	if aria == "" {
		aria = sectionsLabel.Text(cfg.Ctx)
	}

	children := []render.HTML{}
	if cfg.DrawerName != "" {
		children = append(children, sectionMenuTrigger(cfg))
	}
	rail := []render.HTML{}
	if cfg.Filter {
		rail = append(rail, sectionMenuFilter(cfg))
	}
	rail = append(rail, sectionMenuBody(cfg, true))
	children = append(children, html.Div(html.DivConfig{Class: "cui-section-menu__rail"}, rail...))

	cls := "cui-section-menu"
	if cfg.FillColumn {
		cls += " cui-section-menu--fill"
	}
	cls += swapClass("SectionMenu", "cui-section-menu", cfg.SwapAt)
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return sectionMenuStyle.WrapHTML(render.Tag("nav",
		mapWith(map[string]string{"class": cls, "aria-label": aria}, "id", cfg.ID),
		children...,
	))
}

func sectionMenuTrigger(cfg SectionMenuConfig) render.HTML {
	pill := SheetPillConfig{Name: triggerLabel(cfg), Opens: cfg.DrawerName, Inline: cfg.InlineTrigger, SwapAt: cfg.SwapAt}
	if it, g, ok := activeItem(cfg); ok {
		pill.Label, pill.Detail = it.Label, g
	}
	return SheetPill(pill)
}

func triggerLabel(cfg SectionMenuConfig) string {
	if cfg.TriggerLabel == "" {
		return menuLabel.Text(cfg.Ctx)
	}
	return cfg.TriggerLabel
}

func activeItem(cfg SectionMenuConfig) (SectionItem, string, bool) {
	if cfg.Lead != nil && cfg.Lead.Active {
		return *cfg.Lead, "", true
	}
	for _, g := range cfg.Groups {
		for _, it := range g.Items {
			if it.Active {
				return it, g.Label, true
			}
		}
	}
	return SectionItem{}, "", false
}

func sectionMenuFilter(cfg SectionMenuConfig) render.HTML {
	ph := cfg.FilterPlaceholder
	if ph == "" {
		ph = filterLabel.Text(cfg.Ctx)
	}
	return html.Div(html.DivConfig{Class: "cui-section-menu__filter"},
		render.Raw(`<svg class="cui-section-menu__filter-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="11" cy="11" r="7"/><line x1="20" y1="20" x2="16.5" y2="16.5"/></svg>`),
		render.VoidTag("input", map[string]string{
			"class":        "cui-section-menu__filter-input",
			"type":         "search",
			"placeholder":  ph,
			"aria-label":   ph,
			"autocomplete": "off",
			"spellcheck":   "false",
			"enterkeyhint": "search",
		}),
	)
}

// SectionMenuDrawer returns the mobile sheet widget for a SectionMenu, a preset.BottomSheet
// (backdrop + click-outside / Escape / drag-down close + scroll lock + focus trap) carrying the
// same menu body.
func SectionMenuDrawer(cfg SectionMenuConfig) *widget.Builder {
	return sectionMenuDrawer(cfg, nil)
}

// SectionMenuDrawerFunc is SectionMenuDrawer for a menu whose items change after startup: build
// runs on every render of the sheet, with the request's context, so the sheet lists what the rail
// beside it lists on the same request.
func SectionMenuDrawerFunc(build func(ctx context.Context) SectionMenuConfig) *widget.Builder {
	if build == nil {
		panic("interactive: SectionMenuDrawerFunc: build is nil")
	}
	return sectionMenuDrawer(build(context.Background()), build)
}

func sectionMenuDrawer(cfg SectionMenuConfig, build func(context.Context) SectionMenuConfig) *widget.Builder {
	if cfg.DrawerName == "" {
		panic("interactive: SectionMenuDrawer requires cfg.DrawerName")
	}
	return preset.BottomSheet(cfg.DrawerName).
		Hidden().
		LabelledBy(sheetTitleID(cfg)).
		Slot("body", sectionMenuDrawerSlot{cfg: cfg, resolve: build})
}

func sheetTitleID(cfg SectionMenuConfig) string { return cfg.DrawerName + "-title" }

type sectionMenuDrawerSlot struct {
	cfg     SectionMenuConfig
	resolve func(context.Context) SectionMenuConfig
}

func (s sectionMenuDrawerSlot) Render() render.HTML { return s.RenderCtx(s.cfg.Ctx) }

// RenderCtx renders the sheet in the locale of the request showing it: the slot is mounted once, so
// the context it was built with is no reader's.
func (s sectionMenuDrawerSlot) RenderCtx(ctx context.Context) render.HTML {
	if s.resolve != nil {
		rctx := ctx
		if rctx == nil {
			rctx = context.Background()
		}
		name := s.cfg.DrawerName
		s.cfg = s.resolve(rctx)
		s.cfg.DrawerName = name
	}
	if ctx != nil {
		s.cfg.Ctx = ctx
	}
	top := []render.HTML{sheetHead(s.cfg.Ctx, triggerLabel(s.cfg), sheetTitleID(s.cfg),
		map[string]string{"data-cui-action": "close"})}
	cls := "cui-section-menu cui-section-menu--drawer"
	if s.cfg.Filter {
		top = append(top, sectionMenuFilter(s.cfg))
		cls += " cui-section-menu--filter"
	}
	aria := s.cfg.AriaLabel
	if aria == "" {
		aria = sectionsLabel.Text(s.cfg.Ctx)
	}
	return sectionMenuStyle.WrapHTML(render.Tag("nav",
		map[string]string{"class": cls, "aria-label": aria},
		html.Div(html.DivConfig{Class: "cui-section-menu__sheet-top"}, top...),
		sectionMenuBody(s.cfg, false),
	))
}

var _ component.Component = sectionMenuDrawerSlot{}

func sectionMenuBody(cfg SectionMenuConfig, rail bool) render.HTML {
	children := []render.HTML{}
	if cfg.Lead != nil {
		children = append(children, sectionMenuLink(*cfg.Lead, "cui-section-menu__lead"))
	}
	for _, g := range cfg.Groups {
		children = append(children, sectionMenuGroup(cfg.Ctx, g, rail))
	}
	if cfg.Filter {
		empty := cfg.FilterEmpty
		if empty == "" {
			empty = noMatches.Text(cfg.Ctx)
		}
		children = append(children, render.Tag("p", map[string]string{
			"class": "cui-section-menu__empty", "role": "status", "hidden": "",
		}, render.Text(empty)))
	}
	return html.Div(html.DivConfig{Class: "cui-section-menu__body"}, children...)
}

func sectionMenuGroup(ctx context.Context, g SectionGroup, rail bool) render.HTML {
	items := make([]render.HTML, 0, len(g.Items))
	hasActive := false
	for _, it := range g.Items {
		if it.Active {
			hasActive = true
		}
		items = append(items, html.ListItem(html.ListItemConfig{Class: "cui-section-menu__item"},
			sectionMenuLink(it, "cui-section-menu__link")))
	}

	labelChildren := []render.HTML{}
	if g.Eyebrow != "" {
		labelChildren = append(labelChildren,
			html.Span(html.TextConfig{Class: "cui-section-menu__eyebrow"}, render.Text(g.Eyebrow)))
	}
	labelChildren = append(labelChildren,
		html.Span(html.TextConfig{Class: "cui-section-menu__group-label"}, render.Text(g.Label)),
		html.Span(html.TextConfig{Class: "cui-section-menu__count"}, render.Text(i18n.Number(ctx, len(g.Items)))))
	list := html.UnorderedList(html.ListConfig{Class: "cui-section-menu__list"}, items...)

	if rail {
		return render.Tag("div", map[string]string{
			"class": "cui-section-menu__group", "role": "group", "aria-label": g.Label,
		}, html.Div(html.DivConfig{Class: "cui-section-menu__group-summary"}, labelChildren...), list)
	}

	labelChildren = append(labelChildren,
		render.Raw(`<svg class="cui-section-menu__chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="6 9 12 15 18 9"/></svg>`))

	attrs := map[string]string{
		"class":               "cui-section-menu__group",
		"data-hui-disclosure": "",
	}
	if !g.Collapsed || hasActive {
		attrs["open"] = ""
	}
	return render.Tag("details", attrs,
		render.Tag("summary", map[string]string{"class": "cui-section-menu__group-summary"}, labelChildren...),
		list,
	)
}

func sectionMenuLink(it SectionItem, cls string) render.HTML {
	attrs := html.Attrs{}
	c := cls
	if it.Active {
		c += " is-active"
		attrs["aria-current"] = "page"
	}
	return html.Link(html.LinkConfig{Href: it.Href, Text: it.Label, Class: c, ExtraAttrs: attrs})
}

func mapWith(m map[string]string, k, v string) map[string]string {
	if v != "" {
		m[k] = v
	}
	return m
}

var sectionMenuStyle = registry.RegisterStyle("cui-section-menu", sectionMenuCSS)

func sectionMenuCSS(_ style.Theme) string {
	return `[data-cui-comp="cui-section-menu"] {
  --cui-section-menu-inset: var(--ui-section-menu-inset, calc(var(--spacing-sm, 4px) * 3));
  --cui-section-menu-bg: var(--ui-section-menu-bg, var(--color-background, #fff));
  --cui-section-menu-dot-ring: color-mix(in oklab, var(--color-accent, #2563EB) 20%, transparent);
  display: block;
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="cui-section-menu"] [hidden]:not([hidden="until-found" i]) { display: none !important; }

/* ── Body / groups / links (shared by the rail and the sheet) ─────── */
[data-cui-comp="cui-section-menu"] .cui-section-menu__body { display: block; }
/* Links (and the lead, which reads as one) start 14px in from their
   group's header: the gap holds the current page's dot. */
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead,
[data-cui-comp="cui-section-menu"] .cui-section-menu__link {
  position: relative;
  display: block;
  padding-block: calc(var(--spacing-sm, 4px) * 1.25);
  padding-inline: calc(var(--cui-section-menu-inset) + 14px) var(--cui-section-menu-inset);
  color: var(--color-text-muted, #52525B);
  line-height: var(--leading-snug, 1.4);
  text-decoration: none;
  border-radius: var(--radii-sm, 6px);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead { margin-bottom: var(--spacing-sm, 4px); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead:hover,
[data-cui-comp="cui-section-menu"] .cui-section-menu__link:hover { color: var(--color-text, currentColor); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead.is-active,
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead[aria-current="page"],
[data-cui-comp="cui-section-menu"] .cui-section-menu__link.is-active,
[data-cui-comp="cui-section-menu"] .cui-section-menu__link[aria-current="page"] {
  color: var(--color-text, #09090B);
  font-weight: var(--font-weight-semibold, 600);
}
/* The current page's dot sits in the gap before its label, on the
   inline-start side, so a right-to-left rail draws it on the right. */
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead[aria-current="page"]::before,
[data-cui-comp="cui-section-menu"] .cui-section-menu__link[aria-current="page"]::before {
  content: "";
  position: absolute;
  inset-inline-start: calc(var(--cui-section-menu-inset) + 1px);
  inset-block-start: 50%;
  inline-size: var(--ui-section-menu-dot, 6px);
  block-size: var(--ui-section-menu-dot, 6px);
  margin-block-start: calc(var(--ui-section-menu-dot, 6px) / -2);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-accent, #2563EB);
  box-shadow: 0 0 0 calc(var(--stroke-thin, 1px) * 3) var(--cui-section-menu-dot-ring);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__group { border: 0; margin: 0; }
[data-cui-comp="cui-section-menu"] .cui-section-menu__group + .cui-section-menu__group { margin-block-start: var(--spacing-sm, 4px); }
/* The group header pins to the top of whatever scrolls the menu (the
   rail's body, the sheet) while its own links pass under it, and lets
   go when the group ends: a sticky header is held inside its group. */
[data-cui-comp="cui-section-menu"] .cui-section-menu__group-summary {
  position: sticky;
  inset-block-start: var(--cui-section-menu-head-top, 0px);
  z-index: 1;
  display: flex;
  align-items: baseline;
  gap: calc(var(--spacing-sm, 4px) * 1.5);
  cursor: pointer;
  list-style: none;
  padding: calc(var(--spacing-sm, 4px) * 3) var(--cui-section-menu-inset) calc(var(--spacing-sm, 4px) * 1.5);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-semibold, 600);
  letter-spacing: var(--tracking-wider, 0.08em);
  text-transform: uppercase;
  color: var(--color-text-muted, #52525B);
  background: color-mix(in oklab, var(--cui-section-menu-bg) 90%, transparent);
  -webkit-backdrop-filter: blur(6px);
  backdrop-filter: blur(6px);
  user-select: none;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__group-summary::-webkit-details-marker { display: none; }
[data-cui-comp="cui-section-menu"] summary.cui-section-menu__group-summary:hover { color: var(--color-text, currentColor); }
/* A rail header is a label, not a control. */
[data-cui-comp="cui-section-menu"] div.cui-section-menu__group-summary { cursor: default; }
/* The group holding the current page names itself in the accent. */
[data-cui-comp="cui-section-menu"] .cui-section-menu__group:has(.cui-section-menu__link[aria-current="page"]) > .cui-section-menu__group-summary { color: var(--color-accent, #2563EB); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__eyebrow { color: var(--fui-section-menu-eyebrow-color, var(--color-text-subtle, #71717A)); font-family: var(--font-mono, ui-monospace, monospace); letter-spacing: 0; }
[data-cui-comp="cui-section-menu"] .cui-section-menu__group-label { flex: 1; min-width: 0; }
[data-cui-comp="cui-section-menu"] .cui-section-menu__count {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-weight: var(--font-weight-normal, 400);
  letter-spacing: 0;
  color: var(--color-text-subtle, #71717A);
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__chevron {
  align-self: center;
  transition: transform var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
  opacity: var(--ui-section-menu-chevron-opacity, 0.7);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__group[open] > .cui-section-menu__group-summary .cui-section-menu__chevron {
  transform: rotate(180deg);
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="cui-section-menu"] .cui-section-menu__chevron { transition: none; }
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__list {
  list-style: none;
  margin: 0;
  padding: 0 0 var(--spacing-sm, 4px);
}

/* ── Filter ───────────────────────────────────────────────────────── */
/* The input is the box, with the icon laid over its start, so the
   focused element draws its own ring. */
[data-cui-comp="cui-section-menu"] .cui-section-menu__filter {
  position: relative;
  display: flex;
  margin-inline: calc(var(--cui-section-menu-inset) - 4px);
  color: var(--color-text-subtle, #71717A);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__filter-icon {
  position: absolute;
  inset-inline-start: calc(var(--spacing-sm, 4px) * 2.5);
  inset-block-start: 50%;
  translate: 0 -50%;
  pointer-events: none;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__filter-input {
  flex: 1;
  min-width: 0;
  box-sizing: border-box;
  block-size: calc(var(--spacing-sm, 4px) * 9);
  padding-inline: calc(var(--spacing-sm, 4px) * 8) var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-background, #fff);
  color: var(--color-text, currentColor);
  font: inherit;
  outline: none;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__filter-input:focus-visible {
  border-color: var(--color-accent, #2563EB);
  box-shadow: 0 0 0 calc(var(--stroke-thin, 1px) * 3) var(--cui-section-menu-dot-ring);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__filter-input::placeholder { color: var(--color-text-subtle, #71717A); opacity: 1; }
[data-cui-comp="cui-section-menu"] .cui-section-menu__mark {
  background: color-mix(in oklab, var(--color-accent, #2563EB) 16%, transparent);
  color: inherit;
  border-radius: calc(var(--radii-sm, 6px) / 3);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__empty {
  margin: 0;
  padding: var(--spacing-md, 8px) var(--cui-section-menu-inset);
  color: var(--color-text-muted, #52525B);
}

/* ── Sheet: a top bar (title, close, filter) pinned over the groups,
      whose headers pin under it. The bar's height is fixed so the
      headers know where to stop: the title row, plus the filter box
      and its gap when there is one. ─────────────────────────────── */
[data-cui-comp="cui-section-menu"].cui-section-menu--drawer {
  --cui-section-menu-head-top: calc(var(--ui-sheet-close-size, 36px) + var(--spacing-md, 8px));
  --cui-section-menu-bg: var(--ui-section-menu-bg, var(--color-surface, #fff));
}
[data-cui-comp="cui-section-menu"].cui-section-menu--drawer.cui-section-menu--filter { --cui-section-menu-head-top: calc(var(--ui-sheet-close-size, 36px) + var(--spacing-sm, 4px) * 9 + var(--spacing-md, 8px) * 2); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__sheet-top {
  position: sticky;
  inset-block-start: 0;
  z-index: 2;
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
  box-sizing: border-box;
  block-size: var(--cui-section-menu-head-top);
  padding-block-end: var(--spacing-md, 8px);
  background: var(--cui-section-menu-bg);
  --ui-sheet-head-inset: var(--cui-section-menu-inset);
}

/* ── Desktop rail (at or above the swap width; SwapAt picks it, 900px
      by default; the pill hides itself there): a sticky column with every
      group expanded. The filter stays put above a body that scrolls,
      so the group headers pin to the body's top. The sheet is never
      opened there. Generated per swap width by sectionMenuSwap.css. ── */` + sectionMenuSwapCSS()
}

func sectionMenuSwapCSS() string {
	var b strings.Builder
	for _, w := range swapWidths {
		root := swapScope(`[data-cui-comp="cui-section-menu"]`, "cui-section-menu", w.w)
		b.WriteString(sectionMenuSwap{root, w.maxw, w.minw}.css())
	}
	return b.String()
}

type sectionMenuSwap struct{ root, maxw, minw string }

func (w sectionMenuSwap) css() string {
	return strings.NewReplacer("ROOT", w.root, "MAXW", w.maxw, "MINW", w.minw).Replace(`
ROOT .cui-section-menu__rail { display: flex; flex-direction: column; gap: var(--spacing-md, 8px); }
@media (max-width: MAXW) {
  ROOT .cui-section-menu__rail { display: none; }
}
@media (min-width: MINW) {
  ROOT .cui-section-menu__rail {
    position: sticky;
    inset-block-start: calc(var(--ui-sticky-top, 0px) + var(--fui-section-menu-top, 1rem));
    align-self: start;
    max-height: calc(100vh - var(--ui-sticky-top, 0px) - var(--fui-section-menu-top, 1rem) - var(--spacing-md, 8px));
  }
  ROOT .cui-section-menu__filter { flex: 0 0 auto; }
  ROOT .cui-section-menu__rail > .cui-section-menu__body {
    flex: 1 1 auto;
    min-block-size: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    scrollbar-width: thin;
  }
  /* FillColumn: the host pins the column, so the menu fills it and the
     body scrolls inside, with no sticky offset or viewport cap of its
     own. In a column that grows with the page, 100% resolves to auto.
     The host column is drawn edge to edge, so the rail insets its
     parts (the headers' backgrounds still reach the column's edges). */
  :where(ROOT).cui-section-menu--fill { display: flex; flex-direction: column; block-size: 100%; box-sizing: border-box; --cui-section-menu-inset: var(--ui-section-menu-inset, var(--spacing-lg, 16px)); --cui-section-menu-bg: var(--ui-section-menu-bg, var(--color-surface, #fff)); }
  ROOT.cui-section-menu--fill .cui-section-menu__rail {
    position: static;
    padding-block: var(--spacing-lg, 16px) 0;
    flex: 1 1 auto;
    min-block-size: 0;
    max-height: none;
  }
  ROOT.cui-section-menu--fill .cui-section-menu__rail > .cui-section-menu__body { padding-block-end: var(--spacing-xl, 24px); }
}`)
}
