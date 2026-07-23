package ui

import (
	"strings"
	"testing"
)

func TestTextFieldWiresLabelHelpAndTypedAttributes(t *testing.T) {
	h := string(TextField(TextFieldConfig{
		Name: "email", Label: "Email", Value: "a@example.com",
		Placeholder: "you@example.com", AutoComplete: "email",
		Required: true, MinLength: 3, MaxLength: 120, Help: "Work address",
	}))
	for _, want := range []string{
		`for="email"`, `type="text"`, `name="email"`, `id="email"`,
		`value="a@example.com"`, `placeholder="you@example.com"`,
		`autocomplete="email"`, `minlength="3"`, `maxlength="120"`,
		`required`, `aria-describedby="email-help"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("TextField output missing %q: %s", want, h)
		}
	}
}

func TestNumberFieldWiresBoundsAndError(t *testing.T) {
	min, max, step := -10.5, 25.0, 0.5
	h := string(NumberField(NumberFieldConfig{
		Name: "temperature", Label: "Temperature", Value: "3.5",
		Min: &min, Max: &max, Step: &step, Error: "Outside supported range",
	}))
	for _, want := range []string{
		`type="number"`, `min="-10.5"`, `max="25"`, `step="0.5"`,
		`aria-invalid="true"`, `aria-describedby="temperature-error"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("NumberField output missing %q: %s", want, h)
		}
	}
}

func TestDateFieldWiresDateBounds(t *testing.T) {
	h := string(DateField(DateFieldConfig{
		Name: "starts_on", Label: "Starts on", Value: "2026-07-22",
		Min: "2026-01-01", Max: "2026-12-31", Disabled: true,
	}))
	for _, want := range []string{
		`type="date"`, `value="2026-07-22"`, `min="2026-01-01"`,
		`max="2026-12-31"`, `disabled`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("DateField output missing %q: %s", want, h)
		}
	}
}

func TestTypedFieldsRequireNameAndLabel(t *testing.T) {
	for name, render := range map[string]func(){
		"text":   func() { TextField(TextFieldConfig{Label: "Label"}) },
		"number": func() { NumberField(NumberFieldConfig{Name: "n"}) },
		"date":   func() { DateField(DateFieldConfig{}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected invalid typed field config to panic")
				}
			}()
			render()
		})
	}
}
