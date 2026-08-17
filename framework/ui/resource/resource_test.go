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
}

func (s *stubSource) CountAll(_ context.Context, opts crud.ListOptions) (int, error) {
	s.countCalls = append(s.countCalls, opts)
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
	for _, want := range []string{"data-fui-comp=\"ui-page-header\"", "data-fui-comp=\"ui-data-table\"", "Past Due", "$1,234.50", "New Order"} {
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
	if opts.Limit != 10 || opts.Offset != 10 {
		t.Errorf("paging options = limit %d offset %d, want 10/10", opts.Limit, opts.Offset)
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
	for _, want := range []string{"Quick add", "data-fui-signal=\"table-customers\"", "data-fui-rpc=\"/api/tables/customers"} {
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
