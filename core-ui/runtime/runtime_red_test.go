//go:build red

// RED TESTS — open findings, 2026-09-03 round-3 adversarial pass
// (tests-only; no fix applied).
//
// Family: selector/cookie strings built from data-fui-* DOM attributes.
// The repo's own family rule — identifier-class values interpolated into
// CSS attribute selectors go through CSS.escape — is already followed by
// menu.js:104, sse.js:83, runtime.js:663, sortablelist.js:406,
// toggleaction.js:80 and scrollspy.js:35. The sites pinned below do not,
// plus one cookie-concatenation site and one success-path corruption bug.
package runtime

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// ---------------------------------------------------------------------------
// Behavioral reds (chromedp, gadget_e2e_test.go harness).
// ---------------------------------------------------------------------------

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: values concatenated into document.cookie from data-fui-* attributes must be
// component-encoded first; a DOM-sourced dismiss id can never contribute cookie
// delimiters, so the only cookie planted in the module's namespace is the one the
// module meant to plant.
// Surfaces: recordDismiss in src/banner.js (document.cookie = STORAGE_PREFIX + id +
// '=1; path=/; max-age=31536000; SameSite=Lax').
// Finding (severity: medium — needs same-origin markup/attribute injection, the
// gadget-family threat model): the dismiss id is interpolated raw, so a crafted
// data-fui-banner-dismiss-id 'probe=x; Path=/' parses as cookie name
// gofastr.banner-dismiss.probe value x with attacker-chosen attributes (Path/Max-Age/
// Secure/Domain are all injectable the same way; Path is probed here because every
// origin a test browser can use accepts it). The dismissal the module meant to record
// is never stored under its id key, and an attacker-chosen name/value pair IS planted
// inside the gofastr.banner-dismiss.* namespace the server reads back.
// Fix direction: encodeURIComponent(id) at the cookie concatenation (the localStorage
// mirror is already an opaque key), so the id cannot carry ';' or '='.
func TestBannerRedEscapesDismissCookieId(t *testing.T) {
	// Benign banner at boot gives the marker scanner its load trigger;
	// the crafted banner is injected post-boot, the reachable shape for
	// attribute injection (island swap / RPC innerHTML / SPA merge).
	g := startGadgetServer(t, `[]`, `
<div data-fui-comp="ui-banner" id="bn0">
  <button type="button" id="dismiss0" data-fui-banner-dismiss
          data-fui-banner-dismiss-id="boot-marker">x</button>
</div>`)

	ctx := newSeedBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.banner&&window.__gofastr.banner.rescan)`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`(function () {
			var bn = document.createElement('div');
			bn.setAttribute('data-fui-comp', 'ui-banner');
			var b = document.createElement('button');
			b.type = 'button';
			b.setAttribute('data-fui-banner-dismiss', '');
			b.setAttribute('data-fui-banner-dismiss-id', 'probe=x; Path=/');
			bn.appendChild(b);
			document.body.appendChild(bn);
			b.click();
			var want = 'gofastr.banner-dismiss.' + encodeURIComponent('probe=x; Path=/');
			var names = document.cookie.split(';')
				.map(function (c) { return c.trim().split('=')[0]; })
				.filter(function (n) { return n.indexOf('gofastr.banner-dismiss.') === 0; });
			return JSON.stringify({ names: names, want: want });
		})()`, &raw),
	); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Names []string `json:"names"`
		Want  string   `json:"want"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("probe returned %q: %v", raw, err)
	}
	for _, n := range got.Names {
		if n != got.Want {
			t.Errorf("SECURITY: [banner-cookie] crafted dismiss id planted cookie %q; only the encoded name %q is legitimate — recordDismiss concatenates the raw data-fui-banner-dismiss-id into document.cookie, so the crafted id injects cookie attributes and stores an attacker-chosen name/value pair in the gofastr.banner-dismiss.* namespace", n, got.Want)
		}
	}
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: the pane name from data-fui-pane-open/-close/-swap is DOM input; the
// attribute selector built from it must not let a crafted value retarget openPane at
// another pane, plant classes, or throw out of the delegated click listener.
// Surfaces: paneEl in src/panehost.js (host.querySelector('[data-fui-pane="' + pane +
// '"]')), fed by onClick's getAttribute('data-fui-pane-open'|...); sibling site
// panehost.js:154 escapes the same class of input with CSS.escape.
// Finding (severity: medium — needs same-origin markup/attribute injection): 'secondary"],
// [data-fui-pane="tertiary' builds a two-branch selector, so openPane resolves the
// crafted trigger to whichever pane comes first in the DOM (the wrong pane), and
// 'ui-pane-host--'+pane+'-open' carries the spaces into classList.add, which throws
// InvalidCharacterError; a value with a bare quote ('b"ad') makes querySelector itself
// throw. Either throw escapes the delegated listener.
// Fix direction: validate pane against the SIDE list ('secondary'|'tertiary') at the
// onClick boundary (or CSS.escape(pane) in paneEl, matching line 154) so a crafted
// value matches nothing and the click is a no-op.
func TestPaneHostRedEscapesPaneSelector(t *testing.T) {
	// Tertiary first in document order: the two-branch crafted selector
	// must not resolve to it.
	g := startGadgetServer(t, `[]`, `
<div data-fui-pane-host id="ph1">
  <div data-fui-pane="tertiary" id="pane-ter" hidden>T</div>
  <div data-fui-pane="secondary" id="pane-sec" hidden>S</div>
</div>
<div data-fui-pane-host id="ph2">
  <div data-fui-pane="secondary" id="pane-sec2" hidden>S2</div>
</div>`)

	ctx := newSeedBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.panehost)`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`(function () {
			window.__paneErr = '';
			window.addEventListener('error', function (e) { window.__paneErr = e.message || String(e); });
			var b1 = document.createElement('button');
			b1.setAttribute('data-fui-pane-open', 'secondary"], [data-fui-pane="tertiary');
			document.getElementById('ph1').appendChild(b1);
			var b2 = document.createElement('button');
			b2.setAttribute('data-fui-pane-open', 'b"ad');
			document.getElementById('ph2').appendChild(b2);
			b1.click();
			b2.click();
			var bad = [];
			if (!document.getElementById('pane-ter').hasAttribute('hidden')) bad.push('wrong pane opened (tertiary)');
			if (!document.getElementById('pane-sec').hasAttribute('hidden')) bad.push('secondary opened');
			if (!document.getElementById('pane-sec2').hasAttribute('hidden')) bad.push('ph2 secondary opened');
			var h = document.getElementById('ph1');
			for (var i = 0; i < h.classList.length; i++) {
				var c = h.classList[i];
				if (c !== 'ui-pane-host' && !/^ui-pane-host--(secondary|tertiary)-open$/.test(c)) {
					bad.push('non-canonical class ' + c);
				}
			}
			if (window.__paneErr) bad.push('threw: ' + window.__paneErr);
			return JSON.stringify(bad);
		})()`, &raw),
	); err != nil {
		t.Fatal(err)
	}
	var bad []string
	if err := json.Unmarshal([]byte(raw), &bad); err != nil {
		t.Fatalf("probe returned %q: %v", raw, err)
	}
	if len(bad) > 0 {
		t.Errorf("SECURITY: [panehost-selector] crafted data-fui-pane-open value was interpolated raw into the pane selector: %s — panehost.js builds '[data-fui-pane=\"'+pane+'\"]' without CSS.escape (its own line 154 escapes the same class), so a crafted value retargets openPane at the wrong pane and throws out of the delegated click listener", strings.Join(bad, "; "))
	}
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: a post-success UI hint (data-fui-rpc-scroll-to) must degrade without
// corrupting the RPC result — a malformed selector can never overwrite a successful
// response signal with the network-error object.
// Surfaces: src/rpc.js _dispatchRPC — document.querySelector(scrollSel) at the
// data-fui-rpc-scroll-to branch runs inside the main try, so its DOMException lands
// in the catch that writes {ok:false, status:0, text:'Network error...'} over the
// signal set from the 2xx body lines earlier.
// Finding (severity: low — functional corruption, not injection; any page carrying a
// typo'd scroll-to selector reports every successful RPC as a network error).
// Fix direction: wrap the scroll-to lookup (or every post-response hint) in its own
// try/catch like the pushState branch above it, so selector failures degrade to a
// no-op and the success signal stands.
func TestRpcRedScrollSelectorDegradesOnly(t *testing.T) {
	g := startGadgetServer(t, `[]`, `
