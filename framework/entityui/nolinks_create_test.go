package entityui

import (
	"strings"
	"testing"
)

// A NoLinks list keeps New: an entity with no record screen can still
// have a create screen, and a create lands back on the list. NoLinks
// takes the record links and the row menu away, nothing else.
func TestNoLinksListKeepsNew(t *testing.T) {
	x := newInvoiceUI(t)
	body := string(x.ui.List("invoices").Base("/rec/invoices").NoLinks().
		RenderCtx(x.userCtx("/rec/invoices", "", "u1")))
	if !strings.Contains(body, `href="/rec/invoices/create"`) {
		t.Errorf("a NoLinks list drew no New:\n%s", body)
	}
	for _, bad := range []string{`href="/rec/invoices/inv-1"`, "fui-menu"} {
		if strings.Contains(body, bad) {
			t.Errorf("a NoLinks list drew %q:\n%s", bad, body)
		}
	}
}
