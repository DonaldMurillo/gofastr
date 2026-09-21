package headless

import (
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The table: named columns of sortable cells, the structure every
// list screen and every embedded result region renders. Sorting is
// typed props — the active sort, the query the screen carries, the
// parameter names — and every href is built through net/url, never by
// substituting into a caller's pattern string: a column key or a
// carried value that could eat a parameter it was never meant to is
// a defect class this component cannot express, not a guard it runs.

// Table parts. PartCaption, PartHeader and PartBody are the shared
// names. PartRoot is the wrapper div, PartScroll the focusable
// region the table horizontally scrolls inside, and PartTable the
// table itself.
const (
	PartTable  Part = "table"
	PartScroll Part = "scroll"
	PartHead   Part = "head"
	PartRow    Part = "row"
	PartSort   Part = "sort"
	PartCell   Part = "cell"
	PartEmpty  Part = "empty"
)

// Column describes one table column.
type Column struct {
	// Key identifies the column: the value under SortParam in the
	// sort href, and the key a Row's Cells are matched by. Required.
	// Anything a query can encode is a legal Key — that is the point
	// of building the href through net/url.
	Key string
	// Header is the visible column header text. May be empty: an
	// actions or icon column. A header-less column that cannot be
	// sorted is hidden from assistive tech; one that can is named
	// from Strings.TableSortBy, because an anchor with no text is a
	// control with no name.
	Header string
	// Sortable makes the header a sort control: an anchor that
	// re-submits the screen's query with this column's Key and the
	// next direction.
	Sortable bool
	// HeaderAttrs and CellAttrs add attributes to this column's <th>
	// and to every <td> under it, through the same sanitiser as
	// ExtraAttrs (Safe): data-fui-* keys, style, id and class are
	// dropped, names are stored folded, and one attribute under two
	// spellings is refused.
	HeaderAttrs html.Attrs
	CellAttrs   html.Attrs

	// Variant is class-map vocabulary for the whole column: looked
	// up as "<part>--<variant>" on this column's <th> (PartHeader)
	// and each of its <td> (PartCell), so a class map can align a
	// column's text or shade its column without the structure
	// caring. Empty means none.
	Variant string
}

// Row is one table row. The shape is ui.DataTable's, so a caller
// moves between the two without repacking.
type Row struct {
	// ID lands on the <tr> as id=, for a keyed swap: successive
	// renders of a live table differ only on the rows that changed.
	ID string
	// Cells maps a column Key to the rendered cell HTML. A column
	// with no entry renders an empty cell; a key naming no column is
	// refused at render, because a silent drop hides a typo.
	Cells map[string]render.HTML
}

// SortDir is the direction of a sort.
type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

// TableProps configures a table.
type TableProps struct {
	// Columns is the column definitions. Required.
	Columns []Column
	// Rows is the rendered rows.
	Rows []Row
	// Caption is the table's caption: text, escaped. A table is named
	// by its caption and by nothing else — there is no AriaLabel —
	// and a caller who wants the table named gives one.
	Caption string

	// SortBy is the active sort column's Key. Empty means no column
	// is sorted.
	SortBy string
	// SortDir is the active sort's direction. Empty means ascending,
	// the direction an inactive column's anchor sorts by.
	SortDir SortDir

	// Path is the screen's own path: each sort href is it plus the
	// carried query, the sort parameters replaced. Empty means the
	// current document — a relative "?query" href. When set it must
	// be same-origin: a sort anchor pointing off-origin is a mistake
	// refused at render.
	Path string
	// Query is the query the screen's URL already carries — the
	// search, the filters, the page — and survives a sort beside the
	// sort keys.
	Query url.Values
	// SortParam and DirParam name the sort key and direction
	// parameters. They default to "sort" and "dir".
	SortParam string
	DirParam  string

	// Island is where a sort goes when the table is embedded in a
	// region rather than being the page: the sort anchors then carry
	// the RPC contract beside their hrefs — the page without script,
	// the region update with it, the URL written after the swap.
	// Optional, the Form posture: the URL is the truth for a list,
	// and a list screen's sort anchors are plain navigations the
	// client router intercepts when script is present. An Island
	// that looks wired and is not is refused, whichever shape the
	// table renders.
	Island Island

	// Empty renders in the body's one row when there are no rows, in
	// a cell spanning every column. Nil renders an empty <tbody>;
	// the head stays either way — an empty result still has named
	// columns, and the sort controls stay usable.
	Empty render.HTML
	// Summary is a sentence about the result window the caller owns,
	// e.g. "Showing 8 of 10". Appended to the sort sentence the
	// announcement carries, with a space, when given: the primitive
	// knows the sort, and only the caller knows the window.
	Summary string
	// Footer renders after the scroll region, as its sibling inside
	// the root, never inside the table, so a pager's nav landmark
	// never nests in a table. Nil renders nothing — the scroll region
	// is the root's only child.
	Footer render.HTML

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on every part. Nothing here is fillable — the
	// cells are the caller's own content, and a slot that replaced a
	// header would drop the sort control inside it.
	Parts Parts
	// Strings are the strings this component says. Nil means the English
	// defaults; a layer above sets them from the request's language.
	Strings *Strings
}

// Table renders the list: one wrapper div (PartRoot) holding a
// focusable scroll region (PartScroll) around the table, and the
// footer, when there is one, as the region's sibling. The scroll
// region is the element a class map makes the horizontal scroll
// surface — the one a wide table on a narrow screen scrolls inside —
// and it is markup, not styling, because a scroll region that cannot
// take focus cannot be scrolled by keyboard.
//
// The explicit ARIA roles stay on every element. They look redundant
// on a displayed <table> and are not: a cards collapse sets
// display:block on the table's elements, and a table element
// displayed as a block loses its implicit table semantics in Chromium
// and WebKit. The roles are what keep a collapsed table a table for
// assistive technology.
//
// aria-sort is the only sort indicator rendered — ascending and
// descending on the active column, none on every other sortable one —
// and the stylesheet draws from the attribute, so state and
// appearance cannot disagree.
func Table(p TableProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	w := p.Strings.Resolve()

	if len(p.Columns) == 0 {
		panic("headless: Table requires Columns — a table with no columns has no cells to render and, empty, a slot spanning nothing")
	}
	for i, col := range p.Columns {
		if col.Key == "" {
			panic("headless: Table column " + strconv.Itoa(i) + " has no Key — Row.Cells are matched by column Key, so a column without one can neither be sorted nor receive a cell")
		}
	}
	if !p.Island.zero() {
		p.Island.check()
	}
	if p.Path != "" {
		checkSameOrigin("Table", "Path", p.Path)
		if strings.ContainsAny(p.Path, "?#") {
			panic("headless: Table Path " + strconv.Quote(p.Path) +
				" carries its own query or fragment — the carry belongs in Query, where it survives the sort; a Path query is silently replaced, and a value lost with no error is the defect this component exists to make structural")
		}
	}
	cols := make(map[string]bool, len(p.Columns))
	for _, col := range p.Columns {
		// Two columns under one Key would render the same cell twice
		// and sort to the same href: one answer claimed by two headers.
		if cols[col.Key] {
			panic("headless: Table has two columns with the Key " + strconv.Quote(col.Key) + " — Row.Cells are matched by Key, so a repeated one is two columns claiming one cell")
		}
		cols[col.Key] = true
	}
	for _, r := range p.Rows {
		for _, k := range slices.Sorted(maps.Keys(r.Cells)) {
			if !cols[k] {
				panic("headless: Table Row cells name " + strconv.Quote(k) +
					", which is no column's Key — a cell key that matches no column is a typo, not an omission; give the column that Key or drop the cell")
			}
		}
	}

	sortParam := orDefault(p.SortParam, "sort")
	dirParam := orDefault(p.DirParam, "dir")

	capID := ""
	if p.Caption != "" {
		// The caption is the scroll region's name as well as the
		// table's: the id is what aria-labelledby on the region
		// points at, derived the way Section derives its title's —
		// from the ID when there is one, else a slug of the caption
		// (two tables with the same caption on one page collide,
		// which is the reason ID exists).
		capID = p.ID
		if capID == "" {
			capID = slugID("table", p.Caption)
		}
		capID += "-caption"
	}
	kids := []render.HTML{}
	if p.Caption != "" {
		kids = append(kids, b.El("caption", PartCaption,
			Attrs(map[string]string{"id": capID}), render.Text(p.Caption)))
	}
	kids = append(kids, tableHead(b, p, w, sortParam, dirParam), tableBody(b, p))

	// The scroll region is a markup fact, not a styling one: a wide
	// table on a narrow screen scrolls somewhere, and a region that
	// can only be scrolled with a mouse fails WCAG 2.1.1 (axe's
	// scrollable-region-focusable). tabindex is 0 whether or not the
	// table overflows today, because the server cannot know the
	// viewport and a region that becomes scrollable at a narrow
	// width must already be reachable. This is Adrian Roselli's
	// responsive-table pattern: role=region, named by the caption,
	// keyboard-focusable. No caption, no name: the region renders
	// without aria-labelledby rather than pointing at nothing.
	scroll := Attrs(map[string]string{"tabindex": "0", "role": "region"})
	if capID != "" {
		scroll["aria-labelledby"] = capID
	}
	// The scroll region is the focus fallback for an island sort whose
	// column the answer dropped: focus stays inside the table the
	// reader is reading.
	Mark(scroll, "data-hui-table-scroll")
	scrollRegion := b.El("div", PartScroll, scroll,
		b.El("table", PartTable, Attrs(map[string]string{"role": "table"}), kids...))

	// The footer is the scroll region's sibling, never inside the
	// table, so a pager's nav landmark never nests in one.
	rootKids := []render.HTML{scrollRegion}
	if p.Footer != "" {
		rootKids = append(rootKids, p.Footer)
	}
	// The status is the root's last child on every table, island or
	// plain, because a plain table's next page announces too. Empty on
	// the server: the sentence is the answer's, rendered into the
	// announcement attribute, and the module copies it in after a
	// swap — clear then frame, so a repeated identical sentence is
	// said again. role=status already means polite; stating aria-live
	// too can announce twice.
	rootKids = append(rootKids,
		b.El("span", PartStatus, Mark(Attrs(map[string]string{"role": "status"}), "data-hui-table-status")))
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	// The module marker, the signal it records the clicked sort
	// against, and the sentence it copies: everything the behaviour
	// needs travels on the root the component rendered.
	Mark(own, "data-hui-table")
	if !p.Island.zero() {
		own["data-hui-table-signal"] = p.Island.Signal
	}
	if ann := tableAnnouncement(p, w); ann != "" {
		own["data-hui-table-announcement"] = ann
	}
	return b.El("div", PartRoot, own, rootKids...)
}

// tableAnnouncement composes the sentence a reader is told after the
// region changed: the sort the answer carries, in the reader's words,
// and the caller's Summary of the window when there is one. The
// column is named by its Header, or its Key when the header is empty
// — the anchor's own naming rule, so the sentence and the control the
// reader clicked agree on what the column is called. No sort and no
// Summary is no announcement: an attribute nothing says is noise the
// module would copy into the status for nothing.
func tableAnnouncement(p TableProps, w *Strings) string {
	if p.SortBy == "" {
		return p.Summary
	}
	name := p.SortBy
	for _, col := range p.Columns {
		if col.Key == p.SortBy {
			if col.Header != "" {
				name = col.Header
			}
			break
		}
	}
	// An empty SortDir means ascending, the same reading the sort
	// anchors give it.
	dir := w.SortAscending
	if p.SortDir == SortDesc {
		dir = w.SortDescending
	}
	sentence := strings.ReplaceAll(w.TableSortedBy, "{column}", name)
	sentence = strings.ReplaceAll(sentence, "{direction}", dir)
	if p.Summary != "" {
		sentence += " " + p.Summary
	}
	return sentence
}

// tableHead renders the head: one row of column headers.
func tableHead(b Box, p TableProps, w *Strings, sortParam, dirParam string) render.HTML {
	cells := make([]render.HTML, len(p.Columns))
	for i, col := range p.Columns {
		cells[i] = tableHeaderCell(b, p, w, col, sortParam, dirParam)
	}
	// The explicit rowgroup and row roles are load-bearing, not
	// redundant: a cards collapse sets display:block on these
	// elements, and a table element displayed as a block loses its
	// implicit table semantics in Chromium and WebKit. The roles are
	// what keep a collapsed table a table for assistive tech.
	return b.El("thead", PartHead, Attrs(map[string]string{"role": "rowgroup"}),
		b.El("tr", PartRow, Attrs(map[string]string{"role": "row"}), cells...))
}

// tableHeaderCell renders one <th>.
//
// A sortable column's header is an anchor; a non-sortable one is
// text. A non-sortable column with no Header — an actions column — is
// hidden from assistive tech: its cells are self-evidently labelled
// (View, Edit, Delete) and an empty column header announces nothing,
// the axe empty-table-header rule included.
func tableHeaderCell(b Box, p TableProps, w *Strings, col Column, sortParam, dirParam string) render.HTML {
	if !col.Sortable {
		th := Attrs(map[string]string{"scope": "col", "role": "columnheader"})
		if col.Header == "" {
			th["aria-hidden"] = "true"
		}
		if cls := b.Classes.Variant(PartHeader, col.Variant); cls != "" {
			th["class"] = cls
		}
		return b.El("th", PartHeader,
			Merge(Safe(col.HeaderAttrs, "scope", "role", "aria-sort", "aria-hidden", "class"), th),
			render.Text(col.Header))
	}

	// The active column's anchor flips the direction; every other
	// sortable column's sorts ascending.
	dir := p.SortDir
	if dir == "" {
		dir = SortAsc
	}
	next, ariaSort := SortAsc, "none"
	if p.SortBy != "" && col.Key == p.SortBy {
		if dir == SortDesc {
			ariaSort, next = "descending", SortAsc
		} else {
			ariaSort, next = "ascending", SortDesc
		}
	}
	href := tableSortHref(p, col.Key, next, sortParam, dirParam)

	a := Attrs(map[string]string{"href": href})
	if col.Header == "" {
		// The anchor's text is empty, so its name comes from Strings
		// with the column's Key in it: a nameless control cannot be
		// told from the one beside it.
		a["aria-label"] = strings.ReplaceAll(w.TableSortBy, "{column}", col.Key)
	}
	if !p.Island.zero() {
		// The sort key the module records on click and looks for in
		// the swapped-in table. Island anchors only: a plain table's
		// sort is a navigation the router already focuses, and a hook
		// nothing reads on it is markup carried for no one.
		a["data-hui-table-sort"] = col.Key
		// The same element is both destinations: the href is the page
		// without script, the island contract is the region update
		// with it, and the query is shared so the two answer one
		// question.
		a = Merge(a, p.Island.attrs(href, "GET"))
	}
	th := Attrs(map[string]string{"scope": "col", "role": "columnheader", "aria-sort": ariaSort})
	if cls := b.Classes.Variant(PartHeader, col.Variant); cls != "" {
		th["class"] = cls
	}
	return b.El("th", PartHeader,
		Merge(Safe(col.HeaderAttrs, "scope", "role", "aria-sort", "aria-hidden", "class"), th),
		b.El("a", PartSort, a, render.Text(col.Header)))
}

// tableSortHref builds one sort anchor's href: the carried query with
// the sort key and direction replaced, on p.Path. Built through
// net/url and never by substitution. An empty Path is the current
// document, a relative "?query" href.
func tableSortHref(p TableProps, key string, dir SortDir, sortParam, dirParam string) string {
	// The carried query is scrubbed, not refused, because it is
	// request state; see scrubbedQuery.
	q := scrubbedQuery(p.Query)
	// Set replaces, and that is the contract: the query a screen
	// carries may still hold the last sort, and sort=old&sort=name is
	// two answers to one question. Add would append; Set does not.
	q.Set(sortParam, key)
	q.Set(dirParam, string(dir))

	href := "?" + q.Encode()
	if p.Path != "" {
		u, err := url.Parse(p.Path)
		if err != nil {
			panic("headless: Table Path " + strconv.Quote(p.Path) + " does not parse as a URL: " + err.Error())
		}
		u.RawQuery = q.Encode()
		href = u.String()
	}
	// Every href this package writes goes through the anchor policy.
	// Same-origin, the parse and the query refusal have already
	// refused the scheme, the control bytes and the fragment; this
	// catches what can still hide in a path — a percent-encoded
	// newline being the one left.
	if urlsafe.CleanAnchor(href) == "" {
		panic("headless: Table Path " + strconv.Quote(p.Path) + " is not a URL the anchor policy allows")
	}
	return href
}

// scrubbedQuery copies q with every C0 control byte and DEL removed
// from its keys and values, dropping a pair that scrubs to nothing.
// The query a component carries is request state, unlike Path: a
// value with a control byte in it — a crafted ?q= with CR LF —
// percent-encodes to %0D%0A, which the anchor policy refuses for
// every URL this framework writes, and a refusal here would be a 500
// from a link. So the bytes are stripped rather than refused: a
// control byte is never a search a user meant, and a key or value
// that scrubs to nothing is dropped rather than carried as an empty
// filter. Path stays a refusal, because Path is configuration.
func scrubbedQuery(q url.Values) url.Values {
	out := url.Values{}
	for k, vs := range q {
		k = scrubControlBytes(k)
		if k == "" {
			continue
		}
		for _, v := range vs {
			if v = scrubControlBytes(v); v != "" {
				out.Add(k, v)
			}
		}
	}
	return out
}

// scrubControlBytes removes every C0 control byte and DEL from s.
func scrubControlBytes(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func tableBody(b Box, p TableProps) render.HTML {
	if len(p.Rows) == 0 {
		if p.Empty == "" {
			return b.El("tbody", PartBody, Attrs(map[string]string{"role": "rowgroup"}))
		}
		return b.El("tbody", PartBody, Attrs(map[string]string{"role": "rowgroup"}),
			b.El("tr", PartRow, Attrs(map[string]string{"role": "row"}),
				b.El("td", PartEmpty,
					Attrs(map[string]string{"role": "cell", "colspan": strconv.Itoa(len(p.Columns))}),
					p.Empty)))
	}
	rows := make([]render.HTML, len(p.Rows))
	for i, r := range p.Rows {
		cells := make([]render.HTML, len(p.Columns))
		for j, col := range p.Columns {
			td := Attrs(map[string]string{"role": "cell"})
			if col.Header != "" {
				// The header text a cards-collapse stylesheet shows
				// beside the value. Always rendered, never behind a
				// responsive flag: it is content, and a class map
				// that does not collapse simply does not read it.
				td["data-label"] = col.Header
			}
			// The column's variant, when the class map has one for
			// it: the one channel a class map has for styling a whole
			// column, so alignment and shading are the map's to
			// choose without the structure caring.
			if cls := b.Classes.Variant(PartCell, col.Variant); cls != "" {
				td["class"] = cls
			}
			cells[j] = b.El("td", PartCell,
				Merge(Safe(col.CellAttrs, "role", "data-label", "class"), td),
				r.Cells[col.Key])
		}
		rows[i] = b.El("tr", PartRow, Attrs(map[string]string{"role": "row", "id": r.ID}), cells...)
	}
	return b.El("tbody", PartBody, Attrs(map[string]string{"role": "rowgroup"}), rows...)
}

func init() {
	Register(Spec{
		Name: "Table",
		Anatomy: []Part{PartRoot, PartScroll, PartTable, PartCaption, PartHead, PartRow, PartHeader,
			PartSort, PartBody, PartCell, PartEmpty, PartStatus},
		Hooks: []string{"data-hui-table", "data-hui-table-signal", "data-hui-table-sort",
			"data-hui-table-scroll", "data-hui-table-status", "data-hui-table-announcement"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Table(TableProps{
				Caption: "Applications", Path: "/apps",
				Columns: []Column{
					{Key: "name", Header: "Name", Sortable: true},
					{Key: "env", Header: "Environment", Sortable: true},
				},
				Rows:   []Row{{ID: "app-1", Cells: map[string]render.HTML{"name": render.Text("blog"), "env": render.Text("production")}}},
				SortBy: "name", SortDir: SortAsc,
				Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "sorted, carrying a query, one header-less sortable column",
				Why: "aria-sort on the active column and none on the rest, the href's sort and dir replaced rather than " +
					"appended so the query the screen carries survives a sort, and the header-less column's anchor named from " +
					"Strings — an anchor with no text is a control with no name",
				HTML: Table(TableProps{
					Caption: "Applications", Path: "/apps",
					Query: url.Values{"q": {"blog"}, "page": {"3"}, "sort": {"old"}, "dir": {"desc"}},
					Columns: []Column{
						{Key: "name", Header: "Name", Sortable: true},
						{Key: "env", Sortable: true},
						{Key: "region", Header: "Region"},
					},
					SortBy: "name", SortDir: SortAsc,
					Rows: []Row{
						{ID: "app-1", Cells: map[string]render.HTML{"name": render.Text("blog"), "env": render.Text("production"), "region": render.Text("eu-1")}},
						{ID: "app-2", Cells: map[string]render.HTML{"name": render.Text("shop"), "env": render.Text("staging"), "region": render.Text("us-1")}},
					},
				}, s),
			}, {
				Name: "the same table as an island",
				Why: "an embedded table's sort anchors carry the RPC contract beside their hrefs — the page without " +
					"script, the region update with it, the query shared so the two answer one question — and the endpoint " +
					"that carries its own query joins rather than stacks a second one; the announcement carries the sort " +
					"sentence with the caller's Summary appended, for the module to copy into the status after the swap",
				HTML: Table(TableProps{
					Path:  "/apps",
					Query: url.Values{"q": {"blog"}},
					Columns: []Column{
						{Key: "name", Header: "Name", Sortable: true},
						{Key: "env", Header: "Environment", Sortable: true},
					},
					SortBy: "env", SortDir: SortDesc,
					Island: Island{Endpoint: "/island/apps?keep=1", Signal: "apps"},
					Rows: []Row{
						{ID: "app-1", Cells: map[string]render.HTML{"name": render.Text("blog"), "env": render.Text("production")}},
					},
					Summary: "Showing 1 of 8",
				}, s),
			}, {
				Name: "empty with a slot",
				Why: "an empty result still has named columns and usable sort controls — the head stays — and the one " +
					"cell that spans them says what the caller means by nothing here, in the caller's words",
				HTML: Table(TableProps{
					Caption: "Applications", Path: "/apps",
					Columns: []Column{
						{Key: "name", Header: "Name", Sortable: true},
						{Key: "env", Header: "Environment"},
					},
					Empty: render.HTML(`<p>No applications match.</p>`),
				}, s),
			}, {
				Name: "with a footer",
				Why: "the footer renders as the scroll region's sibling, not inside the table, so a pager's nav " +
					"landmark never nests in one — and the region is on the page with and without it, focusable either way",
				HTML: Table(TableProps{
					Caption: "Deployments",
					Columns: []Column{
						{Key: "app", Header: "Application"},
						{Key: "when", Header: "Deployed"},
					},
					Rows:   []Row{{Cells: map[string]render.HTML{"app": render.Text("blog"), "when": render.Text("2 minutes ago")}}},
					Footer: render.HTML(`<nav aria-label="Deployment pages">Page 1 of 2</nav>`),
				}, s),
			}, {
				Name: "bare, nothing to click",
				Why: "a read-only table is still a table: the caption is its name, the roles and scope are all that " +
					"assistive technology is given, the header-less actions column is hidden rather than announced empty, " +
					"and the scroll region is focusable even though this table has nothing to click — overflow is the " +
					"viewport's decision, not the caller's",
				HTML: Table(TableProps{
					Caption: "Recent deployments",
					Columns: []Column{
						{Key: "app", Header: "Application"},
						{Key: "status", Header: "Status"},
						{Key: "actions"},
					},
					Rows: []Row{{Cells: map[string]render.HTML{"app": render.Text("blog"), "status": render.Text("healthy"), "actions": render.Text("View")}}},
				}, s),
			}}
		},
	})
}
