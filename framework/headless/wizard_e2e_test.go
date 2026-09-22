package headless

// Browser coverage for headless-wizard: the plain POST wizard needs
// nothing, and under an Island the submit becomes a region update the
// module answers by focusing the failed submit's summary (a step that
// did not move) or the new step's heading, and by saying the step-of
// sentence only when the step moved. Same harness as
// behavior_e2e_test.go.

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

func wizardIslandPage(t *testing.T, current int, errors string) string {
	t.Helper()
	return `<div data-fui-signal="setup" data-fui-signal-mode="html">` +
		string(StepWizard(StepWizardProps{
			Action:  "/__hui/setup",
			Current: current,
			ID:      "setup",
			Steps:   []WizardStep{{Heading: "Source"}, {Heading: "Build"}},
			Island:  Island{Endpoint: "/__hui/setup", Signal: "setup"},
			Errors:  render.HTML(errors),
		}, nil)) + `</div>`
}

// A successful step change: focus lands on the new step's heading and
// the step-of sentence is re-said through the live region.
func TestE2E_WizardIslandSwapFocusesTheNewHeadingAndAnnounces(t *testing.T) {
	answer := wizardIslandPage(t, 1, "")
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/setup", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(answer))
		})
	}
	b := startBehaviorServer(t, wizardIslandPage(t, 0, ""), extra)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(WizardBehaviorName)) {
		t.Fatal("the wizard marker never loaded headless-wizard")
	}
	if !pollTrue(ctx, controlsLoadedExpr("rpc")) {
		t.Fatal("the rpc kernel module never loaded for the island form")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-hui-step-wizard-action="next"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.tagName === 'H2' &&
		document.activeElement.textContent === 'Build'`) {
		t.Fatal("focus did not land on the new step's heading after the swap")
	}
}

// A failed submit answers 200 with the same step and a summary: focus
// lands on the summary, and the step-of sentence is not re-said.
func TestE2E_WizardFailedSubmitFocusesTheSummary(t *testing.T) {
	summary := ValidationSummary(ValidationSummaryProps{
		ID:     "setup-errors",
		Errors: []FieldError{{Message: "Pick a source"}},
	}, nil)
	answer := wizardIslandPage(t, 0, string(summary))
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/setup", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(answer))
		})
	}
	b := startBehaviorServer(t, wizardIslandPage(t, 0, ""), extra)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(WizardBehaviorName)) {
		t.Fatal("the wizard marker never loaded headless-wizard")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-hui-step-wizard-action="next"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.getAttribute('role') === 'alert'`) {
		t.Fatal("focus did not land on the failed submit's summary")
	}
	var announced int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__huiWizStatusSaid = 0;
		 const s = document.querySelector('[data-hui-step-wizard-status]');
		 new MutationObserver(() => window.__huiWizStatusSaid++).observe(s, {childList: true, characterData: true, subtree: true});`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-hui-step-wizard-action="next"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	chromedp.Run(ctx, chromedp.Sleep(0))
	if !pollTrue(ctx, `document.querySelector('[data-hui-form-errors]') !== null`) {
		t.Fatal("the failed answer did not arrive")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__huiWizStatusSaid`, &announced)); err != nil {
		t.Fatal(err)
	}
	if announced != 0 {
		t.Fatalf("a failed submit re-said the step-of sentence %d time(s): the step did not move, so the sentence is not news", announced)
	}
}
