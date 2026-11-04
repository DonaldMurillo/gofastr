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

func TestOptimisticNamesFirstLostOption(t *testing.T) {
	action := Delete("/api/invoices/42").WithConfirm("Sure?").WithBody(`{}`)
	want := ""
	for range 50 {
		got := func() (msg string) {
			defer func() { msg, _ = recover().(string) }()
			OptimisticUpdate(action, render.Text("Delete"), render.Text("Deleted"))
			return ""
		}()
		if want == "" {
			want = got
		}
		if got != want {
			t.Fatalf("panic names a different option per run: %q then %q", want, got)
		}
	}
}
