package interactive

// SectionMenu: a grouped, collapsible navigation menu for documentation and
// component galleries: sections (collapsible groups) → items (links), with an
// active-item highlight.
//
//   Desktop (≥ 900px): a sticky rail with every group expanded.
//   Mobile (< 900px): a "Sections" trigger button that opens the framework's
//     drawer widget, a real slide-in sheet with a dim backdrop that closes on
//     outside-click / Escape, locks background scroll, and traps focus. None of
//     that is re-implemented here: SectionMenuDrawer returns a preset.Drawer
//     (the same primitive ui.Sidebar uses), which the app mounts once.
//
// Active items are server-rendered (aria-current="page") on the rail and
// stamped client-side by the runtime's active-link pass.
//
// Usage:
//
//	// in the screen / sidebar
//	interactive.SectionMenu(cfg)                       // rail + trigger button
//	// once at startup (cfg.DrawerName must match)
//	widget.MountBuilder(router, interactive.SectionMenuDrawer(cfg))

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
)

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
	// Collapsed starts the group closed in the mobile drawer. A group holding
	// the active item is forced open regardless. (The desktop rail always
	// shows every group expanded.)
	Collapsed bool
}

// SectionMenuConfig configures a SectionMenu and its drawer.
type SectionMenuConfig struct {
	// Lead is an optional ungrouped item rendered above the groups
	// (e.g. an "Overview" / index link).
	Lead   *SectionItem
	Groups []SectionGroup
	// AriaLabel names the nav landmark (e.g. "Documentation sections").
	AriaLabel string
	// TriggerLabel is the mobile trigger button's text. Default "Menu".
	TriggerLabel string
	// DrawerName is the widget name shared by the trigger button
	// (data-cui-open) and SectionMenuDrawer. Required for the mobile sheet;
	// must be unique per distinct menu on a site.
	DrawerName string
	Class      string
	ID         string
}

// SectionMenu renders the desktop rail plus the mobile trigger button. Mount
// the matching drawer once with SectionMenuDrawer.
func SectionMenu(cfg SectionMenuConfig) render.HTML {
	trigger := cfg.TriggerLabel
	if trigger == "" {
		trigger = "Menu"
	}
	aria := cfg.AriaLabel
	if aria == "" {
		aria = "Sections"
	}

	children := []render.HTML{}
	// Mobile trigger, a plain button that opens the drawer widget. It stays
	// in normal flow (no layout shift) and is hidden on the desktop rail.
	if cfg.DrawerName != "" {
		children = append(children, render.Tag("button",
			map[string]string{
				"class":         "cui-section-menu__trigger",
				"type":          "button",
				"data-cui-open": cfg.DrawerName,
				"aria-label":    trigger,
			},
			render.Raw(`<svg class="cui-section-menu__trigger-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/></svg>`),
			html.Span(html.TextConfig{Class: "cui-section-menu__trigger-label"}, render.Text(trigger)),
		))
	}
	children = append(children, html.Div(html.DivConfig{Class: "cui-section-menu__rail"}, sectionMenuBody(cfg, true)))

	cls := "cui-section-menu"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return sectionMenuStyle.WrapHTML(render.Tag("nav",
		mapWith(map[string]string{"class": cls, "aria-label": aria}, "id", cfg.ID),
		children...,
	))
}

// SectionMenuDrawer returns the mobile drawer widget for a SectionMenu, a
// left-edge preset.Drawer (backdrop + click-outside / Escape close + scroll
// lock + focus trap) carrying the same menu body. Mount it once at startup:
//
//	widget.MountBuilder(router, interactive.SectionMenuDrawer(cfg))
func SectionMenuDrawer(cfg SectionMenuConfig) *widget.Builder {
	if cfg.DrawerName == "" {
		panic("interactive: SectionMenuDrawer requires cfg.DrawerName")
	}
	return preset.Drawer(cfg.DrawerName).
		Hidden().
		Slot("body", sectionMenuDrawerSlot{cfg: cfg})
}

