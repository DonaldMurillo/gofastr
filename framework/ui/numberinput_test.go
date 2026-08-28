package ui

import (
	"strings"
	"testing"
)

func TestNumberInputRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NumberInput without Name should panic")
		}
	}()
	NumberInput(NumberInputConfig{Label: "x"})
}

func TestNumberInputRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NumberInput without Label should panic")
		}
	}()
	NumberInput(NumberInputConfig{Name: "x"})
}

func TestNumberInputEmitsTypeNumber(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{Name: "qty", Label: "Quantity", Value: 3}))
	if !strings.Contains(h, `type="number"`) {
		t.Errorf("expected type=number:\n%s", h)
	}
	if !strings.Contains(h, `value="3"`) {
		t.Errorf("expected initial value=3:\n%s", h)
	}
}

func TestNumberInputEmitsStepperButtons(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{Name: "qty", Label: "Quantity", Step: 5}))
	if !strings.Contains(h, `data-fui-number-step="-5"`) {
		t.Errorf("expected minus button with data-fui-number-step=-5:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-number-step="5"`) {
		t.Errorf("expected plus button with data-fui-number-step=5:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-number-for="qty"`) {
		t.Errorf("expected data-fui-number-for=qty on buttons:\n%s", h)
	}
}

func TestNumberInputEmitsMinMaxWhenSet(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{Name: "qty", Label: "Quantity", Min: 1, Max: 99}))
	if !strings.Contains(h, `min="1"`) {
		t.Errorf("expected min=1:\n%s", h)
	}
	if !strings.Contains(h, `max="99"`) {
		t.Errorf("expected max=99:\n%s", h)
	}
}

func TestNumberInputErrorState(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{
		Name: "qty", Label: "Quantity", Error: "Out of range",
	}))
	if !strings.Contains(h, "is-error") {
		t.Errorf("Error state should add .is-error class:\n%s", h)
	}
	if !strings.Contains(h, `aria-invalid="true"`) {
		t.Errorf("Error state should mark input aria-invalid:\n%s", h)
	}
	if !strings.Contains(h, "Out of range") {
		t.Errorf("Error message should render:\n%s", h)
	}
}

func TestNumberInputAccessibleLabelOnButtons(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{Name: "qty", Label: "Quantity"}))
	if !strings.Contains(h, `aria-label="Decrement Quantity"`) {
		t.Errorf("− button should have aria-label=Decrement <Label>:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Increment Quantity"`) {
		t.Errorf("+ button should have aria-label=Increment <Label>:\n%s", h)
	}
}

// extraAttrsOpeningTag returns the opening tag of the first <tag>
// element in h, attributes included. ExtraAttrs-contract tests pin
// where extras land by inspecting that element.
func extraAttrsOpeningTag(t *testing.T, h string, tag string) string {
	t.Helper()
	i := strings.Index(h, "<"+tag)
	if i < 0 {
		t.Fatalf("no <%s> element in output:\n%s", tag, h)
	}
	end := strings.Index(h[i:], ">")
	if end < 0 {
		t.Fatalf("unterminated <%s> element:\n%s", tag, h)
	}
	return h[i : i+end+1]
}

// ExtraAttrs land on the <input> but never override what the component
// owns (#262): step/value/min/max (and the other owned keys) keep their
// framework values; class/id/data-fui-* case-variants are dropped.
func TestNumberInputExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{
		Name: "qty", Label: "Quantity", Min: 1, Max: 9, Step: 2, Value: 4, Class: "mine",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "step": "evil", "Class": "evil", "data-fui-comp": "spoof",
		},
	}))
	input := extraAttrsOpeningTag(t, h, "input")
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(input, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, input)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `type="number"`, `name="qty"`, `step="2"`, `value="4"`,
		`min="1"`, `max="9"`, `class="ui-number-input__input`,
	} {
		if !strings.Contains(input, want) {
			t.Errorf("input missing %q:\n%s", want, input)
		}
	}
	root := h[:strings.Index(h, ">")+1]
	if !strings.Contains(root, `class="ui-number-input mine"`) {
		t.Errorf("wrapper class should stay framework+caller:\n%s", root)
	}
}
