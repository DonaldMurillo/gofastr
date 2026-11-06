package entityui

import (
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// RelatedAt with an empty base draws the related list with no record
// links, no row menu and no New: the entity has no screen to link to.
func TestRelatedAtEmptyBaseDrawsNoLinks(t *testing.T) {
	rows := invoiceRows()
	rows["payments"] = []map[string]any{{"id": "pay-1", "invoice_id": "inv-1", "amount": "40.00"}}
	x := newTestUI(t, invoiceEntities(), rows, withAPI(map[string]string{
		"invoices": "/api/invoices", "payments": "/api/payments", "customers": "/api/customers",
	}))
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RelatedAt("payments", "").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	if !strings.Contains(body, `id="eui-related-payments"`) || !strings.Contains(body, "40.00") {
		t.Fatalf("the payments section or its row is missing:\n%s", body)
	}
	section := body[strings.Index(body, `id="eui-related-payments"`):]
	for _, bad := range []string{"/pay-1", "/create", "fui-menu", "fui-copy"} {
		if strings.Contains(section, bad) {
			t.Errorf("a no-link related list drew %q:\n%s", bad, section)
		}
	}
	// One data column (the pinned invoice_id drops out) and no actions column.
	if n := strings.Count(section, "<th "); n != 1 {
		t.Errorf("no-link related list has %d header cells, want 1:\n%s", n, section)
	}
}

// RelatedAt with a base hangs the list's links off it rather than the
// derived sibling path.
func TestRelatedAtBaseOverridesDerived(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RelatedAt("payments", "/money/payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	if !strings.Contains(body, `href="/money/payments/create?prefill_invoice_id=inv-1"`) {
		t.Fatalf("New does not hang off the given base:\n%s", body)
	}
	if strings.Contains(body, "/rec/payments") {
		t.Fatalf("the derived base leaked past RelatedAt:\n%s", body)
	}
}

// The record keeps one <h1>: a related list is a section of the record,
// so its heading sits below the record's title.
func TestRelatedListsKeepOneH1(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments").
		RenderCtx(x.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")))
	if n := strings.Count(body, "<h1"); n != 1 {
		t.Fatalf("record with a related list has %d <h1>, want 1:\n%s", n, body)
	}
}

func TestRelatedRefusedSectionsAreOmitted(t *testing.T) {
	for _, mode := range []string{"token", "read grant", "crud off"} {
		t.Run(mode, func(t *testing.T) {
			entities := invoiceEntities()
			payments := entities["payments"]
			switch mode {
			case "read grant":
				payments.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "payments:read"}}
			case "crud off":
				off := false
				payments.Exposure = &entity.ExposureConfig{CRUD: &off}
			}
			entities["payments"] = payments
			rows := invoiceRows()
			rows["payments"] = []map[string]any{{"id": "pay-secret", "invoice_id": "inv-1", "amount": "913.27"}}
			fixture := newTestUI(t, entities, rows)
			ctx := fixture.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")
			if mode == "token" {
				ctx = access.WithHeldScopes(ctx, []string{"invoices:read"})
			} else if mode == "read grant" {
				ctx = access.WithRoles(access.WithPolicy(ctx, access.NewRolePolicy()), []string{"reader"})
			}
			record := fixture.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments")
			body := string(record.RenderCtx(ctx))
			if !strings.Contains(body, "INV-1") {
				t.Fatal("parent record was not rendered")
			}
			for _, forbidden := range []string{"?tab=related", "eui-related-payments", "913.27", render.Escape(i18nui.EntitySlotFailed.English()), i18nui.EntitySlotFailedBody.English()} {
				if strings.Contains(body, forbidden) {
					t.Errorf("refused related list disclosed %q", forbidden)
				}
			}
			metadata, err := fixture.ui.meta("invoices")
			if err != nil {
				t.Fatal(err)
			}
			if count := record.relatedCount(ctx, metadata, "/rec/invoices"); count != "" {
				t.Errorf("refused related count = %q", count)
			}
		})
	}
}

func TestRelatedMixedAccessKeepsReadableSections(t *testing.T) {
	entities := invoiceEntities()
	adjustments := entities["payments"]
	adjustments.Table = "adjustments"
	entities["adjustments"] = adjustments
	rows := invoiceRows()
	rows["payments"] = []map[string]any{{"id": "pay-secret", "invoice_id": "inv-1", "amount": "913.27"}}
	rows["adjustments"] = []map[string]any{{"id": "adj-1", "invoice_id": "inv-1", "amount": "77.25"}}
	fixture := newTestUI(t, entities, rows)
	ctx := access.WithHeldScopes(fixture.userCtx("/rec/invoices/inv-1", "?tab=related", "u1"), []string{"invoices:read", "adjustments:read"})
	record := fixture.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related("payments", "adjustments")
	body := string(record.RenderCtx(ctx))
	for _, required := range []string{"?tab=related", `id="eui-related-adjustments"`, "77.25"} {
		if !strings.Contains(body, required) {
			t.Errorf("readable related section lost %q", required)
		}
	}
	for _, forbidden := range []string{"eui-related-payments", "913.27", render.Escape(i18nui.EntitySlotFailed.English())} {
		if strings.Contains(body, forbidden) {
			t.Errorf("mixed related lists disclosed %q", forbidden)
		}
	}
	metadata, err := fixture.ui.meta("invoices")
	if err != nil {
		t.Fatal(err)
	}
	if count := record.relatedCount(ctx, metadata, "/rec/invoices"); count != "1" {
		t.Errorf("readable related count = %q, want 1", count)
	}
}

func TestRelatedResolutionFailuresRemainVisible(t *testing.T) {
	for _, name := range []string{"missing", "customers", "payments"} {
		t.Run(name, func(t *testing.T) {
			fixture := newInvoiceUI(t)
			if name == "payments" {
				fixture.ui.host = relatedMetadataFailureHost{Host: fixture.ui.host}
			}
			record := fixture.ui.Record("invoices", "inv-1").Base("/rec/invoices").Related(name)
			ctx := fixture.userCtx("/rec/invoices/inv-1", "?tab=related", "u1")
			metadata, err := fixture.ui.meta("invoices")
			if err != nil {
				t.Fatal(err)
			}
			tabbed, active, err := record.tabbedBody(ctx, metadata, nil, nil, nil, "/rec/invoices")
			if err != nil || active != "related" || !strings.Contains(string(tabbed), "?tab=related") {
				t.Fatalf("resolution failure hid the Related tab: active=%q, error=%v", active, err)
			}
			for _, body := range []string{string(record.RenderCtx(ctx)), string(tabbed)} {
				for _, required := range []string{render.Escape(i18nui.EntitySlotFailed.English()), i18nui.EntitySlotFailedBody.English()} {
					if !strings.Contains(body, required) {
						t.Errorf("resolution failure lost %q", required)
					}
				}
			}
		})
	}
}

type relatedMetadataFailureHost struct{ Host }

func (host relatedMetadataFailureHost) Crud(ent *entity.Entity) (*crud.CrudHandler, error) {
	if ent.GetName() == "payments" {
		return nil, errors.New("related metadata unavailable")
	}
	return host.Host.Crud(ent)
}
