package headless

import (
	stdhtml "html"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The table's own contract. The universal sweeps (harness_test.go)
// cover every spec; these are the promises specific to a table, the
// way lightbox_test.go holds the viewer's.

// The sort anchor is the same element in both postures. A list screen
// without an Island renders a plain navigation the client router
// intercepts — the URL is the truth for a list — and an embedded table
// carries the RPC contract beside the href, so the no-script path and
// the island update are one element.
func TestTableCarriesTheContractOnItsSortAnchors(t *testing.T) {
	plain := Table(TableProps{Path: "/apps",
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		SortBy:  "name", SortDir: SortAsc}, nil)
	has(t, plain, `href="/apps?dir=desc&amp;sort=name"`, "the sort anchor lost its href")
	hasNot(t, plain, "data-fui-rpc", "a list screen's plain sort carried an island contract it was not given")

	island := Table(TableProps{Path: "/apps",
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		SortBy:  "name", SortDir: SortAsc,
		Island: fixtureIsland}, nil)
	for _, want := range []string{
		`href="/apps?dir=desc&amp;sort=name"`,
		`data-fui-rpc="/island/apps?dir=desc&amp;sort=name"`,
		`data-fui-rpc-method="GET"`,
		`data-fui-rpc-signal="apps"`,
		`data-fui-push-state="/apps?dir=desc&amp;sort=name"`,
	} {
		has(t, island, want, "the sort anchor did not carry both destinations")
	}

	// An endpoint with its own query joins rather than stacks one:
	// "?keep=1?sort=…" is two questions in one URL.
	joined := Table(TableProps{Path: "/apps",
		Island:  Island{Endpoint: "/island/apps?keep=1", Signal: "apps"},
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}}}, nil)
	has(t, joined, `data-fui-rpc="/island/apps?dir=asc&amp;keep=1&amp;sort=name"`,
		"the endpoint's query and the href's stacked instead of joining")

	// A caller cannot forge or override the contract through
	// ExtraAttrs: Safe drops every data-fui-* key, so the only way in
	// is the Island.
	smuggled := Table(TableProps{Path: "/apps", Island: fixtureIsland,
		Columns:    []Column{{Key: "name", Header: "Name", Sortable: true}},
		ExtraAttrs: map[string]string{"data-fui-rpc": "/evil"}}, nil)
	hasNot(t, smuggled, "/evil", "a request arrived through ExtraAttrs, which is for decoration")
	has(t, smuggled, `data-fui-rpc="/island/apps?dir=asc&amp;sort=name"`, "the island's own contract was not rendered")
}

// The behaviour module's hooks. Every table carries the module marker
// on its root, the scroll hook on the focusable region, and the
// status span as the root's last child — a plain table's next page
// announces too. The signal and the sort keys are the island's alone:
// a plain table's sort is a page navigation the router already
// focuses, and hooks nothing reads on it are markup carried for no
// one.
func TestTableBehaviourHooks(t *testing.T) {
	cols := []Column{
		{Key: "name", Header: "Name", Sortable: true},
		{Key: "env", Header: "Environment", Sortable: true},
	}
	island := Table(TableProps{
		Columns: cols, SortBy: "name",
		Island: Island{Endpoint: "/island/apps", Signal: "apps"},
	}, nil)
	has(t, island, `data-hui-table=""`, "the island table root lost the module marker")
	has(t, island, `data-hui-table-signal="apps"`, "the island table root did not name its signal")
	has(t, island, `data-hui-table-sort="name"`, "a sort anchor lost its column key")
	has(t, island, `data-hui-table-sort="env"`, "a sort anchor lost its column key")
	has(t, island, `data-hui-table-scroll=""`, "the scroll region lost the focus-fallback hook")
	has(t, island, `<span data-hui-table-status="" role="status"></span>`, "the status span did not render empty on the server")

	plain := Table(TableProps{Columns: cols, SortBy: "name"}, nil)
	has(t, plain, `data-hui-table=""`, "a plain table root lost the module marker")
	has(t, plain, `data-hui-table-scroll=""`, "a plain table's scroll region lost the hook")
	has(t, plain, `<span data-hui-table-status="" role="status"></span>`, "a plain table carries no status for its next page to announce into")
	hasNot(t, plain, "data-hui-table-signal", "a plain table named a signal no island gave it")
	hasNot(t, plain, "data-hui-table-sort", "a plain table's sort anchors carry keys nothing reads")
}

