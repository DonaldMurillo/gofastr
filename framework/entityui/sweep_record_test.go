package entityui

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/internal/fieldtext"
	"github.com/DonaldMurillo/gofastr/internal/teststorage"
)

func TestSweepRecordDuplicateLeavesMaskedFieldBlank(t *testing.T) {
	x := newInvoiceUI(t)
	maskInvoiceField(t, x, "memo")
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "?duplicate=inv-1", "u1")))
	if strings.Contains(body, "first note") {
		t.Fatalf("SECURITY: the duplicate copied a field the read hook masks:\n%s", body)
	}
	if !strings.Contains(body, `value="cus-1"`) {
		t.Fatalf("the duplicate dropped an unmasked field:\n%s", body)
	}
}

func TestSweepRecordDuplicateBehindRecordGate(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read", Create: "invoices:create"}}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	policy := access.NewRolePolicy()
	for _, p := range []string{"invoices:read", "invoices:create"} {
		if err := policy.Grant("clerk", access.Permission(p)); err != nil {
			t.Fatal(err)
		}
	}
	ctx := x.userCtx("/rec/invoices/create", "?duplicate=inv-1", "u1")
	ctx = access.WithRoles(access.WithPolicy(ctx, policy), []string{"clerk"})
	open := string(x.ui.Create("invoices").Base("/rec/invoices").RenderCtx(ctx))
	if !strings.Contains(open, "first note") {
		t.Fatalf("a readable duplicate did not prefill:\n%s", open)
	}
	denied := access.WithDecider(ctx, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "inv-1" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})
	body := string(x.ui.Create("invoices").Base("/rec/invoices").RenderCtx(denied))
	if strings.Contains(body, "first note") {
		t.Fatalf("SECURITY: a duplicate of a record the gate denies copied its values:\n%s", body)
	}
}

func TestSweepRecordKindInputIsNotElevated(t *testing.T) {
	var xu *UI
	ran, titled := false, true
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read", Update: "invoices:update"}}
	inv.Display.Fields["memo"] = entity.FieldDisplay{Input: "peek"}
	ents["invoices"] = inv
	x := newTestUIExt(t, ents, invoiceRows(), Extensions{Kinds: map[string]Kind{"peek": {Input: func(ic InputContext) render.HTML {
		ran = true
		_, titled = xu.RecordTitle(ic.Ctx, "invoices", "inv-1")
		return ""
	}}}}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	xu = x.ui
	policy := access.NewRolePolicy()
	policy.Register("invoices:read", "invoices:update")
	x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(elevatedClerk(x, policy, "/rec/invoices/inv-1", ""))
	if !ran {
		t.Fatal("setup: the elevated record never drew the kind input")
	}
	if titled {
		t.Fatal("SECURITY: a kind's Input ran with the back office's elevation")
	}
}

func TestSweepRecordDateInputValueIsDateOnly(t *testing.T) {
	d := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	f := schema.Field{Name: "issued_on", Type: schema.Date}
	if got := fieldtext.Form(f, d); got != "2026-03-04" {
		t.Fatalf("fieldtext.Form(Date, time.Time) = %q, want 2026-03-04", got)
	}
	if got := fieldtext.Form(f, &d); got != "2026-03-04" {
		t.Fatalf("fieldtext.Form(Date, *time.Time) = %q, want 2026-03-04", got)
	}
}

func TestSweepRecordActivityHidesFieldMaskedOnlyOnThisRecord(t *testing.T) {
	x := newInvoiceUI(t, withAudit(fakeAudit{entries: []AuditEntry{{
		At: time.Now(), Actor: "u1", Operation: "update",
		Before: map[string]any{"memo": "old-secret-memo"},
		After:  map[string]any{"memo": "new-secret-memo"},
	}}}))
	memoMaskedWhenUndated(t, x)
	if body := activityBody(t, x); strings.Contains(body, "secret-memo") {
		t.Fatalf("SECURITY: the Activity tab drew a value the hook masks on this record:\n%s", body)
	}
}

func TestSweepRecordDuplicateRefusedOnFailedHookedRead(t *testing.T) {
	x := newInvoiceUI(t)
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(context.Context, any) error { return errors.New("redactor down") })
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "?duplicate=inv-1", "u1")))
	if !strings.Contains(body, "Not found") || strings.Contains(body, "<form") {
		t.Fatalf("a duplicate whose hooked read failed drew a create form instead of not-found:\n%s", body)
	}
}