<button type="button" id="rpcbtn" data-fui-rpc="/rpcok" data-fui-rpc-method="POST"
        data-fui-rpc-signal="result" data-fui-rpc-scroll-to="[[">go</button>
<span id="sig" data-fui-signal="result"></span>`)

	ctx := newSeedBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.rpc)`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Click(`#rpcbtn`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.getSignal && window.__gofastr.getSignal('result') !== undefined`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`(function () {
			var s = window.__gofastr.getSignal('result');
			if (s && typeof s === 'object') {
				return JSON.stringify({ object: true, ok: s.ok, status: s.status, text: String(s.text || '') });
			}
			return JSON.stringify({ object: false, type: typeof s });
		})()`, &raw),
	); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Object bool   `json:"object"`
		OK     *bool  `json:"ok"`
		Status *int   `json:"status"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("probe returned %q: %v", raw, err)
	}
	if got.Object && got.OK != nil && !*got.OK {
		t.Errorf("SECURITY: [rpc-scroll-selector] successful RPC overwritten with the error object {ok:%v,status:%v,text:%q} — the data-fui-rpc-scroll-to querySelector throws on a malformed selector inside the main try, and the catch overwrites the response signal set from the 2xx body; the hint must degrade without touching the result", *got.OK, *got.Status, got.Text)
	}
}

