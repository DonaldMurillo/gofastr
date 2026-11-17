package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// TabNavItem is one link in the strip.
type TabNavItem struct {
	// Text is the visible label. Required.
	Text string
	// Href is the tab's destination. Required.
	Href string
	// Current marks the tab being viewed (aria-current="page"); at most one.
	Current bool
	// Badge is an aria-hidden count inside the tab.
	Badge string
}

// TabNavConfig configures the strip.
type TabNavConfig struct {
	// Items are the tabs, in order. Required, at least one.
	Items []TabNavItem

	// Label names the nav landmark. Required.
	Label string
	// End is a control at the strip's trailing edge, beside the nav.
	End render.HTML

	ID    string
	Class string
	// ExtraAttrs land on the nav; class, id and aria-label are dropped.
	ExtraAttrs html.Attrs
}

// TabNav renders a strip of links whose current tab the destination page settles.
func TabNav(cfg TabNavConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: TabNav requires Label — an unnamed nav landmark cannot be told from the page's other navs")
	}
	if len(cfg.Items) == 0 {
		panic("ui: TabNav requires at least one item")
	}
	links := make([]render.HTML, 0, len(cfg.Items))
	current := 0
	for _, it := range cfg.Items {
		if it.Current {
			current++
		}
	}
	if current > 1 {
		panic("ui: TabNav marks at most one item Current — each would claim aria-current=\"page\"")
	}
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
	nav := html.Nav(html.NavConfig{
		Label:      cfg.Label,
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "class", "id", "aria-label"),
	}, links...)
	if cfg.End == "" {
		return tabNavStyle.WrapHTML(nav)
	}
	return tabNavStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-tab-nav-row"},
		headless.Own(nav),
		html.Div(html.DivConfig{Class: "fui-tab-nav__end"}, cfg.End),
	))
}

var tabNavStyle = registry.RegisterStyle("ui-tab-nav", tabNavCSS)

func tabNavCSS(_ style.Theme) string {
	return `/* The strip is the root <nav>, or the <nav> inside the End row. */
:where([data-cui-comp="ui-tab-nav"].fui-tab-nav, [data-cui-comp="ui-tab-nav"] > .fui-tab-nav) {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-xs, 2px);
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link {
  display: inline-flex;
  align-items: baseline;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-block-end: var(--stroke-thick, 2px) solid transparent;
  margin-block-end: calc(-1 * var(--stroke-thin, 1px));
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium, 500);
  text-decoration: none;
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link:hover {
  color: var(--color-text, #09090B);
  text-decoration: underline;
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(-1 * var(--stroke-focus-offset, 2px));
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link[aria-current="page"] {
  color: var(--color-text, #09090B);
  font-weight: var(--font-weight-semibold, 600);
  border-block-end-color: var(--color-primary, #18181B);
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__link[aria-current="page"]:hover {
  text-decoration: none;
}
/* Forced colours repaint the idle tabs' transparent underline in the
   text colour, so every tab would read as current: idle tabs take
   Canvas and the current one Highlight. */
@media (forced-colors: active) {
  [data-cui-comp="ui-tab-nav"] .fui-tab-nav__link { border-block-end-color: Canvas; }
  [data-cui-comp="ui-tab-nav"] .fui-tab-nav__link[aria-current="page"] { border-block-end-color: Highlight; }
}
[data-cui-comp="ui-tab-nav"] .fui-tab-nav__badge {
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-xs, 0.75rem);
  font-variant-numeric: tabular-nums;
}
/* With an End slot the row carries the rule: the tabs sit on it and
   the end control floats at its trailing edge, wrapping under the tabs
   when the row is too narrow for both. */
:where([data-cui-comp="ui-tab-nav"]).fui-tab-nav-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
:where([data-cui-comp="ui-tab-nav"].fui-tab-nav-row) > .fui-tab-nav {
  flex: 1 1 auto;
  min-inline-size: 0;
  border-block-end: 0;
}
[data-cui-comp="ui-tab-nav"].fui-tab-nav-row > .fui-tab-nav__end {
  flex: none;
  padding-block-end: var(--spacing-sm, 4px);
}`
}
