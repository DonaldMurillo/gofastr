package entityui

import (
	"context"
	"fmt"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// seedInvoiceRows adds n open invoices of amount each beside inv-1
// (120.00, draft), in one statement, their ids and numbers prefixed.
func seedInvoiceRows(t *testing.T, x *testUI, prefix string, n int, amount string) {
	t.Helper()
	stmt := fmt.Sprintf(`WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < %d)
		INSERT INTO invoices (id, number, amount, status) SELECT ? || x, ? || x, ?, 'open' FROM c`, n)
	if _, err := x.db.Exec(stmt, prefix, prefix, amount); err != nil {
		t.Fatal(err)
	}
}

// A sum reads every match, past any row count a list would page through,
// and prints its decimals rounded from the database's total, not from a
// float summed row by row.
func TestStatSumIsWholeAndExact(t *testing.T) {
	x := newInvoiceUI(t)
	seedInvoiceRows(t, x, "g", 100005, "0.10")
	ctx := x.userCtx("/dash", "", "u1")
	if got := x.ui.StatValue(ctx, "invoices", "sum", "amount", "", "money"); got != "$10,120.50" {
		t.Fatalf("money sum = %q, want $10,120.50 over all 100,006 rows", got)
	}
	if got := x.ui.StatValue(ctx, "invoices", "sum", "amount", `status = "draft"`, ""); got != "120" {
		t.Fatalf("draft sum = %q, want 120", got)
	}
	if got := x.ui.StatValue(ctx, "invoices", "sum", "amount", `status = "draft"`, "money"); got != "$120.00" {
		t.Fatalf("draft money sum = %q, want $120.00", got)
	}
	if got := x.ui.StatValue(ctx, "invoices", "count", "", "", ""); got != "100,006" {
		t.Fatalf("count = %q, want 100,006", got)
	}
}

// Only "count" (or empty) and "sum" compute anything: another spelling
// is not quietly a count.
func TestStatUnknownAggIsRefused(t *testing.T) {
	x := newInvoiceUI(t)
	ctx := x.userCtx("/dash", "", "u1")
	for _, agg := range []string{"SUM", "avg", " sum"} {
		if got := x.ui.StatValue(ctx, "invoices", agg, "amount", "", ""); got != "—" {
			t.Errorf("agg %q = %q, want the empty placeholder", agg, got)
		}
	}
	if got := x.ui.StatValue(ctx, "invoices", "", "", "", ""); got != "1" {
		t.Errorf("empty agg = %q, want the count 1", got)
	}
	if got := x.ui.StatValue(ctx, "invoices", "sum", "memo", "", ""); got != "—" {
		t.Errorf("sum of a text field = %q, want the empty placeholder", got)
	}
}

// CheckStat refuses at boot every spec StatValue would draw as "—" for
// a reason no request can change.
func TestCheckStatRefusesWhatCannotCompute(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(slices.Clone(inv.Fields), schema.Field{Name: "cost", Type: schema.Decimal, NoQuery: true})
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows())
	for _, ok := range [][5]string{
		{"invoices", "", "", "", ""},
		{"invoices", "count", "", `status = "draft"`, ""},
		{"invoices", "sum", "amount", `status = "draft"`, "money"},
	} {
		if err := x.ui.CheckStat(ok[0], ok[1], ok[2], ok[3], ok[4]); err != nil {
			t.Errorf("CheckStat%q: %v", ok, err)
		}
	}
	for name, bad := range map[string][5]string{
		"unknown entity":     {"ghosts", "count", "", "", ""},
		"unknown agg":        {"invoices", "avg", "amount", "", ""},
		"sum with no field":  {"invoices", "sum", "", "", ""},
		"sum of text":        {"invoices", "sum", "memo", "", ""},
		"sum of no field":    {"invoices", "sum", "nope", "", ""},
		"sum of no-query":    {"invoices", "sum", "cost", "", ""},
		"count with a field": {"invoices", "count", "amount", "", ""},
		"bad filter":         {"invoices", "count", "", `nope = 1`, ""},
		"unknown format":     {"invoices", "sum", "amount", "", "euros"},
		"money on a count":   {"invoices", "count", "", "", "money"},
	} {
		if err := x.ui.CheckStat(bad[0], bad[1], bad[2], bad[3], bad[4]); err == nil {
			t.Errorf("%s: CheckStat%q passed", name, bad)
		}
	}
}

