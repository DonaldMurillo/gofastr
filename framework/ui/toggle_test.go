package ui

import (
	"strings"
	"testing"
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
