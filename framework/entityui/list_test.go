package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func ordersConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "name", Type: schema.String, Required: true},
			schema.Field{Name: "status", Type: schema.Enum, Values: []string{"open", "paid"}, Default: "open"},
			schema.Field{Name: "amount", Type: schema.Decimal},
			schema.Field{Name: "memo", Type: schema.String, NoQuery: true},
		),
		SearchFields: []string{"name"},
		Exposure:     &entity.ExposureConfig{Public: true},
	}
}

func ordersRows() []map[string]any {
	return []map[string]any{
		{"id": "o1", "name": "alpha", "status": "open", "amount": "10.5", "memo": "m1"},
		{"id": "o2", "name": "zeta", "status": "paid", "amount": "20", "memo": "m2"},
	}
}

func TestListRendersHeaderRowsAndNew(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	for _, want := range []string{
		"Orders",        // the plural heading
		"2 orders",      // the count subtitle, a sentence
		"alpha", "zeta", // rows
		`href="/orders/o1"`, // the title cell links to the record
		`href="/orders/create"`, "New Order",
		"data-cui-comp=\"ui-data-table\"",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("list missing %q:\n%s", want, html)
		}
	}
	// Without REST write routes the list renders read-only: no New.
	x2 := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html2 := listHTML(t, x2.ui.List("orders"), x2.ctx("/orders", ""))
	if strings.Contains(html2, "/orders/create") {
		t.Errorf("a read-only entity offered New:\n%s", html2)
	}
}

// Two keyed lists on one page each read only their own params: a_sort
// orders the first, b_dir flips the second, and neither list sees the
// other's state.
func TestKeyedListsReadOwnParams(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	ctx := x.ctx("/page", "?a_sort=name&b_sort=name&b_dir=desc")
	a := listHTML(t, x.ui.List("orders").Key("a"), ctx)
	b := listHTML(t, x.ui.List("orders").Key("b"), ctx)
	if ia, iz := strings.Index(a, "alpha"), strings.Index(a, "zeta"); ia < 0 || iz < 0 || ia > iz {
		t.Errorf("list a ignored ?a_sort=name (want alpha before zeta):\n%s", a)
	}
	if ib, iz := strings.Index(b, "alpha"), strings.Index(b, "zeta"); ib < 0 || iz < 0 || ib < iz {
		t.Errorf("list b ignored ?b_dir=desc (want zeta before alpha):\n%s", b)
	}
}

// An unkeyed list claims only its own facet params: a keyed list whose
// key starts with f_ keeps its state through the unkeyed list's links.
func TestUnkeyedListKeepsFPrefixedKey(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/page", "?f_x_q=alpha&f_status=open"))
	if !strings.Contains(html, "f_x_q=alpha") {
		t.Fatalf("the unkeyed list's links dropped the f_x list's search:\n%s", html)
	}
}

// A NoQuery column renders unsortable, and a typed or bookmarked ?sort=
// on it never reaches the query: the rows keep the default order.
func TestNoQuerySortIgnored(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?sort=memo&dir=desc"))
	// The header is a plain <th>, not a sort anchor.
	if strings.Contains(html, `sort=memo`) {
		t.Errorf("the NoQuery column rendered a sort link:\n%s", html)
	}
	// Insertion order preserved: alpha (row 1) still comes first.
	if ia, iz := strings.Index(html, "alpha"), strings.Index(html, "zeta"); ia < 0 || iz < 0 || ia > iz {
		t.Errorf("?sort=<NoQuery column> reached the query and reordered the rows:\n%s", html)
	}
	// A queryable column still sorts.
	sorted := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?sort=name&dir=desc"))
	if ia, iz := strings.Index(sorted, "alpha"), strings.Index(sorted, "zeta"); ia < 0 || iz < 0 || ia < iz {
		t.Errorf("?sort=name&dir=desc did not reorder the rows:\n%s", sorted)
	}
}

// ?page= beyond the run lands on the last page, under a pager that says
// so — ported from resource's clamp.
func TestPageClampsToLast(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders").PageSize(1), x.ctx("/orders", "?page=99"))
	if !strings.Contains(html, "zeta") {
		t.Fatalf("?page=99 on a two-page run did not land on the last page:\n%s", html)
	}
	if strings.Contains(html, "alpha") {
		t.Fatalf("the clamped page shows a row from page one:\n%s", html)
	}
	if !strings.Contains(html, `aria-current="page" href="/orders?page=2`) {
		t.Fatalf("the pager does not say page 2 is current:\n%s", html)
	}
}

// The clamp's other arm, pinned at the unit: an unknown total may not
// move the reader's page — a failed count is not a run of zero pages.
func TestClampPageKeepsPageWhenCountUnknown(t *testing.T) {
	if got := clampPage(7, 0, 10, false); got != 7 {
		t.Errorf("clampPage(known=false) = %d, want the requested page 7", got)
	}
	if got := clampPage(7, 15, 10, true); got != 2 {
		t.Errorf("clampPage(known=true) = %d, want the last page 2", got)
	}
	if got := clampPage(1, 0, 10, true); got != 1 {
		t.Errorf("clampPage on an empty run = %d, want page 1", got)
	}
}

