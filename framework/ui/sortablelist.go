package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── SortableList ──────────────────────────────────────────────────
//
// Renders headless.SortableList dressed with the fui-sortablelist
// class map: a reorderable <ol> whose rows move under drag-and-drop or
// the keyboard (Space grabs, arrows move, Space drops, Escape cancels)
// and whose commit is the server round trip the pattern shipped — the
// browser previews the move, the server confirms with 2xx or the DOM
// reverts, linked columns share a Group, and a versioned 409 refetches
// the server's own rows through SortableListItems. The registered
// headless-sortablelist module binds it; the no-script fallback is the
// plain ordered list of rows.

// SortableItem is one row: Key is the stable identifier the server
// applies the order by, Label the visible text and drag name, Content
// an optional richer body.
type SortableItem = headless.SortableItem

// SortableListConfig configures one list.
type SortableListConfig struct {
	// Items are the rows in initial order. May be empty (an empty
	// column stays a drop target).
	Items []SortableItem
	// Label is the list's accessible name. Required.
	Label string
	// RPCPath is POSTed after every successful reorder: the new key
	// order, plus container/version fields when configured and
	// moved=<key> on a cross-container drop.
	RPCPath string
	// Group is the board id shared by linked columns (kanban).
	Group string
	// Container is the per-column id that routes the write.
	Container string
	// Version is an optional optimistic-concurrency token; a 409 then
	// fires the conflict path instead of a rollback.
	Version string
	// ConflictRPC is GET-fetched on a versioned 409; its response
	// (fresh rows, e.g. from SortableListItems) replaces the list.
	ConflictRPC string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the wrapper. Keys
	// the component owns are dropped: class and id (use Class / ID)
	// and aria-label (use Label).
	ExtraAttrs html.Attrs

	// Ctx resolves the Strings table through the request's translator.
	Ctx context.Context
}

// SortableList renders the list.
func SortableList(cfg SortableListConfig) render.HTML {
	classes := headless.Classes{
		headless.PartRoot:         "fui-sortablelist",
		headless.PartSortableItem: "fui-sortablelist__item",
		headless.PartSortableGrip: "fui-sortablelist__grip",
		headless.PartLabel:        "fui-sortablelist__label",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	out := headless.SortableList(headless.SortableListProps{
		Items:       cfg.Items,
		Label:       cfg.Label,
		RPCPath:     cfg.RPCPath,
		Group:       cfg.Group,
		Container:   cfg.Container,
		Version:     cfg.Version,
		ConflictRPC: cfg.ConflictRPC,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label"),
		Strings:     StringsFor(cfg.Ctx),
	}, classes)
	return sortablelistStyle.WrapHTML(out)
}

// SortableListItems renders just the rows without the <ol> wrapper:
// the fragment a conflict-recovery endpoint returns to replace a
// list's contents with the server's own rows.
func SortableListItems(cfg SortableListConfig) render.HTML {
	classes := headless.Classes{
		headless.PartRoot:         "fui-sortablelist",
		headless.PartSortableItem: "fui-sortablelist__item",
		headless.PartSortableGrip: "fui-sortablelist__grip",
		headless.PartLabel:        "fui-sortablelist__label",
	}
	// No WrapHTML: the fragment is the list's rows (none when the server
	// empties a column), not one rooted element, and it lands inside a
	// list whose sheet the page already loaded.
	return headless.SortableItems(headless.SortableListProps{
		Items:       cfg.Items,
		Label:       cfg.Label,
		RPCPath:     cfg.RPCPath,
		Group:       cfg.Group,
		Container:   cfg.Container,
		Version:     cfg.Version,
		ConflictRPC: cfg.ConflictRPC,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label"),
		Strings:     StringsFor(cfg.Ctx),
	}, classes)
}

var sortablelistStyle = registry.RegisterStyle("ui-sortablelist", sortablelistCSS)

func sortablelistCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-sortablelist"] {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  background: var(--color-surface, #FFFFFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  cursor: default;
  user-select: none;
  min-block-size: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__item:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__item.is-grabbed {
  background: color-mix(in srgb, var(--color-primary, #4F46E5) 12%, transparent);
  border-color: var(--color-primary, #4F46E5);
  cursor: grabbing;
}
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__item.is-dragging {
  opacity: 0.5;
}
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__item.is-drop-target {
  border-top: 2px solid var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__grip {
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
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__item:active .fui-sortablelist__grip {
  cursor: grabbing;
}
[data-fui-comp="ui-sortablelist"] .fui-sortablelist__label {
  font-weight: 500;
  color: var(--color-text, #18181B);
}
/* The live region the module mints carries its own hook: clip it out
   of the view here rather than with an inline style, which the CSP
   posture drops. */
[data-hui-sortable-live] {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}`
}
