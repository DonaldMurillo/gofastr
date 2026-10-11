package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
)

// An icon-only trigger draws the more glyph and keeps the label as the
// trigger's accessible name, hidden from sight, with no caret.
func TestMenuIconOnlyTrigger(t *testing.T) {
	h := string(Menu(MenuConfig{Label: "Actions for INV-1", IconOnly: true, Items: []MenuItem{{Label: "Open", Href: "/i/1"}}}))
	for _, want := range []string{
		`fui-menu__trigger fui-menu__trigger--icon`,
		`<span class="fui-visually-hidden" data-cui-internal="">Actions for INV-1</span>`,
		`<circle cx="5" cy="12" r="1"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("icon-only trigger missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "▾") {
		t.Errorf("an icon-only trigger draws no caret:\n%s", h)
	}
}

// Do carries a built interactive.Action onto the row; Copy makes the row
// a copy command.
func TestMenuDoAndCopyRows(t *testing.T) {
	del := interactive.Delete("/api/notes/n1").
		WithConfirm("Delete this note?").
		OnSuccess(interactive.Navigate("/notes")).
		OnErrorToast("Could not delete")
	h := string(Menu(MenuConfig{Label: "Row", Items: []MenuItem{
		{Label: "Copy link", Copy: &MenuCopy{Target: "u-1", Toast: "Link copied"}},
		{Label: "Delete", Danger: true, Do: &del},
	}}))
	for _, want := range []string{
		`data-cui-rpc="/api/notes/n1"`,
		`data-cui-rpc-method="DELETE"`,
		`data-cui-confirm="Delete this note?"`,
		`data-cui-rpc-error-toast="Could not delete"`,
		`data-hui-copy-target="u-1"`,
		`fui-menu__item--danger`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("menu rows missing %q:\n%s", want, h)
		}
	}
	if !strings.Contains(h, `data-cui-rpc-navigate="/notes"`) {
		t.Errorf("the action's navigate effect did not reach the row:\n%s", h)
	}
}
