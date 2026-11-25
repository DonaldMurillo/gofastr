package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The declared layout places fields: rows in a grid, sections under
// their heading, a collapsed section inside a Collapsible, and a field
// the form leaves out appends at the end of Main in schema order. An
// Omit field and a Display-omitted field never render.
func TestRecordFormLayoutPlacesFields(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Display.Form = &entity.EntityForm{
		Main: []entity.FormItem{
			{Row: []string{"number", "memo"}},
			{Section: "dates", Items: []entity.FormItem{{Field: "issued_on"}}},
			{Section: "notes", Collapsed: true, Items: []entity.FormItem{{Field: "token"}}},
		},
		Side: []entity.FormItem{{Field: "customer_id"}},
	}
	inv.Display.Fields["amount"] = entity.FieldDisplay{Omit: true}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))

	// A row is a grid over its fields; a section a heading over its
	// body; a collapsed section a Collapsible.
	if !strings.Contains(body, ">Dates<") || !strings.Contains(body, ">Notes<") {
		t.Fatalf("sections render their headings:\n%s", body)
	}
	if !strings.Contains(body, `<details class="fui-collapsible"`) {
		t.Fatalf("the collapsed section renders a Collapsible:\n%s", body)
	}
	// amount is Omit: absent from the form and its value.
	if strings.Contains(body, "120.00") || strings.Contains(body, "amount") {
		t.Fatalf("an Omit field renders nowhere:\n%s", body)
	}
	// customer_id sits in the Side rail; the unplaced leftover (none
	// here: every editable field is placed or omitted) must not
	// duplicate.
	if n := strings.Count(body, `name="customer_id"`); n != 1 {
		t.Fatalf("customer_id renders once, got %d:\n%s", n, body)
	}
}

// A field the form leaves out appends at the end of Main, so adding a
// field to the entity never makes it vanish from the record.
func TestRecordFormLeftoverAppends(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Display.Form = &entity.EntityForm{
		Main: []entity.FormItem{{Field: "number"}},
	}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))

	for _, name := range []string{"memo", "customer_id", "token"} {
		if !strings.Contains(body, `name="`+name+`"`) {
			t.Fatalf("the leftover field %s appends to Main:\n%s", name, body)
		}
	}
}

// A Row item following plain Field items keeps them: the row's cells
// append to their own slice. Routing the append through out's backing
// array overwrote the fields already placed, so every field before a
// row vanished while the row's own fields rendered twice.
func TestRecordFormRowKeepsPriorFields(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Display.Form = &entity.EntityForm{
		Main: []entity.FormItem{
			{Field: "number"},
			{Row: []string{"memo", "token"}},
		},
	}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))

	for _, name := range []string{"number", "memo", "token"} {
		if n := strings.Count(body, `name="`+name+`"`); n != 1 {
			t.Fatalf("field %s renders once, got %d:\n%s", name, n, body)
		}
	}
}

// ShowWhen wraps the field in the conditional region the when module
// evaluates; on the state field the server decides against the stored
// value, so the field is drawn or omitted, never wrapped.
func TestRecordFormShowWhen(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Display.Fields["memo"] = entity.FieldDisplay{ShowWhen: `customer_id != ""`}
	// An invalid ShowWhen shape would have been refused at Entity
	// registration; the entity here is defined directly, so the record
	// tolerates it by rendering unconditioned (never vanishing).
	inv.Display.Fields["token"] = entity.FieldDisplay{ShowWhen: `status = "paid"`}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))

	// The state-field condition is decided against the stored draft:
	// token (condition status = "paid") draws nothing.
	if strings.Contains(body, `name="token"`) {
		t.Fatalf("a state-field condition that does not hold omits the field:\n%s", body)
	}
}

func TestCreateShowWhenUsesStateDefault(t *testing.T) {
	for _, tc := range []struct {
		name         string
		defaultValue any
		showsMemo    bool
	}{
		{"first initial state", nil, true},
		{"explicit default", "open", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ents := invoiceEntities()
			inv := ents["invoices"]
			inv.States.Initial = []string{"draft", "open"}
			for i, f := range inv.Fields {
				if f.Name == "status" {
					inv.Fields[i].Default = tc.defaultValue
				}
			}
			inv.Display.Fields["memo"] = entity.FieldDisplay{ShowWhen: `status = "draft"`}
			inv.Display.Fields["token"] = entity.FieldDisplay{ShowWhen: `status = "open"`}
			ents["invoices"] = inv
			x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
			create := string(x.ui.Create("invoices").Base("/invoices").RenderCtx(x.userCtx("/invoices/create", "?prefill_status=paid", "u1")))
			if memo, token := strings.Contains(create, `name="memo"`), strings.Contains(create, `name="token"`); memo != tc.showsMemo || token == tc.showsMemo {
				t.Errorf("create memo=%t token=%t, want memo=%t token=%t", memo, token, tc.showsMemo, !tc.showsMemo)
			}
			if strings.Contains(create, `name="status"`) {
				t.Error("create renders an editable state control")
			}
			edit := renderRecord(t, x, "inv-1", nil)
			if !strings.Contains(edit, `name="memo"`) || strings.Contains(edit, `name="token"`) {
				t.Error("edit did not use the stored draft state")
			}
		})
	}
}

// A builder-supplied form that names an unknown field, repeats one, or
// overfills a row fails the slot with the generic message.
func TestRecordFormBadBuilderFormFailsSlot(t *testing.T) {
	x := newInvoiceUI(t)
	cases := []struct {
		name string
		form *entity.EntityForm
	}{
		{"unknown", &entity.EntityForm{Main: []entity.FormItem{{Field: "ghost"}}}},
		{"repeat", &entity.EntityForm{Main: []entity.FormItem{{Field: "number"}, {Field: "number"}}}},
		{"wide row", &entity.EntityForm{Main: []entity.FormItem{{Row: []string{"number", "memo", "token", "status"}}}}},
	}
	for _, c := range cases {
		body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Form(c.form).
			RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
		want := strings.ReplaceAll("Couldn't load this section", "'", "&#39;")
		if !strings.Contains(body, want) {
			t.Fatalf("%s: the slot fails generically:\n%s", c.name, body)
		}
	}
}

// A builder Omit keeps the field out of the form whether the layout
// places it or the unplaced-field fallback would append it.
func TestRecordOmitKeepsFieldOutOfForm(t *testing.T) {
	for _, layout := range []string{"fallback", "placed"} {
		t.Run(layout, func(t *testing.T) {
			entities := invoiceEntities()
			if layout == "placed" {
				inv := entities["invoices"]
				inv.Display.Form = &entity.EntityForm{Main: []entity.FormItem{{Field: "memo"}}}
				entities["invoices"] = inv
			}
			x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
			body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Omit("memo").
				RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
			if strings.Contains(body, `name="memo"`) {
				t.Fatal("Omit(memo) left a memo control in the form")
			}
		})
	}
}
