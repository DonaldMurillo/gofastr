package headless

import (
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func enterWizard(current int) render.HTML {
	return StepWizard(StepWizardProps{
		Action: "/setup", Current: current,
		Steps: []WizardStep{
			{Heading: "Source"},
			{Heading: "Build", Fields: []render.HTML{render.HTML(`<input id="wf" name="image" aria-label="Image">`)}},
			{Heading: "Deploy"},
		},
	}, nil)
}

// Implicit submission (Enter in a field) clicks the form's FIRST submit
// button in tree order. On every step past the first that was Back, so
// Enter walked the wizard backwards. The default button is now a
// hidden twin of Next ahead of every other submit control, and the
// visible order stays Back, then Next.
func TestStepWizardDefaultButtonIsNext(t *testing.T) {
	h := string(enterWizard(1))
	first := regexp.MustCompile(`<button[^>]*type="submit"[^>]*>`).FindString(h)
	if !strings.Contains(first, `value="next"`) {
		t.Fatalf("the form's default button (first submit in tree order) is not Next, so Enter in a field submits it: %s", first)
	}
	if !strings.Contains(first, ` hidden=""`) || !strings.Contains(first, `tabindex="-1"`) {
		t.Fatalf("the default twin is seen or reached by Tab: %s", first)
	}
	back, next := strings.Index(h, `value="back"`), strings.LastIndex(h, `value="next"`)
	if back < 0 || next < back {
		t.Fatalf("the visible order changed: Back at %d, Next at %d", back, next)
	}
}

// The browser proof: Enter in a step's field submits wizard_action=next
// through the hidden default, on a middle step and on the last step.
func TestE2E_StepWizardEnterInAFieldSubmitsNext(t *testing.T) {
	b := startBehaviorServer(t, string(enterWizard(1)))
	ctx := behaviorPage(t, b)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('form').addEventListener('submit', (e) => {
			e.preventDefault();
			const fd = new FormData(e.target, e.submitter);
			window.__by = String(fd.getAll('wizard_action'));
		})`, nil),
		chromedp.Focus(`#wf`, chromedp.ByID),
		chromedp.KeyEvent("\r"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__by !== undefined`) {
		t.Fatal("Enter in the step's field submitted nothing")
	}
	var got string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__by`, &got)); err != nil {
		t.Fatal(err)
	}
	if got != "next" {
		t.Fatalf("Enter in a field submitted wizard_action=%q, want next", got)
	}
	// Tab from the field reaches Back, the first visible control, not
	// the hidden twin.
	var focused string
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#wf`, chromedp.ByID),
		chromedp.KeyEvent("\t"),
		chromedp.Evaluate(`document.activeElement.value || document.activeElement.tagName`, &focused),
	); err != nil {
		t.Fatal(err)
	}
	if focused != "back" {
		t.Fatalf("Tab from the field landed on %q, want the visible Back", focused)
	}
}
