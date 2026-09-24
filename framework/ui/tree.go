package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Tree ──────────────────────────────────────────────────────────
//
// Renders headless.Tree dressed with the fui-tree class map: the
// WAI-ARIA treeview whose keyboard contract (roving tabindex, arrows,
// Home/End, type-ahead, expand/collapse through the toggle button)
// the registered headless-tree module binds on the data-hui-tree
// marker. The no-script contract is the rendered tree: leaf links are
// real anchors, static branches are real markup, and a lazy branch
// keeps the kernel's rpc wiring on its toggle with a hidden
// signal-bound group — the same contract the retired
// core-ui/patterns/tree shipped.

// TreeItem is one entry in the tree. It is headless.TreeNode spelled
// for this package's config-struct callers.
type TreeItem = headless.TreeNode

// TreeConfig configures a tree.
type TreeConfig struct {
	// Label is the aria-label on the role="tree" wrapper. Required.
	Label string

	// Items are the root-level entries. Required.
	Items []TreeItem

	// LazySignalPrefix names the signal namespace lazy branches bind
	// their child groups to. Required when any item uses LazyPath.
	LazySignalPrefix string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the wrapper. Keys
	// the component owns are dropped: class and id (use Class / ID)
	// and aria-label (use Label).
	ExtraAttrs html.Attrs
}

// Tree renders the treeview.
func Tree(cfg TreeConfig) render.HTML {
	classes := headless.Classes{
		headless.PartRoot:       "fui-tree",
		headless.PartTreeItem:   "fui-tree__item",
		headless.PartTreeRow:    "fui-tree__row",
		headless.PartTreeToggle: "fui-tree__toggle",
		headless.PartTreeGroup:  "fui-tree__group",
		headless.PartLabel:      "fui-tree__label",
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	out := headless.Tree(headless.TreeProps{
		Label:            cfg.Label,
		Nodes:            cfg.Items,
		LazySignalPrefix: cfg.LazySignalPrefix,
		ID:               cfg.ID,
		ExtraAttrs:       headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label"),
	}, classes)
	return treeStyle.WrapHTML(out)
}

var treeStyle = registry.RegisterStyle("ui-tree", treeCSS)

func treeCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-tree"], [data-fui-comp="ui-tree"] .fui-tree__group {
  list-style: none;
  margin: 0;
  padding: 0;
}
[data-fui-comp="ui-tree"] .fui-tree__group { padding-inline-start: var(--spacing-lg, 16px); }
[data-fui-comp="ui-tree"] .fui-tree__group[hidden] { display: none; }
[data-fui-comp="ui-tree"] .fui-tree__item { display: block; }
[data-fui-comp="ui-tree"] .fui-tree__row {
  display: flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  min-height: var(--spacing-touch-target, 44px);
  padding: var(--spacing-sm, 4px) 6px;
  border-radius: var(--radii-sm, 4px);
}
/* Focus ring only while focus is actually inside the row — the roving
   tabindex means one item ALWAYS carries tabindex="0", so keying the
   outline on the bare attribute painted a permanent ring on it. */
[data-fui-comp="ui-tree"] .fui-tree__item:focus-visible > .fui-tree__row,
[data-fui-comp="ui-tree"] .fui-tree__item:focus-within > .fui-tree__row {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="ui-tree"] .fui-tree__item[aria-selected="true"] > .fui-tree__row {
  background: var(--color-surface-soft, #f1f1f3);
  font-weight: 600;
}
[data-fui-comp="ui-tree"] .fui-tree__toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 1.5em;
  block-size: 1.5em;
  border: none;
  background: transparent;
  color: var(--color-text-muted, #6b7280);
  font: inherit;
  font-size: var(--text-xs, 0.75rem);
  cursor: pointer;
  transition: transform var(--duration-fast, 150ms) var(--easing-standard, ease);
}
[data-fui-comp="ui-tree"] .fui-tree__item[aria-expanded="true"] > .fui-tree__row > .fui-tree__toggle {
  transform: rotate(90deg);
}
[data-fui-comp="ui-tree"] .fui-tree__label {
  color: var(--color-text, #111);
  text-decoration: none;
  flex: 1 1 auto;
  min-inline-size: 0;
  /* The label is a flex item (blockified), so axe's target-size rule
     measures it — unlike inline links, which the rule skips. Give it the
     WCAG 2.2 24px floor; it stays inside the 44px row's content box so the
     row height is unchanged and the text-overflow ellipsis still works. */
  min-block-size: var(--spacing-xl, 24px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
[data-fui-comp="ui-tree"] a.fui-tree__label:hover { text-decoration: underline; }
`
}
