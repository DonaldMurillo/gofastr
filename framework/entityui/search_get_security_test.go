package entityui

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestSearchAndPickerHonorGetHooks(t *testing.T) {
	for _, mode := range []string{"search fields", "title fallback"} {
		t.Run(mode, func(t *testing.T) {
			ents := searchableInvoices()
			if mode == "title fallback" {
				customers := ents["customers"]
				customers.SearchFields = nil
				ents["customers"] = customers
			}
			rows := invoiceRows()
			rows["customers"] = append(rows["customers"], map[string]any{"id": "cus-2", "name": "Acme Support"})
			x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
			ch, err := x.host.Crud(mustEntity(t, x, "customers"))
			if err != nil {
				t.Fatal(err)
			}
			ch.Hooks = hook.NewHookRegistry()
			ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
				data.(*hook.GetPayload).AddWhere("id <> $1", "cus-1")
				return nil
			})
			ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
				data.(*hook.GetPayload).Result["name"] = "Acme (masked)"
				return nil
			})
			ctx := x.userCtx("/customers", "", "u1")
			if _, ok := x.ui.RecordTitle(ctx, "customers", "cus-1"); ok {
				t.Fatal("record title exposed the hidden customer")
			}
			title, ok := x.ui.RecordTitle(ctx, "customers", "cus-2")
			if !ok || title != "Acme (masked)" {
				t.Fatalf("RecordTitle(cus-2) = %q, %t", title, ok)
			}
			if mode == "search fields" {
				got := x.ui.SearchRecords(ctx, "customers", "Acme", 1)
				want := []RecordMatch{{ID: "cus-2", Title: title}}
				if !slices.Equal(got, want) {
					t.Errorf("SearchRecords = %+v, want %+v", got, want)
				}
			}
			for _, query := range []string{`{"q":""}`, `{"q":"Acme"}`} {
				rec := pickPost(t, x, "customer_id", query, nil)
				body := rec.Body.String()
				if rec.Code != http.StatusOK || strings.Contains(body, `data-value="cus-1"`) ||
					!strings.Contains(body, `data-value="cus-2"`) || !strings.Contains(body, `data-label="Acme (masked)"`) {
					t.Errorf("picker %s: status %d, want only masked cus-2: %s", query, rec.Code, body)
				}
			}
		})
	}
}
