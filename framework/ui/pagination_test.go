package ui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// The styled pager's own contract: the render-through adapter over the
// headless primitive. The primitive's tests hold the href math, the
// window and the hooks; these hold what this layer adds — the class
// map, the labels through StringsFor, and the extras sanitiser.

// The class map keeps the names the pattern's sheet read: "pagination"
// on the list, "pagination-gap" on the gap, and nothing on the nav
// landmark or the anchors, which the sheet reaches by tag.
func TestPaginationClassMapKeepsTheSheetsNames(t *testing.T) {
	h := string(Pagination(PaginationConfig{Page: 5, Pages: 12}))
	if !strings.Contains(h, `<div class="pagination">`) {
		t.Errorf("the list did not carry the sheet's pagination class:\n%s", h)
	}
	if !strings.Contains(h, `class="pagination-gap"`) {
		t.Errorf("the gap did not carry the sheet's pagination-gap class:\n%s", h)
	}
	if strings.Contains(h, "<nav class=") {
		t.Errorf("the nav landmark carries a class the headless contract forbids:\n%s", h)
	}
	// The anchors are reached by tag inside .pagination: a class on
	// them is markup no selector reads.
	if n := strings.Count(h, "<a class="); n != 0 {
		t.Errorf("%d anchors carry a class no selector reads:\n%s", n, h)
	}
	// The marker fetches the sheet.
	if !strings.Contains(h, `data-fui-comp="ui-pagination"`) {
		t.Errorf("the pager did not carry its sheet's marker:\n%s", h)
	}
}

// The labels resolve through the i18n keys: the nav's aria-label from
// KeyPaginationLabel, the ends' words from StringsFor.
func TestPaginationLabelsResolveThroughI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyPaginationLabel, "PROBE-PAG")
	swapDefault(t, i18nui.KeyPaginationPrevious, "PROBE-PREV")
	swapDefault(t, i18nui.KeyPaginationNext, "PROBE-NEXT")
	h := string(Pagination(PaginationConfig{Page: 2, Pages: 3}))
	for _, want := range []string{
		`aria-label="PROBE-PAG"`, ">PROBE-PREV</a>", ">PROBE-NEXT</a>",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q:\n%s", want, h)
		}
	}
	// Explicit fields win over the resolved words.
	h2 := string(Pagination(PaginationConfig{Page: 2, Pages: 3, PrevLabel: "Earlier"}))
	if !strings.Contains(h2, ">Earlier</a>") || strings.Contains(h2, ">PROBE-PREV</a>") {
		t.Errorf("PrevLabel did not win over the resolved word:\n%s", h2)
	}
}

// ExtraAttrs is sanitised: the request contract and the hooks are not
// a caller's to forge, and neither is the id the headless layer owns.
func TestPaginationExtrasAreSanitised(t *testing.T) {
	h := string(Pagination(PaginationConfig{Page: 1, Pages: 2,
		ExtraAttrs: map[string]string{
			"data-fui-rpc": "/evil", "data-hui-page": "9", "id": "x", "data-testid": "pager",
		}}))
	for _, banned := range []string{"/evil", `data-hui-page="9"`, `id="x"`} {
		if strings.Contains(h, banned) {
			t.Errorf("ExtraAttrs smuggled %q past the sanitiser:\n%s", banned, h)
		}
	}
	if !strings.Contains(h, `data-testid="pager"`) {
		t.Errorf("a benign extra was dropped:\n%s", h)
	}
}

// A caller's Class appends to the list's own, where the pattern's
// Class always landed.
func TestPaginationClassAppendsToTheList(t *testing.T) {
	h := string(Pagination(PaginationConfig{Page: 1, Pages: 2, Class: "tight"}))
	if !strings.Contains(h, `class="pagination tight"`) {
		t.Errorf("the caller's class did not append to the list's own:\n%s", h)
	}
}

// The typed props reach the primitive: the carry survives a page turn
// with the page parameter replaced, and the island posture puts the
// contract and the page hook on the same anchors that keep their
// hrefs. The DataTable adapter is the one place the pager inherits a
// table's island; a standalone pager says so itself.
func TestPaginationTypedPropsAndIslandPosture(t *testing.T) {
	h := string(Pagination(PaginationConfig{Page: 3, Pages: 9, Path: "/apps",
		Query: url.Values{"q": {"x"}, "p": {"7"}}}))
	if strings.Contains(h, "p=7") {
		t.Errorf("the carried page value survived beside the replaced one:\n%s", h)
	}
	if !strings.Contains(h, `href="/apps?p=2&amp;q=x"`) {
		t.Errorf("the carry did not survive the page turn:\n%s", h)
	}
	if strings.Contains(h, "data-fui-rpc") || strings.Contains(h, "data-hui-page") {
		t.Errorf("a pager with no Island carried a contract or a hook:\n%s", h)
	}

	isled := string(Pagination(PaginationConfig{Page: 1, Pages: 2, Path: "/apps",
		Island: headless.Island{Endpoint: "/island/apps", Signal: "apps"}}))
	for _, want := range []string{
		`data-fui-rpc="/island/apps?p=2"`,
		`data-fui-push-state="/apps?p=2"`,
		`data-hui-page="2"`,
		`href="/apps?p=2"`,
	} {
		if !strings.Contains(isled, want) {
			t.Errorf("the island pager lost %q:\n%s", want, isled)
		}
	}
}

// DataTable passes its Island into a pager that has none, so a pager
// on an island table swaps the same region the table's sort does.
func TestDataTableSharesItsIslandWithThePager(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns:    []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:       []Row{{Cells: map[string]render.HTML{"name": render.Text("a")}}},
		Island:     headless.Island{Endpoint: "/island/rows", Signal: "rows"},
		Pagination: &PaginationConfig{Pages: 2, Page: 1},
	}))
	if !strings.Contains(h, `data-fui-rpc="/island/rows?p=2"`) {
		t.Errorf("the pager did not inherit the table's island:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-table-signal="rows"`) {
		t.Errorf("the table root lost its signal hook:\n%s", h)
	}
}
