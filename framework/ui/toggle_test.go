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

func TestCheckboxRendersWrappedLabel(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "notify", Label: "Email me"})
	for _, want := range []string{
		`data-fui-comp="ui-toggle"`,
		`type="checkbox"`,
		`name="notify"`,
		`id="notify"`,
		"Email me",
		"fui-choice--checkbox",
	} {
		mustContain(t, h, want)
	}
	// The label wraps the control: no for/id pair left to go stale.
	if strings.Contains(string(h), " for=") {
		t.Fatalf("the label wraps the control; a for= pair must not ship:\n%s", h)
	}
}

func TestCheckboxCheckedAndDisabled(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Checked: true, Disabled: true})
	mustContain(t, h, "checked")
	mustContain(t, h, "disabled")
}

func TestCheckboxErrorWiresAriaAndAlertOutsideTheRun(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Error: "Must agree"})
	mustContain(t, h, `aria-invalid="true"`)
	mustContain(t, h, `aria-describedby="n-error"`)
	mustContain(t, h, `id="n-error"`)
	mustContain(t, h, `role="alert"`)
	mustContain(t, h, "Must agree")
	// The message cannot sit inside the label (it would join the
	// control's accessible name); the shell keeps run + message as
	// one unit.
	mustContain(t, h, `class="fui-choice-field"`)
}

func TestCheckboxHelpRidesTheRunAsTheHintPart(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Help: "Optional"})
	mustContain(t, h, "Optional")
	mustContain(t, h, "fui-choice__hint")
	// The hint span sits inside the wrapping label, so it is already
	// part of the control's accessible name: a described-by pointing
	// at it would say everything twice.
	if strings.Contains(string(h), "aria-describedby") {
		t.Fatalf("a checkbox hint rides the name; no described-by should ship:\n%s", h)
	}
}

func TestRadioRequiresValue(t *testing.T) {
	defer func() { recover() }()
	Radio(ToggleConfig{Name: "n", Label: "x"})
	t.Fatal("expected panic without Value")
}

func TestRadioEmitsCorrectTypeAndDerivedID(t *testing.T) {
	h := Radio(ToggleConfig{Name: "color", Value: "red", Label: "Red"})
	mustContain(t, h, `type="radio"`)
	mustContain(t, h, `value="red"`)
	mustContain(t, h, "fui-choice--radio")
	// The id derives from name + value so each radio in a group is
	// distinct.
	mustContain(t, h, `id="color-red"`)
}

func TestSwitchRendersSwitchSemantics(t *testing.T) {
	h := Switch(ToggleConfig{Name: "wifi", Label: "Wi-Fi"})
	mustContain(t, h, `type="checkbox"`)
	mustContain(t, h, `role="switch"`)
	mustContain(t, h, "fui-switch")
	if strings.Contains(string(h), "fui-choice--") {
		t.Fatalf("a switch wears its own root, not a choice variant:\n%s", h)
	}
}

func TestSwitchMessageWiresDescribedBy(t *testing.T) {
	h := Switch(ToggleConfig{Name: "wifi", Label: "Wi-Fi", Help: "Turns off at 2am"})
	mustContain(t, h, `aria-describedby="wifi-hint"`)
	mustContain(t, h, `id="wifi-hint"`)
	mustContain(t, h, "Turns off at 2am")

	e := Switch(ToggleConfig{Name: "wifi", Label: "Wi-Fi", Error: "No radio"})
	mustContain(t, e, `aria-invalid="true"`)
	mustContain(t, e, `aria-describedby="wifi-error"`)
	mustContain(t, e, `role="alert"`)
}

func TestToggleCustomIDOverridesName(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", ID: "custom"})
	mustContain(t, h, `id="custom"`)
	if strings.Contains(string(h), `id="n"`) {
		t.Fatalf("custom ID should override Name as id:\n%s", h)
	}
}

func TestToggleRequiredAttribute(t *testing.T) {
	h := Checkbox(ToggleConfig{Name: "n", Label: "x", Required: true})
	mustContain(t, h, "required")
}

