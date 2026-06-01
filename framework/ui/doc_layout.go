package ui

// DocLayout — the structural skeleton of a documentation / article page: a
// sticky left nav rail, a centered article column, and an optional right
// table-of-contents rail, with breadcrumbs at the top and a prev/next pager
// at the bottom. It owns the grid, the rails, the crumbs, and the pager; it
// does NOT own the article's prose typography — markdown styling is an
// editorial choice the consuming app keeps.
//
// Three shapes, picked automatically:
//   - nav + content + toc  → three columns
//   - nav + content        → two columns (--notoc)
//   - content only         → single centered column (--narrow)
//
// Pass any render.HTML into the Nav slot (e.g. interactive.SectionMenu) and
// compose the footer with DocPrevNext.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// DocCrumb is one breadcrumb. An empty Href marks the current (last) crumb,
// rendered as plain text rather than a link.
type DocCrumb struct {
	Label string
	Href  string
}

// DocPager configures the prev/next footer. Empty Next* fields omit the next
// card (e.g. on the last page).
type DocPager struct {
	PrevHref, PrevLabel string
	NextHref, NextLabel string
}

// DocLayoutConfig configures a DocLayout.
type DocLayoutConfig struct {
	// Nav is the left rail (use DocNav). Empty → single-column narrow doc.
	Nav render.HTML
	// Crumbs renders a breadcrumb trail above the article body.
	Crumbs []DocCrumb
	// CrumbsLabel overrides the breadcrumb nav's aria-label ("Breadcrumb").
	CrumbsLabel string
	// Toc is the optional right rail (in-page table of contents).
	Toc render.HTML
	// Pager, when set, renders a prev/next footer after the body.
	Pager *DocPager
	Class string
	ID    string
}

// DocLayout assembles the doc page skeleton around the article body.
func DocLayout(cfg DocLayoutConfig, body ...render.HTML) render.HTML {
	cls := "ui-doc-layout"
	switch {
	case cfg.Nav == "":
		cls += " ui-doc-layout--narrow"
	case cfg.Toc == "":
		cls += " ui-doc-layout--notoc"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	articleChildren := make([]render.HTML, 0, len(body)+2)
	if len(cfg.Crumbs) > 0 {
		articleChildren = append(articleChildren, docCrumbs(cfg.Crumbs, cfg.CrumbsLabel))
	}
	articleChildren = append(articleChildren, body...)
	if cfg.Pager != nil {
		articleChildren = append(articleChildren, DocPrevNext(*cfg.Pager))
	}
	article := render.Tag("article",
		map[string]string{"class": "ui-doc-layout__content"}, articleChildren...)

	shellChildren := []render.HTML{}
	if cfg.Nav != "" {
		shellChildren = append(shellChildren, cfg.Nav)
	}
	shellChildren = append(shellChildren, article)
	if cfg.Toc != "" {
		shellChildren = append(shellChildren, cfg.Toc)
	}
	return docLayoutStyle.WrapHTML(
		html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, shellChildren...))
}

func docCrumbs(crumbs []DocCrumb, label string) render.HTML {
	if label == "" {
		label = "Breadcrumb"
	}
	children := make([]render.HTML, 0, len(crumbs)*2)
	for i, c := range crumbs {
		if i > 0 {
			children = append(children,
				html.Span(html.TextConfig{Class: "ui-doc-layout__crumb-sep"}, render.Text("/")))
		}
		if c.Href == "" {
			children = append(children,
				html.Span(html.TextConfig{Class: "ui-doc-layout__crumb-current"}, render.Text(c.Label)))
		} else {
			children = append(children, html.Link(html.LinkConfig{Href: c.Href, Text: c.Label}))
		}
	}
	return render.Tag("nav",
		map[string]string{"class": "ui-doc-layout__crumbs", "aria-label": label}, children...)
}

