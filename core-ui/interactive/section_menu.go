package interactive

// SectionMenu — a grouped, collapsible navigation menu for documentation and
// component galleries: sections (collapsible groups) → items (links), with an
// active-item highlight.
//
//   Desktop (≥ 900px): a sticky rail with every group expanded.
//   Mobile (< 900px): a "Sections" trigger button that opens the framework's
//     drawer widget — a real slide-in sheet with a dim backdrop that closes on
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
	// (data-fui-open) and SectionMenuDrawer. Required for the mobile sheet;
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
	// Mobile trigger — a plain button that opens the drawer widget. It stays
	// in normal flow (no layout shift) and is hidden on the desktop rail.
	if cfg.DrawerName != "" {
		children = append(children, render.Tag("button",
			map[string]string{
				"class":         "fui-section-menu__trigger",
				"type":          "button",
				"data-fui-open": cfg.DrawerName,
				"aria-label":    trigger,
			},
			render.Raw(`<svg class="fui-section-menu__trigger-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/></svg>`),
			html.Span(html.TextConfig{Class: "fui-section-menu__trigger-label"}, render.Text(trigger)),
		))
	}
	children = append(children, html.Div(html.DivConfig{Class: "fui-section-menu__rail"}, sectionMenuBody(cfg)))

	cls := "fui-section-menu"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return sectionMenuStyle.WrapHTML(render.Tag("nav",
		mapWith(map[string]string{"class": cls, "aria-label": aria}, "id", cfg.ID),
		children...,
	))
}

// SectionMenuDrawer returns the mobile drawer widget for a SectionMenu — a
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

// sectionMenuDrawerSlot renders the menu body for the drawer — the same groups
// and links as the rail, wrapped in the component marker so the scoped CSS
// applies inside the drawer chrome too.
type sectionMenuDrawerSlot struct{ cfg SectionMenuConfig }

func (s sectionMenuDrawerSlot) Render() render.HTML {
	// A visible close control. data-fui-action="close" is the framework's
	// declarative widget-dismiss hook — the drawer's own runtime closes it.
	closeBtn := render.Tag("button",
		map[string]string{
			"class":           "fui-section-menu__close",
			"type":            "button",
			"data-fui-action": "close",
			"aria-label":      "Close menu",
		},
		render.Raw(`<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"/><line x1="18" y1="6" x2="6" y2="18"/></svg>`),
	)
	return sectionMenuStyle.WrapHTML(html.Div(
		html.DivConfig{Class: "fui-section-menu fui-section-menu--drawer"},
		html.Div(html.DivConfig{Class: "fui-section-menu__drawer-head"}, closeBtn),
		sectionMenuBody(s.cfg),
	))
}

var _ component.Component = sectionMenuDrawerSlot{}

// sectionMenuBody renders the lead item + groups.
func sectionMenuBody(cfg SectionMenuConfig) render.HTML {
	children := []render.HTML{}
	if cfg.Lead != nil {
		children = append(children, sectionMenuLink(*cfg.Lead, "fui-section-menu__lead"))
	}
	for _, g := range cfg.Groups {
		children = append(children, sectionMenuGroup(g))
	}
	return html.Div(html.DivConfig{Class: "fui-section-menu__body"}, children...)
}

