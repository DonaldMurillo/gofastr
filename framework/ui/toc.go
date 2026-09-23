package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── TableOfContents ────────────────────────────────────────────────
//
// Renders headless.TableOfContents dressed with the fui-toc class
// map: a labelled <nav> whose ordered list of "#anchor" links the
// SERVER rendered from explicit Items. The items are required: a
// server-rendered component cannot discover headings that only exist
// in a future browser DOM, and a TOC the runtime fills at hydration is
// an empty landmark a reader without script never sees. The
// headless-toc module's only job is the active state — aria-current
// and a class on the entry whose heading is in view, through the
// observer headless-rail owns.

// TOCItem is one entry in the contents list.
type TOCItem struct {
	// ID is the fragment id of the heading this entry links to,
	// without the leading #. Required.
	ID string
	// Label is the entry's visible text. Required.
	Label string
	// Level is the heading's level, 1 to 6; 0 takes 2. It names the
	// item's modifier class (fui-toc__item--h3, …) so the sheet can
	// indent by depth.
	Level int
}

// TOCConfig configures a TableOfContents.
type TOCConfig struct {
	// Items are the entries, in document order. Required: the
	// no-script contract is this rendered list.
	Items []TOCItem

	// Target is the CSS selector of the content region whose headings
	// the module watches for the active state (e.g. "main",
	// "article"). Optional: without it the links work and nothing is
	// marked.
	Target string

	// Label is the accessible nav-label (defaults to "On this page").
	Label string

	// Sticky adds the position: sticky modifier. Default false: the
	// nav scrolls with the content unless the caller opts in.
	Sticky bool

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the <nav> root.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-hui-* (the toc wiring), and aria-label (use Label).
	ExtraAttrs html.Attrs
}

// TableOfContents renders the contents navigation.
func TableOfContents(cfg TOCConfig) render.HTML {
	items := make([]headless.TOCItem, len(cfg.Items))
	for i, it := range cfg.Items {
		items[i] = headless.TOCItem{ID: it.ID, Label: it.Label, Level: it.Level}
	}
	classes := map[headless.Part]string{
		headless.PartRoot:    "fui-toc",
		headless.PartTOCList: "fui-toc__list",
		headless.PartTOCItem: "fui-toc__item",
		headless.PartTOCLink: "fui-toc__link",
	}
	for l := 1; l <= 6; l++ {
		classes[headless.Part("toc-item--h"+strconv.Itoa(l))] = "fui-toc__item--h" + strconv.Itoa(l)
	}
	if cfg.Sticky {
		classes[headless.PartRoot] += " fui-toc--sticky"
	}
	if cfg.Class != "" {
		classes[headless.PartRoot] += " " + cfg.Class
	}
	nav := headless.TableOfContents(headless.TableOfContentsProps{
		Label:          cfg.Label,
		Items:          items,
		TargetSelector: cfg.Target,
		ID:             cfg.ID,
		ExtraAttrs:     headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-label"),
	}, classes)
	return tocStyle.WrapHTML(nav)
}

var tocStyle = registry.RegisterStyle("ui-toc", tocCSS)

func tocCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-toc"] {
  display: block;
  font-size: var(--text-sm, 0.875rem);
}
[data-fui-comp="ui-toc"].fui-toc--sticky {
  position: sticky;
  inset-block-start: var(--spacing-lg, 16px);
  align-self: start;
  max-block-size: calc(100vh - 4rem);
  overflow-y: auto;
}
[data-fui-comp="ui-toc"]::before {
  content: attr(aria-label);
  display: block;
  font-size: var(--text-xs, 0.75rem);
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-text-muted, #52525B);
  margin-block-end: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-toc"] .fui-toc__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-toc"] .fui-toc__item {
  margin: 0;
}
[data-fui-comp="ui-toc"] .fui-toc__item--h3 {
  margin-inline-start: var(--spacing-md, 8px);
}
[data-fui-comp="ui-toc"] .fui-toc__item--h4,
[data-fui-comp="ui-toc"] .fui-toc__item--h5,
[data-fui-comp="ui-toc"] .fui-toc__item--h6 {
  margin-inline-start: calc(var(--spacing-md, 8px) * 2);
}
[data-fui-comp="ui-toc"] .fui-toc__link {
  display: block;
  padding: var(--spacing-sm, 4px) var(--spacing-sm, 4px);
  border-radius: var(--radii-sm, 4px);
  border-inline-start: 2px solid transparent;
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
  line-height: 1.4;
}
[data-fui-comp="ui-toc"] .fui-toc__link:hover {
  color: var(--color-text, #18181B);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-toc"] .fui-toc__link.is-active,
[data-fui-comp="ui-toc"] .fui-toc__link[aria-current="true"] {
  color: var(--color-primary, #4F46E5);
  border-inline-start-color: var(--color-primary, #4F46E5);
  font-weight: 600;
}
[data-fui-comp="ui-toc"] .fui-toc__link:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}`
}
