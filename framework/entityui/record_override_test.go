package entityui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// The override tests' world: the invoices fixture, write routes, an audit
// reader on the host, the entity marked audited (what App.WithAuditLog
// does), and an AfterUpdate recorder standing where WithAuditLog's audit
// hook stands, reading the same seams (crud.AuditOperationFor and
// crud.StateOverrideReason).
type overrideWorld struct {
	x    *testUI
	ops  []string
	why  []string
	from []map[string]any
}

// nullAudit is an AuditReader with nothing to show: it only turns the
// Activity tab (and the override form) on.
type nullAudit struct{}

func (nullAudit) Trail(context.Context, string, string, int) ([]AuditEntry, error) {
	return nil, nil
}

func newOverrideWorld(t *testing.T, tune func(map[string]entity.EntityConfig)) *overrideWorld {
	t.Helper()
	ents := invoiceEntities()
	if tune != nil {
		tune(ents)
	}
	w := &overrideWorld{}
	w.x = newTestUI(t, ents, invoiceRows(),
		withAPI(map[string]string{"invoices": "/api/invoices"}),
		withAudit(nullAudit{}))
	inv := mustEntity(t, w.x, "invoices")
	inv.MarkAudited()
	ch, err := w.x.host.Crud(inv)
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterUpdate, func(ctx context.Context, data any) error {
		row, ok := data.(map[string]any)
		if !ok {
			t.Errorf("AfterUpdate payload %T, want map[string]any", data)
			return nil
		}
		w.from = append(w.from, row)
		w.ops = append(w.ops, crud.AuditOperationFor(ctx, "invoices", "inv-1"))
		w.why = append(w.why, crud.StateOverrideReason(ctx))
		return nil
	})
	return w
}

// overrideCtx is u1's context holding the override capability under
// policy. roles name the grants to install.
func overrideCtx(policy *access.RolePolicy, roles ...string) context.Context {
	return bulkCtx("u1", policy, roles...)
}

func overridePolicy(t *testing.T, grants ...access.Permission) *access.RolePolicy {
	t.Helper()
	policy := access.NewRolePolicy()
	policy.Register(grants...)
	if err := policy.Grant("boss", grants...); err != nil {
		t.Fatal(err)
	}
	return policy
}

// postOverride drives the handler with the given body and content type.
func postOverride(t *testing.T, w *overrideWorld, ctx context.Context, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/inv-1/_override", strings.NewReader(body))
	req = req.WithContext(ctx)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	w.x.ui.OverrideHandler("invoices").ServeHTTP(rec, req)
	return rec
}

// The form draws only where every gate holds: the builder turns it on,
// the caller holds the capability exactly, the entity declares states
// and the app keeps an audit log.
func TestOverrideFormDrawnForCapableCaller(t *testing.T) {
	w := newOverrideWorld(t, nil)
	policy := overridePolicy(t, "invoices:override_state")

	plain := renderRecord(t, w.x, "inv-1", nil)
	if strings.Contains(plain, "Override status") {
		t.Fatalf("a record without Override() drew the override form:\n%s", plain)
	}

	noCap := renderRecord(t, w.x, "inv-1", func(b *RecordBuilder) { b.Override() })
	if strings.Contains(noCap, "Override status") {
		t.Fatalf("a caller without the capability saw the override form:\n%s", noCap)
	}

	ctx := overrideCtx(policy, "boss")
	body := string(w.x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Override().RenderCtx(ctx))
	for _, want := range []string{
		"<details",
		"Override status",
		`action="/api/invoices/inv-1/_override"`,
		`name="state"`,
		`value="draft"`,
		`value="open"`,
		`value="paid"`,
		`name="reason"`,
		"required",
		"data-cui-confirm",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the override form is missing %q:\n%s", want, body)
		}
	}
	// The current value is the marked one.
	if !strings.Contains(body, `selected="" value="draft"`) && !strings.Contains(body, `value="draft" selected`) {
		t.Errorf("the current state must be the select's marked option:\n%s", body)
	}
}

