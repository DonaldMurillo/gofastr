package runtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// framework/ui.FileDropzone emit (dropzone.go / fileupload.go): the
// fileupload zone carries data-fui-fileupload + .ui-fileupload__filename,
// the dropzone root carries data-fui-comp="ui-dropzone" + an input with
// data-fui-dropzone-preview + a sibling [data-fui-dropzone-preview-for]
// container. A filename is attacker-controlled (the uploader controls the
// name of the file they pick), so the runtime must insert it as TEXT.
const uploadFilenamePage = `
<div class="ui-fileupload" data-fui-comp="ui-fileupload">
  <label class="ui-fileupload__label" for="f1">Doc</label>
  <div class="ui-fileupload__zone" data-fui-fileupload>
    <input type="file" id="f1" name="f" class="ui-fileupload__input">
    <p class="ui-fileupload__prompt">Drop files or click to browse</p>
    <p class="ui-fileupload__filename" aria-live="polite"></p>
  </div>
</div>
<div class="ui-dropzone" data-fui-comp="ui-dropzone">

    <div class="ui-dropzone__zone" data-fui-fileupload="true">
      <input type="file" id="dz1" name="photos" class="ui-dropzone__input" data-fui-dropzone-preview multiple>
      <p class="ui-dropzone__prompt">Drop images</p>
      <p class="ui-dropzone__filename"></p>
    </div>
  </label>
  <div class="ui-dropzone__previews" data-fui-dropzone-preview-for="dz1" aria-live="polite"></div>
</div>
<div id="injhost"></div>`

// setFilesFor assigns a single File with the given name/type to the input
// via DataTransfer (input.files is read-only otherwise) and fires the
// bubbling `change` event both runtime modules listen for.
func setFilesFor(inputID, name, mimeType string) string {
	return `(function(){
		var input = document.getElementById('` + inputID + `');
		var dt = new DataTransfer();
		dt.items.add(new File(['x'], ` + jsQuote(name) + `, { type: '` + mimeType + `' }));
		input.files = dt.files;
		input.dispatchEvent(new Event('change', { bubbles: true }));
	})()`
}

// TestUploadFilenameRenderedAsText pins that a hostile FILENAME lands in
// the DOM as text, never as markup, on both upload surfaces:
//
//   - fileupload.js render() builds the list with li.textContent = name
//   - dropzone.js updateFilename() writes .ui-dropzone__filename via
//     textContent, and updatePreviews() sets img.alt (attribute) + a
//     data: URL from FileReader — no HTML sink.
//
// The payload is a full element with an onerror handler and a quote
// breakout prefix; if any of these paths ever switches to innerHTML /
// insertAdjacentHTML, the browser parses the element, src=x 404s, and the
// canary fires. The text assertions additionally pin that the raw name is
// DISPLAYED (the user must see their real filename), so a fix cannot
// pass by silently dropping the name.
func TestUploadFilenameRenderedAsText(t *testing.T) {
	g := startGadgetServer(t, `[]`, uploadFilenamePage)
	ctx := newSeedBrowserCtx(t)

	var fuText, fuEls, dzText, dzAlt, canary, prevCount string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#f1`, chromedp.ByID),
		// Let the marker scan idle-load both the fileupload and
		// dropzone modules (same budget the other module e2e tests use).
		chromedp.Sleep(700*time.Millisecond),

		// Hostile filenames: element + onerror + quote breakout, once
		// as a non-image (fileupload list path) and once as an image
		// (dropzone preview path).
		chromedp.Evaluate(setFilesFor("f1", `<img src=x onerror="window.__fuXSS=1">.txt`, "text/plain"), nil),
		chromedp.Evaluate(setFilesFor("dz1", `"><img src=x onerror="window.__dzXSS=1">.png`, "image/png"), nil),
		chromedp.Sleep(150*time.Millisecond),

		// fileupload: the filename list must carry the payload as text.
		chromedp.Evaluate(`document.querySelector('.ui-fileupload__filename').textContent`, &fuText),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-fileupload__filename img, .ui-fileupload__filename script, .ui-fileupload__filename iframe, .ui-fileupload__filename svg').length)`, &fuEls),
		// dropzone: .ui-dropzone__filename text + the preview img's alt.
		chromedp.Evaluate(`document.querySelector('.ui-dropzone__filename').textContent`, &dzText),
		chromedp.Evaluate(`(document.querySelector('.ui-dropzone__previews img')||{}).alt || ''`, &dzAlt),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-dropzone__previews img').length)`, &prevCount),
		// Neither onerror may have executed.
		chromedp.Evaluate(`String(window.__fuXSS || window.__dzXSS || 'clean')`, &canary),
	); err != nil {
		t.Fatal(err)
	}

	wantFU := `<img src=x onerror="window.__fuXSS=1">.txt`
	if !strings.Contains(fuText, wantFU) {
		t.Errorf("fileupload: hostile filename not displayed as literal text — textContent = %q", fuText)
	}
	if fuEls != "0" {
		t.Errorf("fileupload: filename region parsed %s element(s) out of the file name — HTML sink on the filename path", fuEls)
	}
	wantDZ := `"><img src=x onerror="window.__dzXSS=1">.png`
	if !strings.Contains(dzText, wantDZ) {
		t.Errorf("dropzone: hostile filename not displayed as literal text — textContent = %q", dzText)
	}
	if prevCount != "1" {
		t.Errorf("dropzone: expected 1 preview img for the image file, got %s", prevCount)
	}
	if !strings.Contains(dzAlt, wantDZ) {
		t.Errorf("dropzone: preview alt does not carry the raw filename (alt = %q)", dzAlt)
	}
	if canary != "clean" {
		t.Errorf("XSS: onerror executed from a filename — canary = %s", canary)
	}
}

