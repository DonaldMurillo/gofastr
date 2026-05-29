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
	"strings"

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
	// A dangerous Href (javascript:/vbscript:/data:/protocol-relative/
	// control bytes) is dropped and the crumb degrades to a plain
	// <span> rather than a clickable XSS vector. An empty result from
	// safeURL also means "no link", so it folds into the current heuristic.
	href := safeURL(c.Href)
	current := c.Current || href == ""
	if current {
		return render.Tag("li", nil,
			render.Tag("span",
				map[string]string{"aria-current": "page"},
				render.Text(c.Text)),
		)
	}
	return render.Tag("li", nil,
		render.Tag("a", map[string]string{"href": href}, render.Text(c.Text)),
	)
}

// safeURL returns u if it is safe to render as an href, and "" if it
// carries a script-executing or origin-ambiguous scheme. Permitted:
// http(s), mailto, tel, relative paths, fragment- and query-only
// references. Dropped: javascript:/vbscript:/data:/file:/blob: and any
// other non-allow-listed scheme, protocol-relative "//host", and any
// value containing control bytes or percent-encoded CR/LF. Mirrors
// framework/ui/safety.go::safeURL and the sibling tree/nestedlist
// builders — the patterns layer bypasses that helper, so the allow-list
// is enforced here.
func safeURL(u string) string {
	if u == "" {
		return ""
	}
	for i := 0; i < len(u); i++ {
		if c := u[i]; c < 0x20 || c == 0x7f {
			return ""
		}
	}
	trimmed := strings.TrimLeft(u, " \t")
	low := strings.ToLower(trimmed)
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return ""
	}
	// Protocol-relative URLs are ambiguous about origin trust.
	if strings.HasPrefix(trimmed, "//") {
		return ""
	}
	// Fragment-only, query-only, or relative paths pass.
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "?") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") {
		return u
	}
	for i := 0; i < len(trimmed); i++ {
		switch c := trimmed[i]; c {
		case ':':
			switch strings.ToLower(trimmed[:i]) {
			case "http", "https", "mailto", "tel":
				return u
			default:
				return ""
			}
		case '/', '?', '#':
			// No scheme before the first path/query/fragment delimiter
			// — relative reference, allowed.
			return u
		}
	}
	// No colon — bare relative reference.
	return u
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
