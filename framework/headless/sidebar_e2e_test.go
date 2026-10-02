package headless

// The sidebar module's behavioural contracts, ported from the retired
// core-ui/runtime sidebar e2e: the server-owned collapse ignores (and
// never writes) localStorage; the auto collapse restores the stored
// state and re-says the custom labels; the button-dialect group toggle
// owns aria-expanded while the panel it names owns hidden; and an
// attribute-borne storage key can only name an entry inside the
// module's namespace.

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

// A server-owned collapsible sidebar: collapse state on the root, NO
// storage key, custom labels on the toggle.
const serverOwnedSidebar = `
<div data-hui-sidebar id="srv" data-collapsed="true">
  <button type="button" id="srv-toggle" data-hui-sidebar-toggle
    aria-controls="srv-inline" aria-expanded="false" aria-label="Expand navigation"
    data-hui-sidebar-collapse-label="Collapse sidebar" data-hui-sidebar-expand-label="Expand sidebar">
    <span aria-hidden="true">&#8249;</span>
  </button>
  <nav aria-label="Primary"><ul id="srv-inline"><li><a href="/">Home</a></li></ul></nav>
</div>`

// The localStorage poison primes the pre-namespace raw spelling AND
// the namespaced one: neither may win against the server's state.
func TestE2E_SidebarServerOwnedCollapseIgnoresStorage(t *testing.T) {
	b := startBehaviorServer(t, serverOwnedSidebar)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sidebarLoaded) {
		t.Fatal("the sidebar marker never loaded headless-sidebar")
	}
	var collapsed, expandedAttr, label, storageLen string
	var afterCollapsed, afterExpanded, afterLabel string
	if err := chromedp.Run(ctx,
		// Poison both spellings first.
		chromedp.Evaluate(`(function () {
			localStorage.setItem('gofastr.sidebar.ui-sidebar-drawer.collapsed', 'false');
			localStorage.setItem('gofastr.sidebar-collapse.' + encodeURIComponent('gofastr.sidebar.ui-sidebar-drawer.collapsed'), 'false');
			return true;
		})()`, nil),
		chromedp.Evaluate(`String(document.getElementById('srv').getAttribute('data-collapsed'))`, &collapsed),
		chromedp.Evaluate(`String(document.getElementById('srv-toggle').getAttribute('aria-expanded'))`, &expandedAttr),
		chromedp.Evaluate(`String(document.getElementById('srv-toggle').getAttribute('aria-label'))`, &label),

		// An in-session toggle still works (the user can expand), but
		// nothing may reach localStorage.
		chromedp.Click(`#srv-toggle`, chromedp.ByID),
		chromedp.Evaluate(`String(document.getElementById('srv').getAttribute('data-collapsed'))`, &afterCollapsed),
		chromedp.Evaluate(`String(document.getElementById('srv-toggle').getAttribute('aria-expanded'))`, &afterExpanded),
		chromedp.Evaluate(`String(document.getElementById('srv-toggle').getAttribute('aria-label'))`, &afterLabel),
		chromedp.Evaluate(`String(localStorage.length)`, &storageLen),
	); err != nil {
		t.Fatal(err)
	}
	if collapsed != "true" || expandedAttr != "false" || label != "Expand navigation" {
		t.Errorf("hydration overwrote server-owned collapse state: data-collapsed=%q aria-expanded=%q aria-label=%q (want true/false/Expand navigation)",
			collapsed, expandedAttr, label)
	}
	if afterCollapsed != "false" || afterExpanded != "true" {
		t.Errorf("in-session toggle broken: data-collapsed=%q aria-expanded=%q (want false/true)", afterCollapsed, afterExpanded)
	}
	if afterLabel != "Collapse sidebar" {
		t.Errorf("toggle did not use the custom collapse label: got %q, want %q", afterLabel, "Collapse sidebar")
	}
	if storageLen != "2" {
		t.Errorf("server-owned toggle wrote to localStorage: length=%q (want 2 — the two poisoned entries, untouched)", storageLen)
	}
}

// The auto (storage-owned) sidebar and the button-dialect groups page.
const autoSidebar = `
<div data-hui-sidebar id="auto" data-hui-sidebar-storage="test.sidebar.key">
  <button type="button" id="auto-toggle" data-hui-sidebar-toggle
    aria-controls="auto-inline" aria-expanded="true" aria-label="Collapse navigation"
    data-hui-sidebar-collapse-label="Collapse sidebar" data-hui-sidebar-expand-label="Expand sidebar">
    <span aria-hidden="true">&#8249;</span>
  </button>
  <nav aria-label="Primary"><ul id="auto-inline"><li><a href="/">Home</a></li></ul></nav>
</div>`

