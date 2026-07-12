// Package sortablelist renders a reorderable list with HTML5
// drag-and-drop plus keyboard fallback (Space to grab, Arrow up/down
// to move within a column, Arrow left/right to move between columns,
// Space again to drop, Esc to cancel).
//
// Single-list usage (back-compat — no new attrs emitted):
//
//	<ol data-fui-comp="ui-sortable-list" role="listbox"
//	    data-fui-sortable data-fui-sortable-rpc="<path>">
//	  <li data-fui-sortable-item data-fui-sort-key="<key>"
//	      draggable="true" tabindex="0" role="option">…</li>
//	</ol>
//
// Kanban (cross-container) usage — render one list per column, all
// sharing the same Group, each with a unique Container id:
//
//	<ol data-fui-sortable data-fui-sortable-group="board-1"
//	    data-fui-sortable-container="todo"
//	    data-fui-sortable-rpc="/api/move"
//	    data-fui-sortable-version="v3"
//	    data-fui-sortable-conflict="/api/conflict?col=todo">
//	  …
//	</ol>
//
// After a successful same-container reorder the runtime POSTs
// `order=<comma-sep-keys>`. A cross-container drop POSTs
// `order=<dest-keys>&moved=<key>&container=<col-id>`. When Version is
// set, a `version=<token>` field is appended to every commit; a 409
// response fires the conflict path (GET ConflictRPC → replace list
// HTML) instead of a blanket rollback. The server is authoritative —
// non-2xx reverts the DOM.
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
	// ExtraAttrs are merged onto the <ol> after all built-in attrs.
	ExtraAttrs html.Attrs
	// Group is the board id shared by linked columns (kanban). Lists
	// with the same non-empty Group allow cross-container drag and
	// keyboard moves between them. Lists with no Group stay isolated
	// (back-compat: existing single lists are unaffected).
	Group string
	// Container is the per-column id sent as the `container` field in
	// a cross-container move payload so the server knows which column
	// the item landed in. Distinct from Group (the board id) because a
	// board has one Group but N Containers — the server needs both to
	// route the write.
	Container string
	// Version is an optional optimistic-concurrency token sent as a
	// `version` body field on every commit. When set, a 409 response
	// fires the conflict path (refetch ConflictRPC HTML) instead of a
	// blanket rollback. Without Version, 409 is treated like any other
	// non-2xx (rollback) — back-compat.
	Version string
	// ConflictRPC, when set alongside Version, is GET-fetched on a
	// 409 response. The response body replaces the destination list's
	// innerHTML (server-rendered reconciliation). Without ConflictRPC,
	// a 409 falls back to rollback + a console warn.
	ConflictRPC string
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
	if cfg.Group != "" {
		listAttrs["data-fui-sortable-group"] = cfg.Group
	}
	if cfg.Container != "" {
		listAttrs["data-fui-sortable-container"] = cfg.Container
	}
	if cfg.Version != "" {
		listAttrs["data-fui-sortable-version"] = cfg.Version
	}
	if cfg.ConflictRPC != "" {
		listAttrs["data-fui-sortable-conflict"] = cfg.ConflictRPC
	}
	for k, v := range cfg.ExtraAttrs {
		listAttrs[k] = v
	}

	rows := renderRows(cfg)

	return sortableListStyle.WrapHTML(render.Tag("ol", listAttrs, rows...))
}

// renderRows builds the <li> items shared by Render and RenderItems.
func renderRows(cfg Config) []render.HTML {
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
	return rows
}

// RenderItems renders just the <li> items without the <ol> wrapper.
// Used by conflict-recovery endpoints that need to replace a list's
// innerHTML with fresh server-rendered rows.
func RenderItems(cfg Config) render.HTML {
	if len(cfg.Items) == 0 {
		panic("sortablelist: ≥1 Item required")
	}
	return render.Join(renderRows(cfg)...)
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
  gap: var(--spacing-xs, 2px);
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
}
.ui-sortable-list__sr {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}`
}
