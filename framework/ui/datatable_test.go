package ui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

func TestDataTableRequiresColumns(t *testing.T) {
	defer func() { recover() }()
	DataTable(DataTableConfig{Rows: []Row{{}}})
	t.Fatal("expected panic on empty Columns")
}

func TestDataTableColumnRequiresKey(t *testing.T) {
	defer func() { recover() }()
	DataTable(DataTableConfig{Columns: []Column{{Header: "x"}}})
	t.Fatal("expected panic on Column without Key")
}

func TestSortableEmptyHeaderAriaLabel(t *testing.T) {
	// A sortable column with an empty Header (icon-only / key-only
	// columns) must still expose an accessible label on its sort
	// anchor, derived from the column Key, otherwise screen readers
	// announce a nameless control. The words come through
	// StringsFor(ctx) into the primitive.
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "price", Header: "", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"price": render.Text("9")}}},
	}))
	if !strings.Contains(h, `aria-label="Sort by price"`) {
		t.Errorf("expected sort anchor to carry aria-label from Key:\n%s", h)
	}
}

func TestDataTableActionsColumnEmptyHeaderOK(t *testing.T) {
	// Empty Header is allowed on non-sortable columns (actions / icons).
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "actions", Header: "", Align: "end"},
		},
		Rows: []Row{{Cells: map[string]render.HTML{
			"name":    render.Text("Alice"),
			"actions": render.Text("✎"),
		}}},
	}))
	if !strings.Contains(h, "Alice") {
		t.Errorf("expected row to render: %s", h)
	}
}

func TestDataTableSortableDefaultsSortParams(t *testing.T) {
	// A sortable column with no custom parameter names renders the
	// default sort and dir parameters, with no pattern required.
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("Alice")}}},
	}))
	q := sortAnchorQuery(t, h)
	if got := q.Get("sort"); got != "name" {
		t.Errorf("default sort param = %q, want name", got)
	}
	if got := q.Get("dir"); got != "asc" {
		t.Errorf("default dir param = %q, want asc", got)
	}
}

func TestDataTableEmptyStateRenders(t *testing.T) {
	// The empty branch keeps the head: named columns and their sort
	// controls stay usable on a zero-result screen, and the styled
	// EmptyState renders in the spanning empty cell.
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    nil,
	}))
	for _, want := range []string{
		"ui-data-table is-empty",
		"<table",
		"<thead",
		">Name<",
		"ui-empty-state",
		"No results",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestDataTableEmptyStateUsesProvidedConfig(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Empty: EmptyStateConfig{
			Title:       "No customers yet",
			Description: "Try inviting one.",
		},
	}))
	for _, want := range []string{"No customers yet", "Try inviting one."} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestDataTableRendersHeadersAndRows(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "email", Header: "Email"},
		},
		Rows: []Row{
			{Cells: map[string]render.HTML{
				"name":  render.Text("Alice"),
				"email": render.Text("a@x.com"),
			}},
			{Cells: map[string]render.HTML{
				"name":  render.Text("Bob"),
				"email": render.Text("b@x.com"),
			}},
		},
	}))
	// The primitive owns the table semantics: explicit roles (kept
	// when a cards collapse displays the elements as blocks) and
	// scope on the column headers.
	for _, want := range []string{
		`role="table"`, `role="rowgroup"`, `role="row"`, `role="cell"`,
		`scope="col"`, `>Name<`, `>Email<`,
		`>Alice<`, `>b@x.com<`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestDataTableSortableColumnRendersClickableLink(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name", Sortable: true},
		},
		Rows: []Row{{Cells: map[string]render.HTML{"name": render.Text("Alice")}}},
	}))
	if !strings.Contains(h, `<a class="ui-data-table__sort"`) {
		t.Errorf("expected sort anchor with its class, got: %s", h)
	}
	q := sortAnchorQuery(t, h)
	if q.Get("sort") != "name" || q.Get("dir") != "asc" {
		t.Errorf("expected default-asc sort=name&dir=asc, got %+v", q)
	}
	if !strings.Contains(h, `aria-sort="none"`) {
		t.Errorf("expected aria-sort=none on inactive sortable column, got: %s", h)
	}
}

