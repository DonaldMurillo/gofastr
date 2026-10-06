package entityui

// Ports of framework/ui/resource's security baseline onto the entityui
// builders. Each test names the resource test it carries in its comment;
// the dispositions (already-pinned / ported / obsolete-with-successor) are
// recorded in this wave's report. This file never edits the wave-1 tests:
// a guard they already pin is verified here only by mutation, not by
// duplication.

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// anyHref matches every href on a page, for finding links by their parsed
// query rather than by byte order.
var anyHref = regexp.MustCompile(`href="([^"]*)"`)

func parsedQuery(t *testing.T, href string) url.Values {
	t.Helper()
	_, query, _ := strings.Cut(href, "?")
	q, err := url.ParseQuery(strings.ReplaceAll(query, "&amp;", "&"))
	if err != nil {
		t.Fatalf("parse href %q: %v", href, err)
	}
	return q
}

// hrefWith returns the first href whose parsed query satisfies want.
func hrefWith(t *testing.T, html string, want func(url.Values) bool) url.Values {
	t.Helper()
	for _, m := range anyHref.FindAllStringSubmatch(html, -1) {
		if q := parsedQuery(t, m[1]); want(q) {
			return q
		}
	}
	return nil
}

// usersPostsWorld is the relation world: posts are public, users keep the
// default posture (a session is required), so an anonymous caller may read
// the screen's own entity but not the one its relation points at.
func usersPostsWorld(t *testing.T, userRows []map[string]any) *testUI {
	t.Helper()
	users := entity.EntityConfig{
		Fields: fields(schema.Field{Name: "name", Type: schema.String}),
	}.WithTimestamps(false)
	posts := entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "title", Type: schema.String},
			schema.Field{Name: "author_id", Type: schema.Relation, To: "users"},
		),
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
		Exposure:  &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false)
	return newTestUI(t,
		map[string]entity.EntityConfig{"users": users, "posts": posts},
		map[string][]map[string]any{"users": userRows, "posts": {
			{"id": "pst-1", "title": "Hello", "author_id": "usr-7k2"},
		}},
	)
}

// eventsWorld is the aggregate world: an events entity in the default
// posture with one row per enum value.
func eventsWorld(t *testing.T) *testUI {
	t.Helper()
	events := entity.EntityConfig{
		Fields: fields(schema.Field{
			Name: "kind", Type: schema.Enum, Values: []string{"click", "view"},
		}),
	}.WithTimestamps(false)
	return newTestUI(t,
		map[string]entity.EntityConfig{"events": events},
		map[string][]map[string]any{"events": {
			{"id": "e1", "kind": "click"}, {"id": "e2", "kind": "view"},
		}},
	)
}

// Ported from TestRelationLabelsRenderWhenRelatedEntityIsReadable and
// TestConfigResolvesRelationLabels: a readable relation still resolves to
// its display value. The refused side is pinned by
// TestRelationCellMutedWhenRefused; this is the allow side, so an
// always-redact regression cannot pass unnoticed.
func TestPortedRelationLabelRendersReadable(t *testing.T) {
	x := usersPostsWorld(t, []map[string]any{{"id": "usr-7k2", "name": "Jane Author"}})
	html := listHTML(t, x.ui.List("posts"), x.userCtx("/posts", "", "u1"))
	if !strings.Contains(html, "Jane Author") {
		t.Fatalf("a readable relation must resolve to its display value:\n%s", html)
	}
}

// Ported from TestGatedRelationRendersMutedNotTheRawID, stronger than the
// wave-1 pin (which checks the cell text only): the raw foreign key
// appears NOWHERE on the page, not in any attribute, and the muted cell
// still leaves the screen's own row visible.
func TestPortedGatedRelationMutedNeverRawID(t *testing.T) {
	x := usersPostsWorld(t, []map[string]any{{"id": "usr-7k2", "name": "Jane Author"}})
	html := listHTML(t, x.ui.List("posts"), x.ctx("/posts", ""))
	if !strings.Contains(html, "Hello") {
		t.Fatalf("gating the relation suppressed the screen's own rows:\n%s", html)
	}
	if strings.Contains(html, "Jane Author") {
		t.Fatalf("the related entity's display value leaked to a caller it refuses:\n%s", html)
	}
	if strings.Contains(html, "usr-7k2") {
		t.Fatalf("the raw foreign key leaked where a name belongs:\n%s", html)
	}
}

