package entityui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A duplicate whose hooked read fails, or returns no row, prefills
// nothing: the masked values the hook would hide never reach the form.
func TestDuplicateFailsClosedOnHook(t *testing.T) {
	for name, h := range map[string]hook.HookFunc{
		"error": func(context.Context, any) error { return errors.New("redactor down") },
		"nil": func(_ context.Context, data any) error {
			data.(*hook.GetPayload).Result = nil
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			x := newInvoiceUI(t)
			ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
			if err != nil {
				t.Fatal(err)
			}
			ch.Hooks = hook.NewHookRegistry()
			ch.Hooks.RegisterHook(hook.AfterGet, h)
			body := string(x.ui.Create("invoices").Base("/rec/invoices").
				RenderCtx(x.userCtx("/rec/invoices/create", "?duplicate=inv-1", "u1")))
			for _, leak := range []string{"tok-secret", "first note"} {
				if strings.Contains(body, leak) {
					t.Fatalf("SECURITY: the duplicate copied %q past a failed hooked read:\n%s", leak, body)
				}
			}
		})
	}
}

// A record the per-record gate denies runs no read: its AfterGet hook
// never fires.
func TestRecordDeniedRunsNoHook(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read"}}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(context.Context, any) error { ran++; return nil })

	policy := access.NewRolePolicy()
	if err := policy.Grant("reader", access.Permission("invoices:read")); err != nil {
		t.Fatal(err)
	}
	base := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"reader"})
	denied := access.WithDecider(base, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "inv-1" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})
	x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(denied)
	if ran != 0 {
		t.Fatalf("the AfterGet hook ran %d times for a denied record", ran)
	}
}

// A BeforeList scope narrows the list and a count stat exactly as it
// narrows GET /api/invoices.
func TestListHonorsBeforeListScope(t *testing.T) {
	x := newInvoiceUI(t)
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("status = $1", "paid")
		return nil
	})
	ctx := x.userCtx("/rec/invoices", "", "u1")
	if body := string(x.ui.List("invoices").Base("/rec/invoices").RenderCtx(ctx)); strings.Contains(body, "INV-1") {
		t.Fatalf("SECURITY: the list showed a row its BeforeList scope hides:\n%s", body)
	}
	if got := x.ui.StatValue(ctx, "invoices", "count", "", "", ""); got != "0" {
		t.Fatalf("SECURITY: the count stat = %q past the BeforeList scope, want 0", got)
	}
}