// No states, no audit log: no form, whatever the capability.
func TestOverrideFormNeedsStatesAndAudit(t *testing.T) {
	// customers declares no states.
	statesless := newTestUI(t,
		map[string]entity.EntityConfig{"customers": invoiceEntities()["customers"]},
		map[string][]map[string]any{"customers": invoiceRows()["customers"]},
		withAPI(map[string]string{"customers": "/api/customers"}), withAudit(nullAudit{}))
	policy := access.NewRolePolicy()
	policy.Register("customers:override_state")
	if err := policy.Grant("boss", "customers:override_state"); err != nil {
		t.Fatal(err)
	}
	ctx := bulkCtx("u1", policy, "boss")
	body := string(statesless.ui.Record("customers", "cus-1").Base("/rec/customers").Override().RenderCtx(ctx))
	if strings.Contains(body, "Override status") {
		t.Fatalf("an entity without states drew the override form:\n%s", body)
	}

	// The host keeps no audit reader.
	noAudit := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body = string(noAudit.ui.Record("invoices", "inv-1").Base("/rec/invoices").Override().RenderCtx(overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")))
	if strings.Contains(body, "Override status") {
		t.Fatalf("an app without an audit log drew the override form:\n%s", body)
	}
}

// The JSON arm answers the RPC caller: a status, a toast header and the
// changed record.
func TestOverrideJSONAnswer(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	raw, _ := json.Marshal(map[string]string{"state": "paid", "reason": "customer paid by phone"})
	rec := postOverride(t, w, ctx, "application/json", string(raw))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if rec.Header().Get("X-Gofastr-Toast") == "" {
		t.Errorf("the JSON answer carries no toast header:\n%s", rec.Body.String())
	}
	if s := invoiceStatus(t, w.x, "inv-1"); s != "paid" {
		t.Fatalf("status = %q, want paid", s)
	}
	if len(w.ops) != 1 || w.ops[0] != "state_override" {
		t.Fatalf("audit operations = %v, want exactly one state_override", w.ops)
	}
	if len(w.why) != 1 || w.why[0] != "customer paid by phone" {
		t.Fatalf("audit reasons = %v, want the submitted reason", w.why)
	}
}

// The plain-form arm answers a native submit with 303 back to the
// record, on the entity's own write base plus the id, never a URL the
// request supplied.
func TestOverridePlainPostRedirects(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	form := url.Values{"state": {"open"}, "reason": {"data repair"}, "back": {"/invoices/inv-1"}}
	rec := postOverride(t, w, ctx, "application/x-www-form-urlencoded", form.Encode())
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/invoices/inv-1" {
		t.Fatalf("Location = %q, want the record screen", loc)
	}
	if s := invoiceStatus(t, w.x, "inv-1"); s != "open" {
		t.Fatalf("status = %q, want open", s)
	}
}

// The reason cap counts characters, as the form's help text says, so
// 500 accented letters fit and 501 do not.
func TestOverrideReasonCapIsCharacters(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	for _, tc := range []struct {
		reason string
		want   int
	}{
		{strings.Repeat("é", 500), http.StatusOK},
		{strings.Repeat("é", 501), http.StatusBadRequest},
	} {
		raw, _ := json.Marshal(map[string]string{"state": "open", "reason": tc.reason})
		if rec := postOverride(t, w, ctx, "application/json", string(raw)); rec.Code != tc.want {
			t.Errorf("%d characters: status %d, want %d: %s", len([]rune(tc.reason)), rec.Code, tc.want, rec.Body.String())
		}
	}
}

// A plain post whose return path leaves the origin is refused before the
// write: no open redirect, and the record keeps its status.
func TestOverrideRefusesForeignBack(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	for _, back := range []string{"https://evil.example/", "//evil.example/", `/\evil.example/`, ""} {
		form := url.Values{"state": {"open"}, "reason": {"data repair"}, "back": {back}}
		rec := postOverride(t, w, ctx, "application/x-www-form-urlencoded", form.Encode())
		if rec.Code != http.StatusBadRequest {
			t.Errorf("back=%q: status %d, want 400", back, rec.Code)
		}
	}
	if s := invoiceStatus(t, w.x, "inv-1"); s != "draft" {
		t.Fatalf("a refused return path still wrote: %s", s)
	}
}

// Only JSON and form bodies are read; anything else is refused before
// the body is parsed, the way the bulk route refuses it.
func TestOverrideRefusesOtherContentTypes(t *testing.T) {
	w := newOverrideWorld(t, nil)
	ctx := overrideCtx(overridePolicy(t, "invoices:override_state"), "boss")
	for _, ct := range []string{"text/plain", "multipart/form-data", ""} {
		rec := postOverride(t, w, ctx, ct, `{"state":"paid","reason":"x"}`)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("Content-Type %q: status %d, want 415", ct, rec.Code)
		}
	}
	if s := invoiceStatus(t, w.x, "inv-1"); s != "draft" {
		t.Fatalf("a refused content type changed the record: %s", s)
	}
}
