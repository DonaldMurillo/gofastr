package entityui

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

var tagRe = regexp.MustCompile(`<(?:button|a)[^>]*>`)

// tagPosting is the first button or link whose attributes name path,
// and its offset in page.
func tagPosting(page, path string) (string, int) {
	for _, loc := range tagRe.FindAllStringIndex(page, -1) {
		tag := page[loc[0]:loc[1]]
		if strings.Contains(tag, path) {
			return html.UnescapeString(tag), loc[0]
		}
	}
	return "", -1
}

// A danger move is never a header button beside Save: it sits in the
// record's menu, above Delete, drawn as a danger item, and runs only
// after the kit's confirm names the move and where it lands.
func TestDangerMoveConfirmsFromMenu(t *testing.T) {
	ents := invoiceEntities()
	inv := ents["invoices"]
	st := *inv.States
	st.Transitions = append(append([]entity.Transition(nil), st.Transitions...),
		entity.Transition{Key: "void", Label: "Void", From: []string{"draft"}, To: "paid", Variant: "danger"})
	inv.States = &st
	ents["invoices"] = inv
	x := newTestUI(t, ents, invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	page := renderRecord(t, x, "inv-1", func(b *RecordBuilder) { b.Delete() })

	trigger := strings.Index(page, "fui-menu__trigger--icon")
	tag, at := tagPosting(page, "/transitions/void")
	if at < 0 {
		t.Fatalf("no control posts the danger move:\n%s", page)
	}
	if strings.Contains(tag, "fui-button") || at < trigger {
		t.Fatalf("the danger move is a header button, not a menu item: %s", tag)
	}
	for _, want := range []string{
		`data-cui-confirm-title="Void this invoice?"`,
		`data-cui-confirm="It moves from Draft to Paid."`,
		`data-cui-confirm-accept="Void invoice"`,
		`data-cui-confirm-tone="danger"`,
		"danger",
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("danger move item missing %s: %s", want, tag)
		}
	}
	if _, del := tagPosting(page, `data-cui-rpc-method="DELETE"`); del < at {
		t.Errorf("the danger move does not sit above Delete")
	}
	if send, _ := tagPosting(page, "/transitions/send"); !strings.Contains(send, "fui-button") {
		t.Errorf("a plain move left the header: %s", send)
	}
}

// A danger app action takes the same road: a menu item behind a confirm
// naming the action and the record.
func TestDangerActionConfirmsFromMenu(t *testing.T) {
	x := newTestHost(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	var ran []ActionContext
	u, err := New(x.host, resendExt("", ui.ButtonDanger, &ran, false))
	if err != nil {
		t.Fatal(err)
	}
	x.ui = u
	page := renderRecord(t, x, "inv-1", nil)
	trigger := strings.Index(page, "fui-menu__trigger--icon")
	tag, at := tagPosting(page, "/api/invoices/_bulk")
	if at < 0 || strings.Contains(tag, "fui-button") || at < trigger {
		t.Fatalf("the danger action is not a menu item: %s\n%s", tag, page)
	}
	for _, want := range []string{
		`data-cui-confirm-title="Resend receipt?"`,
		`data-cui-confirm="It runs on INV-1."`,
		`data-cui-confirm-tone="danger"`,
	} {
		if !strings.Contains(tag, want) {
			t.Errorf("danger action item missing %s: %s", want, tag)
		}
	}
}