// Ported from TestDashboardAggregatesRespectEntityGate: StatValue and the
// group counts read every row of an entity, and the group counts publish
// the DISTINCT VALUES of the grouped column. A refused read is "no data",
// so an aggregate never announces an entity or its column contents.
func TestPortedStatsRefuseGatedEntity(t *testing.T) {
	x := eventsWorld(t)
	ctx := x.ctx("/dash", "")
	if got := x.ui.StatValue(ctx, "events", "count", "", "", ""); got != "—" {
		t.Errorf("StatValue over a refused entity = %q, want the empty placeholder", got)
	}
	if bars := x.ui.GroupBars(ctx, "events", "kind"); len(bars) != 0 {
		t.Errorf("GroupBars over a refused entity returned %d bar(s) — it publishes the grouped column's contents: %v", len(bars), bars)
	}
}

// Ported from TestDashboardAggregatesWorkWhenReadable: the allow side, so
// "always refuse" cannot satisfy the gate above.
func TestPortedStatsCountReadableEntity(t *testing.T) {
	x := eventsWorld(t)
	ctx := x.userCtx("/dash", "", "u1")
	if got := x.ui.StatValue(ctx, "events", "count", "", "", ""); got != "2" {
		t.Errorf("StatValue over a readable entity = %q, want 2", got)
	}
	bars := x.ui.GroupBars(ctx, "events", "kind")
	if len(bars) != 2 {
		t.Fatalf("GroupBars over a readable entity = %d groups, want 2: %v", len(bars), bars)
	}
}

// Ported from TestCreateFormDoesNotLeakGatedRelationOptions: the create
// form deliberately needs no read on the entity it creates, but its
// relation picker reads a DIFFERENT entity, whose own posture decides.
func TestPortedCreateFormHidesGatedOptions(t *testing.T) {
	users := entity.EntityConfig{
		Fields: fields(schema.Field{Name: "name", Type: schema.String}),
	}.WithTimestamps(false)
	notes := entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "title", Type: schema.String},
			schema.Field{Name: "owner_id", Type: schema.Relation, To: "users"},
		),
		Relations: []entity.Relation{entity.BelongsTo("owner", "users", "owner_id")},
		Exposure:  &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false)
	x := newTestUI(t,
		map[string]entity.EntityConfig{"users": users, "notes": notes},
		map[string][]map[string]any{"users": {{"id": "usr-9q", "name": "Jane Author"}}},
		withAPI(map[string]string{"notes": "/api/notes"}),
	)
	render := func(ctx context.Context) string {
		return string(x.ui.Create("notes").Base("/notes").RenderCtx(ctx))
	}

	anon := render(x.ctx("/notes/create", ""))
	if !strings.Contains(anon, `name="title"`) {
		t.Fatalf("the create form itself must render:\n%s", anon)
	}
	if strings.Contains(anon, "Jane Author") || strings.Contains(anon, `value="usr-9q"`) {
		t.Fatalf("the create form listed a relation the caller may not read:\n%s", anon)
	}

	signed := render(x.userCtx("/notes/create", "", "u1"))
	if !strings.Contains(signed, "Jane Author") {
		t.Fatalf("a readable relation's options vanished — the gate is too tight:\n%s", signed)
	}
}

// paymentsConfig is the Related tab's second entity, in the default
// posture: a session is required to read it.
func paymentsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "invoice_id", Type: schema.Relation, To: "invoices"},
			schema.Field{Name: "ref", Type: schema.String},
		),
		Relations: []entity.Relation{entity.BelongsTo("invoice", "invoices", "invoice_id")},
	}.WithTimestamps(false)
}

func invoicesConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Fields:   fields(schema.Field{Name: "number", Type: schema.String}),
		Exposure: &entity.ExposureConfig{Public: true},
		Display:  &entity.DisplayConfig{Singular: "Invoice", Plural: "Invoices", TitleField: "number"},
	}.WithTimestamps(false)
}

