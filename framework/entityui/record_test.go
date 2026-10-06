package entityui

import (
	"context"
	"strings"
	"testing"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// The record tests share the list's helper (newTestUI): real entities,
// a migrated SQLite database, real crud handlers, options for API
// paths, translators and the audit seam.

// asUser signs a context in, for renders driven by a hand-built ctx.
func asUser(ctx context.Context, id string) context.Context {
	return handler.SetUser(ctx, &testUser{id: id})
}

// invoiceEntities is the record's fixture world: invoices with states,
// hints and stamps, their customers, and payments pointing back.
func invoiceEntities() map[string]entity.EntityConfig {
	invoices := entity.EntityConfig{
		Table: "invoices",
		Fields: []schema.Field{
			{Name: "number", Type: schema.String, Required: true, Unique: true},
			{Name: "amount", Type: schema.Decimal},
			{Name: "memo", Type: schema.Text},
			{Name: "token", Type: schema.String, NoQuery: true},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "open", "paid"}, Default: "draft"},
			{Name: "issued_on", Type: schema.Date},
			{Name: "customer_id", Type: schema.Relation, To: "customers"},
		},
		Relations: []entity.Relation{entity.BelongsTo("customer", "customers", "customer_id")},
		States: &entity.StatesConfig{
			Field:   "status",
			Initial: []string{"draft"},
			Transitions: []entity.Transition{
				{Key: "send", From: []string{"draft"}, To: "open", Stamp: "issued_on"},
				{Key: "mark_paid", From: []string{"open"}, To: "paid", Variant: "primary"},
				{Key: "sweep", From: []string{"draft"}, To: "paid", System: true},
			},
		},
		Display: &entity.DisplayConfig{
			Singular:   "Invoice",
			Plural:     "Invoices",
			TitleField: "number",
			Fields: map[string]entity.FieldDisplay{
				"amount": {Locked: true},
				"memo":   {Help: "Shown to the customer"},
			},
		},
	}
	payments := entity.EntityConfig{
		Table: "payments",
		Fields: []schema.Field{
			{Name: "invoice_id", Type: schema.Relation, To: "invoices"},
			{Name: "amount", Type: schema.Decimal},
		},
		Relations: []entity.Relation{entity.BelongsTo("invoice", "invoices", "invoice_id")},
	}
	customers := entity.EntityConfig{
		Table:  "customers",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}
	return map[string]entity.EntityConfig{
		"customers": customers.WithTimestamps(false),
		"payments":  payments.WithTimestamps(false),
		"invoices":  invoices.WithTimestamps(false),
	}
}

// invoiceRows seeds one invoice (INV-1, draft), one customer.
func invoiceRows() map[string][]map[string]any {
	return map[string][]map[string]any{
		"customers": {{"id": "cus-1", "name": "Acme"}},
		"invoices": {{"id": "inv-1", "number": "INV-1", "amount": "120.00", "memo": "first note",
			"token": "tok-secret", "status": "draft", "customer_id": "cus-1"}},
	}
}

// newInvoiceUI is the fixture world with write routes mounted.
func newInvoiceUI(t *testing.T, opts ...testUIOption) *testUI {
	t.Helper()
	return newTestUI(t, invoiceEntities(), invoiceRows(),
		append([]testUIOption{withAPI(map[string]string{
			"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
		})}, opts...)...)
}

// renderRecord renders the invoice record at /rec/invoices/<id> as u1.
func renderRecord(t *testing.T, x *testUI, id string, tune func(*RecordBuilder)) string {
	t.Helper()
	b := x.ui.Record("invoices", id).Base("/rec/invoices")
	if tune != nil {
		tune(b)
	}
	return string(b.RenderCtx(x.userCtx("/rec/invoices/"+id, "", "u1")))
}

// The default posture is secure: an entity with no owner scope, no
// access config and no Public flag requires a session, and the record
// answers the same refusal the JSON API would — no rows, no schema.
func TestRecordAnonymousRefusedDefaultPosture(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.ctx("/rec/invoices/inv-1", "")))
	if !strings.Contains(body, AccessDeniedTitle) {
		t.Fatalf("an anonymous caller must see the refusal, got:\n%s", body)
	}
	if strings.Contains(body, "INV-1") {
		t.Fatalf("SECURITY: the refused record leaked its title:\n%s", body)
	}
}

// Another owner's id and a missing id answer the SAME not-found body:
// which of the two it was never shows.
func TestRecordOtherOwnerNotFoundIdentical(t *testing.T) {
	installOwnerExtractor(t)
	entities := invoiceEntities()
	cfg := entities["invoices"]
	cfg.Scope = &entity.ScopeConfig{OwnerField: "user_id"}
	cfg.Fields = append(cfg.Fields, schema.Field{Name: "user_id", Type: schema.String, Hidden: true})
	entities["invoices"] = cfg
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))

	render := func(rid string) string {
		return string(x.ui.Record("invoices", rid).Base("/rec/invoices").
			RenderCtx(x.userCtx("/rec/invoices/x", "", "u2")))
	}
	other := render("inv-1")
	missing := render("no-such-id")
	if other != missing {
		t.Fatalf("another owner's id and a missing id differ:\nother:\n%s\nmissing:\n%s", other, missing)
	}
	if !strings.Contains(other, i18nui.Defaults[i18nui.KeyEntityNotFound]) {
		t.Fatalf("both must render not-found, got:\n%s", other)
	}
	if strings.Contains(other, "INV-1") {
		t.Fatalf("SECURITY: another owner's record leaked:\n%s", other)
	}
}