func TestSweepRecordMoveNeedsItsExactPermission(t *testing.T) {
	x, _, policy := guardedInvoices(t, entity.AccessControl{Update: "invoices:update"}, 0, Extensions{},
		func(ents map[string]entity.EntityConfig, _ map[string][]map[string]any) {
			inv := ents["invoices"]
			st := *inv.States
			st.Transitions = append([]entity.Transition(nil), st.Transitions...)
			st.Transitions[0].Permission = "invoices:send"
			inv.States = &st
			ents["invoices"] = inv
		})
	grant(t, policy, "writer", "invoices:update")
	grant(t, policy, "sender", "invoices:update", "invoices:send")
	grant(t, policy, "root", access.Wildcard)
	draw := func(role string) string {
		return string(x.ui.Record("invoices", "inv-1").RenderCtx(pageCtx("/invoices/inv-1", policy, "u1", role)))
	}
	for _, role := range []string{"writer", "root"} {
		if strings.Contains(draw(role), "/transitions/send") {
			t.Errorf("SECURITY: role %s without invoices:send sees the send move", role)
		}
	}
	if !strings.Contains(draw("sender"), "/transitions/send") {
		t.Error("a caller holding invoices:send does not see the send move")
	}
}

func TestSweepRecordHeaderSaveNeedsUpdate(t *testing.T) {
	x, _, policy := guardedInvoices(t, entity.AccessControl{Update: "invoices:update"}, 0, Extensions{}, nil)
	grant(t, policy, "writer", "invoices:update")
	policy.Register("viewer")
	draw := func(roles ...string) string {
		return string(x.ui.Record("invoices", "inv-1").RenderCtx(pageCtx("/invoices/inv-1", policy, "u1", roles...)))
	}
	if strings.Contains(draw("viewer"), `id="eui-invoices-save"`) {
		t.Error("a caller who may not update the record sees Save")
	}
	if !strings.Contains(draw("writer"), `id="eui-invoices-save"`) {
		t.Error("a writer does not see Save")
	}
}

func memoMaskedWhenUndated(t *testing.T, x *testUI) {
	t.Helper()
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil &&
			fieldtext.Cell(p.Result["issued_on"]) == "" && fieldtext.Cell(p.Result["issuedOn"]) == "" {
			p.Result["memo"] = "****"
		}
		return nil
	})
}

func TestSweepRecordChangesHideFieldMaskedOnlyOnThisRecord(t *testing.T) {
	x := newInvoiceUI(t)
	memoMaskedWhenUndated(t, x)
	before := map[string]any{"id": "inv-1", "memo": "old-secret-memo", "amount": "1"}
	after := map[string]any{"id": "inv-1", "memo": "new-secret-memo", "amount": "2"}
	if body := string(x.ui.Changes(x.userCtx("/admin/audit", "", "u1"), "invoices", before, after)); strings.Contains(body, "secret-memo") {
		t.Fatalf("SECURITY: the change list drew a value the hook masks on this record:\n%s", body)
	}
}

func TestSweepRecordHookProbeRunsAsCaller(t *testing.T) {
	x, policy := readGatedInvoices(t, Extensions{})
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		if p, ok := data.(*hook.GetPayload); ok && p.Result != nil && !ch.CanReadRecordScoped(ctx, "inv-1") {
			p.Result["memo"] = "****"
		}
		return nil
	})
	m, err := x.ui.meta("invoices")
	if err != nil {
		t.Fatal(err)
	}
	ctx := elevatedClerk(x, policy, "/rec/invoices/inv-1", "")
	if !hookMaskedFields(ctx, m, "inv-1", map[string]any{"memo": "first note"})["memo"] {
		t.Fatal("SECURITY: the read-hook probe ran with the back office's elevation")
	}
}

func TestSweepRecordCopyAPIURLIsThePublicREST(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	admin := x.ui.WithAPIPath(func(e *entity.Entity) (string, bool) { return "/admin/api/" + e.GetName(), true })
	rec := string(admin.Record("invoices", "inv-1").RenderCtx(x.userCtx("/invoices/inv-1", "", "u1")))
	if !strings.Contains(rec, "http://example.com/api/invoices/inv-1") || strings.Contains(rec, "http://example.com/admin/api/") {
		t.Fatalf("Copy API URL does not name the public REST address:\n%s", rec)
	}
}

