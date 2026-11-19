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

type listParams struct {
	sort             string
	dir              string
	page             string
	q                string
	filter           string
	view             string
	cols             string
	saved            string
	rowF, rowO, rowV string
	as               string
	per              string
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

type listState struct {
	m           *meta
	key         string
	p           listParams
	q           url.Values
	path        string
	base        string
	createPath  string
	relBase     map[string]string
	titles      map[string]string
	rawRows     map[string]map[string]any
	inlineDrawn bool
	masked      map[string]bool

	view         string
	implicitView string
	viewPred     *filter.Predicate
	viewSorts    []filter.ParsedSort
	as           string
	columns      []string
	available    []string
	pins         []listWhere

	search     string
	filterText string
	filterPred *filter.Predicate
	filterBad  bool

	sortField string
	sortDesc  bool
	sorts     []filter.ParsedSort

	page  int
	limit int

	offeredTab  bool
	deletedView bool

	savedOn     bool
	savedViews  []SavedView
	savedID     string
	savedName   string
	savedFilter string
	savedGone   bool
}

func (s *listState) facetParam(field string) string {
	return param(s.key, "f_"+field)
}

func (s *listState) pinned(field string) bool {
	for _, w := range s.pins {
		if w.field == field {
			return true
		}
	}
	return false
}

func (s *listState) facets() []string {
	var out []string
	for _, f := range s.m.d.Facets {
		if !s.pinned(f) {
			out = append(out, f)
		}
	}
	return out
}

func (s *listState) createHref() string {
	if len(s.pins) == 0 {
		return s.createPath
	}
	q := url.Values{}
	for _, w := range s.pins {
		q.Set("prefill_"+w.field, w.value)
	}
	return s.createPath + "?" + q.Encode()
}

func (s *listState) resolveColumns(b *ListBuilder) error {
	names := b.columns
	if len(names) == 0 {
		for _, n := range s.m.columns() {
			if !s.pinned(n) {
				names = append(names, n)
			}
		}
	}
	if len(names) == 0 {
		for _, k := range s.m.keys {
			if _, ok := s.m.field(k); ok && !s.m.omitted(k) {
				names = append(names, k)
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
	s.available = slices.Clone(out)
	return nil
}

func (s *listState) sortable(col string) bool {
	f, ok := s.m.field(col)
	return ok && !f.NoQuery
}

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

func (s *listState) resolvePage(b *ListBuilder) {
	limit := b.pageSize
	if limit <= 0 && len(s.m.d.PageSizes) > 0 {
		limit = s.m.d.PageSizes[0]
	}
	if limit <= 0 {
		limit = 25
	}
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
	for _, k := range s.m.keys {
		add(k)
	}
	for _, c := range s.columns {
		add(c)
	}
	for _, tf := range s.m.titleFields() {
		add(tf)
	}
	if s.m.states != nil {
		add(s.m.states.Field)
	}
	if s.as == "cards" {
		card := cardFieldsOf(s)
		add(card.title)
		add(card.subtitle)
		add(card.badge)
		for _, name := range card.meta {
			add(name)
		}
	}
	return out
}

func (s *listState) predicate(b *ListBuilder) (*filter.Predicate, error) {
	return s.narrowed(b, s.viewPred, s.filterPred)
}

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

func (s *listState) searchedOrFiltered() bool {
	return strings.TrimSpace(s.search) != "" || s.filterText != "" || len(s.activeFacets()) > 0
}

func (s *listState) clearSearchHref() string {
	drop := []string{s.p.q, s.p.filter, s.p.page}
	for _, name := range s.activeFacets() {
		drop = append(drop, s.facetParam(name))
	}
	q := s.carry(drop...)
	s.clearFilter(q)
	return listHref(s.path, q)
}

func (s *listState) clearFilter(q url.Values) {
	if s.savedID == "" {
		return
	}
	if s.q.Has(s.p.filter) {
		q.Set(s.p.filter, "")
		return
	}
	if s.filterText != "" {
		q.Del(s.p.saved)
	}
}

func (s *listState) activeFacets() []string {
	var out []string
	for _, name := range s.facets() {
		if v := strings.TrimSpace(s.q.Get(s.facetParam(name))); v != "" {
			out = append(out, name)
		}
	}
	return out
}

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
	} else if v == "" && !drop[s.p.filter] && !drop[s.p.saved] && s.savedID != "" && s.q.Has(s.p.filter) {
		out.Set(s.p.filter, "")
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

func (s *listState) ownsParam(name string) bool {
	switch name {
	case s.p.sort, s.p.dir, s.p.page, s.p.q, s.p.filter, s.p.view,
		s.p.rowF, s.p.rowO, s.p.rowV:
		return true
	}
	field, ok := strings.CutPrefix(name, param(s.key, "f_"))
	if !ok {
		return false
	}
	_, own := s.m.byName[field]
	return own
}

func (s *listState) narrowsParam(name string) bool {
	switch name {
	case s.p.filter, s.p.view, s.p.saved, s.p.rowF, s.p.rowO, s.p.rowV:
		return true
	case s.p.q:
		return len(s.m.e.Config.SearchFields) > 0
	}
	field, ok := strings.CutPrefix(name, param(s.key, "f_"))
	if !ok {
		return false
	}
	_, own := s.m.byName[field]
	return own
}

var defaultPageSizes = []int{25, 50, 100}

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

func listHref(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
