package headless

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The wizard's own contracts: the POST shape the server reads, the
// rail's names, and the island path.

func TestStepWizardPostsItsActions(t *testing.T) {
	steps := []WizardStep{{Heading: "Source"}, {Heading: "Build"}, {Heading: "Deploy"}}
	first := StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Current: 0}, nil)
	has(t, first, `action="/setup"`, "the form lost its action")
	has(t, first, `method="POST"`, "the transition is not the plain POST the server reads")
	has(t, first, `name="wizard_action" type="submit" value="next"`, "the weighted action does not carry wizard_action=next")
	hasNot(t, first, `value="back"`, "the first step renders a Back to a step that does not exist")

	mid := StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Current: 1}, nil)
	has(t, mid, `name="wizard_action" type="submit" value="back"`, "mid-flow does not carry the way back")

	last := StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Current: 2}, nil)
	has(t, last, ">Submit</button>", "the last step's weighted action does not say Submit")
}

func TestStepWizardRailNamesItsSteps(t *testing.T) {
	steps := []WizardStep{{Heading: "Source"}, {Heading: "Build"}}
	got := StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Current: 1}, nil)
	has(t, got, `aria-label="Step 2 of 2"`, "the rail does not say where the reader is")
	has(t, got, `aria-label="Step 1: Source"`, "a dot does not name its step")
	has(t, got, `aria-current="step"`, "the current dot does not say it is current")
	has(t, got, `data-hui-step-wizard-current="1"`, "the wizard does not carry its current step for the swap")
	has(t, got, `data-hui-step-wizard-status=""`, "the announcement node is missing")
	has(t, got, `>Step 2 of 2</span>`, "the step-of sentence is not the live region's SSR text")
}

func TestStepWizardErrorsRideTheExistingFocusHook(t *testing.T) {
	summary := ValidationSummary(ValidationSummaryProps{ID: "w-errors",
		Errors: []FieldError{{Message: "Pick a region"}}}, nil)
	got := StepWizard(StepWizardProps{Action: "/setup", ID: "w",
		Steps: []WizardStep{{Heading: "Source"}}, Errors: summary}, nil)
	has(t, got, `data-hui-form-errors=""`, "the failed submit does not ask the runtime to move focus to the summary")
}

func TestStepWizardIslandCarriesTheRPCContract(t *testing.T) {
	got := StepWizard(StepWizardProps{Action: "/setup", ID: "w",
		Steps:  []WizardStep{{Heading: "Source"}},
		Island: Island{Endpoint: "/island/setup", Signal: "setup"}}, nil)
	has(t, got, `data-fui-rpc="/island/setup"`, "the island submit does not carry the RPC contract")
	has(t, got, `data-fui-rpc-signal="setup"`, "the submit does not name the region it updates")
}

func TestStepWizardRefusesBrokenFlows(t *testing.T) {
	steps := []WizardStep{{Heading: "Source"}}
	refuse(t, "Steps", func() { StepWizard(StepWizardProps{Action: "/setup"}, nil) })
	refuse(t, "Heading", func() {
		StepWizard(StepWizardProps{Action: "/setup", Steps: []WizardStep{{}}}, nil)
	})
	// A heading of spaces names nothing: the emptiness judgment trims,
	// so it refuses exactly like the empty string.
	refuse(t, "Heading", func() {
		StepWizard(StepWizardProps{Action: "/setup", Steps: []WizardStep{{Heading: "  "}}}, nil)
	})
	refuse(t, "outside", func() {
		StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Current: 1}, nil)
	})
	refuse(t, "Action", func() { StepWizard(StepWizardProps{Steps: steps}, nil) })
	refuse(t, "GET or POST", func() {
		StepWizard(StepWizardProps{Action: "/setup", Steps: steps, Method: "PUT"}, nil)
	})
	refuse(t, "requires ID", func() {
		StepWizard(StepWizardProps{Action: "/setup", Steps: steps,
			Errors: render.Text("x")}, nil)
	})
	refuse(t, "needs a Signal", func() {
		StepWizard(StepWizardProps{Action: "/setup", Steps: steps,
			Island: Island{Endpoint: "/island/setup"}}, nil)
	})
}

// The back-to-top anchor is a real link whose extras cannot steal its
// destination.
func TestBackToTopAnchorContract(t *testing.T) {
	got := BackToTop(BackToTopProps{Href: "#main-content", Label: "Back to top"}, nil)
	has(t, got, `href="#main-content"`, "the jump link lost its destination")
	has(t, got, `data-hui-back-to-top=""`, "the module's hook is missing")

	targeted := BackToTop(BackToTopProps{Href: "#toc", Target: "toc", Label: "Up",
		Threshold: 480, Smooth: true}, nil)
	has(t, targeted, `data-hui-back-to-top-target="toc"`, "the target id is missing")
	has(t, targeted, `data-hui-back-to-top-threshold="480"`, "the caller's threshold is missing")
	has(t, targeted, `data-hui-back-to-top-smooth=""`, "the smooth scroll opt-in is missing")
}

func TestBackToTopRefusesBrokenLinks(t *testing.T) {
	refuse(t, "Href", func() { BackToTop(BackToTopProps{Label: "Up"}, nil) })
	refuse(t, "the anchor policy allows", func() {
		BackToTop(BackToTopProps{Href: "javascript:alert(1)", Label: "Up"}, nil)
	})
	refuse(t, "an element id", func() {
		BackToTop(BackToTopProps{Href: "#t", Target: "#main", Label: "Up"}, nil)
	})
	refuse(t, "negative", func() {
		BackToTop(BackToTopProps{Href: "#t", Label: "Up", Threshold: -1}, nil)
	})
}
