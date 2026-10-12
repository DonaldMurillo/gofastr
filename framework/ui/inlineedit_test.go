package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The value is the trigger of a popup holding a one-field form that PUTs
// the field and, saved, returns to the page.
func TestInlineEdit(t *testing.T) {
	h := string(InlineEdit(InlineEditConfig{
		Display: render.Text("Open"),
		Label:   "Edit Status of INV-1",
		Control: render.HTML(`<select name="status"></select>`),
		Action:  "/api/invoices/inv-1",
		Return:  "/invoices?sort=number",
	}))
	for _, want := range []string{
		`<details`, `data-hui-disclosure-dismiss`, ">Open<", ">Edit Status of INV-1<",
		`data-cui-rpc="/api/invoices/inv-1"`, `data-cui-rpc-method="PUT"`,
		`data-cui-rpc-navigate="/invoices?sort=number"`, `<select name="status">`, ">Save<",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("inline edit misses %q:\n%s", want, h)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a cross-origin Return did not panic")
		}
	}()
	InlineEdit(InlineEditConfig{Display: "x", Label: "l", Control: "c", Action: "/a", Return: "//evil.example"})
}
