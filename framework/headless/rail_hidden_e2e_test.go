package headless

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestRailSkipsHiddenTargets(t *testing.T) {
	for _, hidden := range []string{
		`<section id="concealed" hidden>Hidden</section>`,
		`<section id="concealed" style="display:none">Hidden</section>`,
		`<details><summary>More</summary><section id="concealed">Hidden</section></details>`,
	} {
		t.Run(hidden, func(t *testing.T) {
			nav := Rail(RailProps{
				Label: "Sections", ObserveSelector: "#region",
				Items: []RailItem{{Anchor: "visible", Text: "Visible"}, {Anchor: "concealed", Text: "Hidden"}},
			}, nil)
			body := string(nav) + `<div style="height:120vh"></div><div id="region"><section id="visible" style="height:120vh">Visible</section>` + hidden + `</div><div style="height:200vh"></div>`
			ctx := behaviorPage(t, startBehaviorServer(t, body))
			if !pollTrue(ctx, controlsLoadedExpr("headless-rail")) {
				t.Fatal("rail module did not load")
			}
			var concealed bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(railActiveExpr("[data-hui-rail]", "concealed"), &concealed)); err != nil {
				t.Fatal(err)
			}
			if concealed {
				t.Error("hidden target became active above every section")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const section = document.getElementById('visible'); scrollTo(0, section.offsetTop + section.offsetHeight + 100); })()`, nil),
				chromedp.Poll(railActiveExpr("[data-hui-rail]", "visible"), nil, chromedp.WithPollingTimeout(2*time.Second)),
			); err != nil {
				t.Fatalf("rail must retain the last visible section, not the hidden target: %v", err)
			}
		})
	}
}
