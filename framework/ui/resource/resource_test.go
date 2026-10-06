package resource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

type stubSource struct {
	rows       []map[string]any
	countCalls []crud.ListOptions
	listCalls  []crud.ListOptions
	// countErr, when set, is what CountAll returns beside a zero count.
	countErr error
}

func (s *stubSource) CountAll(_ context.Context, opts crud.ListOptions) (int, error) {
	s.countCalls = append(s.countCalls, opts)
	if s.countErr != nil {
		return 0, s.countErr
	}
	return len(s.rows), nil
}

func (s *stubSource) ListAll(_ context.Context, opts crud.ListOptions) ([]map[string]any, error) {
	s.listCalls = append(s.listCalls, opts)
	return s.rows, nil
}

func (s *stubSource) GetOne(_ context.Context, id string, _ []string) (map[string]any, error) {
	for _, row := range s.rows {
		if cell(rowValue(row, "id")) == id {
			return row, nil
		}
	}
	return nil, nil
}

func TestConfigPreservesFieldDisplayAndFormatOverrides(t *testing.T) {
	cfg := Config{
		Entity: "orders",
		Fields: []Field{
			{Key: "status", Label: "State", Type: "enum", Values: []string{"past_due"}},
			{Key: "amount", Label: "Total", Type: "decimal"},
		},
	}

	got := cfg.WithColumns("amount")
	if len(got.Fields) != 1 || got.Fields[0].Key != "amount" || got.Fields[0].Label != "Total" || got.Fields[0].Type != "decimal" {
		t.Fatalf("WithColumns lost field overrides: %#v", got.Fields)
	}
	if cfg.Entity != "orders" || len(cfg.Fields) != 2 {
		t.Fatalf("WithColumns mutated the original config: %#v", cfg)
	}
}

func TestConfigListUsesFrameworkComponentsAndConfiguredFormatting(t *testing.T) {
	cfg := Config{
		Entity:    "orders",
		Title:     "Orders",
		Singular:  "Order",
		BasePath:  "/orders",
		APIPath:   "/api/orders",
		Crud:      &stubSource{rows: []map[string]any{{"id": "o-1", "status": "past_due", "amount": "1234.5"}}},
		PageSize:  25,
		CanCreate: true,
		Fields: []Field{
			{Key: "status", Label: "State", Type: "enum"},
			{Key: "amount", Label: "Total", Type: "decimal"},
		},
	}

	html := string(cfg.List(context.Background()))
	for _, want := range []string{"data-cui-comp=\"ui-page-header\"", "data-cui-comp=\"ui-data-table\"", "Past Due", "$1,234.50", "New Order"} {
		if !strings.Contains(html, want) {
			t.Errorf("List output missing %q:\n%s", want, html)
		}
	}
}

func TestConfigListPassesURLQueryToDataSource(t *testing.T) {
	source := &stubSource{rows: []map[string]any{{"id": "o-1", "name": "Ada", "status": "open", "amount": "10"}}}
	cfg := Config{
		Entity:   "orders",
		Title:    "Orders",
		Singular: "Order",
		BasePath: "/orders",
		Crud:     source,
		Search:   "name",
		PageSize: 10,
		Fields: []Field{
			{Key: "name", Label: "Name", Type: "string"},
			{Key: "status", Label: "Status", Type: "enum"},
			{Key: "amount", Label: "Amount", Type: "decimal"},
		},
		Filters: []Filter{{Key: "status", Label: "Status", Type: "enum", Values: []string{"open"}}},
	}
	req := httptest.NewRequest(http.MethodGet, "/orders?q=ada&status=open&sort=amount&dir=desc&p=2", nil)
	cfg.List(appui.WithRequest(context.Background(), req))

	if len(source.countCalls) != 1 {
		t.Fatalf("CountAll calls = %d, want 1 list query", len(source.countCalls))
	}
	if len(source.listCalls) != 1 {
		t.Fatalf("ListAll calls = %d, want 1", len(source.listCalls))
	}
	opts := source.listCalls[0]
	// The stub holds one row, so the run is one page at size 10: the
	// requested ?p=2 is out of range and the screen clamps it to the
	// last real page (page 1) before fetching — offset 0, never the
	// empty window offset 10 would fetch.
	if opts.Limit != 10 || opts.Offset != 0 {
		t.Errorf("paging options = limit %d offset %d, want 10/0 (p=2 clamped to the one-page run)", opts.Limit, opts.Offset)
	}
	if len(opts.Sorts) != 1 || opts.Sorts[0].Field != "amount" || !opts.Sorts[0].Desc {
		t.Errorf("sort options = %#v, want amount desc", opts.Sorts)
	}
	if len(opts.Filters) != 2 || opts.Filters[0].Field != "name" || opts.Filters[0].Value != "ada" || opts.Filters[1].Field != "status" || opts.Filters[1].Value != "open" {
		t.Errorf("filter options = %#v, want name LIKE ada and status=open", opts.Filters)
	}
}