// Ported from TestGatedRelatedSectionIsHiddenNotAnnounced, rows half. The
// Related tab's design changed with entityui (a refused related list draws
// its refusal notice; the entity names are the app's own declaration on a
// page the caller already passed), so what must survive is that no row of
// the gated entity reaches the page.
func TestPortedRelatedTabGatedEntityNoRows(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"invoices": invoicesConfig(), "payments": paymentsConfig()},
		map[string][]map[string]any{
			"invoices": {{"id": "inv-9", "number": "INV-9"}},
			"payments": {{"id": "pay-1", "invoice_id": "inv-9", "ref": "PAY-77"}},
		},
	)
	render := func(ctx context.Context) string {
		return string(x.ui.Record("invoices", "inv-9").Base("/eui/invoices").Related("payments").
			RenderCtx(ctx))
	}

	anon := render(x.ctx("/eui/invoices/inv-9", "?tab=related"))
	if !strings.Contains(anon, "INV-9") {
		t.Fatalf("the record itself must render:\n%s", anon)
	}
	if strings.Contains(anon, "PAY-77") {
		t.Fatalf("the gated related entity's rows leaked:\n%s", anon)
	}

	signed := render(x.userCtx("/eui/invoices/inv-9", "?tab=related", "u1"))
	if !strings.Contains(signed, "PAY-77") {
		t.Fatalf("a readable related entity's rows vanished — the gate is too tight:\n%s", signed)
	}
}

// Ported from TestGatedRelationInsideARelatedSectionRendersMuted: the
// Related tab's lists are list builders, so their relation cells must hold
// the same muted-not-raw-id contract. The list cell itself is pinned by
// TestRelationCellMutedWhenRefused; this pins the composition through the
// record page. The plans entity is either gated behind a permission
// nobody holds or open, everything else identical.
func TestPortedGatedRelationInRelatedList(t *testing.T) {
	build := func(plans entity.EntityConfig) string {
		t.Helper()
		payments := paymentsConfig()
		payments.Fields = append(payments.Fields, schema.Field{Name: "plan_id", Type: schema.Relation, To: "plans"})
		x := newTestUI(t,
			map[string]entity.EntityConfig{
				"invoices": invoicesConfig(),
				"payments": payments,
				"plans":    plans,
			},
			map[string][]map[string]any{
				"invoices": {{"id": "inv-9", "number": "INV-9"}},
				"payments": {{"id": "pay-1", "invoice_id": "inv-9", "ref": "PAY-77", "plan_id": "pln-3x"}},
				"plans":    {{"id": "pln-3x", "name": "Enterprise"}},
			},
		)
		return string(x.ui.Record("invoices", "inv-9").Base("/eui/invoices").Related("payments").
			RenderCtx(x.userCtx("/eui/invoices/inv-9", "?tab=related", "u1")))
	}

	gatedPlans := entity.EntityConfig{
		Fields: fields(schema.Field{Name: "name", Type: schema.String}),
		// A read permission no caller in this test holds: the gate
		// refuses even the signed-in reader.
		Exposure: &entity.ExposureConfig{Access: entity.AccessControl{Read: "plans:read"}},
	}.WithTimestamps(false)
	gated := build(gatedPlans)
	if !strings.Contains(gated, "PAY-77") {
		t.Fatalf("the readable related section did not render its rows, so the label resolver never ran:\n%s", gated)
	}
	if strings.Contains(gated, "Enterprise") {
		t.Fatalf("a related list resolved display labels from an entity the caller may not read:\n%s", gated)
	}
	if strings.Contains(gated, "pln-3x") {
		t.Fatalf("the gated relation fell back to the raw foreign key instead of rendering muted:\n%s", gated)
	}

	openPlans := entity.EntityConfig{
		Fields: fields(schema.Field{Name: "name", Type: schema.String}),
	}.WithTimestamps(false)
	open := build(openPlans)
	if !strings.Contains(open, "Enterprise") {
		t.Fatalf("a readable relation inside a related list rendered no label — the gate is too tight:\n%s", open)
	}
}

// fiveOrderRows is a five-row orders run for the carry tests (page size 4
// makes two pages; every name matches the search "a&b").
func fiveOrderRows() []map[string]any {
	rows := make([]map[string]any, 0, 5)
	for i, name := range []string{"aa&b1", "aa&b2", "aa&b3", "aa&b4", "aa&b5"} {
		rows = append(rows, map[string]any{
			"id": fmt.Sprintf("o%d", i+1), "name": name, "status": "open", "amount": "1", "memo": "m",
		})
	}
	return rows
}

