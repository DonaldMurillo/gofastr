package headless

// A pending focus-return record belongs to the island answer it waits
// for, not to whatever DOM insertion lands first.

import (
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// insertToastSoon appends an unrelated node (a toast, a poll swap
// elsewhere) 100ms after the click, while the answer is in flight.
const insertToastSoon = `setTimeout(() => { const n = document.createElement('div'); n.textContent = 'a toast'; document.body.appendChild(n); }, 100);`

// The wizard's submit record survives an unrelated insertion: focus
// lands on the new step's heading when the answer arrives.
func TestE2E_WizardFocusSurvivesAnUnrelatedInsertion(t *testing.T) {
	answer := wizardIslandPage(t, 1, "")
	b := startBehaviorServer(t, wizardIslandPage(t, 0, ""), func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/setup", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(600 * time.Millisecond)
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(answer))
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(WizardBehaviorName)) || !pollTrue(ctx, controlsLoadedExpr("rpc")) {
		t.Fatal("modules never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-step-wizard-action="next"]').click();`+insertToastSoon, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-step-wizard-current="1"]') !== null`) {
		t.Fatal("the step answer never landed")
	}
	time.Sleep(300 * time.Millisecond)
	if got := evalString(t, ctx, `document.activeElement ? document.activeElement.tagName + ':' + document.activeElement.textContent : ''`); got != "H2:Build" {
		t.Fatalf("after the step answer focus is on %q, want the new step's heading H2:Build", got)
	}
}

// The repeater's click record survives an unrelated insertion the same
// way: focus lands back on the add control.
func TestE2E_RepeaterFocusSurvivesAnUnrelatedInsertion(t *testing.T) {
	region := func(rows int, status string) string {
		items := make([]RepeaterItem, rows)
		for i := range items {
			items[i] = RepeaterItem{Fields: []render.HTML{render.Text("")}}
		}
		return `<div data-cui-signal="guests" data-cui-signal-mode="html">` + string(Repeater(RepeaterProps{
			Name: "guests", Label: "Guests", Items: items, Action: "/__hui/guests",
			Island: Island{Endpoint: "/__hui/guests", Signal: "guests"}, Status: status,
		}, nil)) + `</div>`
	}
	b := startBehaviorServer(t, region(1, ""), func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/guests", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(600 * time.Millisecond)
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(region(2, "Added a row.")))
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) || !pollTrue(ctx, controlsLoadedExpr("rpc")) {
		t.Fatal("modules never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-repeater-action="add"]').click();`+insertToastSoon, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-repeater-item]').length === 2`) {
		t.Fatal("the island answer never landed")
	}
	time.Sleep(300 * time.Millisecond)
	if got := evalString(t, ctx, `document.activeElement && document.activeElement.getAttribute('data-hui-repeater-action') || document.activeElement.tagName`); got != "add" {
		t.Fatalf("after the add answer focus is on %q, want the add control", got)
	}
}
