package headless

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestE2E_PaneHostDeepLinkReload pins the load half of the deep link's
// refresh/share/Back parity. Popstate replays the keyed trigger, so its
// RPC fills the pane; the load path only opened the pane, so after F5 or
// from a shared link the pane showed its generic content. A load now
// replays the trigger the same way, records no history entry, and opens
// the pane empty when no trigger carries the key, a crafted one included.
// One key can label a trigger for each pane, so the replay clicks the one
// that opens the pane the URL names.
func TestE2E_PaneHostDeepLinkReload(t *testing.T) {
	b := startBehaviorServer(t, string(sharedKeyPaneHost()))
	ctx := behaviorPage(t, b)
	for _, c := range []struct {
		name, value, trigger string // trigger: the id the load clicked, "" for none
	}{
		{name: "keyed", value: "secondary:k1", trigger: "keyed"},
		{name: "unknown key", value: "secondary:nope"},
		{name: "crafted key", value: `secondary:k1"], [data-hui-pane-key="k2`},
		{name: "key on both panes", value: "secondary:dup", trigger: "dup-sec"},
		{name: "keyed swap", value: "tertiary:sw", trigger: "swap"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var before int
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`history.length`, &before),
				chromedp.Navigate(b.srv.URL+"/?pane="+url.QueryEscape(c.value)),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, paneHostLoaded) {
				t.Fatal("the host marker never loaded headless-panehost")
			}
			slot := strings.SplitN(c.value, ":", 2)[0]
			if !pollTrue(ctx, `!document.querySelector('[data-hui-pane="`+slot+`"]').hasAttribute('hidden')`) {
				t.Fatal("the deep-linked pane did not open on load")
			}
			var raw string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify({
					clicked: (function () {
						var m = document.querySelector('[data-hui-panehost]').__huiPaneTrigger;
						var el = m && m.get('`+slot+`');
						return el ? el.id : '';
					})(),
					param: new URL(location.href).searchParams.get('pane'),
					entries: history.length,
				})`, &raw)); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Clicked string `json:"clicked"`
				Param   string `json:"param"`
				Entries int    `json:"entries"`
			}
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("probe returned %q: %v", raw, err)
			}
			if got.Clicked != c.trigger {
				t.Errorf("the load clicked %q, want %q", got.Clicked, c.trigger)
			}
			if got.Param != c.value {
				t.Errorf("the load rewrote the parameter to %q", got.Param)
			}
			// The navigation is one entry; a replay that pushed would add
			// a second.
			if got.Entries != before+1 {
				t.Errorf("history.length %d after the load, %d before it: the replay pushed an entry", got.Entries, before)
			}
		})
	}
}

// sharedKeyPaneHost is a deep-linked host whose key "dup" labels a
// tertiary trigger first in document order and a secondary one after it.
func sharedKeyPaneHost() render.HTML {
	return PaneHost(PaneHostProps{
		Primary: render.Join(
			render.Tag("button", map[string]string{"id": "keyed", "type": "button", "data-hui-pane-open-control": "secondary", "data-hui-pane-key": "k1"}, render.Text("open k1")),
			render.Tag("button", map[string]string{"id": "other", "type": "button", "data-hui-pane-open-control": "secondary", "data-hui-pane-key": "k2"}, render.Text("open k2")),
			render.Tag("button", map[string]string{"id": "dup-ter", "type": "button", "data-hui-pane-open-control": "tertiary", "data-hui-pane-key": "dup"}, render.Text("edit dup")),
			render.Tag("button", map[string]string{"id": "dup-sec", "type": "button", "data-hui-pane-open-control": "secondary", "data-hui-pane-key": "dup"}, render.Text("view dup")),
			render.Tag("button", map[string]string{"id": "close-sec", "type": "button", "data-hui-pane-close": "secondary"}, render.Text("close")),
			render.Tag("button", map[string]string{"id": "swap", "type": "button", "data-hui-pane-swap": "tertiary", "data-hui-pane-key": "sw"}, render.Text("swap")),
		),
		Secondary:     render.Text("S"),
		Tertiary:      render.Text("T"),
		DeepLinkParam: "pane",
	}, nil)
}

// Back replays the trigger that opens the pane the URL names, not the
// first trigger carrying the key: "dup" labels a tertiary trigger ahead
// of the secondary one that wrote ?pane=secondary:dup.
func TestE2E_PaneHostPopstateMatchesPane(t *testing.T) {
	b := startBehaviorServer(t, string(sharedKeyPaneHost()))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	var param, clicked string
	var terOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#dup-sec`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Click(`#close-sec`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`delete document.querySelector('[data-hui-panehost]').__huiPaneTrigger; history.back()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(`new URL(location.href).searchParams.get('pane')`, &param),
		chromedp.Evaluate(`(function () {
				var m = document.querySelector('[data-hui-panehost]').__huiPaneTrigger;
				var el = m && m.get('secondary');
				return el ? el.id : '';
			})()`, &clicked),
		chromedp.Evaluate(`!document.querySelector('[data-hui-pane="tertiary"]').hasAttribute('hidden')`, &terOpen),
	); err != nil {
		t.Fatal(err)
	}
	if param != "secondary:dup" {
		t.Fatalf("Back left pane=%q, want secondary:dup", param)
	}
	if clicked != "dup-sec" || terOpen {
		t.Errorf("Back replayed %q (tertiary open %v), want dup-sec and tertiary closed", clicked, terOpen)
	}
}