// The form's values split by destination: inputs prefill from the
// UNHOOKED read (they round-trip; a mask written back would replace
// the stored column), read-only values show the hooked read. A hook
// that rewrites a column also makes it write-only on the form, so the
// rewritten editable column renders blank while the locked column
// beside it shows the hook's value.
func TestRecordEditPrefillSkipsReadHooks(t *testing.T) {
	x := newInvoiceUI(t)
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil {
			p.Result["memo"] = "MASKED"
			p.Result["amount"] = "999.99"
		}
		return nil
	})
	body := renderRecord(t, x, "inv-1", nil)

	// number is untouched by the hook: its input prefills from the raw
	// row, so an unrelated edit cannot rewrite it.
	if v := attrValue(body, "number", "value"); v != "INV-1" {
		t.Fatalf("the untouched field prefills from the unhooked read, got %q:\n%s", v, body)
	}
	// memo is rewritten by the hook: write-only, never the mask.
	if strings.Contains(body, "MASKED") {
		t.Fatalf("SECURITY: the hook's mask reached the page:\n%s", body)
	}
	if v := attrValue(body, "memo", "value"); v != "" {
		t.Fatalf("a rewritten column renders a blank input, got %q", v)
	}
	// amount is Locked (a detail value): it shows the HOOKED read.
	if !strings.Contains(body, "999.99") {
		t.Fatalf("the locked field shows the hooked read's value:\n%s", body)
	}
	if strings.Contains(body, "120.00") {
		t.Fatalf("the locked field must not show the raw stored value:\n%s", body)
	}
}

// A Locked field, the state field and a stamp render no submittable
// control: no input, select or textarea carries their name, so nothing
// about them is ever submitted.
func TestRecordLockedStateStampSubmitNothing(t *testing.T) {
	x := newInvoiceUI(t)
	body := renderRecord(t, x, "inv-1", nil)
	for _, name := range []string{"amount", "status", "issued_on", "id"} {
		if hasSubmittableControl(body, name) {
			t.Fatalf("field %q must render no submittable control:\n%s", name, body)
		}
	}
	if !strings.Contains(body, "120.00") {
		t.Fatalf("the locked amount renders its value:\n%s", body)
	}
}

// hasSubmittableControl reports a control named name: an input, select
// or textarea carrying that name.
func hasSubmittableControl(body, name string) bool {
	probe := `name="` + name + `"`
	if !strings.Contains(body, probe) {
		return false
	}
	for _, tag := range []string{"<input", "<select", "<textarea"} {
		i := 0
		for {
			j := strings.Index(body[i:], tag)
			if j < 0 {
				break
			}
			seg := body[i+j:]
			k := strings.Index(seg, ">")
			if k < 0 {
				break
			}
			if strings.Contains(seg[:k], probe) {
				return true
			}
			i += j + len(tag)
		}
	}
	return false
}

// A masked field never renders its value: a hook rewrites it, the form
// draws a blank input with the Set/Not set hint, and the stored value
// appears nowhere.
func TestRecordMaskedFieldBlankInput(t *testing.T) {
	x := newInvoiceUI(t)
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil {
			p.Result["token"] = "****"
		}
		return nil
	})
	body := renderRecord(t, x, "inv-1", nil)
	if strings.Contains(body, "tok-secret") {
		t.Fatalf("SECURITY: the masked field rendered its stored value:\n%s", body)
	}
	if !strings.Contains(body, `name="token"`) {
		t.Fatalf("the masked field keeps a blank input:\n%s", body)
	}
	if v := attrValue(body, "token", "value"); v != "" {
		t.Fatalf("the masked field's input must be blank, got value=%q", v)
	}
	if !strings.Contains(body, i18nui.Defaults[i18nui.KeyEntitySet]) {
		t.Fatalf("a set masked field says so:\n%s", body)
	}
}

// attrValue reads value="..." off the control named name.
func attrValue(body, name, attr string) string {
	for _, tag := range []string{"<input", "<textarea"} {
		i := 0
		for {
			j := strings.Index(body[i:], tag)
			if j < 0 {
				break
			}
			seg := body[i+j:]
			k := strings.Index(seg, ">")
			if k < 0 {
				break
			}
			open := seg[:k]
			if strings.Contains(open, `name="`+name+`"`) {
				m := `value="`
				if v := strings.Index(open, m); v >= 0 {
					rest := open[v+len(m):]
					if e := strings.Index(rest, `"`); e >= 0 {
						return rest[:e]
					}
				}
				return ""
			}
			i += j + len(tag)
		}
	}
	return ""
}

func mustEntity(t *testing.T, x *testUI, name string) *entity.Entity {
	t.Helper()
	e, err := x.host.reg.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

var _ = crud.CaseSnake
var _ = appui.RequestFromContext

// A resource-aware Decider may allow the entity and refuse one row:
// that row answers the same not-found a missing id does, byte for
// byte. The scoped GetOne alone cannot produce this refusal — it has
// no permission gate — so the per-record check is load-bearing.
func TestRecordDeciderDeniedRowNotFoundIdentical(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read"}}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))

	policy := access.NewRolePolicy()
	if err := policy.Grant("reader", access.Permission("invoices:read")); err != nil {
		t.Fatal(err)
	}
	base := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"reader"})
	denied := access.WithDecider(base, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.Type == "invoices" && ref.ID == "inv-1" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})

	render := func(id string) string {
		return string(x.ui.Record("invoices", id).Base("/rec/invoices").RenderCtx(denied))
	}
	refused := render("inv-1")
	missing := render("no-such-id")
	if refused != missing {
		t.Fatalf("a denied row and a missing id differ:\ndenied:\n%s\nmissing:\n%s", refused, missing)
	}
	if strings.Contains(refused, "INV-1") {
		t.Fatalf("SECURITY: the denied row leaked:\n%s", refused)
	}
}
