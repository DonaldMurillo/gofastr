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

// Table parts. PartRoot is the wrapper, PartScroll the focusable scroll region, PartTable the table.
const (
	PartTable       Part = "table"
	PartScroll      Part = "scroll"
	PartHead        Part = "head"
	PartRow         Part = "row"
	PartSort        Part = "sort"
	PartCell        Part = "cell"
	PartEmpty       Part = "empty"
	PartTableSelect Part = "table-select" // the label around a select-all checkbox
)

// Column describes one table column.
type Column struct {
	Key         string     // required: the sort value and the key Row.Cells match by
	Header      string     // may be empty: an actions or icon column
	Sortable    bool       // the header becomes a sort anchor
	HeaderAttrs html.Attrs // through Safe, on this column's <th>
	CellAttrs   html.Attrs // through Safe, on every <td> under it
	SelectAll   string     // the header becomes a checkbox governing the row checkboxes of this name
	Variant     string     // class-map vocabulary for the <th> and every <td>
}

// Row is one table row, in ui.DataTable's shape.
type Row struct {
	ID    string                 // the <tr> id, for a keyed swap
	Cells map[string]render.HTML // by column Key; a key naming no column panics
}

// SortDir is the direction of a sort.
type SortDir string

const (
	SortAsc  SortDir = "asc"
	SortDesc SortDir = "desc"
)

// TableProps configures a table.
type TableProps struct {
	Columns []Column // required
	Rows    []Row
	Caption string // the table's only name, escaped text

	SortBy  string  // the active sort column's Key; empty sorts nothing
	SortDir SortDir // empty means ascending

	Path      string     // same-origin path the sort hrefs point at; empty is the current document
	Query     url.Values // the query the screen carries, kept across a sort
	SortParam string     // "sort" by default
	DirParam  string     // "dir" by default

	Island Island // set when the table is embedded in a region: sort anchors carry the RPC contract

	Empty   render.HTML // the one body cell spanning every column when there are no rows
	Summary string      // the caller's sentence about the result window, appended to the announcement
	Footer  render.HTML // rendered after the scroll region, never inside the table

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts    // nothing is fillable
	Strings *Strings // nil means the English defaults
}

// Table renders a wrapper (PartRoot) holding a focusable scroll region around the
// table, the footer, and a status the behaviour announces through.
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

	scroll := Attrs(map[string]string{"tabindex": "0", "role": "region"})
	if capID != "" {
		scroll["aria-labelledby"] = capID
	}
	Mark(scroll, "data-hui-table-scroll")
	emptyHasCallerContent := len(p.Rows) == 0 && p.Empty != ""
	if !emptyHasCallerContent {
		scroll = Internal(scroll)
	}
	scrollRegion := b.El("div", PartScroll, scroll,
		b.El("table", PartTable, Attrs(map[string]string{"role": "table"}), kids...))

	rootKids := []render.HTML{scrollRegion}
	if p.Footer != "" {
		rootKids = append(rootKids, p.Footer)
	}
	rootKids = append(rootKids,
		b.El("span", PartStatus, Internal(Mark(Attrs(map[string]string{"role": "status"}), "data-hui-table-status"))))
	own := Merge(Safe(p.ExtraAttrs), Attrs(map[string]string{"id": p.ID}))
	Mark(own, "data-hui-table")
	if !p.Island.zero() {
		own["data-hui-table-signal"] = p.Island.Signal
	}
	if ann := tableAnnouncement(p, w); ann != "" {
		own["data-hui-table-announcement"] = ann
	}
	return b.El("div", PartRoot, own, rootKids...)
}

// tableAnnouncement is the sort sentence plus the caller's Summary; no sort and no Summary is none.
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
	dir := w.SortAscending
	if p.SortDir == SortDesc {
		dir = w.SortDescending
	}
	sentence := w.fill(w.TableSortedBy, "column", name, "direction", dir)
	if p.Summary != "" {
		sentence += " " + p.Summary
	}
	return sentence
}

func tableHead(b Box, p TableProps, w *Strings, sortParam, dirParam string) render.HTML {
	cells := make([]render.HTML, len(p.Columns))
	for i, col := range p.Columns {
		cells[i] = tableHeaderCell(b, p, w, col, sortParam, dirParam)
	}
	return b.El("thead", PartHead, Attrs(map[string]string{"role": "rowgroup"}),
		b.El("tr", PartRow, Attrs(map[string]string{"role": "row"}), cells...))
}

func tableHeaderCell(b Box, p TableProps, w *Strings, col Column, sortParam, dirParam string) render.HTML {
	if col.SelectAll != "" {
		if col.Sortable {
			panic("headless: Table Column " + strconv.Quote(col.Key) + " is SelectAll and Sortable; a header holds one control")
		}
		th := Attrs(map[string]string{"scope": "col", "role": "columnheader"})
		if cls := b.Classes.Variant(PartHeader, col.Variant); cls != "" {
			th["class"] = cls
		}
		name := col.Header
		if name == "" {
			name = w.TableSelectAll
		}
		box := Mark(Attrs(map[string]string{"type": "checkbox", "aria-label": name}), "data-hui-table-select-all")
		box["data-hui-table-select-all"] = col.SelectAll
		return b.El("th", PartHeader,
			Merge(Safe(col.HeaderAttrs, "scope", "role", "aria-sort", "aria-hidden", "class"), th),
			b.El("label", PartTableSelect, nil, b.El("input", PartControl, box)))
	}
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
		a["aria-label"] = w.fill(w.TableSortBy, "column", col.Key)
	}
	if !p.Island.zero() {
		a["data-hui-table-sort"] = col.Key
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

// tableSortHref builds a sort href through net/url: the carried query with sort and dir set.
func tableSortHref(p TableProps, key string, dir SortDir, sortParam, dirParam string) string {
	q := scrubbedQuery(p.Query)
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
	if urlsafe.CleanAnchor(href) == "" {
		panic("headless: Table Path " + strconv.Quote(p.Path) + " is not a URL the anchor policy allows")
	}
	return href
}

// scrubbedQuery copies q without C0 control bytes and DEL, dropping a key or value that scrubs to empty.
func scrubbedQuery(q url.Values) url.Values {
	out := url.Values{}
	for k, vs := range q {
		k = scrubControlBytes(k)
		if k == "" {
			continue
		}
		for _, v := range vs {
			if v == "" {
				out.Add(k, v)
			} else if v = scrubControlBytes(v); v != "" {
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
				td["data-label"] = col.Header
			}
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
			PartSort, PartBody, PartCell, PartEmpty, PartStatus, PartTableSelect, PartControl},
		Hooks: []string{"data-hui-table", "data-hui-table-signal", "data-hui-table-sort",
			"data-hui-table-scroll", "data-hui-table-status", "data-hui-table-announcement",
			"data-hui-table-select-all"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Table(TableProps{
				Caption: "Applications", Path: "/apps",
				Columns: []Column{
					{Key: "pick", SelectAll: "ids"},
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
				Name: "with a select-all column",
				Why: "the header checkbox governs the row checkboxes its column names, inside this table only; it carries " +
					"no name, so the form never submits it, and it is named from Strings when its column has no header",
				HTML: Table(TableProps{
					Caption: "Applications", Path: "/apps",
					Columns: []Column{
						{Key: "pick", SelectAll: "ids"},
						{Key: "name", Header: "Name"},
					},
					Rows: []Row{
						{ID: "app-1", Cells: map[string]render.HTML{"pick": Choice(ChoiceProps{Type: "checkbox", Name: "ids", Value: "app-1", Label: "Select blog"}, k.For("Choice")), "name": render.Text("blog")}},
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
