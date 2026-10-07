package entityui

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var rpcButtonRe = regexp.MustCompile(`<button[^>]*>`)

// recordActionBody finds the header button labelled label and returns
// the JSON body it posts and the path it posts to.
func recordActionBody(t *testing.T, body, label string) (map[string]any, string, string) {
	t.Helper()
	for _, tag := range rpcButtonRe.FindAllString(body, -1) {
		i := strings.Index(body, tag)
		if !strings.Contains(body[i:min(len(body), i+len(tag)+400)], ">"+label+"<") {
			continue
		}
		attr := func(name string) string {
			m := regexp.MustCompile(name + `="([^"]*)"`).FindStringSubmatch(tag)
			if m == nil {
				return ""
			}
			return html.UnescapeString(m[1])
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(attr("data-cui-rpc-body")), &out); err != nil {
			t.Fatalf("button %q posts no JSON body: %v\n%s", label, err, tag)
		}
		return out, attr("data-cui-rpc"), tag
	}
	return nil, "", ""
}

func resendExt(perm string, variant ui.ButtonVariant, ran *[]ActionContext, fail bool) Extensions {
	return Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "resend", Label: "Resend receipt", Variant: variant, Permission: perm,
		Run: func(_ context.Context, ac ActionContext) error {
			*ran = append(*ran, ac)
			if fail {
				return context.Canceled
			}
			return nil
		},
	}}}}}
}

// An action without Bulk is a record header button in its declared
// variant. Its body names the record scope and the one record, and the
// bulk route runs it on that record alone.
func TestRecordActionRendersAndRuns(t *testing.T) {
	var ran []ActionContext
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), resendExt("", ui.ButtonPrimary, &ran, false),
		withAPI(map[string]string{"invoices": "/api/invoices"}))
	page := renderRecord(t, x, "inv-1", nil)
	body, path, tag := recordActionBody(t, page, "Resend receipt")
	if body == nil {
		t.Fatalf("no Resend receipt button on the record:\n%s", page)
	}
	if path != "/api/invoices/_bulk" || !strings.Contains(tag, "fui-button--primary") {
		t.Fatalf("button posts to %q in %s, want the bulk route as a primary button", path, tag)
	}
	if body["scope"] != "record" || body["ids"] != "inv-1" || body["action"] != "run:resend" {
		t.Fatalf("button body = %v", body)
	}
	if list := listHTML(t, x.ui.List("invoices").Bulk(), x.userCtx("/invoices", "", "u1")); strings.Contains(list, "Resend receipt") {
		t.Fatalf("an action without Bulk was offered on the list:\n%s", list)
	}
	code, out := postBulk(t, x, bulkCtx("u1", nil), body)
	if code != http.StatusOK || len(ran) != 1 || !slices.Equal(ran[0].IDs, []string{"inv-1"}) || ran[0].Run == "" {
		t.Fatalf("status %d %v ran=%+v, want one run over inv-1", code, out, ran)
	}
}

// The record scope runs app actions on exactly one readable record, and
// nothing else: not a built-in action, not two ids, not another owner's
// record. An action without Bulk stays off every list scope.
func TestRecordScopeRefusals(t *testing.T) {
	var ran []ActionContext
	x := ownedInvoices(t, resendExt("", "", &ran, false))
	ctx := bulkCtx("u1", nil)
	for name, tc := range map[string]struct {
		body map[string]any
		code int
	}{
		"built-in":      {map[string]any{"action": "delete", "scope": "record", "ids": "a1"}, http.StatusForbidden},
		"two ids":       {map[string]any{"action": "run:resend", "scope": "record", "ids": []string{"a1", "a2"}}, http.StatusUnprocessableEntity},
		"foreign":       {map[string]any{"action": "run:resend", "scope": "record", "ids": "b1"}, http.StatusUnprocessableEntity},
		"list selected": {map[string]any{"action": "run:resend", "scope": "selected", "ids": "a1"}, http.StatusForbidden},
	} {
		if code, out := postBulk(t, x, ctx, tc.body); code != tc.code {
			t.Errorf("%s: status %d %v, want %d", name, code, out, tc.code)
		}
	}
	if len(ran) != 0 {
		t.Fatalf("a refused request ran the action: %+v", ran)
	}
	if got := invoiceIDs(t, x); len(got) != 4 {
		t.Fatalf("a refused request deleted rows: %v", got)
	}
}

