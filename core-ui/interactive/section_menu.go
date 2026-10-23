package interactive

// SectionMenu: a grouped navigation menu for documentation and component
// galleries: sections (groups) → items (links), with the current page
// marked.
//
//   Desktop (≥ 900px, or the SwapAt width): a sticky rail with every
//     group expanded. Each group's header pins to the top of the rail
//     while its links scroll under it, so the reader always sees which
//     group they are in; the current group's header takes the accent and
//     the current page a dot.
//   Mobile (below it): a pill floating at the bottom of the viewport,
//     naming the current page and its group, that opens the framework's
//     bottom sheet: a dim backdrop, outside-click / Escape / drag-down to
//     close, background scroll lock and a focus trap. None of that is
//     re-implemented here: SectionMenuDrawer returns a preset.BottomSheet,
//     which the app mounts once.
//
// Filter adds a filter box to the rail and the sheet. The sectionmenu
// behaviour (section_menu.js, registered below) runs it and keeps the
// pill's label on the current page across client-side navigations.
//
// Active items are server-rendered (aria-current="page") on the rail and
// stamped client-side by the runtime's active-link pass.
//
// Usage:
//
//	// in the screen / sidebar
//	interactive.SectionMenu(cfg)                       // rail + trigger pill
//	// once at startup (cfg.DrawerName must match)
//	widget.MountBuilder(router, interactive.SectionMenuDrawer(cfg))

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

// The filter and the pill's label are this package's behaviour: they
// bind the menu's own classes, so the module lives beside the component
// and the kernel demand-loads it when a menu is on the page.
var _ = registry.RegisterBehavior("sectionmenu", sectionMenuJS,
	registry.Markers(`[data-cui-comp="cui-section-menu"]`))

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
	// Collapsed starts the group closed in the mobile sheet. A group
	// holding the active item is forced open regardless. (The desktop
	// rail always shows every group expanded.)
	Collapsed bool
}

// SectionMenuConfig configures a SectionMenu and its sheet.
type SectionMenuConfig struct {
	// Lead is an optional ungrouped item rendered above the groups
	// (e.g. an "Overview" / index link).
	Lead   *SectionItem
	Groups []SectionGroup
	// AriaLabel names the nav landmark (e.g. "Documentation sections").
	AriaLabel string
	// TriggerLabel names the mobile pill and titles the sheet. The pill
	// shows the current page and its group instead when there is one,
	// and keeps this as the start of its accessible name. Default "Menu".
	TriggerLabel string
	// DrawerName is the widget name shared by the trigger pill
	// (data-cui-open) and SectionMenuDrawer. Required for the mobile sheet;
	// must be unique per distinct menu on a site.
	DrawerName string
	// Filter adds a filter box above the groups, on the rail and in the
	// sheet: typing hides every link whose label does not contain the
	// text, opens every group holding a match and marks the match;
	// Escape clears it. It filters the menu's own links in the browser:
	// there is no request and no data behind it.
	Filter bool
	// FilterPlaceholder is the filter box's placeholder and accessible
	// name. Default "Filter", in the reader's language.
	FilterPlaceholder string
	// FilterEmpty is the line shown when nothing matches. Default
	// "No matches", in the reader's language.
	FilterEmpty string
	// InlineTrigger keeps the mobile trigger in flow where the menu sits.
	// By default the pill floats at the bottom of the viewport, which is
	// right for the page's own section nav (in a ui.ContentRow, set
	// PhoneNavFlush so the stacked nav column draws no empty band). A
	// menu that is not the page's nav (a demo, a menu inside a card)
	// sets this so its pill does not float over the page.
	InlineTrigger bool
	// FillColumn is for a menu placed in a column its host already pins
	// at a fixed height (a ui.ContentRow{Sticky: true} nav column): on
	// desktop the rail drops its own sticky offset and viewport cap,
	// fills the column top to bottom and scrolls its overflow there. In
	// normal flow leave it off and the rail sticks on its own.
	FillColumn bool
	// SwapAt picks the width below which the rail gives way to the
	// trigger and sheet: 900px by default. In a ui.ContentRow nav
	// column, match the row's Breakpoint (SwapMD for the
	// default, SwapLG for ui.StackBelowLG), so the column
	// never holds the rail beside content too narrow for it. An unknown
	// value panics.
	SwapAt SwapWidth
	Class  string
	ID     string
	// Ctx carries the per-request context the menu's default words and
	// each group's link count (i18n.Number) are written in. When nil,
	// English applies. A sheet mounted once with SectionMenuDrawer
	// speaks the locale of each request that renders it.
	Ctx context.Context
}

// SectionMenu renders the desktop rail plus the mobile trigger pill. Mount
// the matching sheet once with SectionMenuDrawer.
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

// sectionMenuTrigger is the mobile pill: the current page and its group
// when the config marks one, the trigger label otherwise. The
// sectionmenu behaviour rewrites the label and detail on every
// client-side navigation.
func sectionMenuTrigger(cfg SectionMenuConfig) render.HTML {
	pill := SheetPillConfig{Name: triggerLabel(cfg), Opens: cfg.DrawerName, Inline: cfg.InlineTrigger, SwapAt: cfg.SwapAt}
	if it, g, ok := activeItem(cfg); ok {
		pill.Label, pill.Detail = it.Label, g
	}
	return SheetPill(pill)
}

