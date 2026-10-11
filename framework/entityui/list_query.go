package entityui

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// listParams names the fixed query params one list owns, after its key
// is applied: "sort" for an unkeyed list, "due_sort" for .Key("due").
type listParams struct {
	sort   string
	dir    string
	page   string
	q      string
	filter string
	view   string
	cols   string
	saved  string
	// The filter rows' field, operator and value, repeated per row.
	rowF, rowO, rowV string
	// as is the layout the switch picks: "table" or "cards".
	as string
	// per is the rows per page the footer's menu picks.
	per string
}

func listParamsFor(key string) listParams {
	return listParams{
		sort:   param(key, "sort"),
		dir:    param(key, "dir"),
		page:   param(key, "page"),
		q:      param(key, "q"),
		filter: param(key, "filter"),
		view:   param(key, "view"),
		cols:   param(key, "cols"),
		saved:  param(key, "saved"),

		rowF: param(key, "rf"),
		rowO: param(key, "ro"),
		rowV: param(key, "rv"),
		as:   param(key, "as"),
		per:  param(key, "per"),
	}
}

// listState is one list render's resolved request state: what the URL,
// the builder and Display asked for, after every refusal rule ran.
type listState struct {
	m    *meta
	key  string // the namespacing key, "" for the page's one unkeyed list
	p    listParams
	q    url.Values
	path string // the page's own path, for sort/page/view links
	base string // the record-link base, b.Base or the page path
	// relBase is each relation column's related-record base, set when
	// the UI has a record path for the related entity.
	relBase map[string]string
	// titles is each page row's title by id, set by pageTitles.
	titles map[string]string
	// rawRows is the page read without read hooks, by id: set only for
	// a list with InlineEdit, which edits a value only where the hooked
	// and the stored one agree.
	rawRows map[string]map[string]any
	// inlineDrawn is set once a cell's editor is drawn, so the table
	// says how to edit in place.
	inlineDrawn bool

	view string // "" = All
	// implicitView is the view shown with no ?view= param, "" when that
	// is All.
	implicitView string
	viewPred     *filter.Predicate
	viewSorts    []filter.ParsedSort
	as           string // "table" | "cards"
	columns      []string
	available    []string    // the default resolution the cols param and the menu work from
	pins         []listWhere // the builder's Where pins: context, never a column or facet

	search     string
	filterText string
	filterPred *filter.Predicate
	filterBad  bool // the text did not parse: warn, list without it

	sortField string
	sortDesc  bool
	sorts     []filter.ParsedSort

	page  int
	limit int

	// The trash view: offeredTab is whether the strip carries it (the
	// builder asked and the entity soft-deletes); deletedView is whether
	// this render shows it, ?view=deleted.
	offeredTab  bool
	deletedView bool

	// Saved views, on when the builder asked and the UI carries a store.
	savedOn     bool
	savedViews  []SavedView // the caller's, for the strip
	savedID     string      // the open one, "" when none
	savedName   string
	savedFilter string // the open view's filter text, when the URL names none
	savedGone   bool   // the open view no longer applies: callout, All view
}

// facetParam is a facet field's param name: ?<p>f_<field>=.
func (s *listState) facetParam(field string) string {
	return param(s.key, "f_"+field)
}

// pinned reports a field the builder's Where pins.
func (s *listState) pinned(field string) bool {
	for _, w := range s.pins {
		if w.field == field {
			return true
		}
	}
	return false
}

// facets are Display.Facets less the pinned fields: a pinned field holds
// one value on every row, so there is nothing to pick.
func (s *listState) facets() []string {
	var out []string
	for _, f := range s.m.d.Facets {
		if !s.pinned(f) {
			out = append(out, f)
		}
	}
	return out
}