// The announcement is composed on the server, from Strings and the
// caller's Summary, into the attribute the module copies: the sort
// sentence names the column by its Header (its Key when the header is
// empty — the anchor's own naming rule), the direction word follows
// the comma, and the Summary is appended with a space when there is
// one. An empty SortDir reads ascending, the reading the sort anchors
// give it. Nothing to say renders no attribute at all.
func TestTableAnnouncementIsComposedOnTheServer(t *testing.T) {
	cols := []Column{
		{Key: "name", Header: "Name", Sortable: true},
		{Key: "env", Sortable: true},
	}
	has(t, Table(TableProps{Columns: cols, SortBy: "name"}, nil),
		`data-hui-table-announcement="Sorted by Name, ascending"`,
		"an ascending sort did not say its column and direction")
	has(t, Table(TableProps{Columns: cols, SortBy: "env", SortDir: SortDesc}, nil),
		`data-hui-table-announcement="Sorted by env, descending"`,
		"a headerless column was not named by its Key, or the descending word is wrong")
	has(t, Table(TableProps{Columns: cols, SortBy: "name", Summary: "Showing 8 of 10"}, nil),
		`data-hui-table-announcement="Sorted by Name, ascending Showing 8 of 10"`,
		"the caller's Summary was not appended to the sort sentence with a space")
	has(t, Table(TableProps{Columns: cols, Summary: "Showing 8 of 10"}, nil),
		`data-hui-table-announcement="Showing 8 of 10"`,
		"an unsorted window said no Summary it was given")
	hasNot(t, Table(TableProps{Columns: cols}, nil),
		"data-hui-table-announcement",
		"a table with no sort and no Summary carried an announcement anyway")
}

// A translated Strings lands in the announcement attribute: the
// sentence and the direction words are the reader's, and the module
// copies them without knowing a language.
func TestTableAnnouncementIsTranslated(t *testing.T) {
	w := DefaultStrings()
	w.TableSortedBy = "Trié par {column}, {direction}"
	w.SortAscending = "ascendant"
	got := Table(TableProps{
		Columns: []Column{{Key: "name", Header: "Nom", Sortable: true}},
		SortBy:  "name", Strings: w,
	}, nil)
	has(t, got, `data-hui-table-announcement="Trié par Nom, ascendant"`,
		"the announcement did not carry the translated sentence")
	hasNot(t, got, "Sorted by", "the English default leaked past a translated Strings")
}