func TestSweepRecordOverrideNeedsEnforcedStates(t *testing.T) {
	w := newOverrideWorld(t, func(ents map[string]entity.EntityConfig) {
		inv := ents["invoices"]
		st := *inv.States
		st.Advisory = true
		inv.States = &st
		ents["invoices"] = inv
	})
	policy := overridePolicy(t, "invoices:override_state")
	ctx := overrideCtx(policy, "boss")
	if body := string(w.x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Override().RenderCtx(ctx)); strings.Contains(body, "_override") {
		t.Fatalf("an advisory state field drew the override form:\n%s", body)
	}
	if rec := postOverride(t, w, ctx, "application/json", `{"state":"paid","reason":"why"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("override of an advisory state field = %d, want 404", rec.Code)
	}
}

func TestSweepRecordOverrideNeedsRead(t *testing.T) {
	w := newOverrideWorld(t, func(ents map[string]entity.EntityConfig) {
		inv := ents["invoices"]
		inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read"}}
		ents["invoices"] = inv
	})
	policy := overridePolicy(t, "invoices:override_state")
	policy.Register("invoices:read")
	rec := postOverride(t, w, overrideCtx(policy, "boss"), "application/json", `{"state":"paid","reason":"why"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("SECURITY: an override by a caller who may not read the record = %d, want 404", rec.Code)
	}
	assertInvoiceUntouched(t, w)
}

func TestSweepRecordMaskedRequiredRelationIsNotRequired(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields[6].Required = true
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	maskInvoiceField(t, x, "customer_id")
	body := renderRecord(t, x, "inv-1", nil)
	for _, tag := range regexp.MustCompile(`<[^>]*customer_id[^>]*>`).FindAllString(body, -1) {
		if strings.Contains(tag, "required") {
			t.Fatalf("a masked relation's Replace picker is marked required:\n%s", tag)
		}
	}
}

func TestSweepRecordPickerSkipsRowTheGateDenies(t *testing.T) {
	ents := invoiceEntities()
	c := ents["customers"]
	c.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "customers:read"}}
	ents["customers"] = c
	rows := invoiceRows()
	rows["customers"] = append(rows["customers"], map[string]any{"id": "cus-2", "name": "Globex"})
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	policy := access.NewRolePolicy()
	if err := policy.Grant("clerk", "customers:read"); err != nil {
		t.Fatal(err)
	}
	rec := pickPost(t, x, "customer_id", `{"q":""}`, func(r *http.Request) {
		ctx := access.WithRoles(access.WithPolicy(r.Context(), policy), []string{"clerk"})
		ctx = access.WithDecider(ctx, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
			if ref.ID == "cus-1" {
				return access.DecisionDeny
			}
			return access.DecisionAbstain
		})
		*r = *r.WithContext(ctx)
	})
	if body := rec.Body.String(); rec.Code != http.StatusOK || strings.Contains(body, "Acme") || !strings.Contains(body, "Globex") {
		t.Fatalf("SECURITY: the picker answered %d with a row the record gate denies:\n%s", rec.Code, body)
	}
}

func TestSweepRecordPickerRefusesLockedRelation(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Display.Fields["customer_id"] = entity.FieldDisplay{Locked: true}
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	if got := pickPost(t, x, "customer_id", `{"q":""}`, nil).Code; got != http.StatusNotFound {
		t.Fatalf("a locked relation's picker answered %d, want 404", got)
	}
}

func TestSweepRecordHiddenFailsClosedOnReadError(t *testing.T) {
	x := newInvoiceUI(t)
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeGet, func(context.Context, any) error { return nil })
	ctx := asUser(context.Background(), "u1")
	if x.ui.RecordHidden(ctx, "invoices", "inv-1") {
		t.Fatal("setup: a readable record is hidden")
	}
	if err := x.db.Close(); err != nil {
		t.Fatal(err)
	}
	if !x.ui.RecordHidden(ctx, "invoices", "inv-1") {
		t.Fatal("SECURITY: a record whose read failed is not hidden")
	}
}

func TestSweepRecordFileHrefRefusesProtocolRelative(t *testing.T) {
	x := uploadsUI(t, true)
	if got := x.ui.fileHref("//evil.example/a.png"); got != "" {
		t.Fatalf("fileHref(//evil.example/a.png) = %q, want none", got)
	}
}

