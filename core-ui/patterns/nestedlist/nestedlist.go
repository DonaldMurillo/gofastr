// Package nestedlist renders recursive <ul>/<ol> hierarchies with
// optional native <details> collapse on branches. Use it for navigation
// trees, settings menus, and multi-level outlines that don't need the
// lazy-load / RPC machinery of the tree pattern. Pure render — no
// runtime module.
package nestedlist

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. Render's top-level
// <ul>/<ol> goes through Style.WrapHTML so the data-fui-comp marker
// is emitted and the runtime auto-loads the CSS on first appearance.
// Apps no longer need to concatenate this package's CSS by hand.
var Style = registry.RegisterStyle("nestedlist", styleFn)

// Item is one node in the list. A node with Children renders as a
// <details>/<summary>; a leaf node renders as a plain <li> (optionally
// wrapping its label in an <a> when Href is set).
type Item struct {
	// Label is the visible text. Required.
	Label string
	// Href turns a leaf node into a link. Branch nodes ignore Href —
	// their summary is non-navigable on purpose so the disclosure
	// trigger isn't ambiguous.
	Href string
	// Children, when non-empty, makes the item a collapsible branch.
	Children []Item
	// Expanded sets the initial open state on a branch node. Ignored
	// for leaf nodes.
	Expanded bool
	// ID optionally tags this node for anchor links / scroll targets.
	ID string
}

// Config configures the top-level wrapper.
type Config struct {
	Items []Item
	// Ordered renders the root list as <ol>. Nested lists inherit the
	// same element type.
	Ordered bool
	// AriaLabel labels the wrapping list for assistive tech. Required
	// for nav-style usage (settings menu, sidebar TOC).
	AriaLabel string
	ID        string
	Class     string
}

// Render renders the nested list. Branch nodes use native <details>
// for keyboard-accessible collapse without any JS.
func Render(cfg Config) render.HTML {
	cls := "nested-list"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	if cfg.AriaLabel != "" {
		attrs["aria-label"] = cfg.AriaLabel
	}
	return Style.WrapHTML(renderList(cfg.Ordered, attrs, cfg.Items))
}

func renderList(ordered bool, listAttrs map[string]string, items []Item) render.HTML {
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	children := make([]render.HTML, 0, len(items))
	for _, it := range items {
		children = append(children, renderItem(ordered, it))
	}
	return render.Tag(tag, listAttrs, children...)
}

func renderItem(ordered bool, it Item) render.HTML {
	liAttrs := map[string]string{"class": "nested-list__item"}
	if it.ID != "" {
		liAttrs["id"] = it.ID
	}
	if len(it.Children) == 0 {
		// Leaf node.
		var body render.HTML
		if it.Href != "" {
			body = render.Tag("a", map[string]string{
				"class": "nested-list__link",
				"href":  it.Href,
			}, render.Text(it.Label))
		} else {
			body = render.Text(it.Label)
		}
		return render.Tag("li", liAttrs, body)
	}

	// Branch node — collapsible via native <details>.
	detailsAttrs := map[string]string{"class": "nested-list__branch"}
	if it.Expanded {
		detailsAttrs["open"] = ""
	}
	summary := render.Tag("summary", map[string]string{"class": "nested-list__summary"},
		render.Text(it.Label),
	)
	sublist := renderList(ordered, map[string]string{"class": "nested-list__sublist"}, it.Children)
	return render.Tag("li", liAttrs,
		render.Tag("details", detailsAttrs, summary, sublist),
	)
}

// styleFn returns the stylesheet for nested-list. Tokens used:
// --spacing-xs / --spacing-sm / --spacing-md / --spacing-lg,
// --radii-sm, --color-text, --color-text-muted, --color-primary,
// --color-surface-soft, --color-border.
func styleFn(_ style.Theme) string {
	return `
.nested-list,
.nested-list ul,
.nested-list ol {
  list-style: none;
  padding-inline-start: 0;
  margin: 0;
  font-size: 0.9rem;
  line-height: 1.45;
}
.nested-list ul,
.nested-list ol {
  /* Nested indentation: a subtle rail on the inline-start edge
     anchors the eye to the parent branch. */
  padding-inline-start: var(--spacing-md, 12px);
  margin-inline-start: var(--spacing-sm, 6px);
  border-inline-start: 1px solid var(--color-border, #E5E7EB);
}
/* Ordered: re-enable numbering. The outer reset above killed
   list-style on every list under .nested-list, but ol at the root
   AND when nested wants decimal markers back. */
ol.nested-list,
.nested-list ol {
  list-style: decimal;
  padding-inline-start: var(--spacing-lg, 18px);
}
.nested-list ol .nested-list__item {
  padding-inline-start: var(--spacing-xs, 4px);
}
.nested-list__item {
  padding-block: 2px;
}
/* Hide the native <details> disclosure triangle — we draw our own
   custom caret via ::before on the summary so it picks up theme color
   and rotates smoothly on open. */
.nested-list details > summary { list-style: none; }
.nested-list details > summary::-webkit-details-marker { display: none; }
.nested-list__summary,
.nested-list summary {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 6px);
  cursor: pointer;
  font-weight: 600;
  color: var(--color-text, #111827);
  padding: 4px 6px;
  border-radius: var(--radii-sm, 4px);
  user-select: none;
}
.nested-list__summary::before,
.nested-list summary::before {
  content: "";
  display: inline-block;
  inline-size: 0.5rem;
  block-size: 0.5rem;
  border-block-start: 2px solid currentColor;
  border-inline-end: 2px solid currentColor;
  transform: rotate(45deg);  /* points right when collapsed */
  transition: transform 120ms ease;
  opacity: 0.7;
  margin-inline-end: 2px;
}
.nested-list details[open] > summary::before,
.nested-list details[open] > .nested-list__summary::before {
  transform: rotate(135deg); /* points down when open */
}
.nested-list summary:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
.nested-list summary:focus-visible {
  outline: 2px solid var(--color-primary, #2563EB);
  outline-offset: 2px;
}
.nested-list__link {
  display: inline-block;
  color: var(--color-text, #111827);
  text-decoration: none;
  padding: 4px 6px;
  border-radius: var(--radii-sm, 4px);
}
.nested-list__link:hover {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-primary, #2563EB);
}
.nested-list__link:focus-visible {
  outline: 2px solid var(--color-primary, #2563EB);
  outline-offset: 2px;
}
/* Ordered numbers should pick up the muted color so the marker
   doesn't compete with the label text. */
.nested-list ol .nested-list__item::marker {
  color: var(--color-text-muted, #6B7280);
}
`
}
