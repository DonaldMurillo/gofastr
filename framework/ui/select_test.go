package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func requiredSelect() string {
	return string(Select(SelectConfig{
		Name:     "policy",
		Label:    "Policy",
		Required: true,
		Options:  []SelectOption{{Value: "a", Text: "A"}},
	}))
}

// The component root is display: grid, so a marker rendered as a SIBLING of
// the <label> becomes its own grid row: "Policy" on one line, "*" on the
// next. Inside the label it shares the line, matching ui.FormField.
func TestSelectMarkerRendersInsideLabel(t *testing.T) {
	h := requiredSelect()
	marker := strings.Index(h, "ui-form-field__required")
	if marker == -1 {
		t.Fatalf("Required: true rendered no marker:\n%s", h)
	}
	if marker > strings.Index(h, "</label>") {
		t.Fatalf("required marker is a sibling of the <label>, so the grid gives it its own row:\n%s", h)
	}
}

// The marker's red color + spacing rule in formFieldCSS is scoped to
// [data-fui-comp="ui-form-field"]; a page that mounts a Select but never a
// FormField would render the marker unstyled. The component styles what it
// emits.
func TestSelectCSSStylesTheMarker(t *testing.T) {
	if !strings.Contains(selectCSS(style.Theme{}), ".ui-form-field__required") {
		t.Fatal("selectCSS has no rule for .ui-form-field__required — the marker is unstyled unless ui-form-field happens to be on the page")
	}
}

// ExtraAttrs land on the <select> but never override what the component
// owns (#262): name and required keep their framework values.
func TestSelectExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(Select(SelectConfig{
		Name: "country", Label: "Country", Class: "mine", Required: true,
		Options: []SelectOption{{Value: "fr", Text: "France"}},
		ExtraAttrs: map[string]string{
			"data-test": "hook", "name": "evil", "Class": "evil", "data-fui-comp": "spoof",
		},
	}))
	sel := extraAttrsOpeningTag(t, h, "select")
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(sel, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, sel)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `name="country"`, `class="ui-select__input"`, `required=""`,
	} {
		if !strings.Contains(sel, want) {
			t.Errorf("select missing %q:\n%s", want, sel)
		}
	}
}
