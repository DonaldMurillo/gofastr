package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── TabNav ─────────────────────────────────────────────────────────
//
// A horizontal strip of navigation links where one is current: the tabs
// above a list that pick a view, the sub-navigation of a record screen.
// Every tab is a plain <a>, so a tab click is a navigation the client
// router intercepts and a URL the reader can share, bookmark and reload
// — the query-param contract tabs carry since list state moved off
// islands. Selection is server-settled: the current tab carries
// aria-current="page" and no script moves it, because the destination
// page says which tab is current, not a click handler.
//
// Distinct from ui.Tabs, which switches PANELS in place through a
// client signal and keeps one URL for all of them. TabNav changes the
// URL; Tabs changes the panel. A screen whose tabs narrow the same data
// (views over one list) wants TabNav; a screen whose tabs reveal
// different regions of one record wants Tabs.

// TabNavItem is one link in the strip.
type TabNavItem struct {
	// Text is the visible label. Required.
	Text string
	// Href is the tab's destination. Required.
	Href string
	// Current marks this tab as the one being viewed: the link renders
	// with aria-current="page" and the sheet's current treatment.
	Current bool
	// Badge is optional trailing count text inside the tab (a view's
	// row count). Rendered aria-hidden so the link's accessible name
	// stays the tab's label, not the count.
	Badge string
}

// TabNavConfig configures the strip.
type TabNavConfig struct {
	// Items are the tabs, in order. Required, at least one.
	Items []TabNavItem

	// Label is the nav landmark's accessible name ("Views", "Sections").
	// Required: an unnamed nav beside another unnamed nav is a landmark
	// assistive tech cannot tell apart.
	Label string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the <nav> root. Keys the
	// component owns are dropped: class and id (use Class / ID) and
	// aria-label (use Label).
	ExtraAttrs html.Attrs
}

// TabNav renders the strip.
func TabNav(cfg TabNavConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: TabNav requires Label — an unnamed nav landmark cannot be told from the page's other navs")
	}
	if len(cfg.Items) == 0 {
		panic("ui: TabNav requires at least one item")
	}
	links := make([]render.HTML, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		if it.Text == "" {
			panic("ui: TabNav item requires Text")
		}
		if it.Href == "" {
			panic("ui: TabNav item requires Href")
		}
		extra := html.Attrs{"class": "fui-tab-nav__link", "data-cui-internal": ""}
		if it.Current {
			extra["aria-current"] = "page"
		}
		content := []render.HTML{render.Text(it.Text)}
		if it.Badge != "" {
			content = append(content, html.Span(html.TextConfig{
				Class:      "fui-tab-nav__badge",
				ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
			}, render.Text(it.Badge)))
		}
		links = append(links, html.LinkHTML(html.LinkHTMLConfig{
			Href:       it.Href,
			Content:    render.Join(content...),
			ExtraAttrs: extra,
		}))
	}
	cls := "fui-tab-nav"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return tabNavStyle.WrapHTML(html.Nav(html.NavConfig{
		Label:      cfg.Label,
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "class", "id", "aria-label"),
	}, links...))
}

var tabNavStyle = registry.RegisterStyle("ui-tab-nav", tabNavCSS)

func tabNavCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-tab-nav"] .fui-tab-nav) {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-xs, 2px);
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link {
  display: inline-flex;
  align-items: baseline;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-block-end: var(--stroke-thick, 2px) solid transparent;
  margin-block-end: calc(-1 * var(--stroke-thin, 1px));
  color: var(--color-text-muted, #6B7280);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium, 500);
  text-decoration: none;
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link:hover {
  color: var(--color-text, #1F2937);
  text-decoration: underline;
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(-1 * var(--stroke-focus-offset, 2px));
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link[aria-current="page"] {
  color: var(--color-text, #1F2937);
  font-weight: var(--font-weight-semibold, 600);
  border-block-end-color: var(--color-primary, #18181B);
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link[aria-current="page"]:hover {
  text-decoration: none;
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__badge {
  color: var(--color-text-muted, #6B7280);
  font-size: var(--text-xs, 0.75rem);
}`
}
