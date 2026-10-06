package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// WithAPIPath moves every write a screen draws to the given routes, and
// the UI it came from keeps its own.
func TestWithAPIPathMovesWrites(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	admin := x.ui.WithAPIPath(func(e *entity.Entity) (string, bool) {
		return "/admin/api/" + e.GetName(), e.GetName() == "invoices"
	})
	ctx := x.userCtx("/invoices", "", "u1")

	list := listHTML(t, admin.List("invoices").Bulk().Delete(), ctx)
	rec := string(admin.Record("invoices", "inv-1").Delete().RenderCtx(x.userCtx("/invoices/inv-1", "", "u1")))
	for _, want := range []string{
		`data-cui-rpc="/admin/api/invoices/_bulk"`,
		`/admin/api/invoices/_export.csv`,
		`data-cui-rpc="/admin/api/invoices/inv-1"`,
	} {
		if !strings.Contains(list, want) {
			t.Errorf("list missing %s:\n%s", want, list)
		}
	}
	for _, want := range []string{
		`action="/admin/api/invoices/inv-1"`,
		`/admin/api/invoices/inv-1/transitions/send`,
	} {
		if !strings.Contains(rec, want) {
			t.Errorf("record missing %s:\n%s", want, rec)
		}
	}
	for name, html := range map[string]string{"list": list, "record": rec} {
		if strings.Contains(strings.ReplaceAll(html, "/admin/api/", ""), "/api/invoices") {
			t.Errorf("%s still points a write at the REST routes:\n%s", name, html)
		}
	}

	if orig := listHTML(t, x.ui.List("invoices").Bulk(), x.userCtx("/invoices", "", "u1")); !strings.Contains(orig, `data-cui-rpc="/api/invoices/_bulk"`) {
		t.Errorf("the original UI lost its REST routes:\n%s", orig)
	}

	// path answering false draws the entity read-only.
	ro := x.ui.WithAPIPath(func(*entity.Entity) (string, bool) { return "", false })
	if html := string(ro.Record("invoices", "inv-1").RenderCtx(x.userCtx("/invoices/inv-1", "", "u1"))); strings.Contains(html, "<form") {
		t.Errorf("a UI with no write path drew a form:\n%s", html)
	}
}

// The derived UI keeps the host's bulk backing, so a queued run still
// finds its snapshot store.
func TestWithAPIPathKeepsBulkHost(t *testing.T) {
	x := newTestHost(t, invoiceEntities(), invoiceRows())
	mb := newMemBulk()
	u, err := New(bulkTestHost{x.host, mb}, Extensions{Jobs: mb})
	if err != nil {
		t.Fatal(err)
	}
	d := u.WithAPIPath(func(*entity.Entity) (string, bool) { return "/admin/api/invoices", true })
	if bh := d.bulkHost(); bh == nil || bh.BulkStore() != BulkStore(mb) {
		t.Fatalf("derived UI bulk host = %v, want the wrapped host's store", bh)
	}
	if p, _ := d.host.APIPath(nil); p != "/admin/api/invoices" {
		t.Fatalf("derived bulk-backed UI APIPath = %q", p)
	}
}