// aria-sort is the only sort indicator, and it is three-state:
// ascending and descending on the active column, none on every other
// sortable column, and absent on a column that cannot be sorted. The
// active column's anchor flips the direction; every other sortable
// column's sorts ascending.
func TestTableSortIsThreeStateAndCycles(t *testing.T) {
	cols := []Column{
		{Key: "name", Header: "Name", Sortable: true},
		{Key: "env", Header: "Environment", Sortable: true},
		{Key: "region", Header: "Region"},
	}
	asc := Table(TableProps{Path: "/apps", Columns: cols, SortBy: "name", SortDir: SortAsc}, nil)
	// The three headers in order bind each state to its column: the
	// active one ascending with a descending anchor, the inactive
	// sortable one none with an ascending anchor, the unsortable one
	// with no aria-sort at all.
	has(t, asc, `<th aria-sort="ascending" role="columnheader" scope="col"><a href="/apps?dir=desc&amp;sort=name">Name</a></th>`+
		`<th aria-sort="none" role="columnheader" scope="col"><a href="/apps?dir=asc&amp;sort=env">Environment</a></th>`+
		`<th role="columnheader" scope="col">Region</th>`,
		"the three sort states did not land on their own columns")
	// Two anchors, in column order: the active one flips to desc, the
	// inactive one sorts asc.
	hrefs := sortHrefs(t, asc)
	if len(hrefs) != 2 {
		t.Fatalf("found %d sort anchors, want 2", len(hrefs))
	}
	if hrefs[0].Query().Get("dir") != "desc" || hrefs[1].Query().Get("dir") != "asc" {
		t.Errorf("the direction cycle broke: active=%q inactive=%q",
			hrefs[0].Query().Get("dir"), hrefs[1].Query().Get("dir"))
	}

	desc := Table(TableProps{Path: "/apps", Columns: cols, SortBy: "name", SortDir: SortDesc}, nil)
	has(t, desc, `<th aria-sort="descending" role="columnheader" scope="col"><a href="/apps?dir=asc&amp;sort=name">Name</a></th>`,
		"the active descending column does not say so on its own header")
	if got := sortHrefs(t, desc)[0].Query().Get("dir"); got != "asc" {
		t.Errorf("a descending column's anchor sorts %q; it should flip back to asc", got)
	}
}

// The query the screen carries survives a sort beside the new sort
// keys, and the stale sort does not: sort and dir are replaced, not
// appended, and sort=old&sort=name is two answers to one question.
// Asserted by parsing the href back, so no encoding order can make it
// flaky.
func TestTableSortReplacesTheSortAndCarriesTheQuery(t *testing.T) {
	got := Table(TableProps{Path: "/apps",
		Query:   url.Values{"q": {"x"}, "page": {"3"}, "sort": {"old"}, "dir": {"desc"}},
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}}}, nil)
	u := sortHrefs(t, got)[0]
	if u.Path != "/apps" {
		t.Errorf("the sort href left the screen's path: %q", u.Path)
	}
	q := u.Query()
	if q.Get("q") != "x" || q.Get("page") != "3" {
		t.Errorf("the carried query was dropped: q=%q page=%q", q.Get("q"), q.Get("page"))
	}
	if q.Get("sort") != "name" || q.Get("dir") != "asc" {
		t.Errorf("the sort was not replaced: sort=%q dir=%q", q.Get("sort"), q.Get("dir"))
	}
	if len(q["sort"]) != 1 || len(q["dir"]) != 1 {
		t.Errorf("the stale sort survived beside the new one: sort=%v dir=%v", q["sort"], q["dir"])
	}

	// The parameter names are the caller's: a screen that says
	// orderBy keeps saying it.
	named := Table(TableProps{Path: "/apps", SortParam: "orderBy", DirParam: "order",
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}}}, nil)
	nq := sortHrefs(t, named)[0].Query()
	if nq.Get("orderBy") != "name" || nq.Get("order") != "asc" {
		t.Errorf("the named sort parameters did not land: %v", nq)
	}
	if _, ok := nq["sort"]; ok {
		t.Error("the default sort parameter rendered beside the named one")
	}

	// An empty Path is the current document: a relative "?query" href.
	here := Table(TableProps{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}}}, nil)
	if href := hereHrefs(t, here)[0]; href != "?dir=asc&sort=name" {
		t.Errorf("a table with no Path wrote %q; the current document needs only the query", href)
	}
}

