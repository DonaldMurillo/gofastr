package entityui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// A panicking extension tab fails that tab alone: the active tab's
// body is the generic slot message, the page header and the rest of
// the record render on.
func TestRecordPanickingTabFailsTabOnly(t *testing.T) {
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), Extensions{Entities: map[string]Extension{
		"invoices": {Tabs: []Tab{{Key: "boom", Build: func(TabContext) (component.Component, error) {
			panic("kaboom")
		}}}},
	}}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=boom", "u1")))

	// The slot message renders HTML-escaped (Couldn&#39;t).
	want := strings.ReplaceAll(i18nui.Defaults[i18nui.KeyEntitySlotFailed], "'", "&#39;")
	if !strings.Contains(body, want) {
		t.Fatalf("the panicking tab shows the slot message:\n%s", body)
	}
	if !strings.Contains(body, "INV-1") {
		t.Fatalf("the record header renders on:\n%s", body)
	}
	if strings.Contains(body, "kaboom") {
		t.Fatalf("SECURITY: the panic text reached the page:\n%s", body)
	}
}

// The tab strip is query-param navigation: every tab is a link to the
// page's own URL with its ?tab= key, the active one selected, and an
// unknown ?tab= falls back to Edit.
func TestRecordTabsAreQueryLinks(t *testing.T) {
	x := newInvoiceUI(t, withAudit(fakeAudit{}))
	render := func(tab string) string {
		return string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").Activity().
			RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab="+tab, "u1")))
	}
	body := render("")
	for _, link := range []string{
		`href="/rec/invoices/inv-1?tab=edit"`,
		`href="/rec/invoices/inv-1?tab=related"`,
		`href="/rec/invoices/inv-1?tab=activity"`,
	} {
		if !strings.Contains(body, link) {
			t.Fatalf("the strip must link %s:\n%s", link, body)
		}
	}
	if strings.Contains(body, "data-cui-signal-set") {
		t.Fatalf("query tabs navigate; they carry no signal wiring:\n%s", body)
	}
	// The active tab is Edit: the strip marks its link current.
	if !strings.Contains(body, `aria-current="page" class="fui-tab-nav__link" data-cui-internal="" href="/rec/invoices/inv-1?tab=edit"`) {
		t.Fatalf("Edit is active by default:\n%s", body)
	}
	if strings.Count(body, `aria-current="page"`) != 1 {
		t.Fatalf("exactly one tab is current:\n%s", body)
	}
	// An unknown key falls back to Edit's body, not a failure.
	unknown := render("nonsense")
	if strings.Contains(unknown, i18nui.Defaults[i18nui.KeyEntitySlotFailed]) && strings.Contains(unknown, `?tab=nonsense`) {
		t.Fatalf("an unknown tab key shows Edit:\n%s", unknown)
	}
}

// The Related tab names the New link's prefill convention and lists
// the related entity through the list builder.
func TestRecordRelatedTabNewLink(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))

	want := `/rec/payments/create?prefill_invoice_id=inv-1`
	if !strings.Contains(body, `href="`+want+`"`) {
		t.Fatalf("the Related tab's New link pre-fills the foreign key:\n%s", body)
	}
	if !strings.Contains(body, ">Payments<") {
		t.Fatalf("the related entity's plural names its section:\n%s", body)
	}
}