func TestDataTableActiveSortFlipsDirection(t *testing.T) {
	hAsc := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		SortBy:  "name",
		SortDir: SortAsc,
	}))
	if !strings.Contains(hAsc, `aria-sort="ascending"`) {
		t.Errorf("expected aria-sort=ascending, got: %s", hAsc)
	}
	if q := sortAnchorQuery(t, hAsc); q.Get("dir") != "desc" {
		t.Errorf("expected next click flips to desc, got: %s", hAsc)
	}

	hDesc := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		SortBy:  "name",
		SortDir: SortDesc,
	}))
	if !strings.Contains(hDesc, `aria-sort="descending"`) {
		t.Errorf("expected aria-sort=descending, got: %s", hDesc)
	}
	if q := sortAnchorQuery(t, hDesc); q.Get("dir") != "asc" {
		t.Errorf("expected next click flips to asc, got: %s", hDesc)
	}

	// The direction indicator is the stylesheet's, drawn from
	// aria-sort: the markup carries no glyph and no indicator span.
	for _, h := range []string{hAsc, hDesc} {
		if strings.Contains(h, "ui-data-table__sort-indicator") {
			t.Errorf("indicator span must not render: %s", h)
		}
		start := strings.Index(h, ">Name<")
		if start < 0 {
			t.Fatalf("header text not found: %s", h)
		}
		anchorText := h[:start]
		if anchor := anchorText[strings.LastIndex(anchorText, "<a"):]; strings.ContainsAny(anchor, "↑↓") {
			t.Errorf("no glyph may travel in the sort anchor markup: %s", anchor)
		}
	}
}

func TestDataTablePaginationFooterRenders(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		Pagination: &PaginationConfig{
			Pages: 5, Page: 2,
		},
	}))
	if !strings.Contains(h, "ui-data-table__footer") {
		t.Errorf("expected pagination footer, got: %s", h)
	}
	if !strings.Contains(h, `aria-label="Pagination"`) {
		t.Errorf("expected pagination nav, got: %s", h)
	}
	// The typed pager replaced the page parameter in a carry-free
	// query: page 3's anchor is the relative href a plain list screen
	// renders, and the current page is the only one marked.
	if !strings.Contains(h, `href="?p=3"`) {
		t.Errorf("expected the page-3 anchor at ?p=3, got: %s", h)
	}
	if n := strings.Count(h, `aria-current="page"`); n != 1 {
		t.Errorf("exactly one page is current, found %d", n)
	}
	// The footer is the scroll region's sibling, never inside the
	// table: a pager's nav landmark must not nest in one.
	tableEnd := strings.Index(h, "</table>")
	footerAt := strings.Index(h, `class="ui-data-table__footer"`)
	if tableEnd < 0 || footerAt < 0 || footerAt < tableEnd {
		t.Errorf("footer must render after the table (tableEnd=%d footerAt=%d):\n%s", tableEnd, footerAt, h)
	}
}

func TestDataTableMissingCellRendersEmpty(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "email", Header: "Email"},
		},
		Rows: []Row{{Cells: map[string]render.HTML{
			"name": render.Text("Alice"),
			// email intentionally missing
		}}},
	}))
	// Two cells should render even when the second is missing from
	// the Cells map. They collapse to empty content.
	if strings.Count(h, "<td") != 2 {
		t.Errorf("expected 2 <td even with missing cell, got %d in: %s",
			strings.Count(h, "<td"), h)
	}
}

func TestDataTableAlignmentClassesRender(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "amt", Header: "Amount", Align: "end"},
		},
		Rows: []Row{{Cells: map[string]render.HTML{"amt": render.Text("$5")}}},
	}))
	// Alignment travels as the column variant: the same class lands
	// on the column's th and on each of its tds.
	thAt := strings.Index(h, "<th")
	tdAt := strings.Index(h, "<td")
	if thAt < 0 || !strings.Contains(h[thAt:tdAt], "is-align-end") {
		t.Errorf("expected is-align-end on the header, got: %s", h)
	}
	if !strings.Contains(h[tdAt:], "is-align-end") {
		t.Errorf("expected is-align-end on the cell, got: %s", h)
	}
}