// The refusals, each naming what to fix: no columns at all is a
// table with nothing to render; a column with no Key can neither be
// sorted nor receive a cell; two columns under one Key claim one cell; a cell key that matches no
// column is a typo, not an omission; a Path pointing off-origin is a
// sort anchor that leaves the site; an Island that looks wired and is
// not is a region that never updates.
func TestTableRefusesWhatItCannotRender(t *testing.T) {
	sortable := []Column{{Key: "name", Header: "Name", Sortable: true}}
	refuse(t, "Columns", func() {
		Table(TableProps{Empty: render.HTML("<p>nothing</p>")}, nil)
	})
	refuse(t, "Key", func() {
		Table(TableProps{Columns: []Column{{Header: "Name"}}}, nil)
	})
	refuse(t, "two columns", func() {
		Table(TableProps{Columns: []Column{{Key: "name", Header: "Name"}, {Key: "name", Header: "Also name"}}}, nil)
	})
	refuse(t, "owner", func() {
		Table(TableProps{
			Columns: sortable,
			Rows: []Row{{Cells: map[string]render.HTML{
				"owner": render.Text("dom"),
			}}}}, nil)
	})
	refuse(t, "Path", func() {
		Table(TableProps{Path: "https://evil.example/apps", Columns: sortable}, nil)
	})
	refuse(t, "Endpoint", func() {
		Table(TableProps{Columns: sortable, Island: Island{Signal: "apps"}}, nil)
	})
	refuse(t, "Signal", func() {
		Table(TableProps{Columns: sortable, Island: Island{Endpoint: "/island/apps"}}, nil)
	})
}

// A header-less column is an actions or icon column. It cannot be
// sorted: the <th> is hidden from assistive tech, because an empty
// column header announces nothing and axe's empty-table-header rule
// fires on it. It can be sorted: the anchor's text is empty, so its
// name comes from Strings with the column's Key in it — and a
// translated Strings lands in the markup, not the English default.
func TestTableHeaderlessColumns(t *testing.T) {
	got := Table(TableProps{Path: "/apps",
		Columns: []Column{{Key: "actions"}, {Key: "env", Sortable: true}}}, nil)
	has(t, got, `<th aria-hidden="true" role="columnheader" scope="col"></th>`, "the header-less non-sortable column's own th is not hidden from assistive tech")
	has(t, got, `<a aria-label="Sort by env" href="/apps?dir=asc&amp;sort=env"></a>`, "the header-less sortable column's anchor is unnamed")
	// The label is the Strings table's to say, not the component's.
	fr := Table(TableProps{Path: "/apps",
		Columns: []Column{{Key: "env", Sortable: true}},
		Strings: &Strings{TableSortBy: "Trier par {column}"}}, nil)
	has(t, fr, `aria-label="Trier par env"`, "a translated TableSortBy did not reach the anchor")
}

// data-label is the header text a cards-collapse stylesheet shows
// beside the value: content, so the component that knows the header
// writes it, on every cell of every column that has one. A class map
// that does not collapse simply does not read it.
func TestTableCellsCarryTheHeaderTheyShowCollapsed(t *testing.T) {
	got := Table(TableProps{
		Columns: []Column{{Key: "name", Header: "Name"}, {Key: "actions"}},
		Rows: []Row{{Cells: map[string]render.HTML{
			"name": render.Text("blog"), "actions": render.Text("View"),
		}}}}, nil)
	has(t, got, `<td data-label="Name" role="cell">blog</td><td role="cell">View</td>`,
		"the headered column's cell does not carry its header, or the header-less one carries something")
	if n := count(got, `data-label=`); n != 1 {
		t.Errorf("data-label rendered %d times, want 1: only a headered column's cells carry it", n)
	}
}

// The empty body keeps the head: an empty result still has named
// columns and usable sort controls. The slot renders in one cell
// spanning every column; no slot renders an empty <tbody>.
func TestTableEmptyBodyKeepsTheHead(t *testing.T) {
	got := Table(TableProps{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}, {Key: "env", Header: "Environment"}},
		Empty:   render.HTML(`<p>Nothing yet.</p>`)}, nil)
	has(t, got, `<thead`, "an empty table dropped the head")
	has(t, got, `aria-sort="none"`, "an empty table dropped the sort controls")
	has(t, got, `colspan="2"`, "the empty cell does not span the columns")
	has(t, got, `<p>Nothing yet.</p>`, "the empty slot's content did not arrive")

	bare := Table(TableProps{Columns: []Column{{Key: "name", Header: "Name"}}}, nil)
	has(t, bare, "<tbody", "the empty table dropped the body")
	hasNot(t, bare, "<td", "an empty table with no slot rendered a cell")
}