func sectionMenuGroup(g SectionGroup) render.HTML {
	items := make([]render.HTML, 0, len(g.Items))
	hasActive := false
	for _, it := range g.Items {
		if it.Active {
			hasActive = true
		}
		items = append(items, html.ListItem(html.ListItemConfig{Class: "fui-section-menu__item"},
			sectionMenuLink(it, "fui-section-menu__link")))
	}

	labelChildren := []render.HTML{}
	if g.Eyebrow != "" {
		labelChildren = append(labelChildren,
			html.Span(html.TextConfig{Class: "fui-section-menu__eyebrow"}, render.Text(g.Eyebrow)))
	}
	labelChildren = append(labelChildren,
		html.Span(html.TextConfig{Class: "fui-section-menu__group-label"}, render.Text(g.Label)),
		render.Raw(`<svg class="fui-section-menu__chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="6 9 12 15 18 9"/></svg>`))

	attrs := map[string]string{
		"class":               "fui-section-menu__group",
		"data-fui-disclosure": "",
	}
	// Open when not explicitly collapsed, or whenever it holds the active item.
	if !g.Collapsed || hasActive {
		attrs["open"] = ""
	}
	return render.Tag("details", attrs,
		render.Tag("summary", map[string]string{"class": "fui-section-menu__group-summary"}, labelChildren...),
		html.UnorderedList(html.ListConfig{Class: "fui-section-menu__list"}, items...),
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

var sectionMenuStyle = registry.RegisterStyle("fui-section-menu", sectionMenuCSS)

func sectionMenuCSS(_ style.Theme) string {
	return `[data-fui-comp="fui-section-menu"] {
  display: block;
  font-size: var(--text-sm, 0.875rem);
}

/* ── Body / groups / links (shared by the rail and the drawer) ────── */
[data-fui-comp="fui-section-menu"] .fui-section-menu__body { display: block; }
[data-fui-comp="fui-section-menu"] .fui-section-menu__lead {
  display: block;
  padding: var(--spacing-sm, 4px) 0 var(--spacing-sm, 4px) 12px;
  margin-bottom: var(--spacing-md, 12px);
  color: var(--color-text, currentColor);
  font-weight: 500;
  text-decoration: none;
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__lead.is-active,
[data-fui-comp="fui-section-menu"] .fui-section-menu__lead[aria-current="page"] {
  color: var(--color-primary, currentColor);
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__group {
  margin-bottom: var(--spacing-md, 12px);
  border: 0;
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__group-summary {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  list-style: none;
  padding: var(--spacing-sm, 4px) 0;
  margin-bottom: var(--spacing-sm, 4px);
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 11px);
  letter-spacing: 0.02em;
  color: var(--color-text-subtle, #71717A);
  user-select: none;
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__group-summary::-webkit-details-marker { display: none; }
[data-fui-comp="fui-section-menu"] .fui-section-menu__group-summary:hover { color: var(--color-text, currentColor); }
[data-fui-comp="fui-section-menu"] .fui-section-menu__eyebrow { color: var(--fui-section-menu-eyebrow-color, var(--color-text-subtle, #A1A1AA)); }
[data-fui-comp="fui-section-menu"] .fui-section-menu__group-label { flex: 1; }
[data-fui-comp="fui-section-menu"] .fui-section-menu__chevron {
  transition: transform 160ms ease;
  opacity: 0.7;
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__group[open] > .fui-section-menu__group-summary .fui-section-menu__chevron {
  transform: rotate(180deg);
}
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="fui-section-menu"] .fui-section-menu__chevron { transition: none; }
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__list {
  list-style: none;
  margin: 0;
  padding: 0;
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__link {
  display: block;
  padding: 3px 0 3px 22px;
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
  border-left: 2px solid transparent;
  margin-left: -2px;
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__link:hover { color: var(--color-text, currentColor); }
[data-fui-comp="fui-section-menu"] .fui-section-menu__link.is-active,
[data-fui-comp="fui-section-menu"] .fui-section-menu__link[aria-current="page"] {
  color: var(--color-text, #18181B);
  border-left-color: var(--color-primary, currentColor);
}

/* ── Mobile trigger button (hidden on the desktop rail) ───────────── */
[data-fui-comp="fui-section-menu"] .fui-section-menu__trigger {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  cursor: pointer;
  padding: var(--spacing-md, 8px) 14px;
  border: 1px solid var(--color-border, rgba(0,0,0,0.12));
  border-radius: var(--radii-md, 6px);
  background: var(--color-surface, transparent);
  color: var(--color-text, currentColor);
  font-size: var(--text-sm, 13px);
  font-weight: 500;
}

/* ── Drawer body: groups collapse (respect their open state) ──────── */
[data-fui-comp="fui-section-menu"].fui-section-menu--drawer { padding: var(--spacing-sm, 4px); }
[data-fui-comp="fui-section-menu"] .fui-section-menu__drawer-head {
  display: flex;
  justify-content: flex-end;
  margin-bottom: var(--spacing-sm, 8px);
}
[data-fui-comp="fui-section-menu"] .fui-section-menu__close {
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
[data-fui-comp="fui-section-menu"] .fui-section-menu__close:hover {
  background: var(--color-surface-soft, var(--color-surface, transparent));
}
/* The close control is a drawer-only affordance — never shown in the rail. */
[data-fui-comp="fui-section-menu"] .fui-section-menu__rail .fui-section-menu__drawer-head { display: none; }

/* ── Desktop rail (≥ 900px): hide the trigger, show a sticky column
      with every group expanded. The drawer is never opened here. ──── */
[data-fui-comp="fui-section-menu"] .fui-section-menu__rail { display: block; }
@media (max-width: 899.98px) {
  [data-fui-comp="fui-section-menu"] .fui-section-menu__rail { display: none; }
}
@media (min-width: 900px) {
  [data-fui-comp="fui-section-menu"] .fui-section-menu__trigger { display: none; }
  [data-fui-comp="fui-section-menu"] .fui-section-menu__rail {
    position: sticky;
    inset-block-start: var(--fui-section-menu-top, 1rem);
    align-self: start;
    max-height: calc(100vh - var(--fui-section-menu-top, 1rem) - var(--spacing-md, 16px));
    overflow-y: auto;
  }
  /* The rail shows every group expanded — collapse is a drawer behaviour. */
  [data-fui-comp="fui-section-menu"] .fui-section-menu__rail .fui-section-menu__list { display: block; }
  [data-fui-comp="fui-section-menu"] .fui-section-menu__rail .fui-section-menu__chevron { display: none; }
  [data-fui-comp="fui-section-menu"] .fui-section-menu__rail .fui-section-menu__group-summary { cursor: default; }
}`
}
