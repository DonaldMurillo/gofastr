package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestStepWizardRequiresAtLeastOneStep(t *testing.T) {
	defer func() { recover() }()
	StepWizard(StepWizardConfig{Action: "/wiz"})
	t.Fatal("expected panic without Steps")
}

func TestStepWizardRequiresAction(t *testing.T) {
	defer func() { recover() }()
	StepWizard(StepWizardConfig{Steps: []StepWizardStep{{Heading: "A"}}})
	t.Fatal("expected panic without Action")
}

func TestStepWizardRejectsOutOfRangeCurrentStep(t *testing.T) {
	defer func() { recover() }()
	StepWizard(StepWizardConfig{
		Steps:       []StepWizardStep{{Heading: "A"}},
		Action:      "/wiz",
		CurrentStep: 5,
	})
	t.Fatal("expected panic for out-of-range CurrentStep")
}

func TestStepWizardRendersSingleStep(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "name", ID: "name"})
	h := string(StepWizard(StepWizardConfig{
		Steps: []StepWizardStep{
			{Heading: "Personal Info", Description: "Enter your details", Fields: []render.HTML{in}},
		},
		Action: "/wiz",
	}))
	for _, want := range []string{
		`data-fui-comp="ui-step-wizard"`,
		`action="/wiz"`,
		`method="POST"`,
		"ui-step-wizard__indicator",
		"is-current",
		"Personal Info",
		"Enter your details",
		"ui-step-wizard__actions",
		`name="wizard_action"`,
		`value="next"`,
		">Submit<",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in:\n%s", want, h)
		}
	}
	// Single step should NOT have a Back button.
	if strings.Contains(h, ">Back<") {
		t.Error("single step should not have a Back button")
	}
}

func TestStepWizardRendersMultiStepWithNavigation(t *testing.T) {
	in1 := html.Input(html.InputConfig{Type: "text", Name: "n1", ID: "n1"})
	in2 := html.Input(html.InputConfig{Type: "text", Name: "n2", ID: "n2"})
	in3 := html.Input(html.InputConfig{Type: "text", Name: "n3", ID: "n3"})
	h := string(StepWizard(StepWizardConfig{
		Steps: []StepWizardStep{
			{Heading: "Step 1", Fields: []render.HTML{in1}},
			{Heading: "Step 2", Fields: []render.HTML{in2}},
			{Heading: "Step 3", Fields: []render.HTML{in3}},
		},
		CurrentStep: 1,
		Action:      "/wiz",
	}))
	for _, want := range []string{
		"is-current",
		"is-completed",
		">Continue<",
		">Back<",
		`name="wizard_action"`,
		"Step 2",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in:\n%s", want, h)
		}
	}
}

func TestStepWizardSecondStepShowsBackAndSubmit(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(StepWizard(StepWizardConfig{
		Steps: []StepWizardStep{
			{Heading: "A", Fields: []render.HTML{in}},
			{Heading: "B", Fields: []render.HTML{in}},
		},
		CurrentStep: 1,
		Action:      "/wiz",
	}))
	if !strings.Contains(h, ">Submit<") {
		t.Error("last step should show Submit, not Continue")
	}
	if !strings.Contains(h, ">Back<") {
		t.Error("step > 0 should show Back button")
	}
}

func TestStepWizardCustomMethod(t *testing.T) {
	h := string(StepWizard(StepWizardConfig{
		Steps:  []StepWizardStep{{Heading: "A"}},
		Action: "/wiz",
		Method: "GET",
	}))
	if !strings.Contains(h, `method="GET"`) {
		t.Errorf("expected method=GET, got: %s", h)
	}
}

func TestStepWizardHiddenFields(t *testing.T) {
	h := string(StepWizard(StepWizardConfig{
		Steps:        []StepWizardStep{{Heading: "A"}},
		Action:       "/wiz",
		HiddenFields: []render.HTML{render.Tag("input", map[string]string{"type": "hidden", "name": "token", "value": "abc"})},
	}))
	if !strings.Contains(h, `name="token"`) {
		t.Errorf("hidden field should be rendered, got: %s", h)
	}
}

func TestStepWizardCustomClass(t *testing.T) {
	h := string(StepWizard(StepWizardConfig{
		Steps:  []StepWizardStep{{Heading: "A"}},
		Action: "/wiz",
		Class:  "extra",
	}))
	if !strings.Contains(h, "ui-step-wizard extra") {
		t.Errorf("expected custom class, got: %s", h)
	}
}

func TestStepWizardCurrentStepHasAriaCurrent(t *testing.T) {
	in1 := html.Input(html.InputConfig{Type: "text", Name: "n1", ID: "n1"})
	in2 := html.Input(html.InputConfig{Type: "text", Name: "n2", ID: "n2"})
	h := string(StepWizard(StepWizardConfig{
		Steps: []StepWizardStep{
			{Heading: "Step 1", Fields: []render.HTML{in1}},
			{Heading: "Step 2", Fields: []render.HTML{in2}},
		},
		CurrentStep: 1,
		Action:      "/wiz",
	}))
	if !strings.Contains(h, `aria-current="step"`) {
		t.Errorf("current step dot should have aria-current=step, got:\n%s", h)
	}
	// Ensure aria-current appears on the is-current dot, not the is-completed dot.
	// Count occurrences — must be exactly 1.
	count := strings.Count(h, `aria-current="step"`)
	if count != 1 {
		t.Errorf("expected exactly 1 aria-current=step, got %d", count)
	}
}

// StepWizard must validate Method — rejects non-GET/POST.
func TestStepWizardPanicOnInvalidMethod(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("StepWizard with invalid Method should panic")
		}
	}()
	StepWizard(StepWizardConfig{
		Steps:       []StepWizardStep{{Heading: "A"}, {Heading: "B"}},
		CurrentStep: 0,
		Action:      "/wizard",
		Method:      "DELETE",
	})
}
