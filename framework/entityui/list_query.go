package entityui

import (
	"fmt"
	"net/url"
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
}

func listParamsFor(key string) listParams {
	return listParams{
		sort:   param(key, "sort"),
		dir:    param(key, "dir"),
		page:   param(key, "page"),
		q:      param(key, "q"),
		filter: param(key, "filter"),
		view:   param(key, "view"),
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

	view      string // "" = All
	viewPred  *filter.Predicate
	viewSorts []filter.ParsedSort
	as        string // "table" | "cards"
	columns   []string

	search     string
	filterText string
	filterPred *filter.Predicate
	filterBad  bool // the text did not parse: warn, list without it

	sortField string
	sortDesc  bool
	sorts     []filter.ParsedSort

	page  int
	limit int
}

// facetParam is a facet field's param name: ?<p>f_<field>=.
func (s *listState) facetParam(field string) string {
	return param(s.key, "f_"+field)
}

// resolveColumns settles the columns a list shows: the builder's, else
// Display's, else the schema-derived default. Every name must be a
// visible, non-omitted field.
func (s *listState) resolveColumns(b *ListBuilder) error {
	names := b.columns
	if len(names) == 0 {
		names = s.m.columns()
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
func (s *listState) resolveSort() {
	col := s.q.Get(s.p.sort)
	if col != "" && s.sortable(col) {
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
// else the first Display.PageSizes entry, else 25, never above the
// entity's Pagination.MaxListLimit.
func (s *listState) resolvePage(b *ListBuilder) {
	limit := b.pageSize
	if limit <= 0 && len(s.m.d.PageSizes) > 0 {
		limit = s.m.d.PageSizes[0]
	}
	if limit <= 0 {
		limit = 25
	}
	if max := s.maxListLimit(); max > 0 && limit > max {
		limit = max
	}
	s.limit = limit
	page := 1
	if n, err := strconv.Atoi(s.q.Get(s.p.page)); err == nil && n > 1 {
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
	add(s.m.titleField())
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
	var children []*filter.Predicate
	if s.viewPred != nil {
		children = append(children, s.viewPred)
	}
	if s.filterPred != nil {
		children = append(children, s.filterPred)
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
	for _, name := range s.m.d.Facets {
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

// activeFacets are the facets with a value in the URL, for carrying
// them across sort and page links.
func (s *listState) activeFacets() []string {
	var out []string
	for _, name := range s.m.d.Facets {
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
	if s.view != "" && !drop[s.p.view] {
		out.Set(s.p.view, s.view)
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
	case s.p.sort, s.p.dir, s.p.page, s.p.q, s.p.filter, s.p.view:
		return true
	}
	return strings.HasPrefix(name, param(s.key, "f_"))
}

// listHref renders path plus query.
func listHref(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
