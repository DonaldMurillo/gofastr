// Package breadcrumbs renders an ARIA-correct breadcrumb trail.
//
// Output structure:
//
//	<nav aria-label="Breadcrumb">
//	  <ol class="breadcrumbs">
//	    <li><a href="/">Home</a></li>
//	    <li><a href="/docs/">Docs</a></li>
//	    <li><span aria-current="page">Components</span></li>
//	  </ol>
//	</nav>
//
// The last crumb (with empty Href OR Current=true) renders as a <span>
// with aria-current="page". Separators are CSS-driven (no extra DOM).
package breadcrumbs

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Style is the registered stylesheet handle. New's <nav> goes through
// Style.WrapHTML so the data-fui-comp marker is emitted and the
// runtime auto-loads the CSS on first appearance.
var Style = registry.RegisterStyle("breadcrumbs", styleFn)

func styleFn(_ style.Theme) string { return baseCSS }

// Crumb is one step in the breadcrumb trail.
type Crumb struct {
	Text string
	Href string // empty → renders as the current/last crumb
	// Current overrides the empty-Href heuristic: set true on a Crumb
	// with a Href if you want it marked aria-current="page" anyway
	// (e.g. the page itself appearing in its own trail with a link).
	Current bool
}

// Config configures the breadcrumb nav element.
type Config struct {
	Label string // optional aria-label for the <nav>; defaults to "Breadcrumb"
	ID    string
	Class string
}

// New renders the breadcrumb trail.
//
// Panics if no crumbs are provided.
func New(cfg Config, crumbs ...Crumb) render.HTML {
	if len(crumbs) == 0 {
		panic("breadcrumbs: New requires at least one Crumb")
	}
	label := cfg.Label
	if label == "" {
		label = "Breadcrumb"
	}

	cls := "breadcrumbs"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}

	items := make([]render.HTML, len(crumbs))
	for i, c := range crumbs {
		items[i] = renderCrumb(c)
	}

	listAttrs := map[string]string{"class": cls}
	if cfg.ID != "" {
		listAttrs["id"] = cfg.ID
	}
	return Style.WrapHTML(render.Tag("nav", map[string]string{"aria-label": label},
		render.Tag("ol", listAttrs, items...),
	))
}

func renderCrumb(c Crumb) render.HTML {
	if c.Text == "" {
		panic("breadcrumbs: Crumb requires Text")
	}
	current := c.Current || c.Href == ""
	if current {
		return render.Tag("li", nil,
			render.Tag("span",
				map[string]string{"aria-current": "page"},
				render.Text(c.Text)),
		)
	}
	return render.Tag("li", nil,
		render.Tag("a", map[string]string{"href": c.Href}, render.Text(c.Text)),
	)
}

// baseCSS is the stylesheet for breadcrumbs. Tokens: --color-text-muted,
// --color-text, --color-primary, --spacing-sm.
const baseCSS = `
.breadcrumbs {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm, 4px);
  list-style: none;
  margin: 0;
  padding: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #6B7280);
}
.breadcrumbs > li {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
.breadcrumbs > li + li::before {
  content: '/';
  color: var(--color-text-muted, #6B7280);
  opacity: 0.5;
}
.breadcrumbs a {
  color: var(--color-text-muted, #6B7280);
  text-decoration: none;
}
.breadcrumbs a:hover {
  color: var(--color-primary, #4F46E5);
  text-decoration: underline;
}
.breadcrumbs [aria-current="page"] {
  color: var(--color-text, #1F2937);
  font-weight: 600;
}
`
