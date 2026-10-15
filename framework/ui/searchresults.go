package ui

// ─── SearchResults ──────────────────────────────────────────────────
//
// A full-page list of search results: a search form that GETs the page
// itself, a polite status line counting the matches, an ordered list of
// result pages (title link, group, excerpt with its hits marked, and the
// matching sections as deep links), and pagination below. Everything is
// server-rendered and works with no script; battery/sitesearch's
// `sitesearch-page` module hydrates it so typing updates the list in
// place. Distinct from CommandPalette, whose options are combobox rows
// a reader cannot follow without script.

import (
	"context"
	"maps"
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// SearchResult is one matching page.
type SearchResult struct {
	URL, Title string
	// Group labels the result ("Docs"); empty shows none.
	Group string
	// Excerpt is the page's text around its hits.
	Excerpt []SearchSegment
	// Sections are the page's matching sections, each a deep link.
	Sections []SearchSection
}

// SearchSection is one matching section of a result page.
type SearchSection struct {
	URL, Heading string
	Excerpt      []SearchSegment
}

// SearchSegment is a run of excerpt text; Hit marks a run that matched
// the query, drawn in <mark>.
type SearchSegment struct {
	Text string
	Hit  bool
}

// SearchResultsConfig configures a SearchResults.
type SearchResultsConfig struct {
	// Action is the page's own path, which the form GETs. Required.
	Action string
	// Query is the query the results answer. Empty draws the form
	// alone.
	Query string
	// Results are the page of results shown.
	Results []SearchResult
	// Total counts every match, of which Results are one page.
	Total int
	// Page is the page shown, 1-based, and PerPage how many results a
	// page holds. A Total over PerPage draws pagination.
	Page, PerPage int
	// ID is the search input's id. Default "search".
	ID string
	// Ctx resolves the status words and labels. When nil, English
	// applies.
	Ctx   context.Context
	Class string
	// ExtraAttrs forwards additional attributes to the <section> root.
	// Keys the component owns are dropped or overwritten: class,
	// style, data-cui-* and the data-search-page* hooks.
	ExtraAttrs html.Attrs
}

// searchQueryParam and searchPageParam are the query parameters the
// page reads: the form's field and pagination's.
const (
	searchQueryParam = "q"
	searchPageParam  = "page"
)

// SearchResults renders the results page body. It panics without an
// Action: the form must reach the page without script.
func SearchResults(cfg SearchResultsConfig) render.HTML {
	if cfg.Action == "" {
		panic("ui: SearchResults requires Action")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	id := cfg.ID
	if id == "" {
		id = "search"
	}
	count := i18nui.T(ctx, i18nui.KeySearchResultsCount)
	one := i18nui.T(ctx, i18nui.KeySearchResultsOne)
	none := i18nui.T(ctx, i18nui.KeySearchResultsNone)

	form := SearchInput(SearchInputConfig{
		Name: searchQueryParam, ID: id, Action: cfg.Action, Ctx: ctx,
		// A query names identifiers (datatable, ScreenPath): a phone must
		// not capitalize or correct them.
		ExtraAttrs: map[string]string{
			"value": cfg.Query, "data-search-page-input": "",
			"autocapitalize": "none", "autocorrect": "off", "spellcheck": "false",
		},
	})
	status := render.Tag("p", map[string]string{
		"class": "fui-search-results__status", "role": "status", "data-search-page-status": "",
	}, render.Text(searchStatus(strings.TrimSpace(cfg.Query), cfg.Total, count, one, none)))

	items := make([]render.HTML, len(cfg.Results))
	for i, r := range cfg.Results {
		items[i] = searchResultItem(r)
	}
	list := render.Tag("ol", map[string]string{
		"class": "fui-search-results__list", "data-search-page-list": "",
		"aria-label": i18nui.T(ctx, i18nui.KeySearchResultsLabel),
	}, items...)

	parts := []render.HTML{headless.Own(form), headless.Own(status), headless.Own(list)}
	if cfg.PerPage > 0 && cfg.Total > cfg.PerPage {
		pages := 1 + (cfg.Total-1)/cfg.PerPage
		page := min(max(cfg.Page, 1), pages)
		parts = append(parts, headless.Own(render.Tag("div", map[string]string{
			"class": "fui-search-results__pager", "data-search-page-pager": "",
		}, Pagination(PaginationConfig{
			Page: page, Pages: pages, Path: cfg.Action, PageParam: searchPageParam,
			Query: url.Values{searchQueryParam: {cfg.Query}}, Ctx: ctx,
		}))))
	}
	cls := "fui-search-results"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	maps.Copy(attrs, html.Attrs{
		"class":                  cls,
		"data-search-page":       cfg.Action,
		"data-search-page-query": cfg.Query,
		"data-search-page-count": count,
		"data-search-page-one":   one,
		"data-search-page-none":  none,
		"data-search-page-more":  i18nui.T(ctx, i18nui.KeySearchResultsMore),
		"data-search-page-limit": strconv.Itoa(max(cfg.PerPage, 0)),
	})
	return searchResultsStyle.WrapHTML(render.Tag("section", attrs, parts...))
}

// searchStatus words the status line: empty with no query.
func searchStatus(q string, total int, count, one, none string) string {
	if q == "" {
		return ""
	}
	f := count
	switch total {
	case 0:
		f = none
	case 1:
		f = one
	}
	return strings.NewReplacer("{n}", strconv.Itoa(total), "{q}", q).Replace(f)
}

func searchResultItem(r SearchResult) render.HTML {
	head := []render.HTML{searchLink("fui-search-results__title", r.URL, r.Title)}
	if r.Group != "" {
		head = append(head, render.Tag("span", map[string]string{"class": "fui-search-results__group"}, render.Text(r.Group)))
	}
	kids := []render.HTML{render.Tag("div", map[string]string{"class": "fui-search-results__head"}, head...)}
	if len(r.Excerpt) > 0 {
		kids = append(kids, searchExcerpt(r.Excerpt))
	}
	if len(r.Sections) > 0 {
		secs := make([]render.HTML, len(r.Sections))
		for i, s := range r.Sections {
			sec := []render.HTML{searchLink("fui-search-results__section", s.URL, s.Heading)}
			if len(s.Excerpt) > 0 {
				sec = append(sec, searchExcerpt(s.Excerpt))
			}
			secs[i] = render.Tag("li", nil, sec...)
		}
		kids = append(kids, render.Tag("ul", map[string]string{"class": "fui-search-results__sections"}, secs...))
	}
	return render.Tag("li", map[string]string{"class": "fui-search-results__item"}, kids...)
}

func searchLink(class, href, text string) render.HTML {
	return render.Tag("a", map[string]string{"class": class, "href": urlsafe.CleanAnchor(href)}, render.Text(text))
}

func searchExcerpt(segs []SearchSegment) render.HTML {
	runs := make([]render.HTML, len(segs))
	for i, s := range segs {
		if s.Hit {
			runs[i] = render.Tag("mark", nil, render.Text(s.Text))
		} else {
			runs[i] = render.Text(s.Text)
		}
	}
	return render.Tag("p", map[string]string{"class": "fui-search-results__excerpt"}, runs...)
}

var searchResultsStyle = registry.RegisterStyle("ui-search-results", searchResultsCSS)

// Knobs: --ui-search-results-gap (the space between results).
func searchResultsCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-search-results"] {
  display: grid;
  gap: var(--spacing-lg, 16px);
  min-width: 0;
}
[data-cui-comp="ui-search-results"] .fui-search {
  inline-size: 100%;
}
[data-cui-comp="ui-search-results"] .fui-search-results__status {
  margin: 0;
  color: var(--color-text-muted);
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="ui-search-results"] .fui-search-results__status:empty {
  display: none;
}
[data-cui-comp="ui-search-results"] .fui-search-results__list {
  display: grid;
  gap: var(--ui-search-results-gap, var(--spacing-xl, 24px));
  margin: 0;
  padding: 0;
  list-style: none;
}
[data-cui-comp="ui-search-results"] .fui-search-results__item {
  display: grid;
  gap: var(--spacing-xs, 2px);
  min-width: 0;
}
[data-cui-comp="ui-search-results"] .fui-search-results__head {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--spacing-xs, 2px) var(--spacing-md, 8px);
}
[data-cui-comp="ui-search-results"] .fui-search-results__title {
  font-size: var(--text-lg, 1.125rem);
  font-weight: var(--font-weight-semibold, 600);
  color: var(--color-primary);
  text-decoration: none;
  overflow-wrap: anywhere;
}
[data-cui-comp="ui-search-results"] .fui-search-results__title:hover,
[data-cui-comp="ui-search-results"] .fui-search-results__section:hover {
  text-decoration: underline;
}
[data-cui-comp="ui-search-results"] .fui-search-results__group {
  color: var(--color-text-subtle, #71717A);
  font-size: var(--text-xs, 0.75rem);
}
[data-cui-comp="ui-search-results"] .fui-search-results__excerpt {
  margin: 0;
  color: var(--color-text-muted);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-normal, 1.5);
  overflow-wrap: anywhere;
}
[data-cui-comp="ui-search-results"] .fui-search-results__excerpt mark {
  background: transparent;
  color: var(--color-text);
  font-weight: var(--font-weight-medium);
}
[data-cui-comp="ui-search-results"] .fui-search-results__sections {
  display: grid;
  gap: var(--spacing-sm, 4px);
  margin: var(--spacing-xs, 2px) 0 0;
  padding-inline-start: var(--spacing-md, 8px);
  border-inline-start: var(--stroke-thin, 1px) solid var(--color-border);
  list-style: none;
}
[data-cui-comp="ui-search-results"] .fui-search-results__section {
  color: var(--color-text);
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  text-decoration: none;
}
`
}
