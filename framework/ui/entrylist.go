package ui

// ─── EntryList ──────────────────────────────────────────────────────
//
// A dense list of links to read: each entry a title link, the source it
// leads to in muted text beside it, and a meta line under it (who posted
// it, when, its discussion). A news front page, a reading list, a
// digest. Ranked draws the list as an <ol> numbered from Start, so page
// two of a thirty-entry list reads 31, 32, …; the numbers are the
// browser's own list markers, so they cost no markup and read as list
// positions to assistive tech. Distinct from SearchResults, which is a
// whole search page with its form, and from DataTable, which is for
// records with columns.

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
	// Title is the link text. Required.
	Title string
	// Href is where the title leads: another site, or the entry's own
	// page. Required.
	Href string
	// Source is short text beside the title naming where Href leads (a
	// site's domain). Optional.
	Source string
	// Meta is the line under the title: points, author, age, a link to
	// the discussion. It is the caller's markup. Optional.
	Meta render.HTML
	// ID is the row's id, so a page can link to one entry. Optional.
	ID string
}

// EntryListConfig configures an EntryList.
type EntryListConfig struct {
	// Entries are the rows, in order.
	Entries []Entry
	// Label names the list for assistive tech ("Top stories").
	// Required: a page often holds more than one list.
	Label string
	// Ranked numbers the entries, an <ol> starting at Start.
	Ranked bool
	// Start is the first entry's number; less than 1 starts at 1.
	// Ignored unless Ranked.
	Start int
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the list root. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// style, start, aria-label and data-cui-*.
	ExtraAttrs html.Attrs
}

// EntryList renders the list. It panics without a Label.
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
	// The unranked list drops its markers (list-style: none), which
	// costs it list semantics in WebKit/VoiceOver unless it says so.
	attrs["role"] = "list"
	if cfg.Ranked {
		delete(attrs, "role")
		tag = "ol"
		attrs["class"] += " fui-entry-list--ranked"
		attrs["start"] = strconv.Itoa(max(cfg.Start, 1))
	}
	return entryListStyle.WrapHTML(render.Tag(tag, attrs, items...))
}

var entryListStyle = registry.RegisterStyle("ui-entry-list", entryListCSS)

// Knobs: --ui-entry-list-gap (the space between entries) and
// --ui-entry-list-rank-width (the column the rank numbers sit in, wide
// enough for "300."). The items keep display: list-item: a grid or flex
// item draws no ::marker, and the marker is the rank.
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
