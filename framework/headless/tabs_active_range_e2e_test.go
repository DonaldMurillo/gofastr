package headless

import (
	"encoding/json"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A seed past the strip's last tab (5 on a three-tab strip) is painted
// as tab 0 (TabsProps.Active: out of range takes 0), and headless-tabs
// writes that index back into the signal, so the store, every other
// node bound to the signal, and the selected tab agree. Before, the
// store kept 5: a text binding read 5 beside tab 0 selected. An
// in-range seed and an unset signal are left as they are.
func TestE2E_TabsOutOfRangeSeedSettlesToPaintedTab(t *testing.T) {
	strip := func(name string, active int) string {
		return string(Tabs(TabsProps{Name: name, Active: active, Tabs: []Tab{
			{Label: "One", Panel: render.Text("One panel")},
			{Label: "Two", Panel: render.Text("Two panel")},
			{Label: "Three", Panel: render.Text("Three panel")},
		}}, nil))
	}
	page := `<script type="application/json" id="gofastr-signals">{"oor":5,"neg":-1,"ok":1}</script>` +
		strip("oor", 5) + `<span id="oor-text" data-cui-signal="oor">5</span>` +
		strip("neg", -1) + strip("ok", 1) + strip("unset", 0)
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, tabsLoaded) {
		t.Fatal("the strip marker never loaded headless-tabs")
	}
	if !pollTrue(ctx, `String(window.__gofastr.getSignal('oor')) === '0'`) {
		var v string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`String(window.__gofastr.getSignal('oor'))`, &v))
		t.Fatalf("an out-of-range seed stayed %s in the store; the strip paints tab 0", v)
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const strip = (id) => document.getElementById(id + '-tab-0').closest('[data-hui-tabs]');
		const sel = (id) => strip(id).querySelector('[aria-selected="true"]').textContent;
		return JSON.stringify({
			oorActive: strip('oor').getAttribute('data-active'),
			oorSelected: sel('oor'),
			oorText: document.getElementById('oor-text').textContent,
			neg: String(window.__gofastr.getSignal('neg')),
			negSelected: sel('neg'),
			ok: String(window.__gofastr.getSignal('ok')),
			okSelected: sel('ok'),
			unset: String(window.__gofastr.getSignal('unset')),
		});
	})()`, &raw)); err != nil {
		t.Fatal(err)
	}
	var got struct {
		OorActive, OorSelected, OorText string
		Neg, NegSelected                string
		Ok, OkSelected                  string
		Unset                           string
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.OorActive != "0" || got.OorSelected != "One" || got.OorText != "0" {
		t.Errorf("seed 5: data-active %q, selected %q, bound text %q; want 0, One, 0", got.OorActive, got.OorSelected, got.OorText)
	}
	if got.Neg != "0" || got.NegSelected != "One" {
		t.Errorf("seed -1: signal %q, selected %q; want 0, One", got.Neg, got.NegSelected)
	}
	if got.Ok != "1" || got.OkSelected != "Two" {
		t.Errorf("in-range seed 1: signal %q, selected %q; want 1, Two (left alone)", got.Ok, got.OkSelected)
	}
	if got.Unset != "undefined" {
		t.Errorf("an unset signal became %q; the module must not invent one", got.Unset)
	}
}