func triggerLabel(cfg SectionMenuConfig) string {
	if cfg.TriggerLabel == "" {
		return "Menu"
	}
	return cfg.TriggerLabel
}

// activeItem finds the item the config marks Active and the label of the
// group holding it ("" for the lead).
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

// sectionMenuFilter is the filter box. type="search" gives the native
// clear control; the sectionmenu behaviour does the filtering.
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

// SectionMenuDrawer returns the mobile sheet widget for a SectionMenu, a
// preset.BottomSheet (backdrop + click-outside / Escape / drag-down close
// + scroll lock + focus trap) carrying the same menu body. Mount it once
// at startup:
//
//	widget.MountBuilder(router, interactive.SectionMenuDrawer(cfg))
func SectionMenuDrawer(cfg SectionMenuConfig) *widget.Builder {
	if cfg.DrawerName == "" {
		panic("interactive: SectionMenuDrawer requires cfg.DrawerName")
	}
	return preset.BottomSheet(cfg.DrawerName).
		Hidden().
		LabelledBy(sheetTitleID(cfg)).
		Slot("body", sectionMenuDrawerSlot{cfg: cfg})
}

// sheetTitleID names the sheet's title, which labels the sheet's dialog.
func sheetTitleID(cfg SectionMenuConfig) string { return cfg.DrawerName + "-title" }

// sectionMenuDrawerSlot renders the menu body for the sheet, the same groups
// and links as the rail, wrapped in the component marker so the scoped CSS
// applies inside the sheet chrome too.
type sectionMenuDrawerSlot struct{ cfg SectionMenuConfig }

func (s sectionMenuDrawerSlot) Render() render.HTML { return s.RenderCtx(s.cfg.Ctx) }

// RenderCtx renders the sheet in the locale of the request showing it:
// the slot is mounted once, so the context it was built with is no
// reader's.
func (s sectionMenuDrawerSlot) RenderCtx(ctx context.Context) render.HTML {
	if ctx != nil {
		s.cfg.Ctx = ctx
	}
	// The close control's data-cui-action="close" is the framework's
	// declarative widget-dismiss hook: the sheet's own runtime closes it.
	top := []render.HTML{sheetHead(s.cfg.Ctx, triggerLabel(s.cfg), sheetTitleID(s.cfg),
		map[string]string{"data-cui-action": "close"})}
	cls := "cui-section-menu cui-section-menu--drawer"
	if s.cfg.Filter {
		top = append(top, sectionMenuFilter(s.cfg))
		cls += " cui-section-menu--filter"
	}
	// A <nav> like the rail, so the runtime's active-link pass marks the
	// current page in the sheet and opens its group. The rail is hidden
	// whenever the sheet is open, so the two never share a name on screen.
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

// sectionMenuBody renders the lead item + groups, and the filter's empty
// line when the menu has a filter. The desktop rail (rail true) renders
// every group open: collapse is a sheet behaviour, so Collapsed applies
// to the sheet only.
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

	// The rail shows every group, so its groups are labelled regions with
	// a plain header, not disclosures: a <summary> is a focusable target,
	// and one pinned over the links it scrolls past would shrink theirs
	// below the 24px a target needs.
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
	// Open when not explicitly collapsed, and whenever it holds the
	// active item.
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

// mapWith returns m with k=v added only when v is non-empty.
func mapWith(m map[string]string, k, v string) map[string]string {
	if v != "" {
		m[k] = v
	}
	return m
}

var sectionMenuStyle = registry.RegisterStyle("cui-section-menu", sectionMenuCSS)

// Knobs:
//   - --ui-section-menu-bg (var(--color-background); var(--color-surface)
//     with FillColumn, the nav column's colour, and in the sheet): the
//     colour the sticky group headers and the sheet's top bar paint
//     behind their text. Set it to the background of whatever the menu
//     sits on when that is neither.
//   - --ui-section-menu-dot (6px): the current page's dot.
//   - --ui-section-menu-inset (12px; 16px with FillColumn): the inline
//     gap between the rail's edge and its headers; links sit 14px deeper.
//   - --ui-section-menu-chevron-opacity (0.7).
//
// The pill and the sheet's title row are SheetPill's and sheetHead's,
// with their own knobs (--ui-sheet-pill-bottom, --ui-sheet-close-size).
func sectionMenuCSS(_ style.Theme) string {
	return `[data-cui-comp="cui-section-menu"] {
  --cui-section-menu-inset: var(--ui-section-menu-inset, calc(var(--spacing-sm, 4px) * 3));
  --cui-section-menu-bg: var(--ui-section-menu-bg, var(--color-background, #fff));
  --cui-section-menu-dot-ring: color-mix(in srgb, var(--color-accent, #2563EB) 20%, transparent);
  display: block;
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="cui-section-menu"] [hidden] { display: none !important; }

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
  background: color-mix(in srgb, var(--cui-section-menu-bg) 90%, transparent);
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
  background: color-mix(in srgb, var(--color-accent, #2563EB) 16%, transparent);
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

// sectionMenuSwap is one breakpoint's half of the sheet: the root
// selector it scopes to and the widths either side of the swap.
type sectionMenuSwap struct{ root, maxw, minw string }

// css is the rail/trigger swap at one breakpoint. The scope sits in
// :where(), so every swap weighs what the single 900px rule set did.
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
