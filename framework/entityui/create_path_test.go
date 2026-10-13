package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestCreatePathLinks(t *testing.T) {
	x := newInvoiceUI(t)
	custom := x.ui.WithCreatePath(func(e *entity.Entity) (string, bool) {
		return "/forms/" + e.GetName() + "/new", true
	}).WithRecordPath(recPath).WithAPIPath(func(e *entity.Entity) (string, bool) {
		return "/writes/" + e.GetName(), true
	})
	list := listHTML(t, custom.List("invoices").Base("/rec/invoices").Duplicate(), x.userCtx("/rec/invoices", "", "u1"))
	record := string(custom.Record("invoices", "inv-1").Base("/rec/invoices").Duplicate().Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
	related := string(custom.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	empty := listHTML(t, custom.List("payments").Base("/rec/payments"), x.userCtx("/rec/payments", "", "u1"))
	for _, c := range []struct{ name, body, href string }{
		{"list New", list, "/forms/invoices/new"},
		{"list Duplicate", list, "/forms/invoices/new?duplicate=inv-1"},
		{"record Duplicate", record, "/forms/invoices/new?duplicate=inv-1"},
		{"Create another", record, "/forms/invoices/new"},
		{"picker New", record, "/forms/customers/new"},
		{"related Add", related, "/forms/payments/new?prefill_invoice_id=inv-1"},
		{"empty-state New", emptyRegion(t, empty), "/forms/payments/new"},
	} {
		if !strings.Contains(c.body, `href="`+c.href+`"`) {
			t.Errorf("%s missed %s", c.name, c.href)
		}
	}
	if strings.Contains(list+record+related+empty, "/create") {
		t.Error("a create link still uses the default suffix")
	}
	// Cloning the UI leaves its original defaults and write routes intact.
	if body := renderRecord(t, x, "inv-1", func(b *RecordBuilder) { b.Duplicate() }); !strings.Contains(body, `href="/rec/invoices/create"`) {
		t.Error("WithCreatePath changed the original UI")
	}
	if custom.WithCreatePath(nil) != custom {
		t.Error("nil callback should keep the UI unchanged")
	}
}

func TestCreatePathUnavailableHidesLinks(t *testing.T) {
	for _, ok := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_screen", true: "empty_path"}[ok], func(t *testing.T) {
			x := newInvoiceUI(t)
			ui := x.ui.WithRecordPath(recPath).WithCreatePath(func(*entity.Entity) (string, bool) { return "", ok })
			list := listHTML(t, ui.List("invoices").Duplicate(), x.userCtx("/invoices", "", "u1"))
			record := string(ui.Record("invoices", "inv-1").Duplicate().Related("payments").RenderCtx(x.userCtx("/invoices/inv-1", "", "u1")))
			empty := listHTML(t, ui.List("payments"), x.userCtx("/payments", "", "u1"))
			for _, label := range []string{"New Invoice", "New Customer", "New Payment", "Add payment", "Create another", "Duplicate"} {
				if strings.Contains(list+record+empty, label) {
					t.Errorf("unavailable create screen still offers %s", label)
				}
			}
			if !strings.Contains(record, `href="/rec/customers/cus-1"`) {
				t.Error("hiding create links also hid the existing relation's Open link")
			}
		})
	}
}
