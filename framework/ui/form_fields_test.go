package ui

import (
	"github.com/DonaldMurillo/gofastr/core/render"
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
		`required`, `aria-describedby="email-hint"`,
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

// A DateTimeField is a native datetime-local input; Step is in seconds
// and a zero Step leaves the browser's minute default.
func TestDateTimeFieldWiresBoundsAndStep(t *testing.T) {
	h := string(DateTimeField(DateTimeFieldConfig{
		Name: "paid_at", Label: "Paid at", Value: "2026-07-22T09:30",
		Min: "2026-01-01T00:00", Max: "2026-12-31T23:59", Step: 1,
	}))
	for _, want := range []string{
		`type="datetime-local"`, `value="2026-07-22T09:30"`,
		`min="2026-01-01T00:00"`, `max="2026-12-31T23:59"`, `step="1"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("DateTimeField output missing %q: %s", want, h)
		}
	}
	if h := string(DateTimeField(DateTimeFieldConfig{Name: "at", Label: "At"})); strings.Contains(h, "step=") {
		t.Errorf("zero Step drew a step: %s", h)
	}
}

func TestTypedFieldsRequireNameAndLabel(t *testing.T) {
	for name, render := range map[string]func(){
		"text":   func() { TextField(TextFieldConfig{Label: "Label"}) },
		"number": func() { NumberField(NumberFieldConfig{Name: "n"}) },
		"date":   func() { DateField(DateFieldConfig{}) },
		"time":   func() { DateTimeField(DateTimeFieldConfig{Name: "at"}) },
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

// inputOpenTag is the first <input ...> open tag in h.
func inputOpenTag(t *testing.T, h string) string {
	t.Helper()
	start := strings.Index(h, "<input")
	if start < 0 {
		t.Fatalf("no input element:\n%s", h)
	}
	return h[start : start+strings.Index(h[start:], ">")+1]
}

// A typed field's ExtraAttrs land on its input, beside the owned
// attributes they may not displace.
func TestTypedFieldExtraAttrsOnInput(t *testing.T) {
	hook := map[string]string{"data-test": "hook"}
	for _, c := range []struct {
		name string
		h    render.HTML
		owns []string
	}{
		{"text", TextField(TextFieldConfig{Name: "email", Label: "Email", ExtraAttrs: hook}), []string{`type="text"`}},
		{"number", NumberField(NumberFieldConfig{Name: "qty", Label: "Qty", ExtraAttrs: hook}), []string{`type="number"`, `name="qty"`}},
		{"date", DateField(DateFieldConfig{Name: "when", Label: "When", ExtraAttrs: hook}), []string{`type="date"`}},
	} {
		open := inputOpenTag(t, string(c.h))
		if !strings.Contains(open, `data-test="hook"`) {
			t.Errorf("%s: input missing data-test:\n%s", c.name, open)
		}
		for _, w := range c.owns {
			if !strings.Contains(open, w) {
				t.Errorf("%s: owned %s lost:\n%s", c.name, w, open)
			}
		}
	}
}

// A JSON text area carries the check's hook with its sentence.
func TestTextAreaJSONCheck(t *testing.T) {
	h := string(TextArea(TextAreaConfig{Name: "meta", Label: "Meta", JSON: true}))
	if !strings.Contains(h, `data-hui-json="Enter valid JSON"`) {
		t.Errorf("no JSON hook: %s", h)
	}
	if plain := string(TextArea(TextAreaConfig{Name: "notes", Label: "Notes"})); strings.Contains(plain, "data-hui-json") {
		t.Errorf("a plain text area checks JSON: %s", plain)
	}
}

// Each typed field takes LabelHidden: the label stays for assistive
// tech, the root carries the modifier the field sheet hides it by.
func TestTypedFieldsLabelHidden(t *testing.T) {
	for name, h := range map[string]render.HTML{
		"text":     TextField(TextFieldConfig{Name: "a", Label: "A", LabelHidden: true}),
		"number":   NumberField(NumberFieldConfig{Name: "a", Label: "A", LabelHidden: true}),
		"date":     DateField(DateFieldConfig{Name: "a", Label: "A", LabelHidden: true}),
		"datetime": DateTimeField(DateTimeFieldConfig{Name: "a", Label: "A", LabelHidden: true}),
	} {
		if s := string(h); !strings.Contains(s, "fui-field--label-hidden") || !strings.Contains(s, ">A</label>") {
			t.Errorf("%s: no hidden-label modifier, or the label is gone:\n%s", name, s)
		}
	}
	if strings.Contains(string(TextField(TextFieldConfig{Name: "a", Label: "A"})), "label-hidden") {
		t.Error("a plain field carries the hidden-label modifier")
	}
}