// sectionMenuDrawerSlot renders the menu body for the drawer, the same groups
// and links as the rail, wrapped in the component marker so the scoped CSS
// applies inside the drawer chrome too.
type sectionMenuDrawerSlot struct{ cfg SectionMenuConfig }

func (s sectionMenuDrawerSlot) Render() render.HTML {
	// A visible close control. data-cui-action="close" is the framework's
	// declarative widget-dismiss hook, the drawer's own runtime closes it.
	closeBtn := render.Tag("button",
		map[string]string{
			"class":           "cui-section-menu__close",
			"type":            "button",
			"data-cui-action": "close",
			"aria-label":      "Close menu",
		},
		render.Raw(`<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"/><line x1="18" y1="6" x2="6" y2="18"/></svg>`),
	)
	return sectionMenuStyle.WrapHTML(html.Div(
		html.DivConfig{Class: "cui-section-menu cui-section-menu--drawer"},
		html.Div(html.DivConfig{Class: "cui-section-menu__drawer-head"}, closeBtn),
		sectionMenuBody(s.cfg, false),
	))
}

var _ component.Component = sectionMenuDrawerSlot{}

// sectionMenuBody renders the lead item + groups. The desktop rail
// (rail true) renders every group open: collapse is a drawer behaviour,
// so Collapsed applies to the drawer only.
func sectionMenuBody(cfg SectionMenuConfig, rail bool) render.HTML {
	children := []render.HTML{}
	if cfg.Lead != nil {
		children = append(children, sectionMenuLink(*cfg.Lead, "cui-section-menu__lead"))
	}
	for _, g := range cfg.Groups {
		children = append(children, sectionMenuGroup(g, rail))
	}
	return html.Div(html.DivConfig{Class: "cui-section-menu__body"}, children...)
}

