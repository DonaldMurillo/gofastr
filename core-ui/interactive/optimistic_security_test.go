package interactive

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestOptimisticRejectsLostOptions(t *testing.T) {
	base := Delete("/api/invoices/42")
	cases := map[string]Action{
		"confirm":       base.WithConfirm("Cannot be undone"),
		"dialog":        base.WithConfirmDialog(Confirm{Message: "Cannot be undone", Danger: true}),
		"body":          base.WithBody(`{"force":true}`),
		"error toast":   base.OnErrorToast(""),
		"success toast": base.OnSuccessToast(""),
		"toast action":  base.OnSuccessToast("Deleted").OnSuccessToastAction("Undo", Post("/restore")),
		"effect":        base.OnSuccess(Navigate("/invoices")),
	}
	for name, action := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("rendered a destructive action after dropping its options")
				}
			}()
			OptimisticUpdate(action, render.Text("Delete"), render.Text("Deleted"))
		})
	}
}
