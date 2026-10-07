package entityui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// countedInvoices is two owners' invoices with a Drafts view: u1 holds
// one draft and one paid, u2 two drafts.
func countedInvoices(t *testing.T) *testUI {
	t.Helper()
	installOwnerExtractor(t)
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
	inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
	inv.SearchFields = []string{"number"}
	d := *inv.Display
	d.Description = "Every invoice issued."
	d.Views = []entity.ListView{{Key: "drafts", Label: "Drafts", Where: `status = "draft"`}}
	inv.Display = &d
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = []map[string]any{
		{"id": "a1", "number": "A-1", "status": "draft", "owner_id": "u1"},
		{"id": "a2", "number": "A-2", "status": "paid", "owner_id": "u1"},
		{"id": "b1", "number": "B-1", "status": "draft", "owner_id": "u2"},
		{"id": "b2", "number": "B-2", "status": "draft", "owner_id": "u2"},
	}
	return newTestUIExt(t, ents, rows, Extensions{}, withAPI(map[string]string{"invoices": "/api/invoices"}))
}

// tabBadges maps each tab's label to its count badge, "" for none.
func tabBadges(html string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`<a [^>]*class="fui-tab-nav__link"[^>]*>([^<]+)(?:<span [^>]*class="fui-tab-nav__badge"[^>]*>([^<]*)</span>)?</a>`)
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// Each tab counts what its link lists, inside the caller's scope: u1
// sees two invoices and one draft, never u2's drafts.
func TestTabCountsStayInScope(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices").TabCounts(), x.userCtx("/invoices", "", "u1"))
	got := tabBadges(html)
	if got["All"] != "2" || got["Drafts"] != "1" {
		t.Fatalf("tab counts = %v, want All 2, Drafts 1:\n%s", got, html)
	}
	if !strings.Contains(html, "Every invoice issued.") || strings.Contains(html, "2 invoices") {
		t.Errorf("a counted strip did not hand the header the description:\n%s", html)
	}
}

// Another tab's count keeps the page's search and filter: what its link
// would list.
func TestTabCountsKeepTheFilter(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices").TabCounts(), x.userCtx("/invoices", `?view=drafts&filter=number+%3D+%22A-2%22`, "u1"))
	if got := tabBadges(html); got["All"] != "1" || got["Drafts"] != "0" {
		t.Fatalf("tab counts = %v, want All 1, Drafts 0:\n%s", got, html)
	}
}

// The search narrows every tab's count, as it narrows the tab's link.
func TestTabCountsKeepTheSearch(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices").TabCounts(), x.userCtx("/invoices", `?view=drafts&q=A-2`, "u1"))
	if got := tabBadges(html); got["All"] != "1" || got["Drafts"] != "0" {
		t.Fatalf("tab counts = %v, want All 1, Drafts 0:\n%s", got, html)
	}
}

// A BeforeList scope narrows every tab's count exactly as it narrows
// the list.
func TestTabCountsHonorBeforeListScope(t *testing.T) {
	x := countedInvoices(t)
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("status = $1", "draft")
		return nil
	})
	html := listHTML(t, x.ui.List("invoices").TabCounts(), x.userCtx("/invoices", `?view=drafts`, "u1"))
	if got := tabBadges(html); got["All"] != "1" {
		t.Fatalf("SECURITY: tab counts = %v past the BeforeList scope, want All 1:\n%s", got, html)
	}
}

// A saved view's tab counts its own filter in place of the URL's, the
// swap its link makes.
func TestTabCountsSavedViewFilter(t *testing.T) {
	x := countedInvoices(t)
	store := newMemSavedViews()
	x.ui = x.ui.WithSavedViews(store)
	store.put("u1", SavedView{Entity: "invoices", Name: "Paid", Filter: `status = "paid"`})
	html := listHTML(t, x.ui.List("invoices").TabCounts().SavedViews(), x.userCtx("/invoices", `?filter=number+%3D+%22A-9%22`, "u1"))
	if got := tabBadges(html); got["Paid"] != "1" || got["All"] != "0" {
		t.Fatalf("tab counts = %v, want Paid 1, All 0:\n%s", got, html)
	}
}

// Off by default: no badges, and the header keeps its count.
func TestTabCountsOffByDefault(t *testing.T) {
	x := countedInvoices(t)
	html := listHTML(t, x.ui.List("invoices"), x.userCtx("/invoices", "", "u1"))
	if strings.Contains(html, "fui-tab-nav__badge") || !strings.Contains(html, "2 invoices") {
		t.Errorf("a list without TabCounts drew counts on its tabs or lost its header count:\n%s", html)
	}
}
