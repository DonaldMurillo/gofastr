package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// recordForm is the record form's markup, open tag to close.
func recordForm(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, `<form id="eui-invoices-form"`)
	if i < 0 {
		i = strings.Index(body, `id="eui-invoices-form"`)
	}
	if i < 0 {
		t.Fatalf("no record form:\n%s", body)
	}
	i = strings.LastIndex(body[:i+1], "<form")
	j := strings.Index(body[i:], "</form>")
	return body[i : i+j]
}

// Save sits in the header, names the form it submits and answers
// Mod+S; the form draws no submit of its own.
func TestRecordSaveSitsInHeader(t *testing.T) {
	body := renderRecord(t, newInvoiceUI(t), "inv-1", nil)
	for _, want := range []string{`id="eui-invoices-save"`, `form="eui-invoices-form"`, `type="submit"`, `aria-keyshortcuts="Meta+S Control+S"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the header's Save lacks %s:\n%s", want, body)
		}
	}
	if form := recordForm(t, body); strings.Contains(form, `type="submit"`) {
		t.Errorf("the form draws a submit of its own:\n%s", form)
	}
	if !strings.Contains(body, ">Save<") {
		t.Errorf("Save is not labelled Save:\n%s", body)
	}
	if !strings.Contains(buttonTag(t, body, `id="eui-invoices-save"`), "fui-button--until-dirty") {
		t.Errorf("Save does not wait for an edit to look ready:\n%s", body)
	}
}

// buttonTag is the open tag of the button carrying marker.
func buttonTag(t *testing.T, body, marker string) string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("no %s:\n%s", marker, body)
	}
	i = strings.LastIndex(body[:i], "<button")
	return body[i : i+strings.Index(body[i:], ">")]
}

// Beside Save the header has one primary action: a move or app action
// declared primary draws as secondary there, and keeps its variant on
// a tab without Save.
func TestRecordSaveIsTheOnePrimary(t *testing.T) {
	var ran []ActionContext
	ents, rows := invoiceEntities(), invoiceRows()
	rows["invoices"][0]["status"] = "open"
	x := newTestUIExt(t, ents, rows, resendExt("", ui.ButtonPrimary, &ran, false),
		withAPI(map[string]string{"invoices": "/api/invoices"}))
	edit := renderRecord(t, x, "inv-1", nil)
	related := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	for _, label := range []string{"Mark paid", "Resend receipt"} {
		if tag := labelButton(t, edit, label); !strings.Contains(tag, "fui-button--secondary") {
			t.Errorf("beside Save, %s is not secondary: %s", label, tag)
		}
		if tag := labelButton(t, related, label); !strings.Contains(tag, "fui-button--primary") {
			t.Errorf("without Save, %s lost its declared primary: %s", label, tag)
		}
	}
}

// labelButton is the open tag of the button labelled label.
func labelButton(t *testing.T, body, label string) string {
	t.Helper()
	i := strings.Index(body, ">"+label+"<")
	if i < 0 {
		t.Fatalf("no %s button:\n%s", label, body)
	}
	i = strings.LastIndex(body[:i], "<button")
	return body[i : i+strings.Index(body[i:], ">")]
}

// A tab with no form has no Save.
func TestRecordRelatedTabHasNoSave(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	if strings.Contains(body, "eui-invoices-save") {
		t.Errorf("the Related tab draws Save:\n%s", body)
	}
}

// The state field is the header's badge, not a field; its stamp and
// the id sit in Details.
func TestRecordStateIsTheBadge(t *testing.T) {
	body := renderRecord(t, newInvoiceUI(t), "inv-1", nil)
	form := recordForm(t, body)
	details := form[strings.Index(form, ">Details<"):]
	for _, gone := range []string{`name="status"`, ">Status<"} {
		if strings.Contains(form, gone) {
			t.Errorf("the form still draws the state field (%s):\n%s", gone, form)
		}
	}
	if strings.Contains(form[:strings.Index(form, ">Details<")], ">Issued On<") {
		t.Errorf("the stamp is drawn among the fields:\n%s", form)
	}
	for _, want := range []string{">Issued On<", `id="eui-invoices-id" title="inv-1">inv-1<`, ">ID<"} {
		if !strings.Contains(details, want) {
			t.Errorf("Details lacks %s:\n%s", want, details)
		}
	}
	if !strings.Contains(body, ">Draft<") {
		t.Errorf("the badge is missing:\n%s", body)
	}
}

// The header says when the record was created and last updated.
func TestRecordStampLine(t *testing.T) {
	ents := invoiceEntities()
	ents["invoices"] = ents["invoices"].WithTimestamps(true)
	rows := invoiceRows()
	rows["invoices"][0]["created_at"] = "2026-08-02T10:00:00Z"
	rows["invoices"][0]["updated_at"] = "2026-09-02T10:00:00Z"
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := renderRecord(t, x, "inv-1", nil)
	if !strings.Contains(body, "Created Aug 2, 2026 · Updated Sep 2, 2026") {
		t.Errorf("no created/updated line:\n%s", body)
	}
	// Without the columns there is no line.
	if body := renderRecord(t, newInvoiceUI(t), "inv-1", nil); strings.Contains(body, "Created ") {
		t.Errorf("a record without timestamps draws a stamp line:\n%s", body)
	}
}

type recordScreen struct {
	component.ContextOnly
	x *testUI
}

func (s *recordScreen) SetParams(map[string]string) {}

func (s *recordScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Duplicate().RenderCtx(ctx)
}

// Drawn as an intercepted drawer the record wears the drawer's bar
// (close, path, copy link) and its menu drops the copy row; the full
// page draws no bar and keeps the row.
func TestRecordDrawerBar(t *testing.T) {
	x := newInvoiceUI(t)
	a := app.NewApp("rec")
	a.Register("/rec/invoices", &recordScreen{x: x}, nil)
	a.Register("/rec/invoices/:id", &recordScreen{x: x}, nil, app.InterceptFrom("/rec/invoices", app.ScreenDrawer))
	ctx := x.userCtx("/rec/invoices/inv-1", "", "u1")

	drawer, err := a.RenderOverlayResult(ctx, "/rec/invoices/inv-1", app.ScreenDrawer)
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.RenderPartialResult(ctx, "/rec/invoices/inv-1")
	if err != nil {
		t.Fatal(err)
	}
	d, p := string(drawer.HTML), string(page.HTML)
	for _, want := range []string{`data-cui-comp="ui-drawer-bar"`, `data-cui-intercept-close`, `>/rec/invoices/inv-1<`, `http://example.com/rec/invoices/inv-1`} {
		if !strings.Contains(d, want) {
			t.Errorf("the drawer lacks %s:\n%s", want, d)
		}
	}
	if strings.Contains(d, ">Copy link<") {
		t.Errorf("the drawer's menu repeats the bar's copy link:\n%s", d)
	}
	if strings.Contains(p, "ui-drawer-bar") || strings.Contains(p, "data-cui-intercept-close") {
		t.Errorf("the full page draws the drawer's bar:\n%s", p)
	}
	if !strings.Contains(p, ">Copy link<") {
		t.Errorf("the full page's menu lost its copy link:\n%s", p)
	}
}