func TestDataTableCaptionRenders(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		Caption: "Recent customers",
	}))
	if !strings.Contains(h, "<caption") || !strings.Contains(h, "Recent customers") {
		t.Errorf("expected caption to render, got: %s", h)
	}
	// A visible caption carries only the caption class; the hidden
	// recipe must not leak into it. (The status span carries the
	// recipe by design — it must be read and not seen — so the check
	// reads the caption element, not the whole markup.)
	if capAt := strings.Index(h, "<caption"); capAt < 0 || strings.Contains(h[capAt:strings.Index(h, "</caption>")], "ui-visually-hidden") {
		t.Errorf("visible caption must not carry the hidden class: %s", h)
	}
}

func TestDataTableCaptionHidden(t *testing.T) {
	// A hidden caption is still a caption: the element, its id and its
	// text stay in the markup and the scroll region's aria-labelledby
	// still resolves to it — the caption is what names the region for
	// assistive technology. Only the paint is suppressed.
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		Caption: "Invoices", CaptionHidden: true,
	}))
	capAt := strings.Index(h, "<caption")
	if capAt < 0 {
		t.Fatalf("hidden caption must still render: %s", h)
	}
	capTag := h[capAt : strings.Index(h[capAt:], ">")+capAt+1]
	if !strings.Contains(capTag, `class="ui-data-table__caption ui-visually-hidden"`) {
		t.Errorf("hidden caption must append the hidden class to its own: %s", capTag)
	}
	if !strings.Contains(h, ">Invoices<") {
		t.Errorf("hidden caption keeps its text (aria-labelledby points at it): %s", h)
	}
	idStart := strings.Index(capTag, `id="`) + len(`id="`)
	capID := capTag[idStart : strings.Index(capTag[idStart:], `"`)+idStart]
	scrollAt := strings.Index(h, `<div aria-labelledby=`)
	if scrollAt < 0 {
		t.Fatalf("no labelled scroll region in: %s", h)
	}
	scrollTag := h[scrollAt:]
	scrollTag = scrollTag[:strings.Index(scrollTag, ">")+1]
	if !strings.Contains(scrollTag, `aria-labelledby="`+capID+`"`) {
		t.Errorf("scroll region must still be labelled by the hidden caption (want %s): %s", capID, scrollTag)
	}
}

func TestDataTableCaptionHiddenRequiresCaption(t *testing.T) {
	defer func() { recover() }()
	DataTable(DataTableConfig{
		Columns:       []Column{{Key: "name", Header: "Name"}},
		CaptionHidden: true,
	})
	t.Fatal("expected panic on CaptionHidden without Caption")
}

// ─── Responsive cards mode ────────────────────────────────────────

func TestDataTable_ResponsiveCards_AddsModifierClass(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows: []Row{
			{Cells: map[string]render.HTML{"name": render.Text("Ada")}},
		},
		Responsive: ResponsiveCards,
	}))
	if !strings.Contains(h, "ui-data-table--responsive-cards") {
		t.Errorf("expected modifier class on wrapper with ResponsiveCards, got: %s", h)
	}
}

func TestDataTable_ResponsiveCards_AddsDataLabelOnCells(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "email", Header: "Email"},
		},
		Rows: []Row{
			{Cells: map[string]render.HTML{
				"name":  render.Text("Ada"),
				"email": render.Text("ada@x.com"),
			}},
		},
		Responsive: ResponsiveCards,
	}))
	if !strings.Contains(h, `data-label="Name"`) {
		t.Errorf("expected data-label=\"Name\" on cells, got: %s", h)
	}
	if !strings.Contains(h, `data-label="Email"`) {
		t.Errorf("expected data-label=\"Email\" on cells, got: %s", h)
	}
}