// The root is one shape with or without a footer: the wrapper div
// (PartRoot) holds the scroll region (PartScroll) around the table,
// and the footer, when there is one, as the region's sibling — so a
// pager's nav landmark never nests in a table, and the markup a class
// map dresses never changes shape under it.
func TestTableRootIsOneShapeWithAScrollRegion(t *testing.T) {
	cols := []Column{{Key: "app", Header: "Application"}}
	plain := Table(TableProps{Columns: cols}, nil)
	if !strings.HasPrefix(string(plain), "<div") {
		t.Errorf("the root should be the wrapper div, footer or none: %s", plain)
	}
	has(t, plain, `<div data-hui-table=""><div data-hui-table-scroll="" role="region" tabindex="0"><table role="table">`, "the root, the focusable region and the table are not nested in that order")
	has(t, plain, `</table></div><span data-hui-table-status="" role="status"></span></div>`, "the status is not the root's last child")

	withFooter := Table(TableProps{Columns: cols,
		Footer: render.HTML(`<nav aria-label="Pages">Page 1 of 2</nav>`)}, nil)
	close, nav := strings.Index(string(withFooter), "</table>"), strings.Index(string(withFooter), "<nav")
	if close == -1 || nav < close {
		t.Errorf("the footer did not render after the table: %s", withFooter)
	}
	has(t, withFooter, `</table></div><nav aria-label="Pages">Page 1 of 2</nav><span data-hui-table-status="" role="status"></span></div>`, "the footer is not the scroll region's sibling inside the root, with the status last")
}

// The scroll region is named by the caption when there is one — the
// id the caption rendered under is what aria-labelledby points at —
// and carries no aria-labelledby at all when there is not, rather
// than one pointing at nothing.
func TestTableScrollRegionIsNamedByTheCaption(t *testing.T) {
	cols := []Column{{Key: "app", Header: "Application"}}
	named := Table(TableProps{ID: "apps", Caption: "Applications", Columns: cols}, nil)
	has(t, named, `<div aria-labelledby="apps-caption" data-hui-table-scroll="" role="region" tabindex="0"><table role="table"><caption id="apps-caption">Applications</caption>`,
		"the scroll region is not named by the caption it wraps")

	// No ID: the id is derived from the caption the way Section
	// derives its title's.
	derived := Table(TableProps{Caption: "Recent deployments", Columns: cols}, nil)
	has(t, derived, `<div aria-labelledby="table-recent-deployments-caption" data-hui-table-scroll="" role="region" tabindex="0"><table role="table"><caption id="table-recent-deployments-caption">`,
		"the caption id was not derived from the caption text, or the region does not point at it")

	// No caption: no name, and no attribute pointing at nothing.
	unnamed := Table(TableProps{Columns: cols}, nil)
	hasNot(t, unnamed, "aria-labelledby", "a region with no caption carried an aria-labelledby anyway")
}

// A Path that carries its own query or fragment is refused rather
// than silently overwritten: the sort joins its parameters to the
// Path's query, and a Path query would be replaced with no error —
// the exact class of silent loss this component exists to make
// structural.
func TestTableRefusesAPathWithItsOwnQuery(t *testing.T) {
	sortable := []Column{{Key: "name", Header: "Name", Sortable: true}}
	refuse(t, "Query", func() {
		Table(TableProps{Path: "/apps?team=ops", Columns: sortable}, nil)
	})
	refuse(t, "Query", func() {
		Table(TableProps{Path: "/apps#results", Columns: sortable}, nil)
	})
}

