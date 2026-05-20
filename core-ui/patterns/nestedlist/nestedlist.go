// Package nestedlist renders recursive <ul>/<ol> hierarchies with
// optional native <details> collapse on branches. Use it for navigation
// trees, settings menus, and multi-level outlines that don't need the
// lazy-load / RPC machinery of the tree pattern. Pure render — no
// runtime module.
package nestedlist

import (
	"github.com/DonaldMurillo/gofastr/core/render"
)

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
	return renderList(cfg.Ordered, attrs, cfg.Items)
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

// BaseCSS returns the minimal stylesheet for nested-list. Tokens used:
// --spacing-sm, --spacing-md, --color-primary, --color-text.
func BaseCSS() string {
	return `
.nested-list,
.nested-list ul,
.nested-list ol {
  list-style: none;
  padding-inline-start: 0;
  margin: 0;
}
.nested-list ul,
.nested-list ol {
  padding-inline-start: var(--spacing-md, 12px);
}
.nested-list__item {
  padding-block: var(--spacing-sm, 4px);
}
.nested-list details > summary,
.nested-list summary {
  cursor: pointer;
  list-style: revert;
  font-weight: 500;
  color: var(--color-text, #111827);
}
.nested-list summary:focus-visible {
  outline: 2px solid var(--color-primary, #2563EB);
  outline-offset: 2px;
}
.nested-list__link {
  color: inherit;
  text-decoration: none;
}
.nested-list__link:hover,
.nested-list__link:focus-visible {
  text-decoration: underline;
}
`
}