func TestDataTable_ResponsiveScrollAddsDataLabel(t *testing.T) {
	// The label is content, not a mode flag: the primitive emits it
	// for every headered column, and the scroll class map simply
	// does not read it. A cards-collapse stylesheet can then adopt
	// the same table without the markup changing under it.
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows: []Row{
			{Cells: map[string]render.HTML{"name": render.Text("Ada")}},
		},
	}))
	if !strings.Contains(h, `data-label="Name"`) {
		t.Errorf("default DataTable must carry data-label on headered cells, got: %s", h)
	}
	if strings.Contains(h, "ui-data-table--responsive-cards") {
		t.Errorf("default DataTable should not carry the responsive-cards modifier, got: %s", h)
	}
}

func TestDataTable_ResponsiveCards_EmptyHeaderHasNoDataLabel(t *testing.T) {
	// Columns with empty Header (e.g. icon-only / actions column)
	// shouldn't get data-label="": the CSS hides the ::before pseudo
	// for cells without the attribute. Keep the attribute absent so
	// the CSS rule matches.
	h := string(DataTable(DataTableConfig{
		Columns: []Column{
			{Key: "name", Header: "Name"},
			{Key: "actions", Header: ""},
		},
		Rows: []Row{
			{Cells: map[string]render.HTML{
				"name":    render.Text("Ada"),
				"actions": render.HTML("<a href=\"/x\">edit</a>"),
			}},
		},
		Responsive: ResponsiveCards,
	}))
	if strings.Contains(h, `data-label=""`) {
		t.Errorf("empty-header column should not emit data-label=\"\", got: %s", h)
	}
}

func TestDataTableExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"populated": DataTable(DataTableConfig{
			Columns:    []Column{{Key: "id", Header: "ID"}},
			Rows:       []Row{{Cells: map[string]render.HTML{"id": render.Text("1")}}},
			ExtraAttrs: extra,
		}),
		"empty": DataTable(DataTableConfig{
			Columns:    []Column{{Key: "id", Header: "ID"}},
			ExtraAttrs: extra,
		}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

// ─── Island posture ───────────────────────────────────────────────

func TestDataTableIslandSortAnchorCarriesContract(t *testing.T) {
	// An embedded table's sort anchors carry the RPC contract BESIDE
	// their hrefs on the same element: the href is the page without
	// script, the island update with it. The control is an anchor in
	// both postures — never a button that loses the no-script href.
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("x")}}},
		Query:   url.Values{"q": {"z"}},
		Island:  headless.Island{Endpoint: "/island/tbl", Signal: "tbl"},
	}))
	if !strings.Contains(h, `<a class="ui-data-table__sort"`) {
		t.Errorf("island sort control must stay an anchor:\n%s", h)
	}
	if strings.Contains(h, "<button") {
		t.Errorf("island sort control must not render a button:\n%s", h)
	}
	anchor := h[strings.Index(h, "<a class"):strings.Index(h, "</a>")]
	for _, want := range []string{
		`href="?dir=asc&amp;q=z&amp;sort=name"`,
		`data-fui-rpc="/island/tbl?dir=asc&amp;q=z&amp;sort=name"`,
		`data-fui-rpc-method="GET"`,
		`data-fui-rpc-signal="tbl"`,
		`data-fui-push-state="?dir=asc&amp;q=z&amp;sort=name"`,
	} {
		if !strings.Contains(anchor, want) {
			t.Errorf("island sort anchor missing %s:\n%s", want, anchor)
		}
	}
}

// sortAnchorHref extracts the first sort anchor's href from a
// rendered table.
func sortAnchorHref(t *testing.T, h string) string {
	t.Helper()
	const needle = `<a class="ui-data-table__sort" href="`
	i := strings.Index(h, needle)
	if i < 0 {
		t.Fatalf("no sort anchor in:\n%s", h)
	}
	rest := h[i+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("unterminated href in:\n%s", h)
	}
	return rest[:end]
}

// sortAnchorQuery parses the first sort anchor's href into a query.
func sortAnchorQuery(t *testing.T, h string) url.Values {
	t.Helper()
	href := strings.ReplaceAll(sortAnchorHref(t, h), "&amp;", "&")
	q, err := url.ParseQuery(strings.TrimPrefix(href, "?"))
	if err != nil {
		t.Fatalf("sort href %q does not parse: %v", href, err)
	}
	return q
}
