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

// An invalid control inside an InputGroup marks the whole group, the
// way the group already takes the focus ring: the group's border turns
// danger, and the input draws no ring of its own inside it, which left
// the $ prefix outside the red box. The message under it starts with a
// capital, though a server writes it as a fragment ("must be at least 0").
func TestInputGroupInvalidMarksTheGroup(t *testing.T) {
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
			Label: "Amount", For: id, Error: errText,
			Input: func(c headless.FieldControl) render.HTML {
				return ui.InputGroup(ui.InputGroupConfig{
					Prepend: render.Text("$"),
					Input:   ui.Control(ui.ControlConfig{Field: c, Type: "number", Name: id}),
				})
			},
		})
	}
	body := `<div style="padding: 24px; max-inline-size: 30rem">` +
		string(field("bad", "must be at least 0")) + string(field("good", "")) +
		`<span id="danger" style="color: var(--color-danger)"></span></div>`
	for _, scheme := range []string{"light", "dark"} {
		srv := themeTestPageWithHead(t, head, body)
		ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600), prefersScheme(scheme))
		var got map[string]string
		probe := `(() => {
			const g = id => document.getElementById(id).closest(".fui-input-group");
			const cs = (el, pseudo) => getComputedStyle(el, pseudo);
			return {
				danger: cs(document.getElementById("danger")).color,
				badBorder: cs(g("bad")).borderTopColor,
				goodBorder: cs(g("good")).borderTopColor,
				badInputShadow: cs(document.getElementById("bad")).boxShadow,
				invalid: document.getElementById("bad").getAttribute("aria-invalid") || "",
				errorCase: cs(document.getElementById("bad").closest(".fui-field").querySelector(".fui-field__error"), "::first-letter").textTransform,
			};
		})()`
		if err := chromedp.Run(ctx, chromedp.Evaluate(probe, &got)); err != nil {
			t.Fatalf("probe: %v", err)
		}
		if got["invalid"] != "true" {
			t.Fatalf("the fixture's control is not invalid: %v", got)
		}
		if got["badBorder"] != got["danger"] {
			t.Errorf("%s: the invalid group's border is %s, want danger %s", scheme, got["badBorder"], got["danger"])
		}
		if got["goodBorder"] == got["danger"] {
			t.Errorf("%s: a valid group drew the danger border", scheme)
		}
		if got["errorCase"] != "uppercase" {
			t.Errorf("%s: the error message keeps its lowercase start: %v", scheme, got)
		}
		if got["badInputShadow"] != "none" {
			t.Errorf("%s: the input inside the group draws its own ring: %s", scheme, got["badInputShadow"])
		}
	}
}
