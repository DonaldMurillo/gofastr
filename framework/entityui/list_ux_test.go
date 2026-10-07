package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Each row's actions sit behind one icon-only menu named for the record:
// open, copy link (from a hidden URL span), duplicate, and a confirmed
// DELETE that lands back on the list.
func TestListRowMenu(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
		withAPI(map[string]string{"orders": "/api/orders"}),
	)
	h := listHTML(t, x.ui.List("orders").Delete().Duplicate(), x.ctx("/orders", ""))
	for _, want := range []string{
		`fui-menu__trigger--icon`,
		`Actions for alpha`,
		`id="eui-url-orders-0"`,
		`data-hui-copy-target="eui-url-orders-0"`,
		`href="/orders/create?duplicate=o1"`,
		`data-cui-rpc="/api/orders/o1"`,
		`data-cui-rpc-method="DELETE"`,
		`data-cui-rpc-navigate="/orders"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("row menu missing %q:\n%s", want, h)
		}
	}
	if n := strings.Count(h, `fui-menu__trigger--icon`); n != 2 {
		t.Errorf("want one row menu per row (2), got %d", n)
	}
	// Without Delete and Duplicate the menu keeps open and copy only.
	plain := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if strings.Contains(plain, `data-cui-rpc-method="DELETE"`) || strings.Contains(plain, "duplicate=") {
		t.Errorf("a list without Delete/Duplicate offered them:\n%s", plain)
	}
}

// A list stays a table at every width: below the breakpoint it scrolls
// sideways in its own container instead of turning rows into cards.
func TestListScrollsOnPhones(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	h := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if strings.Contains(h, "responsive-cards") {
		t.Errorf("the list collapses into cards on phones:\n%s", h)
	}
	if !strings.Contains(h, "fui-data-table__scroll") {
		t.Errorf("the table has no scroll container:\n%s", h)
	}
}

// A relation column and its form field label as the record they point
// at: customer_id reads "Customer", never "Customer Id".
func TestRelationFieldLabel(t *testing.T) {
	x := newInvoiceUI(t)
	list := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u1"))
	create := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "", "u1")))
	for name, h := range map[string]string{"list": list, "create": create} {
		if !strings.Contains(h, ">Customer<") {
			t.Errorf("%s: no Customer label:\n%s", name, h)
		}
		if strings.Contains(h, "Customer Id") {
			t.Errorf("%s: relation labeled with its id suffix:\n%s", name, h)
		}
	}
}

// With no TitleField and no name or title field, the first plain String
// column names a record: never a NoQuery column (a token), never an
// omitted one, never an enum. An entity with none falls back to "".
func TestTitleFieldFallback(t *testing.T) {
	tickets := entity.EntityConfig{
		Table: "tickets",
		Fields: []schema.Field{
			{Name: "secret", Type: schema.String, NoQuery: true},
			{Name: "status", Type: schema.Enum, Values: []string{"open"}},
			{Name: "internal", Type: schema.String},
			{Name: "code", Type: schema.String},
		},
		Display: &entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"internal": {Omit: true}}},
	}
	counts := entity.EntityConfig{Table: "counts", Fields: []schema.Field{{Name: "n", Type: schema.Int}}}
	x := newTestUI(t,
		map[string]entity.EntityConfig{"tickets": tickets.WithTimestamps(false), "counts": counts.WithTimestamps(false)},
		map[string][]map[string]any{"tickets": {{"id": "t1", "secret": "s3", "status": "open", "internal": "x", "code": "TK-9"}}},
		withAPI(map[string]string{"tickets": "/api/tickets"}),
	)
	m, err := x.ui.meta("tickets")
	if err != nil {
		t.Fatal(err)
	}
	if got := m.titleField(); got != "code" {
		t.Errorf("titleField = %q, want code", got)
	}
	h := listHTML(t, x.ui.List("tickets").Delete(), x.userCtx("/tickets", "", "u1"))
	if !strings.Contains(h, "Actions for TK-9") {
		t.Errorf("row menu not named by the fallback title:\n%s", h)
	}
	c, err := x.ui.meta("counts")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.titleField(); got != "" {
		t.Errorf("an entity with no String column titled by %q", got)
	}
}

// A Where pin is the list's context, not data to show or pick: the
// pinned field drops out of the default columns and the facets, and New
// carries it as a ?prefill_ so the create lands already pointed.
func TestWherePinPrefillsAndHides(t *testing.T) {
	ents := invoiceEntities()
	ents["invoices"].Display.Facets = []string{"customer_id", "status"}
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{
		"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
	}))
	ctx := x.userCtx("/rec/customers/cus-1", "", "u1")
	h := listHTML(t, x.ui.List("invoices").Where("customer_id", "cus-1").Base("/rec/invoices"), ctx)
	if !strings.Contains(h, `href="/rec/invoices/create?prefill_customer_id=cus-1"`) {
		t.Errorf("New does not prefill the pin:\n%s", h)
	}
	if strings.Contains(h, `data-label="Customer"`) {
		t.Errorf("the pinned field still shows as a column:\n%s", h)
	}
	if strings.Contains(h, `name="f_customer_id"`) {
		t.Errorf("the pinned field still shows as a facet:\n%s", h)
	}
	if !strings.Contains(h, `name="f_status"`) {
		t.Errorf("an unpinned facet went missing:\n%s", h)
	}
	// The facet's label sits above its select, so the clear choice is a
	// plain "All", never "All Customer".
	unpinned := listHTML(t, x.ui.List("invoices").Key("u"), ctx)
	if !strings.Contains(unpinned, `value="">All</option>`) || strings.Contains(unpinned, ">All Customer<") {
		t.Errorf("select facet clear choice is not a plain All:\n%s", unpinned)
	}
	// An explicit column list keeps what the caller named.
	named := listHTML(t, x.ui.List("invoices").Key("n").Where("customer_id", "cus-1").Columns("number", "customer_id"), ctx)
	if !strings.Contains(named, `data-label="Customer"`) {
		t.Errorf("a named column was dropped:\n%s", named)
	}
}

// A read-only date on the record reads the way the list prints it:
// "May 6, 2026", not the stored 2026-05-06.
func TestRecordReadOnlyDateMatchesList(t *testing.T) {
	rows := invoiceRows()
	rows["invoices"][0]["issued_on"] = "2026-05-06"
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{
		"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
	}))
	body := renderRecord(t, x, "inv-1", nil)
	if !strings.Contains(body, "May 6, 2026") || strings.Contains(body, ">2026-05-06<") {
		t.Errorf("read-only date not in the list's format:\n%s", body)
	}
}

// The Related tab adds no New of its own: the list's header New (and
// its empty state's) is the one, pointed at this record.
func TestRelatedTabOneNew(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	section := body[strings.Index(body, `id="eui-related-payments"`):]
	all := strings.Count(section, "/rec/payments/create")
	pointed := strings.Count(section, `/rec/payments/create?prefill_invoice_id=inv-1"`)
	if all == 0 || all != pointed {
		t.Errorf("%d of %d New links prefill the record:\n%s", pointed, all, section)
	}
	if strings.Contains(section, "fui-button--secondary") {
		t.Errorf("the Related tab drew its own New beside the list's:\n%s", section)
	}
}
