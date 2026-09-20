package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// ─── ConditionalField ───────────────────────────────────────────────
// Retargeted from the dropped /components/forms suite: the gallery's
// conditionalfield page is live now (a plan radio group gating a
// coupon field).

// The reader-without-script case is the point of the posture: the
// region is VISIBLE on first paint, and the headless module HIDES it
// once it arms and the watched control does not match. The assertion
// runs before any interaction, after boot settles, so it observes the
// armed state — and the armed state must be hidden for "free", shown
// for "pro".
func TestE2E_ConditionalField_HiddenUntilTheWatchedControlMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var regionHidden, couponDisabled bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/conditionalfield"),
		pageReady(),
		// Give the module a tick to load and run its first sync.
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-hui-when]')?.hidden || false`, &regionHidden),
		chromedp.Evaluate(`document.querySelector('input[name="coupon"]')?.disabled || false`, &couponDisabled),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !regionHidden {
		t.Errorf("the region should be hidden while the watched radio is on free (the gallery fixture checks free)")
	}
	if !couponDisabled {
		t.Errorf("the coupon control inside the hidden region should be disabled, so nothing hidden submits")
	}

	// Choosing the watched value shows the region again and hands
	// the control back.
	var shown, couponEnabled bool
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const pro = document.querySelector('input[name="plan"][value="pro"]');
			if (pro) pro.click();
		})()`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`!document.querySelector('[data-hui-when]').hidden`, &shown),
		chromedp.Evaluate(`!document.querySelector('input[name="coupon"]').disabled`, &couponEnabled),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !shown {
		t.Error("choosing the watched value never showed the region")
	}
	if !couponEnabled {
		t.Error("the coupon control stayed disabled after the region showed")
	}
}

// ─── FileDropzone preview strip ─────────────────────────────────────

// The thumbnail strip is the one piece of the drop surface with no
// headless counterpart: the filedropzone module FileReader-reads each
// chosen image and renders it into the preview container, with the
// filename as alt text — an attribute sink, never markup.
func TestE2E_FileDropzone_PreviewStripRendersThumbnails(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/dropzone"),
		pageReady(),
		// Let the marker scan idle-load the filedropzone and headless
		// modules (same budget the other module e2e tests use).
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const input = document.querySelector('input[data-fui-dropzone-preview]');
			if (!input) return '';
			const dt = new DataTransfer();
			// The image's name is hostile on purpose: the strip puts a
			// filename into an alt, and the old runtime test that
			// pinned that sink is gone with its module. Assigning to
			// .alt is a property write, not a parse, so this asserts
			// the sink stays a property write.
			dt.items.add(new File(['x'], '"><img src=x onerror="window.__stripXSS=1">.png', {type: 'image/png'}));
			dt.items.add(new File(['y'], 'notes.txt', {type: 'text/plain'}));
			input.files = dt.files;
			input.dispatchEvent(new Event('change', {bubbles: true}));
			return '';
		})()`, nil),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify((() => {
			const strip = document.querySelector('[data-fui-dropzone-preview-for]');
			const imgs = strip ? [...strip.querySelectorAll('img')] : [];
			return {
				count: imgs.length,
				alts: imgs.map((i) => i.alt),
				srcs: imgs.map((i) => i.src.slice(0, 10)),
				list: (document.querySelector('[data-fui-comp="ui-dropzone"] [data-hui-drop-list]') || {textContent: ''}).textContent,
				// Anything parsed out of the name would land here, and
				// the canary fires if an onerror ever runs.
				stray: (document.querySelector('[data-fui-dropzone-preview-for]') || {querySelectorAll: () => []}).querySelectorAll('img[src="x"], script, iframe').length,
				canary: String(window.__stripXSS || 'clean'),
			};
		})())`, &raw),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var state struct {
		Count  int      `json:"count"`
		Alts   []string `json:"alts"`
		Srcs   []string `json:"srcs"`
		List   string   `json:"list"`
		Stray  int      `json:"stray"`
		Canary string   `json:"canary"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("json: %v", err)
	}
	// Only the image file becomes a thumbnail; the text file does not.
	if state.Count != 1 {
		t.Fatalf("expected 1 preview img for the image file among two chosen, got %d", state.Count)
	}
	const hostile = `"><img src=x onerror="window.__stripXSS=1">.png`
	if len(state.Alts) != 1 || state.Alts[0] != hostile {
		t.Errorf("preview alt should be the raw filename, got %v", state.Alts)
	}
	if state.Stray != 0 {
		t.Errorf("the filename parsed %d element(s) into the strip — the alt is an HTML sink", state.Stray)
	}
	if state.Canary != "clean" {
		t.Fatal("onerror executed from a filename in the preview strip")
	}
	if len(state.Srcs) != 1 || !strings.HasPrefix(state.Srcs[0], "data:image") {
		t.Errorf("preview src should be a data: URL from FileReader, got %v", state.Srcs)
	}
	// The names list (the headless module's half) carries BOTH names.
	if !strings.Contains(state.List, hostile) || !strings.Contains(state.List, "notes.txt") {
		t.Errorf("the chosen-files list should name both files, got %q", state.List)
	}
}
