package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func TestEntityListRejectsRowCountChange(t *testing.T) {
	for _, change := range []string{"drop", "append"} {
		t.Run(change, func(t *testing.T) {
			fixture := newInvoiceUI(t)
			handler, err := fixture.host.Crud(mustEntity(t, fixture, "invoices"))
			if err != nil {
				t.Fatal(err)
			}
			handler.Hooks = hook.NewHookRegistry()
			var calls int
			handler.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
				calls++
				payload := data.(*hook.ListPayload)
				if change == "append" {
					payload.Results = append(payload.Results, payload.Results[0])
				} else {
					payload.Results = nil
				}
				return nil
			})
			ctx := fixture.userCtx("/invoices", "", "u1")
			body := listHTML(t, fixture.ui.List("invoices"), ctx)
			if calls != 1 || !strings.Contains(body, i18nui.EntitySlotFailedBody.Text(ctx)) {
				t.Fatalf("list accepted %s results instead of showing its load refusal (hook calls %d)", change, calls)
			}
		})
	}
}