// A bool facet binds a real bool: the control renders "true"/"false" and
// SQLite's INTEGER storage matches no TEXT spelling, ported from
// resource's bool facet test.
func TestBoolFacetBindsRealBool(t *testing.T) {
	cfg := ordersConfig()
	cfg.Fields = append(cfg.Fields, schema.Field{Name: "active", Type: schema.Bool})
	cfg.Display = &entity.DisplayConfig{Facets: []string{"active"}}
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": cfg},
		map[string][]map[string]any{"orders": {
			{"id": "o1", "name": "alpha", "active": true},
			{"id": "o2", "name": "zeta", "active": false},
		}},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?f_active=true"))
	if !strings.Contains(html, "alpha") {
		t.Fatalf("the true row vanished under the bool facet:\n%s", html)
	}
	if strings.Contains(html, "zeta") {
		t.Fatalf("the false row matched a ?f_active=true facet:\n%s", html)
	}
}

// A declared view narrows the rows and lights its tab.
func TestViewPredicateNarrows(t *testing.T) {
	cfg := ordersConfig()
	cfg.Display = &entity.DisplayConfig{Views: []entity.ListView{
		{Key: "open", Where: `status = "open"`},
	}}
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": cfg},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?view=open"))
	if !strings.Contains(html, "alpha") {
		t.Fatalf("the open view hid the open row:\n%s", html)
	}
	if strings.Contains(html, "zeta") {
		t.Fatalf("the open view showed the paid row:\n%s", html)
	}
	if !strings.Contains(html, `<a aria-current="page" class="fui-tab-nav__link" data-cui-internal="" href="/orders?view=open"`) {
		t.Fatalf("the open tab is not current:\n%s", html)
	}
}

// The active filter draws one chip per top-level AND term, each a link
// to the same URL with that term removed.
func TestFilterChipsWithRemoveLink(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?filter=status+%3D+%22open%22+and+name+%3D+%22alpha%22"))
	for _, want := range []string{
		`status = &quot;open&quot;`, `name = &quot;alpha&quot;`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("chip for %q missing:\n%s", want, html)
		}
	}
	if !strings.Contains(html, `/orders?filter=name+%3D+%22alpha%22"`) {
		t.Errorf("removing the status term must link to the filter without it:\n%s", html)
	}
	// The filter still narrows the rows.
	if strings.Contains(html, "zeta") {
		t.Errorf("the filtered rows include a row the filter excludes:\n%s", html)
	}
}

// The search box narrows over the entity's declared search fields.
func TestSearchNarrows(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?q=zeta"))
	if !strings.Contains(html, "zeta") {
		t.Fatalf("the search hid the matching row:\n%s", html)
	}
	if strings.Contains(html, ">alpha<") {
		t.Fatalf("the search showed the non-matching row:\n%s", html)
	}
}

// The cards presentation draws a grid of cards from the title field and
// the first columns when Display.Card is unset.
func TestCardsPresentation(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders").As("cards"), x.ctx("/orders", ""))
	for _, want := range []string{
		"data-cui-comp=\"ui-card\"",
		`href="/orders/o1"`, "alpha",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("cards missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "data-cui-comp=\"ui-data-table\"") {
		t.Errorf("As(cards) still drew a table:\n%s", html)
	}
}

// The empty state offers New where the caller may create and takes the
// heading level below the list's.
func TestEmptyStateOffersNew(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		nil,
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	for _, want := range []string{
		"No orders yet", // the plural mid-sentence, lowercased
		`href="/orders/create"`,
		"<h2", // the empty heading sits one level under the list's h1
	} {
		if !strings.Contains(html, want) {
			t.Errorf("empty state missing %q:\n%s", want, html)
		}
	}
}

// Every chrome string comes from i18nui: a catalog entry under the key
// wins over the built-in English.
func TestChromeComesFromTranslator(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("en", "ui.entity.count", i18n.Message{Text: "TOTAL {count}"})
	cat.Set("en", "entity.orders.plural", i18n.Message{Text: "Ordres"})
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withTranslator(i18n.NewTranslator(cat, "en")),
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if !strings.Contains(html, "TOTAL 2") {
		t.Errorf("the count subtitle was not translated:\n%s", html)
	}
	if !strings.Contains(html, "Ordres") {
		t.Errorf("the plural was not translated:\n%s", html)
	}
}

// A heading level outside 1..5 fails the slot rather than printing a
// second <h1>: the recover turns the panic into the generic message.
func TestHeadingLevelOutOfRangeFailsSlot(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders").Heading("Orders", 9), x.ctx("/orders", ""))
	if !strings.Contains(html, "Couldn&#39;t load this section") {
		t.Fatalf("an out-of-range heading level must fail the slot:\n%s", html)
	}
}

// Builder names are checked when the list renders: an unknown entity,
// a bad As, a bad Key and an unknown column each fail the slot.
func TestBadBuilderNamesFailSlot(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	ctx := x.ctx("/orders", "")
	cases := map[string]*ListBuilder{
		"unknown entity": x.ui.List("nope"),
		"bad as":         x.ui.List("orders").As("board"),
		"bad key":        x.ui.List("orders").Key("Due"),
		"unknown column": x.ui.List("orders").Columns("wat"),
	}
	for name, b := range cases {
		if html := listHTML(t, b, ctx); !strings.Contains(html, "Couldn&#39;t load this section") {
			t.Errorf("%s: expected the slot to fail:\n%s", name, html)
		}
	}
}
