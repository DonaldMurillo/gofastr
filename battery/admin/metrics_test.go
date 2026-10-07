package admin

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func ordersConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "orders",
		Fields: []schema.Field{
			{Name: "ref", Type: schema.String, Required: true},
			{Name: "amount", Type: schema.Decimal},
			{Name: "status", Type: schema.Enum, Values: []string{"open", "late", "paid"}, Default: "open"},
		},
		Display: &entity.DisplayConfig{Views: []entity.ListView{{Key: "late", Label: "Late", Where: `status = "late"`}}},
	}.WithTimestamps(false)
}

func ordersEnv(t *testing.T, cfg Config) *env {
	t.Helper()
	x := setup(t, map[string]entity.EntityConfig{"orders": ordersConfig()},
		cfg, nil)
	for _, row := range [][3]string{{"a", "10.50", "open"}, {"b", "20.00", "late"}, {"c", "5.25", "late"}, {"d", "99", "paid"}} {
		if _, err := x.db.Exec(`INSERT INTO orders (id, ref, amount, status) VALUES (?, ?, ?, ?)`, row[0], row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	return x
}

// The strip draws each figure, its detail and its link to the view,
// above the entity cards, and polls each figure on its own route.
func TestMetricsStripDrawsAndPolls(t *testing.T) {
	x := ordersEnv(t, Config{Entities: []string{"orders"}, Metrics: []Metric{
		{Label: "Open orders", Entity: "orders", Where: `status = "open"`},
		{Label: "Late", Entity: "orders", Where: `status = "late"`, View: "late", Icon: "clock",
			Detail: &Metric{Label: "owed", Agg: "sum", Field: "amount", Where: `status = "late"`, Format: "money"}},
	}})
	dash := get(x.as(theAdmin), "/admin").Body.String()
	for _, want := range []string{
		"Open orders", "Late", "$25.25 owed",
		`href="/admin/entities/orders?view=late"`,
		`data-cui-poll-src="/admin/_metric/0"`, `data-cui-poll-src="/admin/_metric/1"`,
	} {
		if !strings.Contains(dash, want) {
			t.Errorf("the dashboard lacks %q:\n%s", want, dash)
		}
	}
	if i, j := strings.Index(dash, "Open orders"), strings.Index(dash, `/admin/_count/orders`); i < 0 || j < 0 || i > j {
		t.Errorf("the strip must sit above the entity cards")
	}
	body := get(x.as(theAdmin), "/admin/_metric/1").Body.String()
	if !strings.Contains(body, ">2<") || !strings.Contains(body, "$25.25 owed") {
		t.Errorf("polled metric = %q", body)
	}
	if rr := get(x.as(theAdmin), "/admin/_metric/2"); rr.Code != http.StatusNotFound {
		t.Errorf("a metric past the list answered %d, want 404", rr.Code)
	}
	if rr := get(x.as(aReader), "/admin/_metric/0"); rr.Code != http.StatusForbidden {
		t.Errorf("SECURITY: a reader read a metric: %d", rr.Code)
	}
}

// Every metric that could only ever draw "—" fails the boot.
func TestMetricsRefuseWhatCannotCompute(t *testing.T) {
	for name, m := range map[string]Metric{
		"no label":           {Entity: "orders"},
		"unexposed entity":   {Label: "x", Entity: "ghosts"},
		"unknown agg":        {Label: "x", Entity: "orders", Agg: "avg"},
		"sum of text":        {Label: "x", Entity: "orders", Agg: "sum", Field: "ref"},
		"bad filter":         {Label: "x", Entity: "orders", Where: `nope = 1`},
		"unknown view":       {Label: "x", Entity: "orders", View: "overdue"},
		"bad icon":           {Label: "x", Entity: "orders", Icon: "no-such-icon"},
		"detail no label":    {Label: "x", Entity: "orders", Detail: &Metric{}},
		"detail bad field":   {Label: "x", Entity: "orders", Detail: &Metric{Label: "y", Agg: "sum", Field: "nope"}},
		"detail has a view":  {Label: "x", Entity: "orders", Detail: &Metric{Label: "y", View: "late"}},
		"detail has a child": {Label: "x", Entity: "orders", Detail: &Metric{Label: "y", Detail: &Metric{Label: "z"}}},
	} {
		_, _, err := trySetup(t, map[string]entity.EntityConfig{"orders": ordersConfig()},
			Config{Entities: []string{"orders"}, Metrics: []Metric{m}}, nil)
		if err == nil {
			t.Errorf("%s: the metric was accepted", name)
		}
	}
}

// Needs attention previews each watched view that has rows, linking to
// the full view, and says so when none has any.
func TestAttentionPreviewsWatchedViews(t *testing.T) {
	x := ordersEnv(t, Config{Entities: []string{"orders"}, Attention: []Watch{{Entity: "orders", View: "late", Columns: []string{"ref", "amount"}}}})
	dash := get(x.as(theAdmin), "/admin").Body.String()
	for _, want := range []string{"Needs attention", `href="/admin/entities/orders?view=late"`, `href="/admin/entities/orders/b"`, `href="/admin/entities/orders/c"`} {
		if !strings.Contains(dash, want) {
			t.Errorf("the dashboard lacks %q:\n%s", want, dash)
		}
	}
	if strings.Contains(dash, `href="/admin/entities/orders/a"`) {
		t.Errorf("the panel listed a row outside the view")
	}
	if _, err := x.db.Exec(`UPDATE orders SET status = 'paid'`); err != nil {
		t.Fatal(err)
	}
	dash = get(x.as(theAdmin), "/admin").Body.String()
	if !strings.Contains(dash, "Nothing needs attention.") || strings.Contains(dash, `href="/admin/entities/orders?view=late"`) {
		t.Errorf("an empty watch still drew, or the panel did not say it is clear:\n%s", dash)
	}
	for name, w := range map[string]Watch{
		"unexposed":      {Entity: "ghosts", View: "late"},
		"no view":        {Entity: "orders"},
		"unknown view":   {Entity: "orders", View: "overdue"},
		"unknown column": {Entity: "orders", View: "late", Columns: []string{"nope"}},
		"negative rows":  {Entity: "orders", View: "late", Rows: -1},
		"too many rows":  {Entity: "orders", View: "late", Rows: 21},
	} {
		_, _, err := trySetup(t, map[string]entity.EntityConfig{"orders": ordersConfig()},
			Config{Entities: []string{"orders"}, Attention: []Watch{w}}, nil)
		if err == nil {
			t.Errorf("%s: the watch was accepted", name)
		}
	}
}