// Ported from TestListCarryQueryKeepsFmtVerbs: request-derived values must
// never corrupt the hrefs the page builds. Every link is built through
// net/url (Encode), never fmt or a pattern string: Encode()'s own %XX
// triples would read as flag/width/verb to Sprintf and every link on the
// page would navigate to a corrupted URL.
func TestPortedListHrefsKeepRequestValues(t *testing.T) {
	for _, tc := range []struct {
		name, query, search string
		matchAll            bool
	}{
		{"search-amp", "?q=a%26b", "a&b", true},
		{"search-percent", "?q=50%25+off", "50% off", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := fiveOrderRows()
			if !tc.matchAll {
				for i, r := range rows {
					r["name"] = fmt.Sprintf("50%% off %d", i+1)
				}
			}
			x := newTestUI(t,
				map[string]entity.EntityConfig{"orders": ordersConfig()},
				map[string][]map[string]any{"orders": rows},
			)
			html := listHTML(t, x.ui.List("orders").PageSize(4), x.ctx("/orders", tc.query))
			if strings.Contains(html, "%!") {
				t.Errorf("SECURITY: [fmt-carry] a href on the page carries a fmt directive: %s", html[:min(len(html), 400)])
			}
			sortHref := hrefWith(t, html, func(q url.Values) bool {
				return q.Get("sort") == "name" && len(q["sort"]) == 1
			})
			if sortHref == nil {
				t.Fatalf("[fmt-carry] no sort anchor rendered:\n%s", html[:min(len(html), 400)])
			}
			if got := sortHref.Get("q"); got != tc.search {
				t.Errorf("[fmt-carry] the sort href lost or corrupted the search: q=%q want %q", got, tc.search)
			}
			if tc.matchAll {
				// Five matches over page size 4 is a two-page run, so a
				// page-2 link exists and must carry the search too.
				pq := hrefWith(t, html, func(q url.Values) bool { return q.Get("page") == "2" })
				if pq == nil {
					t.Fatalf("[fmt-carry] no page-2 link rendered (5 rows / page size 4 = 2 pages):\n%s", html[:min(len(html), 400)])
				}
				if got := pq.Get("q"); got != tc.search {
					t.Errorf("[fmt-carry] the page-2 href lost or corrupted the search: q=%q want %q", got, tc.search)
				}
			}
		})
	}
}

// Ported from TestPagerCarriesTheActiveSort: a page turn must not drop the
// order the reader chose. The page-2 href carries sort and dir beside
// everything else the run narrowed to.
func TestPortedPagerKeepsActiveSort(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": fiveOrderRows()},
	)
	html := listHTML(t, x.ui.List("orders").PageSize(4), x.ctx("/orders", "?sort=name&dir=desc"))
	pq := hrefWith(t, html, func(q url.Values) bool { return q.Get("page") == "2" })
	if pq == nil {
		t.Fatalf("[pager-sort-carry] no page-2 link rendered (5 rows / page size 4 = 2 pages):\n%s", html[:min(len(html), 400)])
	}
	if pq.Get("sort") != "name" || pq.Get("dir") != "desc" || len(pq["sort"]) != 1 || len(pq["dir"]) != 1 {
		t.Errorf("[pager-sort-carry] the page-2 href lost the active sort: sort=%q dir=%q values=%v",
			pq.Get("sort"), pq.Get("dir"), pq)
	}
}

// Ported from TestTableHugePageOffsetGuarded and the ?p=0 / negative arms
// of TestPageOutOfRangeIsTheLastPage. The page clamp runs before the
// offset math and OffsetForPage carries its own overflow guard; these pin
// the request-facing ends: an astronomically large page lands on the last
// page, a zero or negative page lands on the first, and neither wraps to
// a wrong window nor fails the slot.
func TestPortedPageBoundsLandInsideRun(t *testing.T) {
	rows := []map[string]any{
		{"id": "o1", "name": "alpha", "status": "open", "amount": "1", "memo": "m"},
		{"id": "o2", "name": "zeta", "status": "paid", "amount": "2", "memo": "m"},
	}
	for _, tc := range []struct {
		query      string
		want, gone string
	}{
		{fmt.Sprintf("?page=%d", int64(1)<<62+2), "zeta", "alpha"},
		{"?page=0", "alpha", "zeta"},
		{"?page=-3", "alpha", "zeta"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			x := newTestUI(t,
				map[string]entity.EntityConfig{"orders": ordersConfig()},
				map[string][]map[string]any{"orders": rows},
			)
			html := listHTML(t, x.ui.List("orders").PageSize(1), x.ctx("/orders", tc.query))
			if !strings.Contains(html, ">"+tc.want+"<") {
				t.Errorf("[page-bounds] request %q did not render row %q:\n%s", tc.query, tc.want, html[:min(len(html), 400)])
			}
			if strings.Contains(html, ">"+tc.gone+"<") {
				t.Errorf("[page-bounds] request %q served the wrong window (row %q rendered):\n%s", tc.query, tc.gone, html[:min(len(html), 400)])
			}
		})
	}
}

