package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A password field the server refuses after a submit turns red like
// one rendered with its error: the form-errors runtime marks only the
// input aria-invalid, and the input is borderless inside its shell, so
// the shell has to take the state from it.
func TestPasswordInvalidAfterSubmitMarksShell(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	field := func(id, errText string) render.HTML {
		return ui.FormField(ui.FormFieldConfig{
			Label: "Password", For: id, Error: errText,
			Input: func(c headless.FieldControl) render.HTML {
				return ui.PasswordInput(ui.PasswordInputConfig{Name: id, ID: id, Field: c})
			},
		})
	}
	body := `<div style="padding: 24px; max-inline-size: 30rem">` +
		string(field("ssr", "is required")) + string(field("late", "")) + string(field("good", "")) +
		`<span id="danger" style="color: var(--color-danger)"></span></div>`
	for _, scheme := range []string{"light", "dark"} {
		srv := themeTestPageWithHead(t, head, body)
		ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600), prefersScheme(scheme))
		var got map[string]string
		probe := `(() => {
			document.getElementById("late").setAttribute("aria-invalid", "true");
			const shell = id => getComputedStyle(document.getElementById(id).closest(".fui-password")).borderTopColor;
			return {
				danger: getComputedStyle(document.getElementById("danger")).color,
				ssr: shell("ssr"), late: shell("late"), good: shell("good"),
			};
		})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
			t.Fatalf("probe: %v", err)
		}
		if got["ssr"] != got["danger"] {
			t.Errorf("%s: a field rendered with its error is not red: %v", scheme, got)
		}
		if got["late"] != got["danger"] {
			t.Errorf("%s: a field the runtime marked invalid keeps a %s border, want %s", scheme, got["late"], got["danger"])
		}
		if got["good"] == got["danger"] {
			t.Errorf("%s: a valid field drew the danger border", scheme)
		}
	}
}