// Count is the list's count, and draws nothing when it cannot answer.
func TestCountReportsWhatItCannotRead(t *testing.T) {
	x := newInvoiceUI(t)
	seedInvoiceRows(t, x, "c", 1233, "1.00")
	if got, ok := x.ui.Count(x.userCtx("/dash", "", "u1"), "invoices", ""); !ok || got != "1,234" {
		t.Fatalf("count = %q %v, want 1,234", got, ok)
	}
	if got, ok := x.ui.Count(x.userCtx("/dash", "", "u1"), "invoices", `status = "draft"`); !ok || got != "1" {
		t.Fatalf("draft count = %q %v, want 1", got, ok)
	}
	if got, ok := x.ui.Count(x.userCtx("/dash", "", "u1"), "invoices", `nope = 1`); ok {
		t.Fatalf("a bad filter counted %q", got)
	}
	if got, ok := x.ui.Count(x.ctx("/dash", ""), "invoices", ""); ok {
		t.Fatalf("SECURITY: an anonymous caller read a count: %q", got)
	}
}

// AfterList hooks mask rows after the query, so a total the database
// computed would count what they hide. The stat totals the masked rows
// instead, and past the row cap draws nothing rather than part of them.
func TestStatSumPassesAfterListHooks(t *testing.T) {
	x := newInvoiceUI(t)
	seedInvoiceRows(t, x, "g", 3, "50.00")
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		for _, r := range data.(*hook.ListPayload).Results {
			if r["id"] == "inv-1" {
				r["amount"] = "****"
			}
		}
		return nil
	})
	ctx := x.userCtx("/dash", "", "u1")
	if got := x.ui.StatValue(ctx, "invoices", "sum", "amount", "", ""); got != "150" {
		t.Fatalf("SECURITY: sum under an AfterList mask = %q, want 150 (inv-1's 120.00 masked)", got)
	}
	if bars := x.ui.GroupBars(ctx, "invoices", "status"); len(bars) != 2 {
		t.Fatalf("groups under an AfterList hook = %v, want draft and open", bars)
	}
	seedInvoiceRows(t, x, "h", statRowCap, "1")
	if got := x.ui.StatValue(ctx, "invoices", "sum", "amount", "", ""); got != "—" {
		t.Fatalf("masked sum past the row cap = %q, want the empty placeholder", got)
	}
}

// Groups come from the database in value order, an enum's in its
// declared order; a field with more values than a chart draws draws none.
func TestStatGroupsOrderAndCap(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = slices.Clone(inv.Fields)
	for i := range inv.Fields {
		if inv.Fields[i].Name == "status" {
			inv.Fields[i].Values = []string{"open", "draft", "paid"}
		}
	}
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	if _, err := x.db.Exec(`INSERT INTO invoices (id, number, amount, status) VALUES ('p1', 'P-1', '1', 'paid'), ('o1', 'O-1', '1', 'open'), ('o2', 'O-2', '1', 'open')`); err != nil {
		t.Fatal(err)
	}
	ctx := x.userCtx("/dash", "", "u1")
	bars := x.ui.GroupBars(ctx, "invoices", "status")
	var got []string
	for _, b := range bars {
		got = append(got, fmt.Sprintf("%s=%v", b.Label, b.Value))
	}
	if fmt.Sprint(got) != "[Open=2 Draft=1 Paid=1]" {
		t.Fatalf("bars = %v, want the enum's declared order", got)
	}
	seedInvoiceRows(t, x, "g", statGroupCap, "1")
	if bars := x.ui.GroupBars(ctx, "invoices", "number"); len(bars) != 0 {
		t.Fatalf("a field with %d+ values drew %d bars, want none", statGroupCap, len(bars))
	}
}

// CountUpTo reads at most limit+1 ids: the number under the cap, the cap
// and more past it, in the caller's scope like Count.
func TestCountUpTo(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	ctx := x.ctx("/", "")
	if n, more, ok := x.ui.CountUpTo(ctx, "orders", "", 5); !ok || more || n != 2 {
		t.Errorf("CountUpTo(5) = %d, %v, %v", n, more, ok)
	}
	if n, more, ok := x.ui.CountUpTo(ctx, "orders", "", 1); !ok || !more || n != 1 {
		t.Errorf("CountUpTo(1) = %d, %v, %v", n, more, ok)
	}
	if _, _, ok := x.ui.CountUpTo(ctx, "nope", "", 5); ok {
		t.Errorf("an unknown entity counted")
	}
}