// Ported from TestEnumFacetFilterStaysString: a bool facet binds a real
// bool, and nothing else does. An enum value that SPELLS "true" must stay
// the string it is, or the facet silently shows zero rows.
func TestPortedEnumFacetBindsStringTrue(t *testing.T) {
	cfg := ordersConfig()
	cfg.Fields[1] = schema.Field{Name: "status", Type: schema.Enum, Values: []string{"true", "draft"}, Default: "draft"}
	cfg.Display = &entity.DisplayConfig{Facets: []string{"status"}}
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": cfg},
		map[string][]map[string]any{"orders": {
			{"id": "o1", "name": "alpha", "status": "true", "amount": "1", "memo": "m"},
			{"id": "o2", "name": "zeta", "status": "draft", "amount": "2", "memo": "m"},
		}},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?f_status=true"))
	if !strings.Contains(html, "alpha") {
		t.Fatalf("the enum row whose value spells \"true\" vanished under its own facet:\n%s", html)
	}
	if strings.Contains(html, "zeta") {
		t.Fatalf("the draft row matched a ?f_status=true facet:\n%s", html)
	}
}

// Ported from TestFormBoolFieldRoundTripsAsTrueFalse: a bool field submits
// exactly "true" or "false" — a hidden "false" before the checkbox, the
// checkbox carrying value="true". A bare checkbox submits "on" (refused by
// the validator) or nothing (false unsaveable).
func TestPortedFormBoolSubmitsTrueFalse(t *testing.T) {
	cfg := ordersConfig()
	cfg.Fields = append(cfg.Fields, schema.Field{Name: "notify", Type: schema.Bool})
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": cfg},
		nil,
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	for _, tc := range []struct {
		query   string
		checked bool
	}{{"?prefill_notify=true", true}, {"", false}} {
		body := string(x.ui.Create("orders").Base("/orders").
			RenderCtx(x.userCtx("/orders/create", tc.query, "u1")))
		var hidden, box string
		for _, seg := range strings.Split(body, "<input") {
			if !strings.Contains(seg, `name="notify"`) {
				continue
			}
			if strings.Contains(seg, `type="hidden"`) {
				hidden = seg
			}
			if strings.Contains(seg, `type="checkbox"`) {
				box = seg
			}
		}
		if hidden == "" || !strings.Contains(hidden, `value="false"`) {
			t.Fatalf("query %q: no hidden name=notify value=false input:\n%s", tc.query, body)
		}
		if box == "" || !strings.Contains(box, `value="true"`) {
			t.Fatalf("query %q: no checkbox name=notify value=true input:\n%s", tc.query, body)
		}
		if got := strings.Contains(box, "checked"); got != tc.checked {
			t.Fatalf("query %q: checked=%v, want %v:\n%s", tc.query, got, tc.checked, body)
		}
	}
}

