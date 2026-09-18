package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// Control is the styled native control for the types the typed fields
// do not name. The contract under test: the field's wiring (id,
// described-by, invalid, required) arrives on the input by
// construction, and the typed attributes (type, value, autocomplete,
// length constraints, numeric bounds) ride with it.
func TestControlCarriesWiringAndTypedAttributes(t *testing.T) {
	fc := headless.FieldControl{
		ID: "ctl-email", DescribedBy: "ctl-email-hint", Invalid: true, Required: true,
	}
	h := string(Control(ControlConfig{
		Field: fc, Type: "email", Name: "email", Value: "a@b.c",
		AutoComplete: "email", MinLength: 3, MaxLength: 120,
	}))
	for _, want := range []string{
		`type="email"`, `name="email"`, `id="ctl-email"`, `value="a@b.c"`,
		`autocomplete="email"`, `minlength="3"`, `maxlength="120"`,
		`aria-describedby="ctl-email-hint"`, `aria-invalid="true"`, `required=""`,
		`class="fui-input"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("Control missing %q:\n%s", want, h)
		}
	}
}

// The numeric and date bounds travel through headless's Owned seam,
// which is the only way in: a caller's ExtraAttrs cannot widen a
// bound behind the config's back.
func TestControlCarriesOwnedBounds(t *testing.T) {
	h := string(Control(ControlConfig{
		Field: headless.FieldControl{ID: "n"},
		Type:  "number", Name: "n", Min: "0", Max: "10", Step: "0.5",
	}))
	for _, want := range []string{`min="0"`, `max="10"`, `step="0.5"`} {
		if !strings.Contains(h, want) {
			t.Errorf("Control bounds missing %q:\n%s", want, h)
		}
	}
	// A bound the caller tries to smuggle through ExtraAttrs is
	// dropped: Owned is the seam, and it is closed.
	h2 := string(Control(ControlConfig{
		Field: headless.FieldControl{ID: "n2"},
		Type:  "number", Name: "n2",
		ExtraAttrs: map[string]string{"max": "999"},
	}))
	if strings.Contains(h2, `max="999"`) {
		t.Errorf("a caller's ExtraAttrs widened a bound:\n%s", h2)
	}
}

// The id and the required flag come from the field, never from the
// config: two sources for one fact is how they drift.
func TestControlTakesTheIDAndRequiredFromTheField(t *testing.T) {
	h := string(Control(ControlConfig{
		Field: headless.FieldControl{ID: "from-field", Required: true},
		Type:  "text", Name: "t",
	}))
	if !strings.Contains(h, `id="from-field"`) || !strings.Contains(h, `required=""`) {
		t.Errorf("the field's id/required did not arrive:\n%s", h)
	}
}

func TestControlRequiresTheFieldAndName(t *testing.T) {
	mustPanic := func(why string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected a panic", why)
			}
		}()
		fn()
	}
	mustPanic("no Field", func() {
		Control(ControlConfig{Type: "text", Name: "t"})
	})
	// A PARTIAL FieldControl must not slip through either: Required
	// with no ID renders a control the label points at and never
	// reaches — the guard names Field.ID, not the empty-value
	// conjunction around it.
	mustPanic("Required but no ID", func() {
		Control(ControlConfig{Field: headless.FieldControl{Required: true}, Type: "text", Name: "t"})
	})
	mustPanic("Invalid but no ID", func() {
		Control(ControlConfig{Field: headless.FieldControl{Invalid: true}, Type: "text", Name: "t"})
	})
	mustPanic("DescribedBy but no ID", func() {
		Control(ControlConfig{Field: headless.FieldControl{DescribedBy: "d"}, Type: "text", Name: "t"})
	})
	mustPanic("no Name", func() {
		Control(ControlConfig{Field: headless.FieldControl{ID: "x"}, Type: "text"})
	})
}
