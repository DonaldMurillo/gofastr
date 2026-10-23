package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// inSpanish asks for Spanish; the translator holding the catalog is the
// UI's (withTranslator) or the request's.
func inSpanish(ctx context.Context) context.Context {
	return i18n.WithContext(ctx, i18n.Locale{Tag: "es"})
}

// An extension's tab, side panel and action are drawn in the request's
// language: each is a phrase the app's catalog translates.
func TestExtensionLabelsTranslate(t *testing.T) {
	tab, panel, action := testPhrase("Shipping notes"), testPhrase("Billing card"), testPhrase("Send again")
	cat := i18n.NewMapCatalog()
	cat.Set("es", tab.Key(), i18n.Message{Text: "Notas de envío"})
	cat.Set("es", panel.Key(), i18n.Message{Text: "Tarjeta de cobro"})
	cat.Set("es", action.Key(), i18n.Message{Text: "Reenviar"})
	draw := func(TabContext) (component.Component, error) { return staticComp("<p>x</p>"), nil }
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), Extensions{Entities: map[string]Extension{
		"invoices": {
			Tabs: []Tab{{Key: "notes", Label: tab, Build: draw}},
			Side: []SidePanel{{Key: "billing", Title: panel, Build: func(RecordContext) (component.Component, error) {
				return staticComp("<p>y</p>"), nil
			}}},
			Actions: []Action{{Key: "resend", Label: action, Run: func(context.Context, ActionContext) error { return nil }}},
		},
	}}, withAPI(map[string]string{"invoices": "/api/invoices"}), withTranslator(i18n.NewTranslator(cat, "en")))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(inSpanish(x.userCtx("/rec/invoices/inv-1", "", "u1"))))
	for _, want := range []string{"Notas de envío", "Tarjeta de cobro", "Reenviar"} {
		if !strings.Contains(body, want) {
			t.Errorf("record page lacks %q:\n%s", want, body)
		}
	}
	for _, english := range []string{"Shipping notes", "Billing card", "Send again"} {
		if strings.Contains(body, english) {
			t.Errorf("record page still says %q", english)
		}
	}
}

// The zero Phrase shows the extension's Key.
func TestExtensionLabelZeroShowsKey(t *testing.T) {
	draw := func(TabContext) (component.Component, error) { return staticComp("<p>x</p>"), nil }
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), Extensions{Entities: map[string]Extension{
		"invoices": {Tabs: []Tab{{Key: "notes", Build: draw}}},
	}}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
	if !strings.Contains(body, ">notes<") {
		t.Errorf("a labelless tab does not show its key:\n%s", body)
	}
}

// The bulk bar names a move the way the record page does: the catalog's
// entity.<entity>.transitions.<key>, not the declared English.
func TestBulkMoveLabelTranslates(t *testing.T) {
	x, _, _ := guardedInvoices(t, entity.AccessControl{}, 0, Extensions{}, nil)
	cat := i18n.NewMapCatalog()
	cat.Set("es", "entity.invoices.transitions.send", i18n.Message{Text: "Enviar"})
	ctx := i18n.WithTranslator(inSpanish(pageCtx("/invoices", nil, "u1")), i18n.NewTranslator(cat, "en"))
	page := listHTML(t, x.ui.List("invoices").Bulk(), ctx)
	if !strings.Contains(page, "Move: Enviar") {
		t.Errorf("the bar's move is not translated:\n%s", page)
	}
}
