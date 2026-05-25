package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── TableOfContents ────────────────────────────────────────────────
//
// Auto-builds from headings inside a target selector. Server emits an
// empty <nav> shell; the runtime walks the target region after first
// paint, harvests every <h2>/<h3> with an id, and renders a sticky
// list with IntersectionObserver-driven active-section tracking.
//
// No-JS users see an empty nav (sized so it doesn't shift layout
// either way) and the in-document headings remain navigable.

// TOCConfig configures a TableOfContents.
type TOCConfig struct {
	// Target is the CSS selector of the content region whose headings
	// the runtime should scan. Required (e.g. "main", "article").
	Target string
	// Label is the accessible nav-label (defaults to "On this page").
	Label string
	// Levels picks which heading levels to harvest. Bit flags:
	// 2 = h2 only, 3 = h3 only, 0 / 5 = h2 + h3. Default 0.
	// We keep the API minimal — most sites want h2 + h3.
	Levels int
	// Sticky toggles the position: sticky behaviour. Default true.
	// When false the nav scrolls with the content.
	Sticky bool
	ID     string
	Class  string
	ExtraAttrs  html.Attrs
}

// TableOfContents renders a TOC nav that the runtime fills in.
func TableOfContents(cfg TOCConfig) render.HTML {
	if cfg.Target == "" {
		panic("ui: TableOfContents requires Target")
	}
	label := cfg.Label
	if label == "" {
		label = "On this page"
	}
	cls := "ui-toc"
	if cfg.Sticky || (!cfg.Sticky && cfg.Class == "") {
		// Default behaviour is sticky; opt-out is Sticky=false AND
		// override via class. The branch above always picks sticky
		// when no explicit class override is given.
	}
	if cfg.Sticky {
		cls += " ui-toc--sticky"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{
		"class":              cls,
		"aria-label":         label,
		"data-fui-toc":       cfg.Target,
		"data-fui-toc-levels": tocLevelsAttr(cfg.Levels),
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}
	return tocStyle.WrapHTML(render.Tag("nav", attrs,
		// Empty <ol> the runtime fills.
		render.Tag("ol", map[string]string{"class": "ui-toc__list"}),
	))
}

func tocLevelsAttr(l int) string {
	switch l {
	case 2:
		return "2"
	case 3:
		return "3"
	default:
		return "2,3"
	}
}

var tocStyle = registry.RegisterStyle("ui-toc", tocCSS)

func tocCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-toc"] {
  display: block;
  font-size: 0.9rem;
}
[data-fui-comp="ui-toc"].ui-toc--sticky {
  position: sticky;
  inset-block-start: var(--spacing-lg, 24px);
  align-self: start;
  max-block-size: calc(100vh - 4rem);
  overflow-y: auto;
}
[data-fui-comp="ui-toc"]::before {
  content: attr(aria-label);
  display: block;
  font-size: 0.75rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-text-muted, #52525B);
  margin-block-end: var(--spacing-sm, 8px);
}
[data-fui-comp="ui-toc"] .ui-toc__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 2px;
}
[data-fui-comp="ui-toc"] .ui-toc__item {
  margin: 0;
}
[data-fui-comp="ui-toc"] .ui-toc__item--h3 {
  margin-inline-start: var(--spacing-md, 12px);
}
[data-fui-comp="ui-toc"] .ui-toc__link {
  display: block;
  padding: 4px var(--spacing-sm, 8px);
  border-radius: var(--radii-sm, 4px);
  border-inline-start: 2px solid transparent;
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
  line-height: 1.4;
}
[data-fui-comp="ui-toc"] .ui-toc__link:hover {
  color: var(--color-text, #18181B);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-toc"] .ui-toc__link.is-active {
  color: var(--color-primary, #4F46E5);
  border-inline-start-color: var(--color-primary, #4F46E5);
  font-weight: 600;
}
[data-fui-comp="ui-toc"] .ui-toc__link:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}`
}
