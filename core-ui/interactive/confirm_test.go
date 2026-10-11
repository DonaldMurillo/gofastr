package interactive

import (
	"maps"
	"testing"
)

func TestWithConfirmDialogEmitsWording(t *testing.T) {
	got := Delete("/api/invoices/1").WithConfirmDialog(Confirm{
		Title:   "Delete this invoice?",
		Message: "This cannot be undone.",
		Accept:  "Delete",
		Danger:  true,
	}).Attrs()
	want := map[string]string{
		"data-cui-rpc":            "/api/invoices/1",
		"data-cui-rpc-method":     "DELETE",
		"data-cui-confirm":        "This cannot be undone.",
		"data-cui-confirm-title":  "Delete this invoice?",
		"data-cui-confirm-accept": "Delete",
		"data-cui-confirm-tone":   "danger",
	}
	if !maps.Equal(map[string]string(got), want) {
		t.Errorf("attrs = %v\nwant %v", got, want)
	}
}

// WithConfirm is the message alone: no title, no accept label, no tone,
// so the kit dialog keeps its own.
func TestWithConfirmIsMessageOnly(t *testing.T) {
	got := Post("/api/x").WithConfirm("Sure?").Attrs()
	if got["data-cui-confirm"] != "Sure?" {
		t.Errorf("data-cui-confirm = %q, want Sure?", got["data-cui-confirm"])
	}
	for _, k := range []string{"data-cui-confirm-title", "data-cui-confirm-accept", "data-cui-confirm-tone"} {
		if _, ok := got[k]; ok {
			t.Errorf("WithConfirm emitted %s: %v", k, got)
		}
	}
}

func TestWithConfirmDialogNeedsMessage(t *testing.T) {
	defer expectPanic(t, "a Confirm with no Message must be refused")
	Post("/api/x").WithConfirmDialog(Confirm{Title: "Sure?", Danger: true})
}