// The carried query is request state: a value or key with a control
// byte in it percent-encodes to something the anchor policy refuses,
// and refusing at render would be a 500 from a crafted list URL. The
// bytes are stripped, and a pair that scrubs to nothing is dropped;
// the rest of the query survives, parsed back rather than matched.
func TestTableScrubsControlBytesFromTheCarriedQuery(t *testing.T) {
	got := Table(TableProps{Path: "/apps",
		Query: url.Values{
			"q":        {"ev\r\nSet-Cookie: x=1"},
			"\x01team": {"ops"},
			"gone":     {"\x00\x7f"},
			"keep":     {"yes"},
		},
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}}}, nil)
	q := sortHrefs(t, got)[0].Query()
	if q.Get("q") != "evSet-Cookie: x=1" {
		t.Errorf("the control bytes were not stripped from the value: %q", q.Get("q"))
	}
	if q.Get("team") != "ops" {
		t.Errorf("the control byte was not stripped from the key: %v", q)
	}
	if _, ok := q["gone"]; ok {
		t.Error("a value that scrubbed to nothing was carried as an empty filter")
	}
	if q.Get("keep") != "yes" || q.Get("sort") != "name" {
		t.Errorf("the rest of the query did not survive the scrub: %v", q)
	}
}

// A column's Variant is the one channel a class map has for styling a
// whole column: the class lands on that column's <th> and on every
// one of its <td>, and on no other column's. A nil class map or an
// empty Variant renders nothing.
func TestTableColumnVariantLandsOnHeaderAndCells(t *testing.T) {
	classes := Classes{
		PartRoot:    "tbl",
		PartHeader:  "tbl__h",
		PartCell:    "tbl__c",
		"cell--end": "tbl__c--end", "header--end": "tbl__h--end",
	}
	got := Table(TableProps{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "cost", Header: "Cost", Variant: "end"},
		},
		Rows: []Row{
			{Cells: map[string]render.HTML{"name": render.Text("blog"), "cost": render.Text("12")}},
			{Cells: map[string]render.HTML{"name": render.Text("shop"), "cost": render.Text("34")}},
		},
	}, classes)
	if n := count(got, `tbl__h--end`); n != 1 {
		t.Errorf("the variant landed on %d headers, want the one column's", n)
	}
	if n := count(got, `tbl__c--end`); n != 2 {
		t.Errorf("the variant landed on %d cells, want one per row of the column", n)
	}
	// The variant joins the part's own class rather than replacing
	// it, on the one column's header and cells, and the other
	// column's carry the part class alone.
	has(t, got, `<th class="tbl__h" role="columnheader" scope="col">Name</th><th class="tbl__h tbl__h--end" role="columnheader" scope="col">Cost</th>`,
		"the header variant did not land on its own column alone, joined to the part class")
	has(t, got, `<td class="tbl__c" data-label="Name" role="cell">blog</td><td class="tbl__c tbl__c--end" data-label="Cost" role="cell">12</td>`,
		"the cell variant did not land on its own column alone, joined to the part class")
	// And a nil class map renders none of it.
	bare := Table(TableProps{
		Columns: []Column{{Key: "cost", Header: "Cost", Variant: "end"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"cost": render.Text("12")}}},
	}, nil)
	hasNot(t, bare, "class=", "a variant rendered a class at the nil class map")
}

// ─── helpers ───────────────────────────────────────────────────────

var anchorHrefAttr = regexp.MustCompile(`<a [^>]*?href="([^"]*)"`)

// sortHrefs parses every anchor href in the markup back into a URL,
// in document order, so no encoding order can make an assertion
// flaky. The table renders no anchor but its sort controls, so every
// href found is one.
func sortHrefs(t *testing.T, got render.HTML) []*url.URL {
	t.Helper()
	var out []*url.URL
	for _, m := range anchorHrefAttr.FindAllStringSubmatch(string(got), -1) {
		u, err := url.Parse(stdhtml.UnescapeString(m[1]))
		if err != nil {
			t.Fatalf("sort href %q does not parse: %v", m[1], err)
		}
		out = append(out, u)
	}
	return out
}

// hereHrefs returns the raw hrefs as written, for the relative
// "?query" shape url.Parse would resolve against nothing.
func hereHrefs(t *testing.T, got render.HTML) []string {
	t.Helper()
	var out []string
	for _, m := range anchorHrefAttr.FindAllStringSubmatch(string(got), -1) {
		out = append(out, stdhtml.UnescapeString(m[1]))
	}
	return out
}
