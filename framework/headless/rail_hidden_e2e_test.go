package headless

import (
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
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
				var state string
				_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({scrollY, targets: ['visible', 'concealed'].map(id => { const target = document.getElementById(id); return {id, rect: target.getBoundingClientRect().toJSON(), rects: target.getClientRects().length, visible: target.checkVisibility(), active: document.querySelector('a.is-active')?.getAttribute('href')}; })})`, &state))
				t.Fatalf("rail must retain the last visible section, not the hidden target: %v; state: %s", err, state)
			}
		})
	}
}

func TestRailBootSkipsHiddenTargets(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		body   string
		active string
	}{
		{"hidden first", `<section id="concealed" hidden>Hidden</section><section id="visible">Visible</section>`, "visible"},
		{"hidden last", `<section id="visible">Visible</section><section id="concealed" hidden>Hidden</section>`, "visible"},
		{"closed details", `<details><summary>More</summary><section id="concealed">Hidden</section></details><section id="visible">Visible</section>`, "visible"},
		{"all hidden", `<section id="concealed" hidden>Hidden</section>`, ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			nav := Rail(RailProps{
				Label: "Sections", ObserveSelector: "#region",
				Items: []RailItem{{Anchor: "concealed", Text: "Hidden"}, {Anchor: "visible", Text: "Visible"}},
			}, nil)
			ctx := behaviorPage(t, startBehaviorServer(t, string(nav)+`<div style="height:120vh"></div><div id="region">`+scenario.body+`</div>`))
			if !pollTrue(ctx, controlsLoadedExpr("headless-rail")) {
				t.Fatal("rail module did not load")
			}
			var active string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))`, nil, func(params *runtime.EvaluateParams) *runtime.EvaluateParams { return params.WithAwaitPromise(true) }),
				chromedp.Evaluate(`document.querySelector('[data-hui-rail] a.is-active')?.getAttribute('href').slice(1) || ''`, &active),
			); err != nil {
				t.Fatal(err)
			}
			if active != scenario.active {
				t.Fatalf("initial active target = %q, want %q", active, scenario.active)
			}
		})
	}
}
