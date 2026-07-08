package html

import (
	"strings"
	"testing"
)

// ============================================================================
// Checkbox
// ============================================================================

func TestCheckbox(t *testing.T) {
	t.Run("renders type checkbox with name, value, id", func(t *testing.T) {
		h := Checkbox(CheckboxConfig{
			Name:  "accept",
			Value: "yes",
			ID:    "accept-terms",
		})
		assertContains(t, h, `<input`)
		assertContains(t, h, `type="checkbox"`)
		assertContains(t, h, `name="accept"`)
		assertContains(t, h, `value="yes"`)
		assertContains(t, h, `id="accept-terms"`)
		assertNotContains(t, h, `checked`)
		assertNotContains(t, h, `</input>`)
	})

	t.Run("renders checked attribute", func(t *testing.T) {
		h := Checkbox(CheckboxConfig{
			Name:    "newsletter",
			Checked: true,
		})
		assertContains(t, h, `type="checkbox"`)
		assertContains(t, h, `checked="checked"`)
	})

	t.Run("omits value when empty", func(t *testing.T) {
		h := Checkbox(CheckboxConfig{Name: "flag"})
		assertContains(t, h, `name="flag"`)
		assertNotContains(t, h, `value=`)
	})

	t.Run("applies class", func(t *testing.T) {
		h := Checkbox(CheckboxConfig{Name: "opt", Class: "form-check"})
		assertContains(t, h, `class="form-check"`)
	})

	t.Run("panics without Name", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic for Checkbox without Name")
			}
			if !strings.Contains(r.(string), "Checkbox requires Name") {
				t.Errorf("unexpected panic message: %v", r)
			}
		}()
		Checkbox(CheckboxConfig{})
	})
}

// ============================================================================
// Radio
// ============================================================================

func TestRadio(t *testing.T) {
	t.Run("renders type radio with name, value, id", func(t *testing.T) {
		h := Radio(RadioConfig{
			Name:  "color",
			Value: "red",
			ID:    "color-red",
		})
		assertContains(t, h, `<input`)
		assertContains(t, h, `type="radio"`)
		assertContains(t, h, `name="color"`)
		assertContains(t, h, `value="red"`)
		assertContains(t, h, `id="color-red"`)
		assertNotContains(t, h, `checked`)
		assertNotContains(t, h, `</input>`)
	})

	t.Run("renders checked attribute", func(t *testing.T) {
		h := Radio(RadioConfig{
			Name:    "size",
			Value:   "lg",
			Checked: true,
		})
		assertContains(t, h, `type="radio"`)
		assertContains(t, h, `checked="checked"`)
	})

	t.Run("applies class", func(t *testing.T) {
		h := Radio(RadioConfig{Name: "plan", Value: "pro", Class: "radio-btn"})
		assertContains(t, h, `class="radio-btn"`)
	})

	t.Run("panics without Name", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic for Radio without Name")
			}
			if !strings.Contains(r.(string), "Radio requires Name") {
				t.Errorf("unexpected panic message: %v", r)
			}
		}()
		Radio(RadioConfig{Value: "val"})
	})

	t.Run("panics without Value", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected panic for Radio without Value")
			}
			if !strings.Contains(r.(string), "Radio requires Value") {
				t.Errorf("unexpected panic message: %v", r)
			}
		}()
		Radio(RadioConfig{Name: "color"})
	})
}

func TestButtonRequiresLabelOrAriaLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Button with no Label and no aria-label must panic — it renders an inert, unnamed <button>")
		}
	}()
	Button(ButtonConfig{})
}

func TestButtonIconOnlyWithAriaLabelAllowed(t *testing.T) {
	h := string(Button(ButtonConfig{ExtraAttrs: Attrs{"aria-label": "Close"}}))
	if !strings.Contains(h, `aria-label="Close"`) {
		t.Errorf("icon-only button should keep the supplied aria-label:\n%s", h)
	}
}
