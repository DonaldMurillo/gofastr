package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestCheckboxRequiresName(t *testing.T) {
	defer func() { recover() }()
	Checkbox(ToggleConfig{Label: "x"})
	t.Fatal("expected panic with empty Name")
}

func TestCheckboxRequiresLabel(t *testing.T) {
	defer func() { recover() }()
	Checkbox(ToggleConfig{Name: "n"})
	t.Fatal("expected panic with empty Label")
}

func TestCheckboxRendersAssociatedLabel(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "notify", Label: "Email me"})
	for _, want := range []string{
		`data-fui-comp="ui-toggle"`,
		`type="checkbox"`,
		`name="notify"`,
		`id="notify"`,
		`for="notify"`,
		"Email me",
		"ui-toggle--checkbox",
	} {
		mustContain(t, h, want)
	}
}

func TestCheckboxCheckedAndDisabled(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Checked: true, Disabled: true})
	mustContain(t, h, "checked")
	mustContain(t, h, "disabled")
	mustContain(t, h, "is-disabled")
}

func TestCheckboxErrorWiresAriaAndAlert(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Error: "Must agree"})
	mustContain(t, h, `aria-invalid="true"`)
	mustContain(t, h, `aria-describedby="n-error"`)
	mustContain(t, h, `role="alert"`)
	mustContain(t, h, "Must agree")
	mustContain(t, h, "is-error")
}

func TestCheckboxHelpAddsAriaDescribedBy(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Help: "Optional"})
	mustContain(t, h, `aria-describedby="n-help"`)
	mustContain(t, h, "Optional")
}

func TestRadioRequiresValue(t *testing.T) {
	defer func() { recover() }()
	Radio(ToggleConfig{Name: "n", Label: "x"})
	t.Fatal("expected panic without Value")
}

func TestRadioEmitsCorrectType(t *testing.T) {
	h := Radio(ToggleConfig{Name: "color", Value: "red", Label: "Red"})
	mustContain(t, h, `type="radio"`)
	mustContain(t, h, `value="red"`)
	mustContain(t, h, "ui-toggle--radio")
}

func TestSwitchEmitsSwitchModifier(t *testing.T) {
	h := Switch(ToggleConfig{Name: "wifi", Label: "Wi-Fi"})
	mustContain(t, h, "ui-toggle--switch")
	mustContain(t, h, `type="checkbox"`)
}

func TestToggleCustomIDOverridesName(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", ID: "custom"})
	mustContain(t, h, `id="custom"`)
	mustContain(t, h, `for="custom"`)
	if strings.Contains(string(h), `id="n"`) {
		t.Fatalf("custom ID should override Name as id:\n%s", h)
	}
}

func TestToggleRequiredAttribute(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Required: true})
	mustContain(t, h, "required")
}

// Same defect class as ui.Select (#188): .ui-toggle-group is display: grid,
// so a marker joined as a SIBLING of the <legend> gets its own grid row.
func TestRadioGroupMarkerInsideLegend(t *testing.T) {
	h := string(RadioGroup(RadioGroupConfig{
		Name: "plan", Legend: "Plan", Required: true,
		Options: []RadioGroupOption{{Value: "a", Label: "A"}},
	}))
	marker := strings.Index(h, "ui-form-field__required")
	if marker == -1 {
		t.Fatalf("Required: true rendered no marker:\n%s", h)
	}
	if marker > strings.Index(h, "</legend>") {
		t.Fatalf("required marker is a sibling of the <legend>, so the grid gives it its own row:\n%s", h)
	}
}

func TestCheckboxGroupMarkerInsideLegend(t *testing.T) {
	h := string(CheckboxGroup(CheckboxGroupConfig{
		Name: "tags", Legend: "Tags", Required: true,
		Options: []CheckboxGroupOption{{Value: "a", Label: "A"}},
	}))
	marker := strings.Index(h, "ui-form-field__required")
	if marker == -1 {
		t.Fatalf("Required: true rendered no marker:\n%s", h)
	}
	if marker > strings.Index(h, "</legend>") {
		t.Fatalf("required marker is a sibling of the <legend>, so the grid gives it its own row:\n%s", h)
	}
}

// The marker's styling lives in formFieldCSS scoped to ui-form-field; the
// toggle groups emit the class, so their stylesheet must style it too.
func TestToggleCSSStylesTheMarker(t *testing.T) {
	if !strings.Contains(toggleCSS(style.Theme{}), ".ui-form-field__required") {
		t.Fatal("toggleCSS has no rule for .ui-form-field__required — group markers are unstyled unless ui-form-field happens to be on the page")
	}
}

func TestToggleExtraAttrsOnRoot(t *testing.T) {
	h := Checkbox(ToggleConfig{
		Name: "n", Label: "x",
		ExtraAttrs: map[string]string{"data-test": "hook", "for": "evil"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root label missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `for="n"`) {
		t.Errorf("owned for= must win over ExtraAttrs:\n%s", root)
	}
	if !strings.Contains(string(h), `type="checkbox"`) {
		t.Errorf("input type lost:\n%s", h)
	}
}

func TestRadioGroupExtraAttrsOnRoot(t *testing.T) {
	h := RadioGroup(RadioGroupConfig{
		Name:       "plan",
		Legend:     "Plan",
		Options:    []RadioGroupOption{{Value: "pro", Label: "Pro"}},
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("fieldset missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `role="radiogroup"`) {
		t.Errorf("owned role lost:\n%s", root)
	}
}

func TestCheckboxGroupExtraAttrsOnRoot(t *testing.T) {
	h := CheckboxGroup(CheckboxGroupConfig{
		Name:       "feats",
		Legend:     "Features",
		Options:    []CheckboxGroupOption{{Value: "a", Label: "A"}},
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("fieldset missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `role="group"`) {
		t.Errorf("owned role lost:\n%s", root)
	}
}
