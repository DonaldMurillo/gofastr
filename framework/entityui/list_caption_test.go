package entityui

import (
	"strings"
	"testing"
)

// A list's table is named by its heading, so two lists on one page (the
// admin's Needs attention panel) are two differently named regions.
func TestListTableNamedByHeading(t *testing.T) {
	x := newInvoiceUI(t)
	for heading, b := range map[string]*ListBuilder{
		"Overdue invoices": x.ui.List("invoices").Heading("Overdue invoices", 3),
		"Invoices":         x.ui.List("invoices"),
	} {
		page := listHTML(t, b, x.userCtx("/invoices", "", "u1"))
		if !strings.Contains(page, ">"+heading+"</caption>") {
			t.Errorf("the table is not named %q:\n%s", heading, page)
		}
		if !strings.Contains(page, `<div aria-labelledby=`) {
			t.Errorf("the scroll region is not labelled by the caption:\n%s", page)
		}
	}
}
