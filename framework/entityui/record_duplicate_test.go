package entityui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A duplicate starts blank on a field a unique index covers, as on a
// Unique field, so saving the copy unchanged does not collide. The
// relation in the same index keeps its value: the number alone makes
// the copy distinct.
func TestDuplicateBlanksUniqueIndex(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields[0].Unique = false
	inv.Indices = []entity.Index{{Columns: []string{"customer_id", "number"}, Unique: true}}
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "?duplicate=inv-1", "u1")))
	if regexp.MustCompile(`name="number"[^>]*value="INV-1"|value="INV-1"[^>]*name="number"`).MatchString(body) {
		t.Errorf("the duplicate copied a number its unique index refuses:\n%s", body)
	}
	if !strings.Contains(body, `name="customer_id" type="hidden" value="cus-1"`) {
		t.Errorf("the duplicate dropped the customer:\n%s", body)
	}
	if !regexp.MustCompile(`name="memo"[^>]*>first note<`).MatchString(body) {
		t.Errorf("the duplicate dropped the memo:\n%s", body)
	}
}

// An index of relations alone (one invoice per customer) blanks them.
func TestDuplicateBlanksRelationIndex(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Indices = []entity.Index{{Columns: []string{"customer_id"}, Unique: true}}
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	body := string(x.ui.Create("invoices").Base("/rec/invoices").
		RenderCtx(x.userCtx("/rec/invoices/create", "?duplicate=inv-1", "u1")))
	if strings.Contains(body, `name="customer_id" type="hidden" value="cus-1"`) {
		t.Errorf("the duplicate kept a customer its unique index refuses:\n%s", body)
	}
}
