package headless

// TestE2E_PaneHostCraftedValueNoOp pins the pane name from
// data-hui-pane-open-control as DOM input: a crafted value must not
// retarget the open at another pane, plant non-canonical classes, or
// throw out of the delegated click listener (the selector interpolates
// the value; escaping keeps a crafted one a no-op).

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestE2E_PaneHostCraftedValueNoOp(t *testing.T) {
	// Tertiary first in document order: a two-branch crafted selector
	// must not resolve to it.
	host := PaneHost(PaneHostProps{
		Primary:   render.Text("The list"),
		Secondary: render.Text("S"),
		Tertiary:  render.Text("T"),
	}, nil)
	b := startBehaviorServer(t, string(host))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	var raw string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function () {
			window.__paneErr = '';
			window.addEventListener('error', function (e) { window.__paneErr = e.message || String(e); });
			var host = document.querySelector('[data-hui-panehost]');
			var ter = host.querySelector('[data-hui-pane="tertiary"]');
			var sec = host.querySelector('[data-hui-pane="secondary"]');
			var b1 = document.createElement('button');
			b1.setAttribute('data-hui-pane-open-control', 'secondary"], [data-hui-pane="tertiary');
			host.appendChild(b1);
			b1.click();
			var bad = [];
			if (!ter.hasAttribute('hidden')) bad.push('wrong pane opened (tertiary)');
			if (!sec.hasAttribute('hidden')) bad.push('secondary opened');
			if (window.__paneErr) bad.push('threw: ' + window.__paneErr);
			return JSON.stringify(bad);
		})()`, &raw)); err != nil {
		t.Fatal(err)
	}
	var bad []string
	if err := json.Unmarshal([]byte(raw), &bad); err != nil {
		t.Fatalf("probe returned %q: %v", raw, err)
	}
	if len(bad) > 0 {
		t.Errorf("SECURITY: [panehost-selector] crafted data-hui-pane-open-control value was interpolated raw into the pane selector: %s — a crafted value must match nothing so the click is a no-op", strings.Join(bad, "; "))
	}
}

// TestE2E_PaneHostDeepLinkRoundTrip pins the query deep link's rules:
// a keyed trigger records `?<param>=<pane>:<key>` through the
// navigator's history choke point, an unkeyed trigger leaves the URL
// alone, closing strips only the parameter that names the closing
// pane, Back closes, and Forward replays the KEYED TRIGGER (so the
// content it loads comes back) without appending a history entry.
func TestE2E_PaneHostDeepLinkRoundTrip(t *testing.T) {
	host := PaneHost(PaneHostProps{
		Primary: render.Join(
			render.Tag("button", map[string]string{"id": "keyed", "type": "button", "data-hui-pane-open-control": "secondary", "data-hui-pane-key": "k1"}, render.Text("open k1")),
			render.Tag("button", map[string]string{"id": "unkeyed", "type": "button", "data-hui-pane-open-control": "tertiary"}, render.Text("open tertiary")),
			render.Tag("button", map[string]string{"id": "close-ter", "type": "button", "data-hui-pane-close": "tertiary"}, render.Text("close tertiary")),
		),
		Secondary:     render.Text("S"),
		Tertiary:      render.Text("T"),
		DeepLinkParam: "pane",
	}, nil)
	b := startBehaviorServer(t, string(host))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	const param = `new URL(location.href).searchParams.get('pane')`
	const secOpen = `!document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`
	const terOpen = `!document.querySelector('[data-hui-pane="tertiary"]').hasAttribute('hidden')`
	var afterKeyed, afterUnkeyed, afterCloseTer, afterBack, afterFwd string
	var secAfterKeyed, terAfterUnkeyed, terAfterClose, secAfterBack, secAfterFwd bool
	var lenAfterKeyed, lenAfterFwd, clicks int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__clicks = 0; document.getElementById('keyed').addEventListener('click', function () { window.__clicks++; });`, nil),
		chromedp.Click(`#keyed`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(param, &afterKeyed),
		chromedp.Evaluate(secOpen, &secAfterKeyed),
		chromedp.Evaluate(`history.length`, &lenAfterKeyed),
		chromedp.Click(`#unkeyed`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(param, &afterUnkeyed),
		chromedp.Evaluate(terOpen, &terAfterUnkeyed),
		chromedp.Click(`#close-ter`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(param, &afterCloseTer),
		chromedp.Evaluate(terOpen, &terAfterClose),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(param, &afterBack),
		chromedp.Evaluate(secOpen, &secAfterBack),
		chromedp.Evaluate(`history.forward()`, nil),
		chromedp.Sleep(time.Second),
		chromedp.Evaluate(param, &afterFwd),
		chromedp.Evaluate(secOpen, &secAfterFwd),
		chromedp.Evaluate(`history.length`, &lenAfterFwd),
		chromedp.Evaluate(`window.__clicks`, &clicks),
	); err != nil {
		t.Fatal(err)
	}
	if afterKeyed != "secondary:k1" || !secAfterKeyed {
		t.Errorf("keyed open: pane=%q open=%v, want secondary:k1 and open", afterKeyed, secAfterKeyed)
	}
	if afterUnkeyed != "secondary:k1" || !terAfterUnkeyed {
		t.Errorf("unkeyed tertiary open changed the URL or did not open: pane=%q open=%v", afterUnkeyed, terAfterUnkeyed)
	}
	if afterCloseTer != "secondary:k1" || terAfterClose {
		t.Errorf("closing the tertiary pane must not strip the secondary's deep link: pane=%q tertiary open=%v", afterCloseTer, terAfterClose)
	}
	if afterBack != "" || secAfterBack {
		t.Errorf("Back: pane=%q open=%v, want no param and closed", afterBack, secAfterBack)
	}
	if afterFwd != "secondary:k1" || !secAfterFwd {
		t.Errorf("Forward: pane=%q open=%v, want secondary:k1 and open", afterFwd, secAfterFwd)
	}
	if clicks != 2 {
		t.Errorf("Forward must replay the keyed trigger (content comes back through its click): clicks=%d, want 2", clicks)
	}
	if lenAfterFwd != lenAfterKeyed {
		t.Errorf("the replay appended to history: length %d after open, %d after Forward", lenAfterKeyed, lenAfterFwd)
	}
}

// TestE2E_PaneHostSwapIsOneHistoryEntry pins the swap's history shape:
// swapping from a deep-linked secondary to a keyed tertiary is one user
// action, so the sibling's close writes nothing and the open records
// the result — one entry, not a strip followed by a push.
func TestE2E_PaneHostSwapIsOneHistoryEntry(t *testing.T) {
	host := PaneHost(PaneHostProps{
		Primary: render.Join(
			render.Tag("button", map[string]string{"id": "sec", "type": "button", "data-hui-pane-open-control": "secondary", "data-hui-pane-key": "a"}, render.Text("a")),
			render.Tag("button", map[string]string{"id": "swap", "type": "button", "data-hui-pane-swap": "tertiary", "data-hui-pane-key": "b"}, render.Text("b")),
		),
		Secondary:     render.Text("S"),
		Tertiary:      render.Text("T"),
		DeepLinkParam: "pane",
	}, nil)
	b := startBehaviorServer(t, string(host))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	var before, after int
	var param string
	if err := chromedp.Run(ctx,
		chromedp.Click(`#sec`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`history.length`, &before),
		chromedp.Click(`#swap`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`history.length`, &after),
		chromedp.Evaluate(`new URL(location.href).searchParams.get('pane')`, &param),
	); err != nil {
		t.Fatal(err)
	}
	if param != "tertiary:b" {
		t.Errorf("swap did not record the tertiary pane: pane=%q", param)
	}
	if after != before+1 {
		t.Errorf("a swap must leave one history entry: length %d before, %d after", before, after)
	}
}

// TestE2E_PaneHostBareCloseClosesTopmost pins the bare close: a
// data-hui-pane-close with an EMPTY value closes the topmost open pane
// (presence decides, not the value), so the with-value arm of the
// click handler can never strand an empty-valued trigger.
func TestE2E_PaneHostBareCloseClosesTopmost(t *testing.T) {
	host := PaneHost(PaneHostProps{
		Primary: render.Join(
			render.Tag("button", map[string]string{"id": "open-sec", "type": "button", "data-hui-pane-open-control": "secondary"}, render.Text("sec")),
			render.Tag("button", map[string]string{"id": "open-ter", "type": "button", "data-hui-pane-open-control": "tertiary"}, render.Text("ter")),
			render.Tag("button", map[string]string{"id": "bare", "type": "button", "data-hui-pane-close": ""}, render.Text("close")),
		),
		Secondary: render.Text("S"),
		Tertiary:  render.Text("T"),
	}, nil)
	b := startBehaviorServer(t, string(host))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	var secOpen, terHidden bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('open-sec').click()`, nil),
		chromedp.Evaluate(`document.getElementById('open-ter').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('bare').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`, &secOpen),
		chromedp.Evaluate(`document.querySelector('[data-hui-pane="tertiary"]').hasAttribute('hidden')`, &terHidden),
	); err != nil {
		t.Fatal(err)
	}
	if !secOpen || !terHidden {
		t.Errorf("bare close must close the TOPMOST pane only: secondary open=%v tertiary hidden=%v", secOpen, terHidden)
	}
}

// TestE2E_PaneHostAPIAndEvents pins the documented programmatic API and
// events: __gofastr.openPane/closePane address the host by id (else the
// first host) and every open and close dispatches pane-host:open /
// pane-host:close from the host with detail.pane.
func TestE2E_PaneHostAPIAndEvents(t *testing.T) {
	host := PaneHost(PaneHostProps{
		ID:        "host-a",
		Primary:   render.Text("P"),
		Secondary: render.Text("S"),
	}, nil)
	page := string(host) + `<script>window.__evts = [];
document.addEventListener('pane-host:open', function (e) { window.__evts.push(['open', e.detail.pane, e.target.id]); });
document.addEventListener('pane-host:close', function (e) { window.__evts.push(['close', e.detail.pane, e.target.id]); });</script>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	var open, closed bool
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`__gofastr.openPane('host-a', 'secondary')`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`, &open),
		chromedp.Evaluate(`__gofastr.closePane('host-a', 'secondary')`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`, &closed),
		chromedp.Evaluate(`JSON.stringify(window.__evts)`, &raw),
	); err != nil {
		t.Fatal(err)
	}
	if !open || !closed {
		t.Fatalf("programmatic open/close broken: open=%v closed=%v", open, closed)
	}
	if raw != `[["open","secondary","host-a"],["close","secondary","host-a"]]` {
		t.Errorf("events wrong: got %s, want one open and one close from host-a with detail.pane", raw)
	}
}

// TestE2E_PaneHostOutsideTriggerTarget pins the outside trigger: a
// trigger beyond every host drives the host its
// data-hui-pane-host-target names, not the first host on the page.
func TestE2E_PaneHostOutsideTriggerTarget(t *testing.T) {
	mk := func(id string) render.HTML {
		return PaneHost(PaneHostProps{
			ID:        id,
			Primary:   render.Text("P " + id),
			Secondary: render.Text("S " + id),
		}, nil)
	}
	page := string(mk("first")) + string(mk("second")) +
		`<button type="button" id="outside" data-hui-pane-open-control="secondary" data-hui-pane-host-target="second">open</button>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, paneHostLoaded) {
		t.Fatal("the host marker never loaded headless-panehost")
	}
	var firstClosed, secondOpen bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('outside').click()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('first').querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`, &firstClosed),
		chromedp.Evaluate(`!document.getElementById('second').querySelector('[data-hui-pane="secondary"]').hasAttribute('hidden')`, &secondOpen),
	); err != nil {
		t.Fatal(err)
	}
	if !secondOpen || !firstClosed {
		t.Errorf("outside trigger must open the targeted host: second open=%v, first still closed=%v", secondOpen, firstClosed)
	}
}
