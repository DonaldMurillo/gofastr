package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// Entry is one row of an EntryList.
type Entry struct {
	// Title and Href are required.
	Title string
	Href  string
	// Source names where Href leads, beside the title.
	Source string
	// Meta is the caller's markup for the line under the title.
	Meta render.HTML
	ID   string
}

// EntryListConfig configures an EntryList.
type EntryListConfig struct {
	Entries []Entry
	// Label names the list. Required.
	Label string
	// Ranked draws an <ol> numbered from Start (at least 1).
	Ranked bool
	Start  int
	// Long marks a list rendered unpaginated that may run to hundreds of
	// rows: rows far off-screen skip layout and paint until scrolled near
	// (content-visibility: auto). They stay in the accessibility tree,
	// reachable by Tab and by find-in-page.
	Long  bool
	ID    string
	Class string
	// ExtraAttrs land on the list; class, id, style, start, aria-label
	// and data-cui-* are the component's. A role replaces role="list".
	ExtraAttrs html.Attrs
}

// EntryList renders a dense list of links to read, each with a source
// and a meta line. It panics without a Label.
func EntryList(cfg EntryListConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: EntryList requires Label — the page may hold more than one list")
	}
	items := make([]render.HTML, len(cfg.Entries))
	for i, e := range cfg.Entries {
		head := []render.HTML{render.Tag("a", map[string]string{
			"class": "fui-entry-list__title", "href": urlsafe.CleanAnchor(e.Href),
		}, render.Text(e.Title))}
		if e.Source != "" {
			head = append(head, render.Tag("span", map[string]string{"class": "fui-entry-list__source"}, render.Text(e.Source)))
		}
		kids := []render.HTML{headless.Own(render.Tag("div", map[string]string{"class": "fui-entry-list__head"}, head...))}
		if e.Meta != "" {
			kids = append(kids, render.Tag("div", map[string]string{"class": "fui-entry-list__meta"}, e.Meta))
		}
		attrs := map[string]string{"class": "fui-entry-list__item"}
		if e.ID != "" {
			attrs["id"] = e.ID
		}
		item := render.Tag("li", attrs, kids...)
		if e.Meta == "" {
			item = headless.Own(item)
		}
		items[i] = item
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id", "start", "aria-label")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	cls := "fui-entry-list"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs["class"] = cls
	attrs["aria-label"] = cfg.Label
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	tag := "ul"
	if _, set := attrs["role"]; !set && !cfg.Ranked {
		attrs["role"] = "list"
	}
	if cfg.Ranked {
		tag = "ol"
		attrs["class"] += " fui-entry-list--ranked"
		attrs["start"] = strconv.Itoa(max(cfg.Start, 1))
	}
	if cfg.Long {
		attrs["class"] += " fui-entry-list--long"
	}
	return entryListStyle.WrapHTML(render.Tag(tag, attrs, items...))
}

var entryListStyle = registry.RegisterStyle("ui-entry-list", entryListCSS)

func entryListCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-entry-list"] {
  display: grid;
  gap: var(--ui-entry-list-gap, var(--spacing-md, 8px));
  margin: 0;
  padding: 0;
  list-style: none;
  min-width: 0;
}
[data-cui-comp="ui-entry-list"].fui-entry-list--ranked {
  padding-inline-start: var(--ui-entry-list-rank-width, 4.5ch);
  list-style: decimal;
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__item {
  min-width: 0;
}
/* A long list's rows skip rendering off-screen. Paint containment clips
   at the row's edge, so the clip edge is widened to keep a focus ring
   and, on a ranked list, the number in the gutter. */
[data-cui-comp="ui-entry-list"].fui-entry-list--long .fui-entry-list__item {
  content-visibility: auto;
  contain-intrinsic-size: auto var(--ui-entry-list-row-estimate, 3rem);
  overflow-clip-margin: calc(var(--stroke-focus, 2px) + var(--stroke-focus-offset, 2px));
}
[data-cui-comp="ui-entry-list"].fui-entry-list--long.fui-entry-list--ranked .fui-entry-list__item {
  overflow-clip-margin: var(--ui-entry-list-rank-width, 4.5ch);
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__meta {
  margin-block-start: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__item::marker {
  color: var(--color-text-muted);
  font-size: var(--text-sm, 0.875rem);
  font-variant-numeric: tabular-nums;
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0 var(--spacing-sm, 4px);
  min-width: 0;
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__title {
  color: var(--color-text);
  font-weight: var(--font-weight-medium, 500);
  line-height: var(--leading-snug, 1.4);
  text-decoration: none;
  overflow-wrap: anywhere;
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__title:visited {
  color: var(--color-text-muted);
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__title:hover {
  text-decoration: underline;
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__source,
[data-cui-comp="ui-entry-list"] .fui-entry-list__meta {
  color: var(--color-text-muted);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-snug, 1.4);
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__source {
  overflow-wrap: anywhere;
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__meta a {
  color: inherit;
  text-decoration: none;
}
[data-cui-comp="ui-entry-list"] .fui-entry-list__meta a:hover {
  color: var(--color-text);
  text-decoration: underline;
}
`
}
