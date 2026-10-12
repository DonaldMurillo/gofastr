package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

// A confirm dialog's wording (title, accept label, danger tone) rides
// a button or a form only beside the data-cui-confirm it words.
func TestConfirmWordingRidesTheGate(t *testing.T) {
	worded := html.Attrs{
		"data-cui-rpc":            "/invoices/1",
		"data-cui-rpc-method":     "DELETE",
		"data-cui-confirm":        "This cannot be undone.",
		"data-cui-confirm-title":  "Delete this invoice?",
		"data-cui-confirm-accept": "Delete",
		"data-cui-confirm-tone":   "danger",
	}
	got := Button(ButtonProps{Label: "Delete", Action: worded}, nil)
	for k, v := range worded {
		has(t, got, k+`="`+v+`"`, "the button dropped "+k)
	}
	form := Form(FormProps{Action: "/invoices/1", Request: worded}, nil)
	for k, v := range worded {
		has(t, form, k+`="`+v+`"`, "the form dropped "+k)
	}
}

func TestConfirmWordingRefusals(t *testing.T) {
	cases := []struct {
		name  string
		attrs html.Attrs
		say   string
	}{
		{"title with no gate", html.Attrs{"data-cui-confirm-title": "Sure?"}, "no data-cui-confirm"},
		{"accept with no gate", html.Attrs{"data-cui-confirm-accept": "Delete"}, "no data-cui-confirm"},
		{"tone with no gate", html.Attrs{"data-cui-confirm-tone": "danger"}, "no data-cui-confirm"},
		{"empty title", html.Attrs{"data-cui-confirm": "x", "data-cui-confirm-title": ""}, "empty"},
		{"unknown tone", html.Attrs{"data-cui-confirm": "x", "data-cui-confirm-tone": "warning"}, "danger is the only tone"},
	}
	for _, c := range cases {
		for _, seam := range []string{"Action", "Form Request"} {
			t.Run(c.name+"/"+seam, func(t *testing.T) {
				defer func() {
					r := recover()
					if r == nil {
						t.Fatalf("%s rendered %v", seam, c.attrs)
					}
					if !strings.Contains(r.(string), c.say) {
						t.Errorf("the refusal does not say %q: %v", c.say, r)
					}
				}()
				attrs := html.Attrs{"data-cui-rpc": "/x"}
				for k, v := range c.attrs {
					attrs[k] = v
				}
				if seam == "Action" {
					Button(ButtonProps{Label: "x", Action: attrs}, nil)
				} else {
					Form(FormProps{Action: "/x", Request: attrs}, nil)
				}
			})
		}
	}
}

// The gate's own spelling in any case still counts as the gate.
func TestConfirmWordingSeesAFoldedGate(t *testing.T) {
	got := Button(ButtonProps{Label: "x", Action: html.Attrs{
		"data-cui-rpc":           "/x",
		"DATA-CUI-CONFIRM":       "Sure?",
		"data-cui-confirm-title": "Really?",
	}}, nil)
	has(t, got, `data-cui-confirm-title="Really?"`, "a folded gate was not seen")
}
