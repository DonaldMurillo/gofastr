package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// Pins the attribute-borne storage-key namespace, found by the 2026-09-04
// red-probe round; fixed in sidebar.js (setCollapsed/setup) and
// widgethelpers.js (wirePersist) by writing and reading a literal-prefixed,
// component-encoded key ("gofastr.sidebar-collapse."/"gofastr.persist." +
// encodeURIComponent(key), the banner.js dismissKey shape) with a one-time
// migration of the legacy raw entry.
//
// Property: a storage key the runtime writes that comes from a data-fui-*
// attribute must not let injected markup write outside the namespace the
// feature owns (banner.js already encodes; this is the parity decision).
// Surfaces: core-ui/runtime/src/sidebar.js::setCollapsed/setup (localStorage
// keyed by data-fui-sidebar-storage) and
// core-ui/runtime/src/widgethelpers.js::wirePersist (localStorage keyed by
// data-fui-persist-storage); contrast surface src/banner.js::dismissKey
// which encodes. The sibling pin
// sidebar_server_state_e2e_test.go::TestSidebarGroupToggleAndAutoLabelRestore
// covers the same namespace through a server-served page (including the
// legacy migration on the /auto restore path).

// TestSidebarStorageKeyIsEncoded: an injected sidebar root whose
// data-fui-sidebar-storage names another feature's key must not be able to
// write that key on collapse — and the toggle must still persist, inside
// the sidebar's own namespace.
func TestSidebarStorageKeyIsEncoded(t *testing.T) {
	g := startGadgetServer(t, `[]`, `<div id="host"></div>`)
	ctx := newSeedBrowserCtx(t)

	var foreign, namespaced string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Post-boot injection: the reachable shape for attribute injection
		// (island swap / RPC innerHTML / SPA page merge).
		chromedp.Evaluate(`document.getElementById('host').innerHTML =
			'<div class="ui-sidebar ui-sidebar--collapsible" id="sbx" data-fui-sidebar ' +
			'data-fui-sidebar-storage="gofastr.planted-by-attr">' +
			'<button type="button" id="sbxc" data-fui-sidebar-collapse>Collapse</button></div>'; true`, nil),
		// Wait for the demand-loaded sidebar module to wire (MutationObserver
		// scan), then click the collapse button.
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.sidebar)`, nil,
			chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Click(`#sbxc`, chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`String(localStorage.getItem('gofastr.planted-by-attr'))`, &foreign),
		chromedp.Evaluate(`String(localStorage.getItem('gofastr.sidebar-collapse.' + encodeURIComponent('gofastr.planted-by-attr')))`, &namespaced),
	); err != nil {
		t.Fatal(err)
	}
	if foreign != "null" && foreign != "" {
		t.Errorf("SECURITY: an attribute-borne data-fui-sidebar-storage value wrote "+
			"localStorage['gofastr.planted-by-attr']=%q — the key must be namespaced and encoded "+
			"so injected markup cannot clobber any localStorage key on the origin", foreign)
	}
	if namespaced != "true" {
		t.Errorf("the collapse toggle must still persist inside the sidebar namespace: "+
			"localStorage['gofastr.sidebar-collapse.gofastr.planted-by-attr']=%q, want \"true\"", namespaced)
	}
}

// TestPersistStorageKeyIsEncoded: an injected input whose
// data-fui-persist-storage names another feature's key must not be able to
// write that key — and the draft must still persist, inside the persist
// namespace.
func TestPersistStorageKeyIsEncoded(t *testing.T) {
	g := startGadgetServer(t, `[]`, `<div id="host"></div>`)
	ctx := newSeedBrowserCtx(t)

	var foreign, namespaced string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// widgethelpers is demand-loaded by widget mounts, and this page
		// carries none: load it directly, then inject the field. The
		// runtime's rescan loop wires nodes added after module load exactly
		// as an island swap would.
		chromedp.Evaluate(`window.__gofastr.loadModule('widgethelpers'); true`, nil),
		chromedp.Poll(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.widgethelpers)`, nil,
			chromedp.WithPollingTimeout(8*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		// Post-boot injection, same reachable shape as the sidebar probe.
		// The field arrives inside a wrapper subtree: boot's observer hands
		// module scanners the added NODE, and widgethelpers' scan wires
		// descendants of it — exactly how an island swap delivers the field.
		chromedp.Evaluate(`document.getElementById('host').innerHTML =
			'<div id="wrap"><textarea id="draft" data-fui-persist-storage="kiln-input-draft"></textarea></div>'; true`, nil),
		chromedp.Sleep(150*time.Millisecond),
		// Typing in the injected field persists the draft.
		chromedp.Evaluate(`document.getElementById('draft').value = 'planted draft';`+
			`document.getElementById('draft').dispatchEvent(new Event('input', { bubbles: true })); true`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(localStorage.getItem('kiln-input-draft'))`, &foreign),
		chromedp.Evaluate(`String(localStorage.getItem('gofastr.persist.' + encodeURIComponent('kiln-input-draft')))`, &namespaced),
	); err != nil {
		t.Fatal(err)
	}
	if foreign != "null" && foreign != "" {
		t.Errorf("SECURITY: an attribute-borne data-fui-persist-storage value wrote "+
			"localStorage['kiln-input-draft']=%q — the key must be namespaced and encoded "+
			"so injected markup cannot clobber any localStorage key on the origin", foreign)
	}
	if namespaced != "planted draft" {
		t.Errorf("the draft must still persist inside the persist namespace: "+
			"localStorage['gofastr.persist.kiln-input-draft']=%q, want \"planted draft\"", namespaced)
	}
}
