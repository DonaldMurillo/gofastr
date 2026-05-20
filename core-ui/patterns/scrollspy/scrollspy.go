// Package scrollspy attaches IntersectionObserver-based section
// tracking to any list of in-page anchors. As the user scrolls, the
// runtime sets `aria-current="true"` and `.is-active` on the anchor
// whose target is currently in view.
//
// This is the standalone pattern extracted from the TOC widget's
// runtime — use it for sidebars, sticky in-page nav bars, or any
// list of `<a href="#id">` links that should reflect scroll position.
//
// Wiring:
//
//	scrollspy.Wrap(scrollspy.Config{ObserveSelector: "main"},
//	    nestedlist.Render(nestedlist.Config{ Items: ... }),
//	)
//
// The wrapping element gains `data-fui-scrollspy="<selector>"` and
// the runtime auto-loads `scrollspy.js` on first appearance.
package scrollspy

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. Wrap calls Style.WrapHTML
// so the data-fui-comp marker is emitted and the runtime auto-loads
// the CSS on first appearance.
var Style = registry.RegisterStyle("scrollspy", styleFn)

func styleFn(_ style.Theme) string { return baseCSS }

// Config configures the scrollspy behavior.
type Config struct {
	// ObserveSelector is a CSS selector for the region whose anchored
	// elements should be observed (e.g. "main", "article",
	// ".doc-body"). Required.
	ObserveSelector string

	// TargetSelector overrides the default "h2[id], h3[id]" — set it
	// when the section anchors aren't headings (e.g. "section[id]",
	// "[data-spy]").
	TargetSelector string

	// Class is appended to the wrapper's class list.
	Class string

	// ID optionally tags the wrapper.
	ID string
}

// Wrap returns a `<div>` carrying the scrollspy data-attrs around
// `child`. The child is typically a nav list of `<a href="#id">`
// anchors (e.g. a `core-ui/patterns/nestedlist` render output).
func Wrap(cfg Config, child render.HTML) render.HTML {
	if cfg.ObserveSelector == "" {
		panic("scrollspy: Wrap requires ObserveSelector")
	}
	cls := "scrollspy"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"class":              cls,
		"data-fui-scrollspy": cfg.ObserveSelector,
	}
	if cfg.TargetSelector != "" {
		attrs["data-fui-scrollspy-target"] = cfg.TargetSelector
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return Style.WrapHTML(render.Tag("div", attrs, child))
}

const baseCSS = `
/* Style hook for the active anchor — the runtime sets both
   aria-current="true" and .is-active so apps can target either. */
.scrollspy a.is-active,
.scrollspy a[aria-current="true"] {
  color: var(--color-primary, #2563EB);
  font-weight: 600;
}
`
