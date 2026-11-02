package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// buttonTag returns the opening tag of the first <button> carrying
// marker, or fails.
func buttonTag(t *testing.T, h, marker string) string {
	t.Helper()
	for _, tag := range regexp.MustCompile(`<button[^>]*>`).FindAllString(h, -1) {
		if strings.Contains(tag, marker) {
			return tag
		}
	}
	t.Fatalf("no <button> carrying %s in:\n%s", marker, h)
	return ""
}

// classTokens returns the class attribute's tokens of an opening tag.
func classTokens(tag string) []string {
	m := regexp.MustCompile(`\sclass="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return nil
	}
	return strings.Fields(m[1])
}

// The repeater's row Remove and its Add rendered as bare native
// buttons: the class map gave them only fui-repeater__remove /
// fui-repeater__add, which no rule styled as a control, while every
// other kit control wears the button sheet. They take the kit's
// button classes now (the LoadAlways ui-button sheet styles them
// under any marker), and every token they wear is one that sheet
// actually draws.
func TestRepeaterControlsWearKitButtonClasses(t *testing.T) {
	h := string(Repeater(RepeaterConfig{
		Name:  "guests",
		Label: "Guests",
		Items: []render.HTML{render.Text("a"), render.Text("b")},
	}))
	sheet := buttonCSS(style.DefaultTheme())
	for _, c := range []struct {
		marker string
		want   []string
	}{
		{`data-hui-repeater-action="remove"`, []string{"fui-button", "fui-button--ghost", "fui-repeater__remove"}},
		{`data-hui-repeater-action="add"`, []string{"fui-button", "fui-button--secondary", "fui-repeater__add"}},
	} {
		got := classTokens(buttonTag(t, h, c.marker))
		for _, w := range c.want {
			if !containsToken(got, w) {
				t.Errorf("%s: class %q missing from %v", c.marker, w, got)
			}
			if strings.HasPrefix(w, "fui-button") && !strings.Contains(sheet, "."+w+" {") {
				t.Errorf("%s: class %q has no rule in the ui-button sheet", c.marker, w)
			}
		}
	}
}

// The hidden default-submit button headless.Repeater and
// headless.StepWizard render ahead of their visible controls must
// never get a class from a ui class map: a class whose rule sets a
// display (the button sheet's inline-flex) beats the UA's [hidden]
// rule and puts the button back on screen.
func TestDefaultActionTakesNoClass(t *testing.T) {
	for name, m := range map[string]headless.Classes{
		"repeaterClasses":     repeaterClasses,
		"formRepeaterClasses": formRepeaterClasses,
		"stepWizardClasses":   stepWizardClasses,
	} {
		if c := m.Class(headless.PartDefaultAction); c != "" {
			t.Errorf("%s gives PartDefaultAction class %q", name, c)
		}
	}
	rendered := map[string]string{
		"Repeater": string(Repeater(RepeaterConfig{Name: "guests", Items: []render.HTML{render.Text("a")}})),
		"FormRepeater": string(FormRepeater(FormRepeaterConfig{Name: "links",
			Items: [][]render.HTML{{render.Text("a")}}})),
		"StepWizard": string(StepWizard(StepWizardConfig{Action: "/w",
			Steps: []StepWizardStep{{Heading: "One"}, {Heading: "Two"}}})),
	}
	for name, h := range rendered {
		marker := "data-hui-repeater-default"
		if name == "StepWizard" {
			marker = `hidden=""`
		}
		tag := buttonTag(t, h, marker)
		if !strings.Contains(tag, `hidden=""`) {
			t.Errorf("%s: default button lost its hidden attribute: %s", name, tag)
		}
		if toks := classTokens(tag); len(toks) > 0 {
			t.Errorf("%s: default button carries classes %v: %s", name, toks, tag)
		}
	}
}

func containsToken(toks []string, want string) bool {
	for _, t := range toks {
		if t == want {
			return true
		}
	}
	return false
}
