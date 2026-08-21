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
