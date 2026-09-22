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
	// The hooks the headless-controls module resolves the input by,
	// one per button; the step size the module reads off the input's
	// own step attribute, so the two can never disagree.
	if !strings.Contains(h, `data-hui-number-input-decrement=""`) {
		t.Errorf("expected the decrement hook on the minus button:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-number-input-increment=""`) {
		t.Errorf("expected the increment hook on the plus button:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-number-input-for="qty"`) {
		t.Errorf("expected data-hui-number-input-for=qty on buttons:\n%s", h)
	}
	if !strings.Contains(h, `step="5"`) {
		t.Errorf("expected the configured step on the input the module reads:\n%s", h)
	}
}

func TestNumberInputEmitsMinMaxWhenSet(t *testing.T) {
	h := string(NumberInput(NumberInputConfig{Name: "qty", Label: "Quantity", Min: 1, Max: 99, Value: 5}))
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
	// Extras land on the root and the input's own attributes cannot
	// be reached by them: the sanitiser drops every key the component
	// owns and every forged hook, wherever the caller spelled it.
	root := h[:strings.Index(h, ">")+1]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(h, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, h)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `type="number"`, `name="qty"`, `step="2"`, `value="4"`,
		`min="1"`, `max="9"`, `class="fui-number-input__input`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q:\n%s", want, h)
		}
	}
	if !strings.Contains(root, `class="fui-number-input mine"`) {
		t.Errorf("wrapper class should stay framework+caller:\n%s", root)
	}
}
