package entityui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The bar offers a move only to a caller holding its Permission by name,
// as the record screen does: one who may update but not send is never
// offered "Move: send", and posting it is refused.
func TestBulkMoveNeedsMovePermission(t *testing.T) {
	x, _, policy := guardedInvoices(t, entity.AccessControl{Update: "invoices:update"}, 0, Extensions{}, func(ents map[string]entity.EntityConfig, _ map[string][]map[string]any) {
		ents["invoices"].States.Transitions[0].Permission = "invoices:send"
	})
	policy.Register("invoices:send")
	grant(t, policy, "editor", "invoices:update")
	grant(t, policy, "sender", "invoices:update", access.Permission("invoices:send"))
	page := listHTML(t, x.ui.List("invoices").Bulk(), pageCtx("/invoices", policy, "u1", "editor"))
	if strings.Contains(page, `value="move:send"`) {
		t.Errorf("the bar offered a move the caller may not make:\n%s", page)
	}
	if code, out := postBulk(t, x, bulkCtx("u1", policy, "editor"), map[string]any{"action": "move:send", "scope": "selected", "ids": "inv-1"}); code != http.StatusForbidden {
		t.Fatalf("status %d: %v, want 403", code, out)
	}
	if page := listHTML(t, x.ui.List("invoices").Bulk(), pageCtx("/invoices", policy, "u1", "sender")); !strings.Contains(page, `value="move:send"`) {
		t.Errorf("the bar hid a move the caller holds:\n%s", page)
	}
}
