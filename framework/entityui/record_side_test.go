package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// sidePanel is a side panel that draws the record's number.
func sidePanel(seen *Record) SidePanel {
	return SidePanel{Key: "billing", Title: "Billing contact", Build: func(rc RecordContext) (component.Component, error) {
		*seen = rc.Record
		return staticComp("<p>panel for " + rc.Record.ID + "</p>"), nil
	}}
}

// An extension side panel draws in the Edit tab's side column under its
// title, built from the record as the read hooks left it.
func TestRecordSidePanelDraws(t *testing.T) {
	var seen Record
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), Extensions{Entities: map[string]Extension{
		"invoices": {Side: []SidePanel{sidePanel(&seen)}, Tabs: []Tab{{Key: "notes", Build: func(TabContext) (component.Component, error) {
			return staticComp("<p>notes</p>"), nil
		}}}},
	}}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
	if !strings.Contains(body, "<p>panel for inv-1</p>") || !strings.Contains(body, "Billing contact") {
		t.Fatalf("the side panel is not drawn:\n%s", body)
	}
	if seen.ID != "inv-1" || seen.Values == nil {
		t.Fatalf("the panel saw %+v", seen)
	}
	// It sits in the frame's side column, after the record's details.
	side := body[strings.Index(body, i18nui.Defaults[i18nui.KeyEntityDetails]):]
	if !strings.Contains(side, "panel for inv-1") {
		t.Fatalf("the panel is not after the details:\n%s", body)
	}
	// Another tab draws no side column.
	other := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=notes", "u1")))
	if !strings.Contains(other, "<p>notes</p>") || strings.Contains(other, "panel for inv-1") {
		t.Fatalf("the side panel drew outside Edit:\n%s", other)
	}
}

// A create form has no record, so it draws no side panel.
func TestRecordSidePanelNotOnCreate(t *testing.T) {
	var seen Record
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), Extensions{Entities: map[string]Extension{
		"invoices": {Side: []SidePanel{sidePanel(&seen)}},
	}}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "", "u1")))
	if strings.Contains(body, "panel for") || seen.ID != "" {
		t.Fatalf("a create form drew the side panel:\n%s", body)
	}
}

// A panicking side panel fails that panel alone; its text never reaches
// the page and the form renders on.
func TestRecordPanickingSidePanelFailsPanelOnly(t *testing.T) {
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), Extensions{Entities: map[string]Extension{
		"invoices": {Side: []SidePanel{{Key: "boom", Title: "Boom", Build: func(RecordContext) (component.Component, error) {
			panic("kaboom")
		}}}},
	}}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "", "u1")))
	want := strings.ReplaceAll(i18nui.Defaults[i18nui.KeyEntitySlotFailed], "'", "&#39;")
	if !strings.Contains(body, want) {
		t.Fatalf("the panel shows the slot message:\n%s", body)
	}
	if strings.Contains(body, "kaboom") {
		t.Fatalf("SECURITY: the panic text reached the page:\n%s", body)
	}
	if !strings.Contains(body, `name="number"`) {
		t.Fatalf("the form renders on:\n%s", body)
	}
}

// New refuses a side panel with a bad or duplicate key or no Build.
func TestSidePanelChecks(t *testing.T) {
	build := func(RecordContext) (component.Component, error) { return staticComp(""), nil }
	for name, panels := range map[string][]SidePanel{
		"bad key":   {{Key: "Bad Key", Build: build}},
		"duplicate": {{Key: "a", Build: build}, {Key: "a", Build: build}},
		"no build":  {{Key: "a"}},
	} {
		t.Run(name, func(t *testing.T) {
			x := newTestHost(t, invoiceEntities(), invoiceRows())
			_, err := New(x.host, Extensions{Entities: map[string]Extension{"invoices": {Side: panels}}})
			if err == nil {
				t.Fatal("New accepted the panel")
			}
		})
	}
}