const groupsSidebar = `
<div data-hui-sidebar id="groups">
  <nav aria-label="Secondary"><ul>
    <li>
      <button type="button" id="grp" data-hui-sidebar-group-toggle
        aria-expanded="false" aria-controls="groups-inline-g1">
        <span>Settings</span>
      </button>
      <ul id="groups-inline-g1" hidden>
        <li><a href="/settings/profile">Profile</a></li>
      </ul>
    </li>
  </ul></nav>
</div>`

func TestE2E_SidebarAutoRestoreAndGroupToggle(t *testing.T) {
	b := startBehaviorServer(t, "",
		func(mux *http.ServeMux) {
			mux.HandleFunc("/auto", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, `<!doctype html><html><head>`+
					`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
					`<span id="ready">ready</span>%s%s`+
					`<script src="/__gofastr/runtime.js"></script></body></html>`,
					runtime.BehaviorsJSON(), autoSidebar, groupsSidebar)
			})
		})
	// behaviorPage navigates to "/" first; seed the store there (the
	// marker-free page runs no module), then move to the sidebar page
	// so the boot scan restores the seeded state.
	ctx := behaviorPage(t, b)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`localStorage.setItem('gofastr.sidebar-collapse.' + encodeURIComponent('test.sidebar.key'), 'true'); true`, nil),
		chromedp.Navigate(b.srv.URL+"/auto"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, sidebarLoaded) {
		t.Fatal("the sidebar marker never loaded headless-sidebar")
	}
	var autoCollapsed, autoLabel, storageAfterToggle string
	var grpExpanded, grpHidden, grpDisplay string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`String(document.getElementById('auto').getAttribute('data-collapsed'))`, &autoCollapsed),
		chromedp.Evaluate(`String(document.getElementById('auto-toggle').getAttribute('aria-label'))`, &autoLabel),

		// Auto mode persists: toggling writes the key.
		chromedp.Click(`#auto-toggle`, chromedp.ByID),
		chromedp.Evaluate(`String(localStorage.getItem('gofastr.sidebar-collapse.' + encodeURIComponent('test.sidebar.key')))`, &storageAfterToggle),

		// The group dialect: button owns aria-expanded, panel owns hidden.
		chromedp.Evaluate(`getComputedStyle(document.getElementById('groups-inline-g1')).display`, &grpDisplay),
		chromedp.Click(`#grp`, chromedp.ByID),
		chromedp.Evaluate(`String(document.getElementById('grp').getAttribute('aria-expanded'))`, &grpExpanded),
		chromedp.Evaluate(`String(document.getElementById('groups-inline-g1').hasAttribute('hidden'))`, &grpHidden),
	); err != nil {
		t.Fatal(err)
	}
	if autoCollapsed != "true" {
		t.Errorf("auto mode did not restore the stored collapsed state: data-collapsed=%q", autoCollapsed)
	}
	if autoLabel != "Expand sidebar" {
		t.Errorf("restored state did not use the custom expand label: got %q, want %q", autoLabel, "Expand sidebar")
	}
	if storageAfterToggle != "false" {
		t.Errorf("auto mode must persist the toggle: localStorage=%q, want \"false\"", storageAfterToggle)
	}
	if grpDisplay != "none" {
		t.Errorf("closed group panel computed display=%q before the click — hidden must translate to display:none", grpDisplay)
	}
	if grpExpanded != "true" || grpHidden != "false" {
		t.Errorf("group toggle click did not expand: aria-expanded=%q hidden=%q (want true/false)", grpExpanded, grpHidden)
	}
}

// The storage-key namespace contract: an attribute-borne
// data-hui-sidebar-storage value (markup injected after boot) can only
// name an entry inside the module's namespace — never an arbitrary
// origin key.
func TestE2E_SidebarStorageKeyIsEncoded(t *testing.T) {
	// A dormant marker loads the module at boot; the planted sidebar
	// arrives afterwards through the injection path.
	b := startBehaviorServer(t, `<div data-hui-sidebar data-hui-sidebar-collapse="none"></div><div id="host"></div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sidebarLoaded) {
		t.Fatal("the sidebar marker never loaded headless-sidebar")
	}
	var foreign, namespaced string
	// Post-boot injection: the reachable shape for attribute injection
	// (island swap / RPC innerHTML / SPA page merge). The kernel's
	// demand scan loads the module from the injected marker alone.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('host').innerHTML =
			'<div data-hui-sidebar id="sbx" data-hui-sidebar-storage="gofastr.planted-by-attr">' +
			'<button type="button" id="sbxc" data-hui-sidebar-toggle>Collapse</button></div>'; true`, nil),
		chromedp.Sleep(300*1e6),
		chromedp.Evaluate(`document.getElementById('sbxc').click(); true`, nil),
		chromedp.Sleep(300*1e6),
		chromedp.Evaluate(`String(localStorage.getItem('gofastr.planted-by-attr'))`, &foreign),
		chromedp.Evaluate(`String(localStorage.getItem('gofastr.sidebar-collapse.' + encodeURIComponent('gofastr.planted-by-attr')))`, &namespaced),
	); err != nil {
		t.Fatal(err)
	}
	if foreign != "null" && foreign != "" {
		t.Errorf("SECURITY: an attribute-borne data-hui-sidebar-storage value wrote "+
			"localStorage['gofastr.planted-by-attr']=%q — the key must be namespaced and encoded "+
			"so injected markup cannot clobber any localStorage key on the origin", foreign)
	}
	if namespaced != "true" && namespaced != "false" {
		t.Errorf("the collapse toggle must still persist inside the sidebar namespace: "+
			"localStorage['gofastr.sidebar-collapse.gofastr.planted-by-attr']=%q, want true|false", namespaced)
	}
}

// The primitive's OWN render (not a hand-written fixture): with
// Collapse "auto" the root nav carries the collapse mode, and a click
// on a LINK inside it is navigation — never default-prevented, never a
// collapse. Only the toggle button collapses the sidebar. (The port
// matched [data-hui-sidebar-collapse] as a click target, so every
// click inside the nav resolved to the root and collapsed it.)
func TestE2E_SidebarLinkClickIsNavigationNotCollapse(t *testing.T) {
	sidebar := Sidebar(SidebarProps{
		Variant:            "collapsible",
		Collapse:           "auto",
		CollapseStorageKey: "k",
		Items:              []SidebarItem{{Label: "Intro", Href: "#intro"}},
		NavLabel:           "Docs",
		ID:                 "sb",
	}, nil)
	b := startBehaviorServer(t, string(sidebar))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, sidebarLoaded) {
		t.Fatal("the sidebar marker never loaded headless-sidebar")
	}
	var before, afterLink, afterToggle string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__prev = 'unset'; document.addEventListener('click', function (e) { window.__prev = e.defaultPrevented; });`, nil),
		chromedp.Evaluate(`String(document.getElementById('sb').getAttribute('data-collapsed'))`, &before),
		chromedp.Evaluate(`document.querySelector('#sb a[href="#intro"]').click()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`String(window.__prev)`, &afterLink),
		chromedp.Evaluate(`String(document.getElementById('sb').getAttribute('data-collapsed'))`, &afterToggle),
	); err != nil {
		t.Fatal(err)
	}
	if afterLink != "false" {
		t.Errorf("the link click was default-prevented (%v): a sidebar link is navigation, not a collapse control", afterLink)
	}
	if afterToggle != before {
		t.Errorf("the link click changed data-collapsed: before=%q after=%q", before, afterToggle)
	}
	var flipped, labelAfter string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#sb [data-hui-sidebar-toggle]').click()`, nil),
		chromedp.Sleep(150*1e6),
		chromedp.Evaluate(`String(document.getElementById('sb').getAttribute('data-collapsed'))`, &flipped),
		chromedp.Evaluate(`String(document.querySelector('#sb [data-hui-sidebar-toggle]').getAttribute('aria-label'))`, &labelAfter),
	); err != nil {
		t.Fatal(err)
	}
	if flipped == before || flipped == "" {
		t.Errorf("the toggle click must flip data-collapsed: before=%q after=%q", before, flipped)
	}
	// The primitive's toggle carries no label attributes, so the module
	// has no wording of its own to re-say: the button keeps the name it
	// shipped (the nav label). An English fallback here would be a
	// sentence every translated page says in English.
	if labelAfter != "Docs" {
		t.Errorf("the module relabelled a toggle that carries no data labels: aria-label=%q, want the shipped %q", labelAfter, "Docs")
	}
}