// DocPrevNext renders the prev/next pager. The previous card is always shown
// (callers point it at an index fallback); the next card is omitted when
// NextHref is empty.
func DocPrevNext(p DocPager) render.HTML {
	cards := []render.HTML{
		html.LinkHTML(html.LinkHTMLConfig{
			Href:  p.PrevHref,
			Class: "ui-doc-layout__prev",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "ui-doc-layout__pager-dir"}, render.Text("← Previous")),
				html.Span(html.TextConfig{Class: "ui-doc-layout__pager-ttl"}, render.Text(p.PrevLabel)),
			),
		}),
	}
	if p.NextHref != "" {
		cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
			Href:  p.NextHref,
			Class: "ui-doc-layout__next",
			Content: render.Join(
				html.Span(html.TextConfig{Class: "ui-doc-layout__pager-dir"}, render.Text("Next →")),
				html.Span(html.TextConfig{Class: "ui-doc-layout__pager-ttl"}, render.Text(p.NextLabel)),
			),
		}))
	}
	return html.Div(html.DivConfig{Class: "ui-doc-layout__foot"},
		html.Div(html.DivConfig{Class: "ui-doc-layout__foot-nav"}, cards...))
}

var docLayoutStyle = registry.RegisterStyle("ui-doc-layout", docLayoutCSS)

func docLayoutCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-doc-layout"] {
  display: grid;
  grid-template-columns: var(--ui-doc-layout-rail, 220px) minmax(0, 1fr) var(--ui-doc-layout-rail, 220px);
  gap: var(--ui-doc-layout-gap, var(--spacing-2xl, 48px));
  max-width: var(--ui-doc-layout-max-width, 1360px);
  margin-inline: auto;
  padding: var(--ui-doc-layout-pad, var(--spacing-xl, 32px));
}
[data-fui-comp="ui-doc-layout"].ui-doc-layout--notoc {
  grid-template-columns: var(--ui-doc-layout-rail, 220px) minmax(0, 1fr);
}
[data-fui-comp="ui-doc-layout"].ui-doc-layout--narrow {
  grid-template-columns: minmax(0, 1fr);
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__content {
  max-width: var(--ui-doc-layout-content-max, 720px);
  min-width: 0;
}
[data-fui-comp="ui-doc-layout"].ui-doc-layout--narrow .ui-doc-layout__content {
  margin-inline: auto;
}

/* The Nav slot brings its own styling (e.g. interactive.SectionMenu, which
   ships the sticky rail + mobile sheet). DocLayout only sizes its column. */

/* Breadcrumbs */
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__crumbs {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--color-text-subtle, #71717A);
  margin-bottom: var(--spacing-lg, 16px);
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__crumbs a {
  color: var(--ui-doc-layout-crumb-link-color, inherit);
  text-decoration: none;
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__crumb-sep { color: var(--ui-doc-layout-crumb-sep-color, inherit); }
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__crumb-current { color: var(--color-text-muted, #52525B); }

/* Prev/next pager */
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__foot {
  margin-top: var(--spacing-2xl, 48px);
  padding-top: var(--spacing-xl, 32px);
  border-top: 1px solid var(--color-border, rgba(0,0,0,0.1));
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__foot-nav {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--spacing-lg, 16px);
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__prev,
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__next {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: var(--spacing-lg, 16px);
  background: var(--color-surface, transparent);
  border: 1px solid var(--color-border, rgba(0,0,0,0.1));
  border-radius: var(--radius-md, 6px);
  text-decoration: none;
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__next { text-align: right; }
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__prev:hover,
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__next:hover {
  border-color: var(--color-border-strong, var(--color-border, rgba(0,0,0,0.2)));
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__pager-dir {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 11px;
  color: var(--color-text-subtle, #71717A);
}
[data-fui-comp="ui-doc-layout"] .ui-doc-layout__pager-ttl { color: var(--color-text, #18181B); font-weight: 500; }

/* Collapse to a single column on narrow viewports. A grid track resolves to
   its content's min-content and overflows; display:block lets each child take
   the container width and the content scroll internally. */
@media (max-width: 900px) {
  [data-fui-comp="ui-doc-layout"] {
    display: block;
    max-width: none;
    padding: var(--spacing-lg, 24px);
  }
  [data-fui-comp="ui-doc-layout"] .ui-doc-layout__content {
    max-width: none;
    overflow-x: hidden;
  }
  [data-fui-comp="ui-doc-layout"] .ui-doc-layout__foot-nav { grid-template-columns: 1fr; }
}`
}