func TestConfigResolvesRelationLabels(t *testing.T) {
	customers := &stubSource{rows: []map[string]any{{"id": "c-1", "name": "Ada Lovelace"}}}
	orders := &stubSource{rows: []map[string]any{{"id": "o-1", "customer_id": "c-1"}}}
	cfg := Config{
		Entity:    "orders",
		Title:     "Orders",
		Singular:  "Order",
		BasePath:  "/orders",
		Crud:      orders,
		Fields:    []Field{{Key: "customer_id", Label: "Customer", Type: "relation"}},
		Relations: map[string]Relation{"customer_id": {Crud: customers, Display: "name"}},
	}

	if html := string(cfg.List(context.Background())); !strings.Contains(html, "Ada Lovelace") {
		t.Fatalf("relation label missing from list:\n%s", html)
	}
}

func TestConfigEmptyStatesPreserveHeadingOrder(t *testing.T) {
	cfg := Config{
		Entity:   "orders",
		Title:    "Orders",
		Singular: "Order",
		BasePath: "/orders",
		Crud:     &stubSource{},
		Fields:   []Field{{Key: "name", Label: "Name", Type: "string"}},
	}

	if list := string(cfg.List(context.Background())); !strings.Contains(list, "<h2") {
		t.Fatalf("list empty state must render an h2 below the page header:\n%s", list)
	}
	if detail := string(cfg.Detail(context.Background(), "missing")); !strings.Contains(detail, "<h1") {
		t.Fatalf("standalone not-found detail must render an h1:\n%s", detail)
	}
	if form := string(cfg.Form(context.Background(), "missing")); !strings.Contains(form, "<h1") {
		t.Fatalf("standalone not-found form must render an h1:\n%s", form)
	}
}

// A list nested under a page's own h1 takes a lower title level, and its
// empty state follows one level below; an out-of-range level panics
// rather than print a second h1.
func TestHeadingLevelShiftsTitleAndEmpty(t *testing.T) {
	cfg := Config{
		Entity:   "orders",
		Title:    "Orders",
		Singular: "Order",
		BasePath: "/orders",
		Crud:     &stubSource{},
		Fields:   []Field{{Key: "name", Label: "Name", Type: "string"}},
	}
	for _, tc := range []struct {
		level       int
		title, empt string
	}{{0, "<h1", "<h2"}, {1, "<h1", "<h2"}, {2, "<h2", "<h3"}, {5, "<h5", "<h6"}} {
		list := string(cfg.WithHeadingLevel(tc.level).List(context.Background()))
		if !strings.Contains(list, tc.title) || !strings.Contains(list, tc.empt) {
			t.Errorf("HeadingLevel %d: want title %s and empty state %s:\n%s", tc.level, tc.title, tc.empt, list)
		}
		if tc.level == 2 && strings.Contains(list, "<h1") {
			t.Errorf("HeadingLevel 2 still renders an h1:\n%s", list)
		}
	}
	for _, level := range []int{-1, 6, 9} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("HeadingLevel %d rendered instead of panicking", level)
				}
			}()
			_ = cfg.WithHeadingLevel(level).List(context.Background())
		}()
	}
}

// A related list is a section of the detail page, under its <h1>: the
// list's title is an <h2> and its empty state an <h3>, so the page
// keeps one <h1>.
func TestRelatedListsSitUnderDetailH1(t *testing.T) {
	cfg := Config{
		Entity: "customers", Title: "Customers", Singular: "Customer", BasePath: "/customers",
		Crud:   &stubSource{rows: []map[string]any{{"id": "c1", "name": "Ada"}}},
		Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
		Related: []RelatedList{
			{Title: "Invoices", ForeignKey: "customer_id", Crud: &stubSource{rows: []map[string]any{{"id": "i1", "number": "INV-1"}}},
				Fields: []Field{{Key: "number", Label: "Number", Type: "string"}}},
			{Title: "Notes", ForeignKey: "customer_id", Crud: &stubSource{},
				Fields: []Field{{Key: "body", Label: "Body", Type: "string"}}},
		},
	}
	html := string(cfg.Detail(context.Background(), "c1"))
	if n := strings.Count(html, "<h1"); n != 1 {
		t.Errorf("detail page with related lists has %d <h1>, want 1:\n%s", n, html)
	}
	for _, want := range []string{">Invoices</h2>", ">Notes</h2>", ">No notes yet</h3>"} {
		if !strings.Contains(html, want) {
			t.Errorf("detail page lacks %q:\n%s", want, html)
		}
	}
}

