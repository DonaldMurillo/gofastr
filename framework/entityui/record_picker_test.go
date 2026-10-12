package entityui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A relation field is a Picker: the hidden input submits the stored id,
// the search input shows the record's title, and searches post to the
// entity's picker endpoint for that field.
func TestRecordRelationIsAPicker(t *testing.T) {
	x := newInvoiceUI(t)
	body := renderRecord(t, x, "inv-1", nil)
	for _, want := range []string{
		`data-hui-combobox-pick=""`,
		`name="customer_id" type="hidden" value="cus-1"`,
		`value="Acme"`,
		`data-cui-rpc="/api/invoices/_pick?field=customer_id"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the relation field misses %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, `<select id="eui-f-customer_id"`) {
		t.Errorf("the relation field is still a select")
	}
}

// pickPost posts a picker search for invoices.<field> as u1.
func pickPost(t *testing.T, x *testUI, field, body string, mut func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/_pick?field="+field, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(x.userCtx("/api/invoices/_pick?field="+field, "", "u1"))
	if mut != nil {
		mut(req)
	}
	rec := httptest.NewRecorder()
	x.ui.PickerHandler("invoices").ServeHTTP(rec, req)
	return rec
}

func manyCustomers(n int) map[string][]map[string]any {
	rows := invoiceRows()
	for i := range n {
		rows["customers"] = append(rows["customers"], map[string]any{"id": fmt.Sprintf("cus-%03d", i+100), "name": fmt.Sprintf("Customer %03d", i)})
	}
	return rows
}

func searchableInvoices() map[string]entity.EntityConfig {
	ents := invoiceEntities()
	c := ents["customers"]
	c.SearchFields = []string{"name"}
	ents["customers"] = c
	return ents
}

// A search answers the matching records as picker rows, never cached.
func TestPickerHandlerSearches(t *testing.T) {
	x := newTestUI(t, searchableInvoices(), manyCustomers(3), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	rec := pickPost(t, x, "customer_id", `{"q":"acm"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	h := rec.Body.String()
	if !strings.Contains(h, `data-value="cus-1"`) || !strings.Contains(h, `data-label="Acme"`) || strings.Contains(h, "Customer 000") {
		t.Errorf("search rows:\n%s", h)
	}
	if !strings.Contains(h, `id="eui-f-customer_id-listbox-opt-0"`) {
		t.Errorf("rows do not carry the field's listbox ids:\n%s", h)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", rec.Header().Get("Cache-Control"))
	}
}

// Past the row cap the answer says the list is cut off.
func TestPickerHandlerSaysWhenCutOff(t *testing.T) {
	x := newTestUI(t, searchableInvoices(), manyCustomers(pickerRows+5), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	h := pickPost(t, x, "customer_id", `{"q":""}`, nil).Body.String()
	if n := strings.Count(h, `role="option"`); n != pickerRows+1 {
		t.Errorf("want %d rows and a note, got %d:\n%s", pickerRows, n, h)
	}
	if !strings.Contains(h, `aria-disabled="true"`) || !strings.Contains(h, "Type to find others") {
		t.Errorf("no cut-off note:\n%s", h)
	}
	few := pickPost(t, x, "customer_id", `{"q":"Customer 00"}`, nil).Body.String()
	if strings.Contains(few, "Type to find others") {
		t.Errorf("a short answer carries the cut-off note:\n%s", few)
	}
}

// Without SearchFields a search matches record titles.
func TestPickerHandlerMatchesTitlesWithoutSearchFields(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), manyCustomers(3), withAPI(map[string]string{"invoices": "/api/invoices", "customers": "/api/customers"}))
	h := pickPost(t, x, "customer_id", `{"q":"customer 001"}`, nil).Body.String()
	if !strings.Contains(h, `data-label="Customer 001"`) || strings.Contains(h, "Acme") {
		t.Errorf("title match:\n%s", h)
	}
}

// The endpoint refuses what is not a picker search: another method, a
// cross-site post, a field that is not a relation, an oversized body.
func TestPickerHandlerRefusals(t *testing.T) {
	x := newInvoiceUI(t)
	cases := map[string]struct {
		field, body string
		mut         func(*http.Request)
		want        int
	}{
		"get":          {"customer_id", `{}`, func(r *http.Request) { r.Method = http.MethodGet }, http.StatusMethodNotAllowed},
		"cross-site":   {"customer_id", `{}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, http.StatusForbidden},
		"not relation": {"number", `{}`, nil, http.StatusNotFound},
		"unknown":      {"nope", `{}`, nil, http.StatusNotFound},
		"too big":      {"customer_id", `{"q":"` + strings.Repeat("a", 8<<10) + `"}`, nil, http.StatusRequestEntityTooLarge},
		"bad json":     {"customer_id", `{`, nil, http.StatusBadRequest},
	}
	for name, c := range cases {
		if got := pickPost(t, x, c.field, c.body, c.mut).Code; got != c.want {
			t.Errorf("%s: status %d, want %d", name, got, c.want)
		}
	}
}

// SECURITY: a caller who may not read the related entity learns nothing
// about its records, and an anonymous caller is refused the host entity.
func TestPickerHandlerReadGates(t *testing.T) {
	x := postsWithGatedAuthor(t, false)
	req := httptest.NewRequest(http.MethodPost, "/api/posts/_pick?field=author_id", strings.NewReader(`{"q":""}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(x.ctx("/api/posts/_pick?field=author_id", ""))
	rec := httptest.NewRecorder()
	x.ui.PickerHandler("posts").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Jane") {
		t.Fatalf("SECURITY: a gated relation answered %d:\n%s", rec.Code, rec.Body)
	}
	inv := newInvoiceUI(t)
	req = httptest.NewRequest(http.MethodPost, "/api/invoices/_pick?field=customer_id", strings.NewReader(`{"q":""}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(inv.ctx("/api/invoices/_pick?field=customer_id", ""))
	rec = httptest.NewRecorder()
	inv.ui.PickerHandler("invoices").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Acme") {
		t.Fatalf("SECURITY: an anonymous picker search answered %d:\n%s", rec.Code, rec.Body)
	}
}

// SECURITY: a caller refused the host entity learns nothing through its
// picker, even when the related entity is open to them.
func TestPickerHandlerHostGate(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{
			"users": {Fields: fields(schema.Field{Name: "name", Type: schema.String}), Exposure: &entity.ExposureConfig{Public: true}},
			"posts": {Fields: fields(
				schema.Field{Name: "title", Type: schema.String},
				schema.Field{Name: "author_id", Type: schema.Relation, To: "users"},
			)},
		},
		map[string][]map[string]any{"users": {{"id": "usr-1", "name": "Jane Author"}}},
		withAPI(map[string]string{"posts": "/api/posts", "users": "/api/users"}),
	)
	req := httptest.NewRequest(http.MethodPost, "/api/posts/_pick?field=author_id", strings.NewReader(`{"q":""}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(x.ctx("/api/posts/_pick?field=author_id", ""))
	rec := httptest.NewRecorder()
	x.ui.PickerHandler("posts").ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || strings.Contains(rec.Body.String(), "Jane") {
		t.Fatalf("SECURITY: a refused host answered %d:\n%s", rec.Code, rec.Body)
	}
}

var _ = context.Background