// A required group marks every leaf's input required — that is how
// HTML makes a radio group required — and draws no legend asterisk:
// headless.Group owns the legend and takes plain text, and an
// aria-hidden asterisk communicated nothing to assistive tech anyway.
// A required group carries two different things, and needs both: the
// rule the browser enforces, on the leaves, and the cue a sighted
// reader looks for, on the legend. A group with only the rule looks
// optional; one with only the cue lies.
func TestRequiredGroupMarksTheLegendAndTheLeaves(t *testing.T) {
	h := string(RadioGroup(RadioGroupConfig{
		Name: "plan", Legend: "Plan", Required: true,
		Options: []RadioGroupOption{{Value: "a", Label: "A"}, {Value: "b", Label: "B"}},
	}))
	// The leading space keeps this off the legend's data-required.
	if got := strings.Count(h, ` required=""`); got != 2 {
		t.Fatalf("required group marked %d leaves, want 2:\n%s", got, h)
	}
	if !strings.Contains(h, `data-required`) {
		t.Fatalf("the legend carries no required state, so nothing draws the mark:\n%s", h)
	}
	// The asterisk is drawn by the stylesheet from that state, never
	// written into the markup, so it stays out of the accessible name.
	if strings.Contains(h, "*") {
		t.Fatalf("the required mark is written into the markup; it belongs to the sheet:\n%s", h)
	}
	plain := string(RadioGroup(RadioGroupConfig{
		Name: "plan", Legend: "Plan",
		Options: []RadioGroupOption{{Value: "a", Label: "A"}},
	}))
	if strings.Contains(plain, "required") {
		t.Fatalf("an optional group carries a required state:\n%s", plain)
	}
}

func TestRadioGroupStructure(t *testing.T) {
	h := string(RadioGroup(RadioGroupConfig{
		Name:    "freq",
		Legend:  "Notification frequency",
		Options: []RadioGroupOption{{Value: "all", Label: "Always"}, {Value: "none", Label: "Never"}},
	}))
	for _, want := range []string{
		"<fieldset", "</fieldset>", "<legend", "Notification frequency",
		`class="fui-choice-group"`, `id="freq-group"`,
		`type="radio"`, `name="freq"`, `id="freq-group-all"`, `id="freq-group-none"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in:\n%s", want, h)
		}
	}
	// The fieldset is the native group semantic; headless renders no
	// role on it and neither does the styled layer.
	if strings.Contains(h, `role="radiogroup"`) || strings.Contains(h, `role="group"`) {
		t.Errorf("the fieldset's native group semantic is the contract; no role should ship:\n%s", h)
	}
}

// The group's error belongs on the group — never on each leaf — and
// the fieldset's described-by names it.
func TestGroupErrorBelongsOnTheGroup(t *testing.T) {
	h := string(RadioGroup(RadioGroupConfig{
		Name: "plan", Legend: "Plan", Error: "Pick one",
		Options: []RadioGroupOption{{Value: "a", Label: "A"}},
	}))
	if !strings.Contains(h, `aria-describedby="plan-group-error"`) {
		t.Fatalf("fieldset described-by missing:\n%s", h)
	}
	if !strings.Contains(h, `id="plan-group-error"`) || !strings.Contains(h, "Pick one") {
		t.Fatalf("group error message missing:\n%s", h)
	}
	if strings.Contains(h, "aria-invalid") {
		t.Fatalf("a group error marks no single leaf invalid:\n%s", h)
	}
	// Either/or: the help is dropped when the error is set.
	if strings.Contains(h, "plan-group-hint") {
		t.Fatalf("error and hint are either/or in this family:\n%s", h)
	}
}

func TestGroupHelpWiresDescribedBy(t *testing.T) {
	h := RadioGroup(RadioGroupConfig{
		Name: "plan", Legend: "Plan", Help: "You can change this later",
		Options: []RadioGroupOption{{Value: "a", Label: "A"}},
	})
	mustContain(t, h, `aria-describedby="plan-group-hint"`)
	mustContain(t, h, `id="plan-group-hint"`)
	mustContain(t, h, "You can change this later")
}

func TestToggleCSSStylesTheFamily(t *testing.T) {
	css := toggleCSS(style.Theme{})
	for _, want := range []string{
		".fui-choice", ".fui-choice--checkbox", ".fui-choice--radio",
		".fui-switch", ".fui-choice-group", ".fui-choice-field",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("toggleCSS missing %q — part of the family ships unstyled", want)
		}
	}
}

func TestToggleExtraAttrsLandOnTheInput(t *testing.T) {
	h := Checkbox(ToggleConfig{
		Name: "n", Label: "x",
		ExtraAttrs: map[string]string{"data-test": "hook", "for": "evil"},
	})
	s := string(h)
	i := strings.Index(s, "<input")
	input := s[i : i+strings.Index(s[i:], ">")+1]
	if !strings.Contains(input, `data-test="hook"`) {
		t.Errorf("input missing data-test:\n%s", input)
	}
	if strings.Contains(input, "for=") {
		t.Errorf("the label wraps the control; a caller's for= is inert and must not ship:\n%s", input)
	}
	if !strings.Contains(input, `type="checkbox"`) {
		t.Errorf("input type lost:\n%s", input)
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
}