// ---------------------------------------------------------------------------
// Source-contract reds (runtime_security_test.go readSrc harness).
//
// Property (one pin per module surface): selector strings built from
// data-fui-* attributes escape or guard the interpolated value, matching
// menu.js:104 / sse.js:83 / runtime.js:663 (the repo's family rule).
// ---------------------------------------------------------------------------

// redSelectorCallArg locates the querySelector/querySelectorAll call whose
// argument contains anchor and returns the argument text and its start
// offset. ok is false when the anchor or its enclosing call is missing.
func redSelectorCallArg(src, anchor string) (string, int, bool) {
	i := strings.Index(src, anchor)
	if i < 0 {
		return "", -1, false
	}
	start := strings.LastIndex(src[:i], "querySelector(")
	if j := strings.LastIndex(src[:i], "querySelectorAll("); j > start {
		start = j
	}
	if start < 0 {
		return "", -1, false
	}
	rel := strings.IndexByte(src[start:start+24], '(')
	if rel < 0 {
		return "", -1, false
	}
	open := start + rel
	depth := 0
	for k := open; k < len(src); k++ {
		switch src[k] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[open+1 : k], open + 1, true
			}
		}
	}
	return "", -1, false
}

// redAssertEscapedSelector asserts the querySelector(All) argument containing
// anchor references CSS.escape: identifier-class values (ids, names, keys)
// interpolated into CSS attribute selectors must be escaped, so a crafted
// value carrying '"]' cannot re-target the lookup or throw an
// invalid-selector error out of the handler.
func redAssertEscapedSelector(t *testing.T, src, file, anchor string) {
	t.Helper()
	arg, argPos, ok := redSelectorCallArg(src, anchor)
	if !ok {
		t.Fatalf("anchor %q not found inside a querySelector call in %s — module changed, update this pin", anchor, file)
	}
	if !strings.Contains(arg, "CSS.escape") {
		tag := strings.TrimSuffix(filepath.Base(file), ".js")
		t.Errorf("SECURITY: [%s-selector] %s:%d interpolates a data-fui-* value raw into the selector %q — a crafted value with '\"]' re-targets the lookup at an attacker-chosen element or throws; menu.js:104, sse.js:83 and runtime.js:663 wrap the same class of input in CSS.escape (the family rule)",
			tag, file, redSrcLine(src, argPos), arg)
	}
}

// redGuardedLookup asserts the querySelector call starting at anchor sits
// inside a try block: for attributes whose value IS a selector by design
// (data-fui-copy-text-from, data-fui-fill-input, data-fui-charcount-source,
// data-fui-shortcut-target) escaping is wrong; the contract is that a
// malformed selector degrades to a no-op instead of aborting the delegated
// handler (before its preventDefault), the same containment copy.js's own
// JSON.parse already practices in fireToast.
func redGuardedLookup(t *testing.T, src, file, anchor string) {
	t.Helper()
	pos := strings.Index(src, anchor)
	if pos < 0 {
		t.Fatalf("anchor %q not found in %s — module changed, update this pin", anchor, file)
	}
	rel := strings.IndexByte(src[pos:pos+len(anchor)], '(')
	if rel < 0 {
		t.Fatalf("anchor %q carries no call paren in %s", anchor, file)
	}
	if !redInsideTry(src, pos+rel) {
		tag := strings.TrimSuffix(filepath.Base(file), ".js")
		t.Errorf("SECURITY: [%s-selector] %s:%d runs the data-fui-* selector lookup %q unguarded — a malformed selector throws a DOMException out of the delegated handler (before its preventDefault); the lookup must be wrapped in try/catch or otherwise validated, the guard side of the family rule menu.js:104 / sse.js:83 set",
			tag, file, redSrcLine(src, pos), anchor)
	}
}