// createHref is New's link: <base>/create, carrying each Where pin as a
// ?prefill_<field>= so the create lands pointed at the list's context
// (a payments list on an invoice creates a payment for that invoice).
// The create screen ignores a pin its form may not set.
func (s *listState) createHref() string {
	if len(s.pins) == 0 {
		return s.base + "/create"
	}
	q := url.Values{}
	for _, w := range s.pins {
		q.Set("prefill_"+w.field, w.value)
	}
	return s.base + "/create?" + q.Encode()
}

// resolveColumns settles the columns a list shows: the builder's, else
// Display's, else the schema-derived default less the pinned fields
// (every row repeats the pin). Every name must be a visible,
// non-omitted field.
func (s *listState) resolveColumns(b *ListBuilder) error {
	names := b.columns
	if len(names) == 0 {
		for _, n := range s.m.columns() {
			if !s.pinned(n) {
				names = append(names, n)
			}
		}
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		if _, ok := s.m.field(name); !ok {
			return fmt.Errorf("entityui: entity %q: list column %q is not a visible field", s.m.name, name)
		}
		if s.m.omitted(name) {
			return fmt.Errorf("entityui: entity %q: list column %q is omitted by Display.Fields", s.m.name, name)
		}
		if seen[name] {
			return fmt.Errorf("entityui: entity %q: list column %q is listed twice", s.m.name, name)
		}
		seen[name] = true
		out = append(out, name)
	}
	s.columns = out
	// The available set the columns menu offers and the cols param and a
	// saved view are measured against: the default resolution itself.
	s.available = slices.Clone(out)
	return nil
}

// sortable is the column guard, ported from resource: displayed and
// sortable are not the same set. A NoQuery column shows its value but
// the API refuses ORDER BY on it, so it renders unsortable and a typed
// or bookmarked ?sort= on it is dropped before it reaches the query.
func (s *listState) sortable(col string) bool {
	f, ok := s.m.field(col)
	return ok && !f.NoQuery
}

// resolveSort reads ?sort= and ?dir=, refusing anything but a shown,
// queryable column; with none, the view's declared Sort is the order.
func (s *listState) resolveSort(b *ListBuilder) {
	col := s.q.Get(s.p.sort)
	if col != "" && s.sortable(col) && !b.top {
		s.sortField = col
		s.sortDesc = s.q.Get(s.p.dir) == "desc"
		s.sorts = []filter.ParsedSort{{Field: col, Desc: s.sortDesc}}
		return
	}
	s.sorts = s.viewSorts
	if len(s.viewSorts) > 0 {
		s.sortField = s.viewSorts[0].Field
		s.sortDesc = s.viewSorts[0].Desc
	}
}

// resolvePage reads ?page= and settles the page size: the builder's,
// else ?per= when it is a size on offer, else the first
// Display.PageSizes entry, else 25, never above the entity's
// Pagination.MaxListLimit.
func (s *listState) resolvePage(b *ListBuilder) {
	limit := b.pageSize
	if limit <= 0 && len(s.m.d.PageSizes) > 0 {
		limit = s.m.d.PageSizes[0]
	}
	if limit <= 0 {
		limit = 25
	}
	// The footer's menu picks among the sizes on offer; anything else
	// on the URL is ignored, never a size of its own.
	if n, err := strconv.Atoi(s.q.Get(s.p.per)); err == nil && slices.Contains(s.pageSizes(b), n) {
		limit = n
	}
	if max := s.maxListLimit(); max > 0 && limit > max {
		limit = max
	}
	s.limit = limit
	page := 1
	if n, err := strconv.Atoi(s.q.Get(s.p.page)); err == nil && n > 1 && !b.top {
		page = n
	}
	s.page = page
}

func (s *listState) maxListLimit() int {
	if s.m.e.Config.Pagination != nil {
		return s.m.e.Config.Pagination.MaxListLimit
	}
	return 0
}

