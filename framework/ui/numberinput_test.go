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