// Ported from TestRelationSelectOptionsAreDeterministic: the facet's
// options come out of a map, and a map's iteration order is randomized per
// run — the options must be ordered (sorted by label) and two renders of
// the same relation byte-identical. The ids insert in u1..u6 order while
// the names sort differently, so id order cannot satisfy the check.
func TestPortedRelationFacetOptionsSorted(t *testing.T) {
	names := map[string]string{
		"u1": "Grace Hopper", "u2": "Ada Lovelace", "u3": "Edsger Dijkstra",
		"u4": "Blaise Pascal", "u5": "Alan Turing", "u6": "Cain",
	}
	users := make([]map[string]any, 0, len(names))
	for _, id := range []string{"u1", "u2", "u3", "u4", "u5", "u6"} {
		users = append(users, map[string]any{"id": id, "name": names[id]})
	}
	userCfg := entity.EntityConfig{
		Fields: fields(schema.Field{Name: "name", Type: schema.String}),
	}.WithTimestamps(false)
	posts := entity.EntityConfig{
		Fields: fields(
			schema.Field{Name: "title", Type: schema.String},
			schema.Field{Name: "author_id", Type: schema.Relation, To: "users"},
		),
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
		Exposure:  &entity.ExposureConfig{Public: true},
		Display:   &entity.DisplayConfig{Facets: []string{"author_id"}},
	}.WithTimestamps(false)
	x := newTestUI(t,
		map[string]entity.EntityConfig{"users": userCfg, "posts": posts},
		map[string][]map[string]any{"users": users, "posts": {
			{"id": "p1", "title": "Hello", "author_id": "u2"},
		}},
	)

	// Two fresh contexts: the key claim would refuse a second render of
	// the same unkeyed list on one context.
	first := listHTML(t, x.ui.List("posts"), x.userCtx("/posts", "", "u1"))
	second := listHTML(t, x.ui.List("posts"), x.userCtx("/posts", "", "u1"))
	if first != second {
		t.Fatalf("two renders of the same relation facet differ:\n%s\n---\n%s", first, second)
	}
	labelRe := regexp.MustCompile(`<option[^>]*value="(u\d)"[^>]*>([^<]*)</option>`)
	opts := labelRe.FindAllStringSubmatch(first, -1)
	if len(opts) != len(names) {
		t.Fatalf("facet options = %d, want %d:\n%s", len(opts), len(names), first)
	}
	for i := 1; i < len(opts); i++ {
		if opts[i][2] < opts[i-1][2] {
			t.Errorf("facet options are not sorted by label: %q before %q:\n%s", opts[i-1][2], opts[i][2], first)
		}
	}
}

// Ported from TestDetailTransitionUnknownVariantSecondary: a transition
// whose variant string is unrecognised renders the quiet secondary button,
// so a row action never outranks the page's primary action.
func TestPortedUnknownVariantRendersSecondary(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.States.Transitions = append(inv.States.Transitions, entity.Transition{
		Key: "bogus", From: []string{"draft"}, To: "paid", Variant: "bogus",
	})
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{
		"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
	}))
	body := renderRecord(t, x, "inv-1", nil)
	btn := regexp.MustCompile(`<button[^>]*transitions/bogus[^>]*>`).FindString(body)
	if btn == "" {
		t.Fatalf("the bogus-variant move drew no button:\n%s", body)
	}
	if !strings.Contains(btn, "fui-button--secondary") {
		t.Errorf("a transition with an unknown variant must render the secondary button:\n%s", btn)
	}
	if strings.Contains(btn, "fui-button--primary") {
		t.Errorf("a transition with an unknown variant must not render the primary button:\n%s", btn)
	}
}

// Ported from TestDetailActionsCarryErrorToast: a refused Delete or move
// (a 409 for a referenced record) must reach the user: each record action
// carries the runtime's error-toast hook, not only an OnSuccess navigate.
func TestPortedActionsCarryErrorToast(t *testing.T) {
	x := newInvoiceUI(t)
	body := renderRecord(t, x, "inv-1", func(b *RecordBuilder) { b.Delete() })
	for _, want := range []string{
		`data-cui-rpc-error-toast="Could not send."`,
		`data-cui-rpc-error-toast="Could not delete this Invoice."`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the record's actions are missing %s:\n%s", want, body)
		}
	}
}

// Ported from the shift arms of TestHeadingLevelShiftsTitleAndEmpty (the
// out-of-range arm is pinned by TestHeadingLevelOutOfRangeFailsSlot): a
// list nested under a page's own h1 takes a lower title level, its empty
// state one below that, and no second h1 is printed.
func TestPortedHeadingShiftsTitleAndEmpty(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		nil,
	)
	html := listHTML(t, x.ui.List("orders").Heading("Overdue", 2), x.ctx("/orders", ""))
	for _, want := range []string{"<h2", "<h3"} {
		if !strings.Contains(html, want) {
			t.Errorf("HeadingLevel 2: want the title and empty state at %s:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<h1") {
		t.Errorf("HeadingLevel 2 still renders an h1:\n%s", html)
	}
}