// clampPage lands a requested page inside the real run, ported from
// resource with its comments: ?page=999 on a two-page list is a URL
// anyone can type, and the typed pager refuses a page outside 1..Pages.
// The clamp lands the reader on the last page's rows under a pager
// saying the last page — never an empty page under a pager that claims
// another. No rows means one page: page 1, the empty state, no pager.
// A failed count (known false) is not a run of zero pages: the rows are
// fetched at the page asked for and no pager renders, so nothing is
// refused and the reader is not shown page 1's rows under a URL that
// says another.
func clampPage(page, total, limit int, known bool) int {
	if pages := pagesFor(total, limit); known && page > pages {
		return max(pages, 1)
	}
	return page
}

func pagesFor(total, limit int) int {
	if limit <= 0 {
		return 1
	}
	return max((total+limit-1)/limit, 1)
}

// readFields are the columns the read asks for: the shown ones plus the
// primary key, the title field and the state field (a transition reads
// it from the row). NoQuery columns may be read: NoQuery keeps a field
// out of filters and sorts, not out of the row.
func (s *listState) readFields() []string {
	out := make([]string, 0, len(s.columns)+3)
	seen := map[string]bool{}
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add(s.m.pk)
	for _, c := range s.columns {
		add(c)
	}
	for _, tf := range s.m.titleFields() {
		add(tf)
	}
	if s.m.states != nil {
		add(s.m.states.Field)
	}
	return out
}

// predicate ANDs every narrowing term into one tree: the view's, the
// filter text's, and the builder's pinned .Where terms. ListAll compiles
// the tree as ONE parenthesized clause beside the owner, tenant,
// soft-delete and read scopes, so nothing here can widen past them.
func (s *listState) predicate(b *ListBuilder) (*filter.Predicate, error) {
	return s.narrowed(b, s.viewPred, s.filterPred)
}

// narrowed ANDs a view's predicate and a filter's with the builder's
// Where pins: the list's own predicate, or a view tab's.
func (s *listState) narrowed(b *ListBuilder, view, filt *filter.Predicate) (*filter.Predicate, error) {
	var children []*filter.Predicate
	if view != nil {
		children = append(children, view)
	}
	if filt != nil {
		children = append(children, filt)
	}
	for _, w := range b.where {
		f, ok := s.m.field(w.field)
		if !ok {
			return nil, fmt.Errorf("entityui: entity %q: Where(%q) names a Hidden or unknown field", s.m.name, w.field)
		}
		if f.NoQuery {
			return nil, fmt.Errorf("entityui: entity %q: Where(%q) names a NoQuery field", s.m.name, w.field)
		}
		children = append(children, &filter.Predicate{Field: w.field, Op: filter.OpEq, Value: w.value})
	}
	return andPredicate(children), nil
}

// andPredicate folds trees into one AND group, or passes a lone tree
// through, or returns nil for none. dsl builds the same shape.
func andPredicate(children []*filter.Predicate) *filter.Predicate {
	switch len(children) {
	case 0:
		return nil
	case 1:
		return children[0]
	}
	group := &filter.Predicate{Children: make([]filter.Predicate, 0, len(children))}
	for _, c := range children {
		group.Children = append(group.Children, *c)
	}
	return group
}

// facetFilters are the active facet terms as flat equality filters,
// applied to the count and the page read alike so a facet paginates
// correctly. A bool facet binds a real bool: the control renders
// "true"/"false" and SQLite's INTEGER storage matches no TEXT spelling.
func (s *listState) facetFilters() []filter.ParsedFilter {
	var out []filter.ParsedFilter
	for _, name := range s.facets() {
		fld, ok := s.m.field(name)
		if !ok {
			continue
		}
		v := strings.TrimSpace(s.q.Get(s.facetParam(name)))
		if v == "" {
			continue
		}
		pf := filter.ParsedFilter{Field: name, Op: filter.OpEq, Value: v}
		if fld.Type == schema.Bool {
			pf = pf.Coerced(schema.Bool)
		}
		out = append(out, pf)
	}
	return out
}