func TestConfigWithIslandRendersTableRPCAndRejectsAnonymousCalls(t *testing.T) {
	cfg := Config{
		Entity:   "customers",
		Title:    "Customers",
		Singular: "Customer",
		BasePath: "/app/customers",
		APIPath:  "/api/customers",
		Crud:     &stubSource{rows: []map[string]any{{"id": "c-1", "name": "Ada"}}},
		Fields:   []Field{{Key: "name", Label: "Name", Type: "string"}},
	}.WithIsland("/api/tables/customers").WithActions(render.Text("Quick add"))

	html := string(cfg.List(context.Background()))
	for _, want := range []string{"Quick add", "data-cui-signal=\"table-customers\"", "data-cui-rpc=\"/api/tables/customers"} {
		if !strings.Contains(html, want) {
			t.Errorf("island list missing %q:\n%s", want, html)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/tables/customers", nil)
	rr := httptest.NewRecorder()
	cfg.TableHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous TableHandler status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := money("1234.5"); got != "$1,234.50" {
		t.Fatalf("money = %q", got)
	}
	if got := title("past_due"); got != "Past Due" {
		t.Fatalf("title = %q", got)
	}
	if got := rowValue(map[string]any{"genericName": "x"}, "generic_name"); got != "x" {
		t.Fatalf("rowValue snake→camel fallback = %v, want x", got)
	}
	h := format(Field{Key: "status", Type: "enum"}, "past_due", nil)
	if !strings.Contains(string(h), "Past Due") {
		t.Fatalf("enum format missing label: %s", h)
	}
}

// relationSelect walks the labels map, and a map's iteration order is
// randomized per run — the options it writes must not be. The order is
// sorted by value (the mapwriter rule), and two renders of the same
// relation are byte-identical.
func TestRelationSelectOptionsAreDeterministic(t *testing.T) {
	labels := map[string]string{
		"c-3": "Cain", "c-1": "Ada Lovelace", "c-5": "Edsger Dijkstra",
		"c-2": "Grace Hopper", "c-6": "Blaise Pascal", "c-4": "Alan Turing",
	}
	cfg := Config{}
	first := string(cfg.relationSelect(Field{Key: "customer_id", Label: "Customer"}, "f-customer_id", labels, "c-2"))
	second := string(cfg.relationSelect(Field{Key: "customer_id", Label: "Customer"}, "f-customer_id", labels, "c-2"))
	if first != second {
		t.Fatalf("two renders of the same relation differ:\n%s\n---\n%s", first, second)
	}
	// The order is not merely stable, it is sorted: assert the
	// <option> value sequence directly.
	want := []string{"", "c-1", "c-2", "c-3", "c-4", "c-5", "c-6"}
	got := []string{}
	for _, seg := range strings.Split(first, "<option ") {
		if v := betweenAttr(seg, "value"); v != "" || len(got) == 0 {
			got = append(got, v)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("option count = %d, want %d:\n%s", len(got), len(want), first)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("option %d = %q, want %q (sorted by value):\n%s", i, got[i], want[i], first)
		}
	}
	// The current value stays selected wherever it sits in the order.
	if !strings.Contains(first, `selected="" value="c-2"`) {
		t.Errorf("the current relation is not marked selected:\n%s", first)
	}
}

// TestDetailTransitionUnknownVariantSecondary pins the site default:
// a transition whose variant string is unrecognised renders the quiet
// secondary button, so a row action never outranks the page's primary
// action.
func TestDetailTransitionUnknownVariantSecondary(t *testing.T) {
	cfg := Config{
		Entity:   "orders",
		Title:    "Orders",
		Singular: "Order",
		BasePath: "/orders",
		APIPath:  "/api/orders",
		Crud:     &stubSource{rows: []map[string]any{{"id": "o-1", "status": "past_due"}}},
		Transitions: []Transition{
			{Label: "Ship", Status: "shipped", Variant: "bogus"},
		},
		Fields: []Field{{Key: "status", Label: "State", Type: "enum"}},
	}
	html := string(cfg.Detail(context.Background(), "o-1"))
	if !strings.Contains(html, "fui-button--secondary") {
		t.Errorf("a transition with an unknown variant must render the secondary button:\n%s", html)
	}
	if strings.Contains(html, "fui-button--primary") {
		t.Errorf("a transition with an unknown variant must not render the primary button:\n%s", html)
	}
}

// A refused Delete or transition (a 409 for a referenced record) must
// reach the user: each detail action carries the runtime's error-toast
// hook, not only an OnSuccess navigate.
func TestDetailActionsCarryErrorToast(t *testing.T) {
	cfg := Config{
		Entity:   "orders",
		Title:    "Orders",
		Singular: "order",
		BasePath: "/orders",
		APIPath:  "/api/orders",
		CanEdit:  true,
		Crud:     &stubSource{rows: []map[string]any{{"id": "o-1", "status": "open"}}},
		Transitions: []Transition{
			{Label: "Ship", Status: "shipped"},
		},
		Fields: []Field{{Key: "status", Label: "State", Type: "enum"}},
	}
	html := string(cfg.Detail(context.Background(), "o-1"))
	for _, want := range []string{
		`data-cui-rpc-error-toast="Could not delete this order."`,
		`data-cui-rpc-error-toast="Could not ship."`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail actions missing %s:\n%s", want, html)
		}
	}
}

// betweenAttr pulls attr="…" out of an <option …-shaped segment.
func betweenAttr(seg, attr string) string {
	needle := attr + `="`
	i := strings.Index(seg, needle)
	if i < 0 {
		return ""
	}
	rest := seg[i+len(needle):]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return rest[:j]
	}
	return ""
}
