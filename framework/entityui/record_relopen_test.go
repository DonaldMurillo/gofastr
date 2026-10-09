package entityui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// relOpen matches the open link drawn beside a relation picker.
var relOpen = regexp.MustCompile(`<a aria-label="Open Customer" [^>]*href="(/rec/customers/[^"]*)"`)

// A relation with a value and a record screen draws a link to that
// record beside its picker.
func TestRelationPickerOpensRecord(t *testing.T) {
	x := newInvoiceUI(t)
	x.ui = x.ui.WithRecordPath(recPath)
	body := renderRecord(t, x, "inv-1", nil)
	m := relOpen.FindStringSubmatch(body)
	if m == nil || m[1] != "/rec/customers/cus-1" {
		t.Fatalf("no open link to the customer beside the select:\n%s", body)
	}
	if !strings.Contains(body, `class="fui-picker"`) {
		t.Fatalf("the link is not on the picker's row:\n%s", body)
	}
}

// No record path, no link; an empty relation (the create form) has
// nothing to open.
func TestRelationOpenNeedsPathAndValue(t *testing.T) {
	x := newInvoiceUI(t)
	if body := renderRecord(t, x, "inv-1", nil); strings.Contains(body, "/customers/cus-1") {
		t.Fatalf("a UI with no record path drew an open link:\n%s", body)
	}
	x.ui = x.ui.WithRecordPath(recPath)
	body := string(x.ui.Create("invoices").Base("/rec/invoices").RenderCtx(x.userCtx("/rec/invoices/new", "", "u1")))
	if relOpen.MatchString(body) {
		t.Fatalf("the create form drew an open link:\n%s", body)
	}
}

// A Decider that refuses the one related row draws no link, though the
// caller may read the entity and the row is in its scope.
func TestRelationOpenDeniedRow(t *testing.T) {
	ents := invoiceEntities()
	cus := ents["customers"]
	cus.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "customers:read"}}
	ents["customers"] = cus
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	x.ui = x.ui.WithRecordPath(recPath)
	policy := access.NewRolePolicy()
	if err := policy.Grant("reader", access.Permission("customers:read")); err != nil {
		t.Fatal(err)
	}
	reader := access.WithRoles(access.WithPolicy(x.userCtx("/rec/invoices/inv-1", "", "u1"), policy), []string{"reader"})
	b := x.ui.Record("invoices", "inv-1").Base("/rec/invoices")
	if body := string(b.RenderCtx(reader)); !relOpen.MatchString(body) {
		t.Fatalf("a reader got no open link:\n%s", body)
	}
	denied := access.WithDecider(reader, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "cus-1" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})
	if body := string(b.RenderCtx(denied)); strings.Contains(body, `href="/rec/customers/cus-1"`) {
		t.Fatalf("SECURITY: a denied customer got an open link:\n%s", body)
	}
}

// A related record the caller may not read draws no link, and so no
// foreign key in an href.
func TestRelationOpenRefusedRecord(t *testing.T) {
	installOwnerExtractor(t)
	ents := invoiceEntities()
	cus := ents["customers"]
	cus.Fields = append(cus.Fields, schema.Field{Name: "user_id", Type: schema.String, Hidden: true})
	cus.Scope = &entity.ScopeConfig{OwnerField: "user_id"}
	ents["customers"] = cus
	rows := invoiceRows()
	rows["customers"] = []map[string]any{{"id": "cus-1", "name": "Acme", "user_id": "u2"}}
	x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	x.ui = x.ui.WithRecordPath(recPath)
	body := renderRecord(t, x, "inv-1", nil)
	if strings.Contains(body, `href="/rec/customers/cus-1"`) {
		t.Fatalf("SECURITY: an unreadable customer got an open link:\n%s", body)
	}
}

// A relation offers New beside the picker: a link to the related
// entity's create screen, only for a caller who may create one.
func TestRelationPickerOffersNew(t *testing.T) {
	x := newInvoiceUI(t)
	x.ui = x.ui.WithRecordPath(recPath)
	body := renderRecord(t, x, "inv-1", nil)
	if !strings.Contains(body, `href="/rec/customers/create"`) || !strings.Contains(body, `aria-label="New Customer"`) {
		t.Fatalf("no New link beside the picker:\n%s", body)
	}
	anon := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(x.ctx("/rec/invoices/inv-1", "")))
	if strings.Contains(anon, `href="/rec/customers/create"`) {
		t.Fatalf("SECURITY: a caller who may not create customers got New:\n%s", anon)
	}
}
