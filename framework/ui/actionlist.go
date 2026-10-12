package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── ActionList ─────────────────────────────────────────────────────
//
// A short list of rows, each a link or a button carrying an RPC, drawn
// as a menu's rows are. It is the Menu's sibling for a panel that also
// holds other controls (an account panel with a theme switch): a menu
// panel is role=menu and may hold only menu rows, so the rows of a
// mixed panel are a plain list the Tab key walks.

// ActionListItem is one row: Href makes it a link, Do a button.
type ActionListItem struct {
	// Label is the row's text. Required.
	Label string
	// Href navigates. Exactly one of Href and Do.
	Href string
	// Do is the row's RPC, effects and toasts included (a Sign out
	// that navigates on success). Exactly one of Href and Do.
	Do *interactive.Action
	// Icon names a registered icon drawn before the label.
	Icon string
	// Danger draws the row in the danger colour.
	Danger bool
}

// ActionListConfig configures an ActionList.
type ActionListConfig struct {
	// Items are the rows. Required.
	Items []ActionListItem
	// Label names the list for assistive tech.
	Label string
	ID    string
	Class string
	// ExtraAttrs land on the root <ul>; class, id and aria-label are the
	// component's.
	ExtraAttrs html.Attrs
}

// ActionList renders the rows.
func ActionList(cfg ActionListConfig) render.HTML {
	if len(cfg.Items) == 0 {
		panic("ui: ActionList requires Items")
	}
	rows := make([]render.HTML, len(cfg.Items))
	for i, it := range cfg.Items {
		if strings.TrimSpace(it.Label) == "" {
			panic("ui: ActionList item " + strconv.Itoa(i) + " requires Label")
		}
		if (it.Href == "") == (it.Do == nil) {
			panic("ui: ActionList item " + strconv.Quote(it.Label) + " sets exactly one of Href and Do")
		}
		var body []render.HTML
		if it.Icon != "" {
			if !IconRegistered(it.Icon) {
				panic("ui: ActionList Icon " + strconv.Quote(it.Icon) + " is not a registered icon")
			}
			body = append(body, Icon(it.Icon, IconConfig{Size: "16", ExtraAttrs: html.Attrs{"aria-hidden": "true"}}))
		}
		body = append(body, html.Span(html.TextConfig{Class: "fui-action-list__label"}, render.Text(it.Label)))
		class := "fui-action-list__item"
		if it.Danger {
			class += " fui-action-list__item--danger"
		}
		var row render.HTML
		if it.Href != "" {
			row = render.Tag("a", html.Attrs{"class": class, "href": it.Href}, body...)
		} else {
			attrs := html.Attrs{}
			for k, v := range it.Do.Attrs() {
				attrs[k] = v
			}
			attrs["class"] = class
			attrs["type"] = "button"
			row = render.Tag("button", attrs, body...)
		}
		rows[i] = render.Tag("li", html.Attrs{"data-cui-internal": ""}, headless.Own(row))
	}
	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "aria-label")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	attrs["class"] = cls("fui-action-list", cfg.Class)
	if cfg.Label != "" {
		attrs["aria-label"] = cfg.Label
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return actionListStyle.WrapHTML(render.Tag("ul", attrs, rows...))
}

var actionListStyle = registry.RegisterStyle("ui-action-list", actionListCSS)

// actionListCSS draws the rows as a menu's rows: full width, a touch
// target tall, the soft surface on hover and focus.
func actionListCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-action-list"]).fui-action-list {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}
[data-cui-comp="ui-action-list"] .fui-action-list__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  inline-size: 100%;
  box-sizing: border-box;
  text-align: start;
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  background: transparent;
  color: inherit;
  border: 0;
  border-radius: var(--radii-md, 8px);
  cursor: pointer;
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  text-decoration: none;
  min-height: var(--spacing-touch-target, 44px);
}
[data-cui-comp="ui-action-list"] .fui-action-list__item:hover,
[data-cui-comp="ui-action-list"] .fui-action-list__item:focus-visible {
  background: var(--color-surface-soft);
}
[data-cui-comp="ui-action-list"] .fui-action-list__item:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: calc(-1 * var(--stroke-focus, 2px));
}
[data-cui-comp="ui-action-list"] .fui-action-list__item svg { flex: none; color: var(--color-text-muted); }
[data-cui-comp="ui-action-list"] .fui-action-list__item--danger { color: var(--color-danger); }
[data-cui-comp="ui-action-list"] .fui-action-list__label { flex: 1; min-inline-size: 0; }
`
}
