package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A float cell is plain decimal, never Go's exponent form, so a
// million reaches the locale formatter as digits it accepts.
func TestFloatCellIsPlainDecimal(t *testing.T) {
	if got := cell(1e6); got != "1000000" {
		t.Errorf("cell(1e6) = %q, want 1000000", got)
	}
	if got := cell(float32(0.5)); got != "0.5" {
		t.Errorf("cell(float32 0.5) = %q, want 0.5", got)
	}
	en := i18n.WithContext(context.Background(), i18n.Locale{Tag: "en"})
	if got := decimal(en, schema.Field{Type: schema.Float}, cell(1e6)); got != "1,000,000.00" {
		t.Errorf("decimal of a million = %q, want 1,000,000.00", got)
	}
}

// An int cell takes the locale's digits and no grouping: a year stays
// one run.
func TestIntCellUsesLocaleDigits(t *testing.T) {
	ar := i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	if got := integer(ar, "2026"); got != "٢٠٢٦" {
		t.Errorf("ar-EG: got %q want ٢٠٢٦", got)
	}
	if got := integer(nil, "2026"); got != "2026" {
		t.Errorf("English: got %q want 2026", got)
	}
	if got := integer(ar, "not a number"); got != "not a number" {
		t.Errorf("non-integer: got %q, want it as stored", got)
	}
}

// A Decimal or Float shows the column's fraction digits: four for the
// default DECIMAL(19,4), the RawType's scale when it names one, and at
// least two.
func TestDecimalShowsColumnScale(t *testing.T) {
	en := context.Background()
	plain := schema.Field{Name: "amount", Type: schema.Decimal}
	cents := schema.Field{Name: "amount", Type: schema.Decimal, RawType: "numeric(10, 2)"}
	whole := schema.Field{Name: "amount", Type: schema.Decimal, RawType: "DECIMAL(12,0)"}
	ratio := schema.Field{Name: "ratio", Type: schema.Float}
	for _, tc := range []struct {
		f        schema.Field
		in, want string
	}{
		{plain, "0.0049", "0.0049"},
		{plain, "10.5", "10.50"},
		{plain, "1.23456", "1.2346"},
		{cents, "0.0049", "0.00"},
		{cents, "19.5", "19.50"},
		{whole, "7.6", "8"},
		{ratio, "0.125", "0.125"},
	} {
		if got := decimal(en, tc.f, tc.in); got != tc.want {
			t.Errorf("decimal(%s %q) = %q, want %q", tc.f.RawType, tc.in, got, tc.want)
		}
	}
}

// The list and the read-only record show a small decimal, not 0.00.
func TestListShowsFourPlaceDecimal(t *testing.T) {
	rows := ordersRows()
	rows[0]["amount"] = "0.0049"
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": rows},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if !strings.Contains(html, "0.0049") {
		t.Errorf("list rounded 0.0049 away:\n%s", html)
	}
	inv := invoiceRows()
	inv["invoices"][0]["amount"] = "0.0049" // amount is Locked: read-only
	y := newTestUI(t, invoiceEntities(), inv, withAPI(map[string]string{"invoices": "/api/invoices"}))
	if body := renderRecord(t, y, "inv-1", nil); !strings.Contains(body, "0.0049") {
		t.Errorf("record rounded 0.0049 away:\n%s", body)
	}
}