func sectionMenuGroup(g SectionGroup, forceOpen bool) render.HTML {
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
		render.Raw(`<svg class="cui-section-menu__chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="6 9 12 15 18 9"/></svg>`))

	attrs := map[string]string{
		"class":               "cui-section-menu__group",
		"data-hui-disclosure": "",
	}
	// Open when not explicitly collapsed, whenever it holds the active
	// item, and always in the rail.
	if !g.Collapsed || hasActive || forceOpen {
		attrs["open"] = ""
	}
	return render.Tag("details", attrs,
		render.Tag("summary", map[string]string{"class": "cui-section-menu__group-summary"}, labelChildren...),
		html.UnorderedList(html.ListConfig{Class: "cui-section-menu__list"}, items...),
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

func sectionMenuCSS(_ style.Theme) string {
	return `[data-cui-comp="cui-section-menu"] {
  display: block;
  font-size: var(--text-sm, 0.875rem);
}

/* ── Body / groups / links (shared by the rail and the drawer) ────── */
[data-cui-comp="cui-section-menu"] .cui-section-menu__body { display: block; }
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead {
  display: block;
  padding: var(--spacing-sm, 4px) 0 var(--spacing-sm, 4px) 12px;
  margin-bottom: var(--spacing-md, 8px);
  color: var(--color-text, currentColor);
  font-weight: var(--font-weight-medium);
  text-decoration: none;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead.is-active,
[data-cui-comp="cui-section-menu"] .cui-section-menu__lead[aria-current="page"] {
  color: var(--color-text, currentColor);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__group {
  margin-bottom: var(--spacing-md, 8px);
  border: 0;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__group-summary {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  list-style: none;
  /* The label starts where the lead does (12px), so on a rail with no
     padding of its own it does not touch the edge; the links indent
     under it at 22px. */
  padding: var(--spacing-sm, 4px) 0 var(--spacing-sm, 4px) 12px;
  margin-bottom: var(--spacing-sm, 4px);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text-muted, #52525B);
  user-select: none;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__group-summary::-webkit-details-marker { display: none; }
[data-cui-comp="cui-section-menu"] .cui-section-menu__group-summary:hover { color: var(--color-text, currentColor); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__eyebrow { color: var(--fui-section-menu-eyebrow-color, var(--color-text-subtle, #A1A1AA)); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__group-label { flex: 1; }
[data-cui-comp="cui-section-menu"] .cui-section-menu__chevron {
  transition: transform 160ms ease;
  opacity: 0.7;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__group[open] > .cui-section-menu__group-summary .cui-section-menu__chevron {
  transform: rotate(180deg);
}
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="cui-section-menu"] .cui-section-menu__chevron { transition: none; }
}
/* A group's links hang off a hairline rule under its label (12px in),
   and the active link darkens its stretch of that rule. The rule sits
   inside the rail, so the marker shows on a rail with no padding of its
   own; the link text still starts at 22px. */
[data-cui-comp="cui-section-menu"] .cui-section-menu__list {
  list-style: none;
  margin: 0;
  margin-inline-start: 12px;
  padding: 0;
  border-inline-start: 1px solid var(--color-border, #E4E4E7);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__link {
  display: block;
  padding-block: 3px;
  padding-inline: 9px 0;
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
  border-inline-start: 1px solid transparent;
  margin-inline-start: -1px;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__link:hover { color: var(--color-text, currentColor); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__link.is-active,
[data-cui-comp="cui-section-menu"] .cui-section-menu__link[aria-current="page"] {
  color: var(--color-text, #18181B);
  font-weight: var(--font-weight-medium);
  border-inline-start-color: var(--color-text, currentColor);
}

/* ── Mobile trigger button (hidden on the desktop rail) ───────────── */
[data-cui-comp="cui-section-menu"] .cui-section-menu__trigger {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  cursor: pointer;
  padding: var(--spacing-md, 8px) 14px;
  border: 1px solid var(--color-border, rgba(0,0,0,0.12));
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, transparent);
  color: var(--color-text, currentColor);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
}

/* ── Drawer body: groups collapse (respect their open state) ──────── */
[data-cui-comp="cui-section-menu"].cui-section-menu--drawer { padding: var(--spacing-sm, 4px); }
[data-cui-comp="cui-section-menu"] .cui-section-menu__drawer-head {
  display: flex;
  justify-content: flex-end;
  margin-bottom: var(--spacing-sm, 4px);
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  border: 1px solid var(--color-border, rgba(0,0,0,0.12));
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, transparent);
  color: var(--color-text, currentColor);
  cursor: pointer;
}
[data-cui-comp="cui-section-menu"] .cui-section-menu__close:hover {
  background: var(--color-surface-soft, var(--color-surface, transparent));
}
/* The close control is a drawer-only affordance — never shown in the rail. */
[data-cui-comp="cui-section-menu"] .cui-section-menu__rail .cui-section-menu__drawer-head { display: none; }

/* ── Desktop rail (≥ 900px): hide the trigger, show a sticky column
      with every group expanded. The drawer is never opened here. ──── */
[data-cui-comp="cui-section-menu"] .cui-section-menu__rail { display: block; }
@media (max-width: 899.98px) {
  [data-cui-comp="cui-section-menu"] .cui-section-menu__rail { display: none; }
}
@media (min-width: 900px) {
  [data-cui-comp="cui-section-menu"] .cui-section-menu__trigger { display: none; }
  [data-cui-comp="cui-section-menu"] .cui-section-menu__rail {
    position: sticky;
    inset-block-start: var(--fui-section-menu-top, 1rem);
    align-self: start;
    max-height: calc(100vh - var(--fui-section-menu-top, 1rem) - var(--spacing-md, 8px));
    overflow-y: auto;
  }
  /* The rail shows every group expanded — collapse is a drawer behaviour. */
  [data-cui-comp="cui-section-menu"] .cui-section-menu__rail .cui-section-menu__list { display: block; }
  /* A rail group a reader clicks shut stays shown: browsers hide closed
     <details> content through ::details-content. */
  [data-cui-comp="cui-section-menu"] .cui-section-menu__rail .cui-section-menu__group::details-content { content-visibility: visible; }
  [data-cui-comp="cui-section-menu"] .cui-section-menu__rail .cui-section-menu__chevron { display: none; }
  [data-cui-comp="cui-section-menu"] .cui-section-menu__rail .cui-section-menu__group-summary { cursor: default; }
}`
}