// jsQuote renders a Go string as a JS string literal (the payload
// contains both quote styles, so build it via JSON encoding).
func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestFileUploadZoneInjectedAfterLoadGetsWired pins the MutationObserver
// scanner path for fileupload zones. wireFileUploads is called with each
// INSERTED node as root; a zone inserted as that node itself (island /
// RPC innerHTML swap) is not its own descendant, so the scanner must
// test the root element too. Observable without the fix: the zone is
// never wired, the change listener is never attached, and the filename
// list never renders.
func TestFileUploadZoneInjectedAfterLoadGetsWired(t *testing.T) {
	g := startGadgetServer(t, `[]`, uploadFilenamePage)
	ctx := newSeedBrowserCtx(t)

	var injText, injBound string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#f1`, chromedp.ByID),
		// Both modules loaded + initial zones wired.
		chromedp.Sleep(700*time.Millisecond),

		// Island-swap shape: fill an element that is ALREADY in the
		// document (how sse.js and rpc signal swaps apply island HTML:
		// el.innerHTML = html). Appending a fresh wrapper and filling
		// it in the same task would hand the observer a wrapper-record
		// whose descendants include the zone — a different, working
		// path that hides the gap this test pins.
		chromedp.Evaluate(`document.getElementById('injhost').innerHTML = '<div class="ui-fileupload__zone" data-fui-fileupload>' +
			'<input type="file" id="f9" name="g">' +
			'<p class="ui-fileupload__filename" aria-live="polite"></p></div>'`, nil),
		chromedp.Evaluate(setFilesFor("f9", `injected.txt`, "text/plain"), nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('f9').closest('[data-fui-fileupload]').__fuiWired === true)`, &injBound),
		chromedp.Evaluate(`document.getElementById('f9').closest('[data-fui-fileupload]').querySelector('.ui-fileupload__filename').textContent`, &injText),
	); err != nil {
		t.Fatal(err)
	}

	if injBound != "true" {
		t.Errorf("injected fileupload zone was not wired — __fuiWired = %s, want true (the MutationObserver scanner passes the inserted zone itself as root; it is not its own descendant)", injBound)
	}
	if !strings.Contains(injText, "injected.txt") {
		t.Errorf("injected zone is dead: filename textContent = %q, want it to list injected.txt", injText)
	}
}
