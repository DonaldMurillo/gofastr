package ui

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// SortableItem is one row: Key orders it on the server, Label names it.
type SortableItem = headless.SortableItem

// SortableListConfig configures one list.
type SortableListConfig struct {
	// Items are the rows in order; an empty list stays a drop target.
	Items []SortableItem
	// Label is the list's accessible name. Required.
	Label string
	// RPCPath is POSTed the new key order after a move.
	RPCPath string
	// Group links columns of one board; Container names this column.
	Group     string
	Container string
	// Version is the optimistic-concurrency token the rows were read
	// at; a 409 then fetches ConflictRPC instead of rolling back.
	Version string
	// ConflictRPC answers fresh rows (SortableListItems) on a 409.
	ConflictRPC string
	ID          string
	Class       string
	// ExtraAttrs land on the wrapper, minus class, id and aria-label.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// sortableListParts is the props and part classes both sortable render
// entry points share. The caller that renders the wrapper appends cfg.Class.
func sortableListParts(cfg SortableListConfig) (headless.SortableListProps, headless.Classes) {
	classes := headless.Classes{
		headless.PartRoot:         "fui-sortablelist",
		headless.PartSortableItem: "fui-sortablelist__item",
		headless.PartSortableGrip: "fui-sortablelist__grip",
		headless.PartLabel:        "fui-sortablelist__label",
	}
	return headless.SortableListProps{
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
	}, classes
}

// SortableList renders headless.SortableList: an <ol> reordered by drag
// or keyboard, committed by the RPCPath round trip.
func SortableList(cfg SortableListConfig) render.HTML {
	props, classes := sortableListParts(cfg)
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	out := headless.SortableList(props, classes)
	return sortablelistStyle.WrapHTML(out)
}

// SortableListItems renders the rows alone, for a ConflictRPC response.
func SortableListItems(cfg SortableListConfig) render.HTML {
	props, classes := sortableListParts(cfg)
	return headless.SortableItems(props, classes)
}

// RenewSortableVersion sends a versioned list the server's current
// version; call it on a move's 2xx and on the ConflictRPC response.
func RenewSortableVersion(w http.ResponseWriter, version string) {
	headless.RenewSortableVersion(w, version)
}

var sortablelistStyle = registry.RegisterStyle("ui-sortablelist", sortablelistCSS)

func sortablelistCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-sortablelist"] {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-sm, 4px);
}
:where([data-cui-comp="ui-sortablelist"]) .fui-sortablelist__item {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  background: var(--color-surface, #FFFFFF);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  box-shadow: var(--shadow-xs);
  font-size: var(--text-sm, 0.875rem);
  cursor: default;
  user-select: none;
  min-block-size: var(--spacing-touch-target, 44px);
}
[data-cui-comp="ui-sortablelist"] .fui-sortablelist__item:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-sortablelist"] .fui-sortablelist__item.is-grabbed {
  background: color-mix(in oklab, var(--color-primary, #18181B) 12%, transparent);
  border-color: var(--color-primary, #18181B);
  cursor: grabbing;
}
[data-cui-comp="ui-sortablelist"] .fui-sortablelist__item.is-dragging {
  opacity: var(--opacity-disabled, 0.5);
}
[data-cui-comp="ui-sortablelist"] .fui-sortablelist__item.is-drop-target {
  border-top: var(--stroke-thick, 2px) solid var(--color-primary, #18181B);
}
[data-cui-comp="ui-sortablelist"] .fui-sortablelist__grip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: var(--ui-sortablelist-grip-size, 28px);
  height: var(--ui-sortablelist-grip-size, 28px);
  padding: 0;
  background: transparent;
  border: 0;
  color: var(--color-text-muted, #52525B);
  cursor: grab;
}
[data-cui-comp="ui-sortablelist"] .fui-sortablelist__item:active .fui-sortablelist__grip {
  cursor: grabbing;
}
[data-cui-comp="ui-sortablelist"] .fui-sortablelist__label {
  font-weight: var(--font-weight-medium);
  color: var(--color-text, #09090B);
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
