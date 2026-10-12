package entityui

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestRecordTitleNamesReadableRecord(t *testing.T) {
	x := newInvoiceUI(t)
	got, ok := x.ui.RecordTitle(asUser(context.Background(), "u1"), "invoices", "inv-1")
	if !ok || got != "INV-1" {
		t.Fatalf("RecordTitle = %q, %v; want INV-1, true", got, ok)
	}
	if _, ok := x.ui.RecordTitle(asUser(context.Background(), "u1"), "invoices", "no-such-id"); ok {
		t.Fatal("a missing id named a title")
	}
	if _, ok := x.ui.RecordTitle(asUser(context.Background(), "u1"), "nope", "inv-1"); ok {
		t.Fatal("an unknown entity named a title")
	}
}

// The title is read behind the record's own gate: a row a Decider
// refuses names nothing, the same answer a missing id gets.
func TestRecordTitleRefusesDeniedRow(t *testing.T) {
	entities := invoiceEntities()
	inv := entities["invoices"]
	inv.Exposure = &entity.ExposureConfig{Access: entity.AccessControl{Read: "invoices:read"}}
	entities["invoices"] = inv
	x := newTestUI(t, entities, invoiceRows())

	policy := access.NewRolePolicy()
	if err := policy.Grant("reader", access.Permission("invoices:read")); err != nil {
		t.Fatal(err)
	}
	base := access.WithRoles(access.WithPolicy(asUser(context.Background(), "u1"), policy), []string{"reader"})
	if _, ok := x.ui.RecordTitle(base, "invoices", "inv-1"); !ok {
		t.Fatal("a reader got no title")
	}
	denied := access.WithDecider(base, func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "inv-1" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	})
	if got, ok := x.ui.RecordTitle(denied, "invoices", "inv-1"); ok {
		t.Fatalf("a denied row named %q", got)
	}
	noRole := asUser(context.Background(), "u1")
	if got, ok := x.ui.RecordTitle(access.WithPolicy(noRole, policy), "invoices", "inv-1"); ok {
		t.Fatalf("a caller without invoices:read got %q", got)
	}
}