// redInsideTry reports whether pos sits inside a try block: some enclosing
// block's opening brace directly follows the try keyword.
func redInsideTry(s string, pos int) bool {
	for {
		i := innermostOpenerBefore(s, pos)
		if i < 0 {
			return false
		}
		j := skipSpaceBack(s, i-1)
		if j >= 3 && s[j-3:j+1] == "try" {
			// Reject identifiers merely ending in "try".
			if k := j - 4; k < 0 || !redWordByte(s[k]) {
				return true
			}
		}
		pos = i
	}
}

// redWordByte reports whether c can appear in a JS identifier.
func redWordByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// redSrcLine reports the 1-based line number of pos in src.
func redSrcLine(src string, pos int) int {
	return strings.Count(src[:pos], "\n") + 1
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: the watched-field name from data-when-name is DOM input and must be
// escaped in the '[name="..."]' lookup.
// Surfaces: evaluateField in src/conditionalfield.js ("[name=" + whenName + "]").
// Finding (severity: low — developer-supplied name today, injected markup is the
// vector): a whenName carrying '"]' re-targets the visibility evaluation at other
// elements or throws; the field-name lookup never escapes.
// Fix direction: CSS.escape(whenName), the family rule.
func TestConditionalFieldRedEscapesSelector(t *testing.T) {
	src := readSrc(t, filepath.Join("src", "conditionalfield.js"))
	redAssertEscapedSelector(t, src, filepath.Join("src", "conditionalfield.js"), `+ whenName + '"]'`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: the option id resolved from a checked checkbox's DOM id must be escaped
// in the label lookup.
// Surfaces: renderChips in src/multiselect.js ("label[for=" + cb.id + "]").
// Finding (severity: low): a crafted option id carrying '"]' re-targets the chip
// label lookup or throws out of renderChips; no escaping at the site.
// Fix direction: CSS.escape(cb.id), the family rule.
func TestMultiselectRedEscapesSelector(t *testing.T) {
	src := readSrc(t, filepath.Join("src", "multiselect.js"))
	redAssertEscapedSelector(t, src, filepath.Join("src", "multiselect.js"), `label[for="' + cb.id + '"]`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: the range input's DOM id must be escaped in the output mirror lookup.
// Surfaces: syncOutput in src/slider.js ("output[for=" + id + "]").
// Finding (severity: low): an input id carrying '"]' throws out of the delegated
// input handler or mirrors into the wrong output; no escaping at the site.
// Fix direction: CSS.escape(id), the family rule.
func TestSliderRedEscapesSelector(t *testing.T) {
	src := readSrc(t, filepath.Join("src", "slider.js"))
	redAssertEscapedSelector(t, src, filepath.Join("src", "slider.js"), `output[for="' + id + '"]`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: the pair id from data-fui-range-slider is DOM input and must be escaped
// in all three pairFor lookups (min, max, output).
// Surfaces: pairFor in src/rangeslider.js — "input[data-fui-range-slider=" + id +
// "]" twice (min/max) and "output[data-fui-range-slider-value=" + id + "]".
// Finding (severity: low): a crafted pair id re-targets the cross-clamp at other
// inputs or throws out of the delegated input handler.
// Fix direction: CSS.escape(id) at all three sites, the family rule.
func TestRangeSliderRedEscapesSelector(t *testing.T) {
	file := filepath.Join("src", "rangeslider.js")
	src := readSrc(t, file)
	redAssertEscapedSelector(t, src, file, `+ '"].ui-range-slider__input--min'`)
	redAssertEscapedSelector(t, src, file, `+ '"].ui-range-slider__input--max'`)
	redAssertEscapedSelector(t, src, file, `data-fui-range-slider-value="' + id`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: ids interpolated into the deferred-slide lookups (the carousel's own id,
// and the manifest's slide keys) must be escaped.
// Surfaces: hydrateVirtual in src/carousel.js — "data-fui-carousel-deferred-for=" +
// id (the element's DOM id) and "data-fui-carousel-defer=" + k (manifest JSON keys).
// Finding (severity: low): a crafted id or manifest key carrying '"]' re-targets the
// manifest lookup (wrong or no hydration) or throws inside hydrateVirtual.
// Fix direction: CSS.escape at both sites, the family rule.
func TestCarouselRedEscapesSelector(t *testing.T) {
	file := filepath.Join("src", "carousel.js")
	src := readSrc(t, file)
	redAssertEscapedSelector(t, src, file, `data-fui-carousel-deferred-for="' + id`)
	redAssertEscapedSelector(t, src, file, `data-fui-carousel-defer="' + k`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: data-fui-copy-text-from is a selector BY DESIGN, so its lookup must at
// least degrade: a malformed selector cannot abort the delegated click handler
// before its preventDefault.
// Surfaces: the click handler in src/copy.js (document.querySelector(sel)).
// Finding (severity: low — DoS of the copy gadget; not an injection sink): '[unclosed'
// throws a DOMException out of the handler, the click's default action runs.
// Fix direction: try/catch around the lookup (copy.js's own fireToast already
// wraps JSON.parse the same way), or equivalent validation.
func TestCopyRedEscapesSelector(t *testing.T) {
	src := readSrc(t, filepath.Join("src", "copy.js"))
	redGuardedLookup(t, src, filepath.Join("src", "copy.js"), `document.querySelector(sel)`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: data-fui-fill-input and data-fui-charcount-source are selectors BY
// DESIGN; both lookups must degrade on malformed input instead of aborting the
// delegated click handler / wire pass.
// Surfaces: src/widgethelpers.js — the fill-input handler (widget.querySelector(sel)
// || document.querySelector(sel)) and wireCount (document.querySelector(sel)).
// Finding (severity: low — DoS of the fill/charcount helpers; not injection sinks):
// a malformed selector throws before preventDefault in fill-input.
// Fix direction: try/catch (or validation) around both lookups.
func TestWidgetHelpersRedEscapesSelector(t *testing.T) {
	file := filepath.Join("src", "widgethelpers.js")
	src := readSrc(t, file)
	redGuardedLookup(t, src, file, `&& widget.querySelector(sel)`)
	redGuardedLookup(t, src, file, `sel && document.querySelector(sel)`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: data-fui-shortcut-target is a selector BY DESIGN; the focus resolution
// must degrade on a malformed selector, not throw out of the document keydown
// listener.
// Surfaces: resolveTarget in src/shortcut.js (el.querySelector(sel) ||
// document.querySelector(sel)).
// Finding (severity: low — DoS of the shortcut chord): a malformed selector throws
// out of the keydown handler, the chord does nothing and the error is uncaught.
// Fix direction: try/catch around the lookups in resolveTarget.
func TestShortcutRedEscapesSelector(t *testing.T) {
	file := filepath.Join("src", "shortcut.js")
	src := readSrc(t, file)
	redGuardedLookup(t, src, file, `el.querySelector(sel)`)
	redGuardedLookup(t, src, file, `document.querySelector(sel)`)
}

// RED TEST — open finding, 2026-09-03 round-3 adversarial pass (tests-only; no fix applied).
// Property: a script src built from a data-fui-* attribute id must carry the same
// shape check loadModule applies to its names — parity within one runtime.
// Surfaces: scan in src/actionloader.js (s.src = '/__gofastr/widget/' + id + '.js...'
// from data-component / data-widget) vs runtime.js loadModule's /^[\w-]+$/ gate.
// Finding (severity: low — manifest-gated: the id must already be a key of
// window.__gofastr_actions, server-controlled, so traversal ids are unreachable
// today; this is the parity gap, the second line of defense loadModule already has):
// actionloader interpolates the id with no shape check of its own. Note component
// ids may legitimately contain dots (see its own comment), so the check here wants
// /^[\w.-]+$/ rather than loadModule's /^[\w-]+$/.
// Fix direction: reject ids failing /^[\w.-]+$/ before building the src (the dot is
// required for legitimate ids; '/' must stay impossible).
func TestActionLoaderRedChecksModuleShape(t *testing.T) {
	file := filepath.Join("src", "actionloader.js")
	src := readSrc(t, file)
	build := strings.Index(src, `'/__gofastr/widget/' + id`)
	if build < 0 {
		t.Fatalf("src construction anchor not found in %s — module changed, update this pin", file)
	}
	guard := strings.Index(src, `/^[\w.-]+$/`)
	if guard < 0 {
		guard = strings.Index(src, `/^[\w-]+$/`)
	}
	if guard < 0 || guard > build {
		t.Errorf("SECURITY: [actionloader-module-shape] %s:%d builds '/__gofastr/widget/'+id+'.js' from data-component/data-widget with no shape check before it — runtime.js loadModule gates the same class of DOM-sourced name on /^[\\w-]+$/; parity says actionloader must gate its id (dots allowed) before the src build",
			file, redSrcLine(src, build))
	}
}