// searchedOrFiltered reports a search, a typed or saved filter, or a
// facet narrowing the list.
func (s *listState) searchedOrFiltered() bool {
	return strings.TrimSpace(s.search) != "" || s.filterText != "" || len(s.activeFacets()) > 0
}

// clearSearchHref is the list without its search, filter and facets:
// the view and columns stay, and the sort resets as on any narrowing
// link. An open saved view whose filter narrows the list goes too, the
// way the chip bar's Clear all drops it.
func (s *listState) clearSearchHref() string {
	drop := []string{s.p.q, s.p.filter, s.p.page}
	for _, name := range s.activeFacets() {
		drop = append(drop, s.facetParam(name))
	}
	q := s.carry(drop...)
	if s.savedID != "" && !s.q.Has(s.p.filter) && s.filterText != "" {
		q.Del(s.p.saved)
	}
	return listHref(s.path, q)
}

// activeFacets are the facets with a value in the URL, for carrying
// them across sort and page links.
func (s *listState) activeFacets() []string {
	var out []string
	for _, name := range s.facets() {
		if v := strings.TrimSpace(s.q.Get(s.facetParam(name))); v != "" {
			out = append(out, name)
		}
	}
	return out
}

// carry builds the query a sort, page or view link keeps: this list's
// own search, filter, facets and view, plus every request param the
// list does not own (another keyed list's state on the same URL).
// exclude names params the caller is about to set itself.
func (s *listState) carry(exclude ...string) url.Values {
	drop := map[string]bool{}
	for _, name := range exclude {
		drop[name] = true
	}
	out := url.Values{}
	for k, vs := range s.q {
		if s.ownsParam(k) || drop[k] {
			continue
		}
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	if v := strings.TrimSpace(s.search); v != "" && !drop[s.p.q] {
		out.Set(s.p.q, v)
	}
	if v := s.filterText; v != "" && !drop[s.p.filter] {
		out.Set(s.p.filter, v)
	}
	if !drop[s.p.view] {
		if s.view != "" {
			out.Set(s.p.view, s.view)
		} else if s.implicitView != "" {
			out.Set(s.p.view, allView)
		}
	}
	for _, name := range s.activeFacets() {
		if p := s.facetParam(name); !drop[p] {
			out.Set(p, strings.TrimSpace(s.q.Get(p)))
		}
	}
	return out
}

// ownsParam reports whether name is one of this list's own params.
func (s *listState) ownsParam(name string) bool {
	switch name {
	case s.p.sort, s.p.dir, s.p.page, s.p.q, s.p.filter, s.p.view,
		s.p.rowF, s.p.rowO, s.p.rowV:
		return true
	}
	// A facet param names one of this entity's fields: an unkeyed list's
	// "f_" prefix would otherwise claim a keyed list's params whose key
	// starts with f_.
	field, ok := strings.CutPrefix(name, param(s.key, "f_"))
	if !ok {
		return false
	}
	_, own := s.m.byName[field]
	return own
}

// defaultPageSizes are the rows-per-page choices an entity with no
// Display.PageSizes offers.
var defaultPageSizes = []int{25, 50, 100}

// pageSizes are the sizes the footer offers: the entity's
// Display.PageSizes, else 25, 50 and 100, each within its
// Pagination.MaxListLimit. A builder that fixed its size, a preview and
// an embedded list offer none.
func (s *listState) pageSizes(b *ListBuilder) []int {
	if b.pageSize > 0 || b.top || b.embedded {
		return nil
	}
	sizes := s.m.d.PageSizes
	if len(sizes) == 0 {
		sizes = defaultPageSizes
	}
	max := s.maxListLimit()
	var out []int
	for _, n := range sizes {
		if n > 0 && (max <= 0 || n <= max) {
			out = append(out, n)
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// listHref renders path plus query.
func listHref(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
