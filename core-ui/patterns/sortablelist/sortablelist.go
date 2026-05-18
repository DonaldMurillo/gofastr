// Package sortablelist renders a reorderable list with HTML5
// drag-and-drop plus keyboard fallback (Space to grab, Arrow up/down
// to move, Space again to drop, Esc to cancel).
//
// Output structure:
//
//	<ol data-fui-comp="ui-sortable-list" role="listbox"
//	    data-fui-sortable data-fui-sortable-rpc="<path>">
//	  <li data-fui-sortable-item data-fui-sort-key="<key>"
//	      draggable="true" tabindex="0" role="option">
//	    <button class="ui-sortable-list__grip" aria-label="Drag <label>">⋮⋮</button>
//	    <span class="ui-sortable-list__label">…</span>
//	  </li>
//	  …
//	</ol>
//
// After a successful reorder (mouse or keyboard) the runtime POSTs
// the new key sequence to RPCPath as form-encoded `order=<comma-sep-keys>`.
// The server is authoritative — it can reject the reorder by returning
// non-2xx, in which case the runtime reverts the DOM.
package sortablelist

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Item is one entry in the list.
type Item struct {
	// Key is the stable identifier the server uses to apply the new
	// order (required).
	Key string
	// Label is the visible row text + accessible label for the drag
	// handle (required).
	Label string
	// Content, when set, replaces Label as the row body. Use for
	// richer rows (icons, badges, etc.). The grip's aria-label still
	// uses Label.
	Content render.HTML
}

// Config configures a SortableList.
type Config struct {
	// Items are the entries in initial order (≥1).
	Items []Item
	// Label is the accessible label for the list (required, used as
	// aria-label on the <ol>).
	Label string
	// RPCPath, when set, is POSTed with order=<keys> after every
	// successful reorder. Server responds 2xx to confirm or non-2xx
	// to reject + revert.
	RPCPath string
	ID      string
	Class   string
	Attrs   html.Attrs
}

// Render renders the SortableList.
func Render(cfg Config) render.HTML {
	if cfg.Label == "" {
		panic("sortablelist: Label required")
	}
	if len(cfg.Items) == 0 {
		panic("sortablelist: ≥1 Item required")
	}
	cls := "ui-sortable-list"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	listAttrs := html.Attrs{
		"class":             cls,
		"role":              "listbox",
		"aria-label":        cfg.Label,
		"data-fui-sortable": "true",
	}
	if cfg.ID != "" {
		listAttrs["id"] = cfg.ID
	}
	if cfg.RPCPath != "" {
		listAttrs["data-fui-sortable-rpc"] = cfg.RPCPath
	}
	for k, v := range cfg.Attrs {
		listAttrs[k] = v
	}

	rows := make([]render.HTML, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		if it.Key == "" {
			panic("sortablelist: Item requires Key")
		}
		if it.Label == "" {
			panic("sortablelist: Item requires Label")
		}
		body := it.Content
		if body == "" {
			body = html.Span(html.TextConfig{Class: "ui-sortable-list__label"},
				render.Text(it.Label))
		}
		// The <li> itself is the focusable + draggable interactive
		// element (axe nested-interactive: a focusable <li> mustn't
		// contain a <button>). The visual grip is a decorative <span>
		// with aria-hidden=true; the per-item drag label is announced
		// via aria-label on the <li>.
		rows = append(rows, render.Tag("li",
			map[string]string{
				"class":                  "ui-sortable-list__item",
				"role":                   "option",
				"draggable":              "true",
				"tabindex":               "0",
				"data-fui-sortable-item": "true",
				"data-fui-sort-key":      it.Key,
				"aria-roledescription":   "sortable item",
				"aria-label":             "Drag " + it.Label,
			},
			render.Tag("span", map[string]string{
				"class":       "ui-sortable-list__grip",
				"aria-hidden": "true",
			}, render.HTML(sortableGripIcon())),
			body,
		))
	}

	return sortableListStyle.WrapHTML(render.Tag("ol", listAttrs, rows...))
}

func sortableGripIcon() string {
	return `<svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor" aria-hidden="true" xmlns="http://www.w3.org/2000/svg"><circle cx="5" cy="3" r="1.2"/><circle cx="5" cy="7" r="1.2"/><circle cx="5" cy="11" r="1.2"/><circle cx="9" cy="3" r="1.2"/><circle cx="9" cy="7" r="1.2"/><circle cx="9" cy="11" r="1.2"/></svg>`
}

var sortableListStyle = registry.RegisterStyle("ui-sortable-list", sortableListCSS)

func sortableListCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-sortable-list"] {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 2px;
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  background: var(--color-surface, #FFFFFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  cursor: default;
  user-select: none;
  min-block-size: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__item:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__item.is-grabbed {
  background: color-mix(in srgb, var(--color-primary, #4F46E5) 12%, transparent);
  border-color: var(--color-primary, #4F46E5);
  cursor: grabbing;
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__item.is-dragging {
  opacity: 0.5;
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__item.is-drop-target {
  border-top: 2px solid var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__grip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  background: transparent;
  border: 0;
  color: var(--color-text-muted, #52525B);
  cursor: grab;
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__item:active .ui-sortable-list__grip {
  cursor: grabbing;
}
[data-fui-comp="ui-sortable-list"] .ui-sortable-list__label {
  font-weight: 500;
  color: var(--color-text, #18181B);
}`
}
