package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestWizardRendersSteps(t *testing.T) {
	h := Wizard(WizardConfig{
		Name: "setup",
		Steps: []WizardStep{
			{Title: "Account", Content: render.Text("STEP1")},
			{Title: "Profile", Content: render.Text("STEP2")},
			{Title: "Confirm", Content: render.Text("STEP3")},
		},
	})
	for _, want := range []string{
		"ui-wizard",
		"Account", "Profile", "Confirm",
		"STEP1", "STEP2", "STEP3",
		"ui-wizard-step-indicator",
		"ui-wizard-nav",
		"ui-wizard-next",
		"Next",
		`data-wizard-steps="3"`,
	} {
		mustContain(t, h, want)
	}
}

func TestWizardStepIndicatorStates(t *testing.T) {
	h := Wizard(WizardConfig{
		Name:        "w",
		CurrentStep: 1,
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
			{Title: "C", Content: render.Text("c")},
		},
	})
	mustContain(t, h, "ui-wizard-step-complete")  // step 0
	mustContain(t, h, "ui-wizard-step-current")   // step 1
	mustContain(t, h, "ui-wizard-step-upcoming")  // step 2
	mustContain(t, h, `aria-current="step"`)
}

func TestWizardShowsBackOnNonFirstStep(t *testing.T) {
	h := Wizard(WizardConfig{
		Name:        "w",
		CurrentStep: 1,
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
		},
	})
	mustContain(t, h, "ui-wizard-back")
	mustContain(t, h, "Back")
}

func TestWizardNoBackOnFirstStep(t *testing.T) {
	h := Wizard(WizardConfig{
		Name: "w",
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
		},
	})
	if strings.Contains(string(h), "ui-wizard-back") {
		t.Fatalf("first step should not have back button:\n%s", h)
	}
}

func TestWizardSubmitOnlyOnLastStep(t *testing.T) {
	h := Wizard(WizardConfig{
		Name:        "w",
		CurrentStep: 0,
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
		},
	})
	// On first step, should have Next not Submit
	mustContain(t, h, "Next")
	if strings.Contains(string(h), `type="submit"`) {
		t.Fatalf("non-last step should not have submit button:\n%s", h)
	}
}

func TestWizardLastStepHasSubmit(t *testing.T) {
	h := Wizard(WizardConfig{
		Name:        "w",
		CurrentStep: 1,
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
		},
	})
	mustContain(t, h, `type="submit"`)
}

func TestWizardHiddenSteps(t *testing.T) {
	h := Wizard(WizardConfig{
		Name:        "w",
		CurrentStep: 0,
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
		},
	})
	// Step 0 should NOT be hidden, step 1 SHOULD be hidden
	s := string(h)
	step0Idx := strings.Index(s, `data-step="0"`)
	step1Idx := strings.Index(s, `data-step="1"`)
	if step0Idx < 0 || step1Idx < 0 {
		t.Fatalf("expected data-step attrs:\n%s", h)
	}
	// Between step 0 and step 1, there should NOT be "hidden" for step 0
	between := s[step0Idx:step1Idx]
	if strings.Contains(between, `hidden`) {
		t.Fatalf("current step should not be hidden:\n%s", between)
	}
}

func TestWizardRPCAttrs(t *testing.T) {
	h := Wizard(WizardConfig{
		Name:        "w",
		CurrentStep: 0,
		RPCPath:     "/api/wizard",
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
		},
	})
	// The & in the URL gets HTML-escaped to &amp;
	mustContain(t, h, `data-fui-rpc="/api/wizard?direction=next&amp;step=1"`)
}

func TestWizardCustomLabels(t *testing.T) {
	h := Wizard(WizardConfig{
		Name:        "w",
		CurrentStep: 1,
		SubmitLabel: "Finish",
		NextLabel:   "Continue",
		BackLabel:   "Go back",
		Steps: []WizardStep{
			{Title: "A", Content: render.Text("a")},
			{Title: "B", Content: render.Text("b")},
		},
	})
	mustContain(t, h, "Finish")
	mustContain(t, h, "Go back")
}