// The Activity tab never shows a Hidden or masked field: both are
// removed from Before and After before the diff is built.
func TestRecordActivityHidesMaskedAndHidden(t *testing.T) {
	x := newInvoiceUI(t, withAudit(fakeAudit{entries: []AuditEntry{{
		At: time.Now(), Actor: "u1", Operation: "update",
		Before: map[string]any{"number": "INV-1", "token": "tok-secret", "user_hash": "h"},
		After:  map[string]any{"number": "INV-2", "token": "tok-secret2", "user_hash": "h2"},
	}}}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Activity().
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=activity", "u1")))

	if !strings.Contains(body, `fui-change-list__label" data-cui-internal="">Number</span>`) ||
		!strings.Contains(body, `from </span>INV-1</del>`) || !strings.Contains(body, `to </span>INV-2</ins>`) {
		t.Fatalf("the change list shows the changed readable field:\n%s", body)
	}
	if strings.Contains(body, "tok-secret") || strings.Contains(body, "user_hash") {
		t.Fatalf("SECURITY: the activity diff leaked a masked or hidden field:\n%s", body)
	}
}

// fakeAudit is the host's audit seam in tests.
type fakeAudit struct {
	entries []AuditEntry
	err     error
}

func (f fakeAudit) Trail(context.Context, string, string, int) ([]AuditEntry, error) {
	return f.entries, f.err
}

// The REST handler's 422 field names are the schema names the form's
// controls carry, so the runtime's envelope handling can place them.
func TestRecord422FieldNamesMatchControls(t *testing.T) {
	x := newInvoiceUI(t)
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	create := ch.Create()
	req := httptest.NewRequest(http.MethodPost, "/api/invoices", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(asUser(req.Context(), "u1"))
	rec := httptest.NewRecorder()
	create.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
		t.Fatalf("create without required fields = %d, want a refusal naming fields: %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Fields map[string][]string `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("parse 422 body: %v", err)
	}
	if len(envelope.Fields) == 0 {
		t.Fatal("the 422 names no fields")
	}
	body := renderRecord(t, x, "inv-1", nil)
	for name := range envelope.Fields {
		if !strings.Contains(body, `name="`+name+`"`) {
			t.Fatalf("the 422's field %q has no control of that name on the form", name)
		}
	}
}

// Every chrome string resolves through i18nui: with a translator
// mapping the keys, the page carries the mapped words and no bare key.
func TestRecordChromeComesFromTranslator(t *testing.T) {
	cat := i18n.NewMapCatalog()
	mapped := map[i18nui.Key]string{
		i18nui.KeyEntityTabEdit:    "BEARBEITEN",
		i18nui.KeyEntitySave:       "SPEICHERN",
		i18nui.KeyEntitySaved:      "GESPEICHERT",
		i18nui.KeyEntityDetails:    "ANGABEN",
		i18nui.KeyEntityLeaveGuard: "UNGESPEICHERTE ÄNDERUNGEN",
		i18nui.KeyEntityNotFound:   "NICHT GEFUNDEN",
	}
	for k, v := range mapped {
		cat.Set("de", string(k), i18n.Message{Text: v})
	}
	x := newInvoiceUI(t, withTranslator(i18n.NewTranslator(cat, "en")))
	withLocale := func() string {
		ctx := i18n.WithContext(x.userCtx("/rec/invoices/inv-1", "", "u1"), i18n.Locale{Tag: "de"})
		return string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(ctx))
	}
	body := withLocale()
	for _, v := range mapped {
		if v == mapped[i18nui.KeyEntityNotFound] {
			continue // not-found is not on this page
		}
		if !strings.Contains(body, v) {
			t.Fatalf("chrome %q missing from the page:\n%s", v, body)
		}
	}
	// The leave guard's words are the translator's, not the default.
	if !strings.Contains(body, `data-hui-leave-guard-message="UNGESPEICHERTE ÄNDERUNGEN"`) {
		t.Fatalf("the leave guard carries the translated words:\n%s", body)
	}
	missingRec := func() string {
		ctx := i18n.WithContext(x.userCtx("/rec/invoices/none", "", "u1"), i18n.Locale{Tag: "de"})
		return string(x.ui.Record("invoices", "no-such-id").Base("/rec/invoices").RenderCtx(ctx))
	}()
	if !strings.Contains(missingRec, "NICHT GEFUNDEN") {
		t.Fatalf("not-found resolves through the translator:\n%s", missingRec)
	}
}