func TestSweepRecordOverrideFormNeedsExactCapability(t *testing.T) {
	w := newOverrideWorld(t, nil)
	policy := access.NewRolePolicy()
	policy.Register("invoices:override_state")
	if err := policy.Grant("root", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	body := string(w.x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Override().RenderCtx(overrideCtx(policy, "root")))
	if strings.Contains(body, "_override") {
		t.Fatalf("SECURITY: a Wildcard role without the exact capability saw the override form:\n%s", body)
	}
}

func TestSweepRecordShowWhenParsesOneTerm(t *testing.T) {
	draw := func(cond string) string {
		ents := invoiceEntities()
		inv := ents["invoices"]
		inv.Display.Fields["memo"] = entity.FieldDisplay{ShowWhen: cond}
		ents["invoices"] = inv
		x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
		return renderRecord(t, x, "inv-1", nil)
	}
	if body := draw(`token = "x"`); !strings.Contains(body, "fui-when") {
		t.Fatalf("a ShowWhen on a NoQuery controller drew memo unconditioned:\n%s", body)
	}
	if body := draw(`customer_id != ""`); !strings.Contains(body, `name="memo"`) || strings.Contains(body, "fui-when") {
		t.Fatalf("a ShowWhen that is not one = or in term did not draw memo unconditioned:\n%s", body)
	}
}

func TestSweepRecordFloatAndStoredUUIDInputs(t *testing.T) {
	const ref = "0b0c6a52-3c39-4f41-9c8a-4a1d4f9d6a11"
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "rate", Type: schema.Float}, schema.Field{Name: "ref", Type: schema.UUID})
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"][0]["rate"] = 0.5
	rows["invoices"][0]["ref"] = ref
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := renderRecord(t, x, "inv-1", nil)
	if !regexp.MustCompile(`name="rate" step="0\.000000001"`).MatchString(body) {
		t.Errorf("a Float input's step refuses its precision:\n%s", body)
	}
	if strings.Contains(body, `name="ref"`) || !strings.Contains(body, ref) {
		t.Errorf("a stored UUID is not drawn read-only:\n%s", body)
	}
}

func TestSweepRecordRelationNewNeedsCreate(t *testing.T) {
	ents := invoiceEntities()
	c := ents["customers"]
	c.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Create: "customers:create"}}
	ents["customers"] = c
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	x.ui = x.ui.WithRecordPath(recPath)
	body := renderRecord(t, x, "inv-1", nil)
	if !strings.Contains(body, `name="customer_id"`) {
		t.Fatalf("setup: the caller got no editable picker:\n%s", body)
	}
	if strings.Contains(body, `href="/rec/customers/create"`) {
		t.Fatalf("SECURITY: a caller who may not create customers got New beside the picker:\n%s", body)
	}
}

func TestSweepRecordUploadRequiredOnlyWhenEmpty(t *testing.T) {
	x := newTestUIExt(t,
		map[string]entity.EntityConfig{"products": {Fields: fields(
			schema.Field{Name: "name", Type: schema.String, Required: true},
			schema.Field{Name: "photo", Type: schema.Image, Required: true},
		), Exposure: &entity.ExposureConfig{Public: true}}},
		map[string][]map[string]any{"products": {{"id": "p1", "name": "Drill", "photo": "uploads/products/photo/drill.png"}}},
		Extensions{FilesURL: "/uploads/"},
		withAPI(map[string]string{"products": "/api/products"}), withStorage(teststorage.New(t, t.TempDir())))
	body := string(x.ui.Record("products", "p1").RenderCtx(x.ctx("/products/p1", "")))
	for _, tag := range regexp.MustCompile(`<input[^>]*type="file"[^>]*>`).FindAllString(body, -1) {
		if strings.Contains(tag, "required") {
			t.Fatalf("a required upload with a stored file is still required, so an untouched save cannot submit:\n%s", tag)
		}
	}
}

func TestSweepRecordPickerRefusesNonJSON(t *testing.T) {
	x := newInvoiceUI(t)
	rec := pickPost(t, x, "customer_id", `q=a`, func(r *http.Request) { r.Header.Set("Content-Type", "application/x-www-form-urlencoded") })
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a form-encoded picker search = %d, want 415", rec.Code)
	}
}
