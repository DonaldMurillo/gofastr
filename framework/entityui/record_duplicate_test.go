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

// The record menu offers Create another (the entity's create screen)
// beside Duplicate, and Copy API URL: the record's REST address on this
// origin, held in a hidden span the copy reads.
func TestRecordMenuCreateAnotherAndAPIURL(t *testing.T) {
	x := newInvoiceUI(t)
	body := renderRecord(t, x, "inv-1", func(b *RecordBuilder) { b.Duplicate() })
	for _, want := range []string{
		`href="/rec/invoices/create"`,
		">Create another<",
		">Copy API URL<",
		`id="eui-rec-api"`,
		`http://example.com/api/invoices/inv-1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the record menu misses %q:\n%s", want, body)
		}
	}
	// No create, no Create another.
	anon := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").Duplicate().RenderCtx(x.ctx("/rec/invoices/inv-1", "")))
	if strings.Contains(anon, ">Create another<") {
		t.Errorf("a caller who may not create got Create another")
	}
}