// An action's Permission is held by name per record: a caller without it
// sees no button and its post is refused; a failing Run answers an error.
func TestRecordActionPermissionAndFailure(t *testing.T) {
	var ran []ActionContext
	x := newTestUIExt(t, invoiceEntities(), invoiceRows(), resendExt("invoices:resend", "", &ran, true),
		withAPI(map[string]string{"invoices": "/api/invoices"}))
	policy := access.NewRolePolicy()
	if err := policy.Grant("root", access.Wildcard); err != nil {
		t.Fatal(err)
	}
	if err := policy.Grant("clerk", "invoices:resend"); err != nil {
		t.Fatal(err)
	}
	as := func(role string) context.Context {
		return access.WithRoles(access.WithPolicy(x.userCtx("/rec/invoices/inv-1", "", "u1"), policy), []string{role})
	}
	page := string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(as("root")))
	if strings.Contains(page, "Resend receipt") {
		t.Fatalf("a Wildcard role was offered a named-permission action:\n%s", page)
	}
	body := map[string]any{"action": "run:resend", "scope": "record", "ids": "inv-1"}
	if code, _ := postBulk(t, x, bulkCtx("u1", policy, "root"), body); code != http.StatusForbidden || len(ran) != 0 {
		t.Fatalf("Wildcard post: status %d ran=%d, want 403 and no run", code, len(ran))
	}
	page = string(x.ui.Record("invoices", "inv-1").Base("/rec/invoices").RenderCtx(as("clerk")))
	if b, _, _ := recordActionBody(t, page, "Resend receipt"); b == nil {
		t.Fatalf("the named grant was not offered the action:\n%s", page)
	}
	if code, _ := postBulk(t, x, bulkCtx("u1", policy, "clerk"), body); code != http.StatusInternalServerError || len(ran) != 1 {
		t.Fatalf("failing Run: status %d ran=%d, want 500 after one run", code, len(ran))
	}
}

// An entity with bulk off still runs its record actions.
func TestRecordActionWithBulkOff(t *testing.T) {
	var ran []ActionContext
	ents := invoiceEntities()
	inv := ents["invoices"]
	d := *inv.Display
	d.NoBulk = true
	inv.Display = &d
	ents["invoices"] = inv
	x := newTestUIExt(t, ents, invoiceRows(), resendExt("", "", &ran, false), withAPI(map[string]string{"invoices": "/api/invoices"}))
	if code, out := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "run:resend", "scope": "record", "ids": "inv-1"}); code != http.StatusOK || len(ran) != 1 {
		t.Fatalf("status %d %v ran=%d", code, out, len(ran))
	}
	if code, _ := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "delete", "scope": "selected", "ids": "inv-1"}); code != http.StatusNotFound {
		t.Fatalf("a list scope on a bulk-off entity answered %d, want 404", code)
	}
}

// A Variant no Button knows is refused at New, naming the action.
func TestActionVariantChecked(t *testing.T) {
	x := newTestHost(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	var ran []ActionContext
	_, err := New(x.host, resendExt("", ui.ButtonVariant("loud"), &ran, false))
	if err == nil || !strings.Contains(err.Error(), `"resend"`) {
		t.Fatalf("New = %v, want the bad variant refused naming resend", err)
	}
}

// The record header keeps its moves and app actions as buttons and folds
// Copy link, Duplicate and Delete into one icon-only menu named for the
// record, so the header row fits a phone.
func TestRecordHeaderMenu(t *testing.T) {
	x := newInvoiceUI(t)
	page := renderRecord(t, x, "inv-1", func(b *RecordBuilder) { b.Duplicate().Delete() })
	trigger := strings.Index(page, "fui-menu__trigger--icon")
	if trigger < 0 || !strings.Contains(page, "Actions for INV-1") {
		t.Fatalf("no actions menu named for the record on its header:\n%s", page)
	}
	for _, want := range []string{
		`data-hui-copy-target="eui-rec-link"`,
		`href="/rec/invoices/create?duplicate=inv-1"`,
		`data-cui-rpc-method="DELETE"`,
	} {
		if i := strings.Index(page, want); i < trigger {
			t.Errorf("%s is not in the header menu:\n%s", want, page)
		}
	}
	for _, tag := range rpcButtonRe.FindAllString(page, -1) {
		if strings.Contains(tag, `data-cui-rpc-method="DELETE"`) && strings.Contains(tag, "fui-button") {
			t.Errorf("Delete is still a header button: %s", tag)
		}
	}
	if move := strings.Index(page, "/transitions/send"); move < 0 || move > trigger {
		t.Errorf("the move is not a header button before the menu:\n%s", page)
	}
}
