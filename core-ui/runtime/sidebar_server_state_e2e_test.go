package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// startSidebarServer serves the runtime plus named pages. A page without
// sidebar markers (conventionally "/seed") lets a test prime localStorage
// before any demand module has run.
func startSidebarServer(t *testing.T, pages map[string]string) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path[len("/__gofastr/runtime/"):]
		if len(name) > 3 {
			name = name[:len(name)-3]
		}
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	})
	for path, body := range pages {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><html><head></head><body>
%s
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`, body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// serverCollapsedSidebar mirrors what framework/ui.Sidebar renders for
// Variant: SidebarCollapsible + Collapse: SidebarCollapseCollapsed:
// data-collapsed on the root, NO storage attribute, and
// aria-expanded="false" on the button. One deliberate deviation: the
// button carries the DEFAULT expand name ("Expand navigation"), not
// the custom label the component renders with ExpandLabel set — the
// server-side label wiring is covered in framework/ui
// (TestSidebarCollapseLabelsConfigurableBothStates). The custom label
// overrides ride along as data attributes so the post-toggle check
// still exercises the runtime's custom-label path.
const serverCollapsedSidebar = `
<div class="ui-sidebar ui-sidebar--collapsible" id="srv" data-fui-sidebar data-collapsed="true">
  <div class="ui-sidebar__inline" id="srv-inline">
    <button type="button" class="ui-sidebar__collapse" data-fui-sidebar-collapse
      aria-controls="srv-inline" aria-expanded="false" aria-label="Expand navigation"
      data-fui-sidebar-collapse-label="Collapse sidebar" data-fui-sidebar-expand-label="Expand sidebar">
      <span aria-hidden="true">&#8249;</span>
    </button>
    <nav class="ui-sidebar__nav" aria-label="Primary"><ul class="ui-sidebar__list">
      <li class="ui-sidebar__item"><a class="ui-sidebar__link" href="/"><span class="ui-sidebar__label">Home</span></a></li>
    </ul></nav>
  </div>
</div>`

// TestSidebarServerOwnedCollapseIgnoresLocalStorage pins the whole point
// of SidebarCollapseCollapsed/Expanded (#298): a per-user collapse state
// restored from the database must survive first paint on a device whose
// localStorage says otherwise, and the runtime must not write a local
// value back that could later win.
func TestSidebarServerOwnedCollapseIgnoresLocalStorage(t *testing.T) {
	srv := startSidebarServer(t, map[string]string{
		"/seed": `<span id="seeded">seed</span>`,
		"/page": serverCollapsedSidebar,
	})

	ctx := newSeedBrowserCtx(t)
	var collapsed, expandedAttr, label string
	var afterCollapsed, afterExpanded, afterLabel string
	var storageValue, storageLen string
	if err := chromedp.Run(ctx,
		// Prime the localStorage poison first, on a marker-free page so
		// no module has run yet: the DEFAULT key says "expanded".
		chromedp.Navigate(srv.URL+"/seed"),
		chromedp.WaitVisible(`#seeded`, chromedp.ByID),
		chromedp.Evaluate(`localStorage.setItem('gofastr.sidebar.ui-sidebar-drawer.collapsed', 'false'); 'ok'`, nil),

		chromedp.Navigate(srv.URL+"/page"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// The module is demand-loaded by the marker scan; give it a
		// moment to arrive and run its setup pass.
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('srv').getAttribute('data-collapsed'))`, &collapsed),
		chromedp.Evaluate(`String(document.querySelector('#srv [data-fui-sidebar-collapse]').getAttribute('aria-expanded'))`, &expandedAttr),
		chromedp.Evaluate(`String(document.querySelector('#srv [data-fui-sidebar-collapse]').getAttribute('aria-label'))`, &label),

		// An in-session toggle still works (the user can expand), but
		// nothing may reach localStorage.
		chromedp.Click(`[data-fui-sidebar-collapse]`),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('srv').getAttribute('data-collapsed'))`, &afterCollapsed),
		chromedp.Evaluate(`String(document.querySelector('#srv [data-fui-sidebar-collapse]').getAttribute('aria-expanded'))`, &afterExpanded),
		chromedp.Evaluate(`String(document.querySelector('#srv [data-fui-sidebar-collapse]').getAttribute('aria-label'))`, &afterLabel),
		chromedp.Evaluate(`String(localStorage.getItem('gofastr.sidebar.ui-sidebar-drawer.collapsed'))`, &storageValue),
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
	// The flip must use the button's custom collapse label, not the
	// built-in default.
	if afterLabel != "Collapse sidebar" {
		t.Errorf("toggle did not use the custom collapse label: got %q, want %q", afterLabel, "Collapse sidebar")
	}
	if storageValue != "false" || storageLen != "1" {
		t.Errorf("server-owned toggle wrote to localStorage: value=%q length=%q (want false/1 — untouched)",
			storageValue, storageLen)
	}
}

// autoSidebarMarkup is the Auto (localStorage-owned) collapsible sidebar
// with custom labels.
const autoSidebarMarkup = `
<div class="ui-sidebar ui-sidebar--collapsible" id="auto" data-fui-sidebar data-fui-sidebar-storage="test.sidebar.key">
  <div class="ui-sidebar__inline" id="auto-inline">
    <button type="button" class="ui-sidebar__collapse" data-fui-sidebar-collapse
      aria-controls="auto-inline" aria-expanded="true" aria-label="Collapse navigation"
      data-fui-sidebar-collapse-label="Collapse sidebar" data-fui-sidebar-expand-label="Expand sidebar">
      <span aria-hidden="true">&#8249;</span>
    </button>
    <nav class="ui-sidebar__nav" aria-label="Primary"><ul class="ui-sidebar__list">
      <li class="ui-sidebar__item"><a class="ui-sidebar__link" href="/"><span class="ui-sidebar__label">Home</span></a></li>
    </ul></nav>
  </div>
</div>`

// groupsSidebarMarkup is a persistent sidebar whose groups use the
// button dialect — and NOTHING else: no collapse button, so the
// group-toggle marker alone must demand-load the sidebar module.
const groupsSidebarMarkup = `
<div class="ui-sidebar ui-sidebar--persistent" id="groups" data-fui-sidebar>
  <div class="ui-sidebar__inline" id="groups-inline">
    <nav class="ui-sidebar__nav" aria-label="Secondary"><ul class="ui-sidebar__list">
      <li class="ui-sidebar__item">
        <button type="button" class="ui-sidebar__link ui-sidebar__group-toggle" id="grp"
          data-fui-sidebar-group-toggle aria-expanded="false" aria-controls="groups-inline-g1">
          <span class="ui-sidebar__label">Settings</span>
        </button>
        <ul class="ui-sidebar__sublist" id="groups-inline-g1" hidden>
          <li class="ui-sidebar__item ui-sidebar__item--sub">
            <a class="ui-sidebar__link" href="/settings/profile"><span class="ui-sidebar__label">Profile</span></a>
          </li>
        </ul>
      </li>
    </ul></nav>
  </div>
</div>`

func TestSidebarGroupToggleAndAutoLabelRestore(t *testing.T) {
	srv := startSidebarServer(t, map[string]string{
		"/seed":   `<span id="seeded">seed</span>`,
		"/auto":   autoSidebarMarkup,
		"/groups": groupsSidebarMarkup,
	})

	ctx := newSeedBrowserCtx(t)
	var autoCollapsed, autoLabel string
	var storageAfterToggle string
	var moduleLoaded, grpExpanded, grpHidden, grpLinkShown, grpClosedDisplay string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/seed"),
		chromedp.WaitVisible(`#seeded`, chromedp.ByID),
		chromedp.Evaluate(`localStorage.setItem('test.sidebar.key', 'true'); 'ok'`, nil),

		// Auto mode: the stored value is restored after hydration, and
		// the button name flips to the CUSTOM expand label.
		chromedp.Navigate(srv.URL+"/auto"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('auto').getAttribute('data-collapsed'))`, &autoCollapsed),
		chromedp.Evaluate(`String(document.querySelector('#auto [data-fui-sidebar-collapse]').getAttribute('aria-label'))`, &autoLabel),

		// Auto mode still persists: toggling the rail writes the key.
		chromedp.Click(`#auto [data-fui-sidebar-collapse]`),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(localStorage.getItem('test.sidebar.key'))`, &storageAfterToggle),

		// A fresh document whose ONLY sidebar marker is the group
		// toggle: the module must load from that marker alone and wire
		// the delegated click.
		chromedp.Navigate(srv.URL+"/groups"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`String(!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.sidebar))`, &moduleLoaded),

		// Before the click: the closed group must actually be laying
		// out as hidden. computed display:none is the behavioural
		// consequence of the hidden attribute the button dialect
		// writes (and of the [hidden] precedence rule in the sidebar
		// stylesheet).
		chromedp.Evaluate(`getComputedStyle(document.getElementById('groups-inline-g1')).display`, &grpClosedDisplay),

		chromedp.Click(`#grp`, chromedp.ByID),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(document.getElementById('grp').getAttribute('aria-expanded'))`, &grpExpanded),
		chromedp.Evaluate(`String(document.getElementById('groups-inline-g1').hasAttribute('hidden'))`, &grpHidden),
		chromedp.Evaluate(`getComputedStyle(document.getElementById('groups-inline-g1')).display`, &grpLinkShown),
	); err != nil {
		t.Fatal(err)
	}

	if autoCollapsed != "true" {
		t.Errorf("Auto mode did not restore the stored collapsed state: data-collapsed=%q", autoCollapsed)
	}
	if autoLabel != "Expand sidebar" {
		t.Errorf("restored state did not use the custom expand label: got %q, want %q", autoLabel, "Expand sidebar")
	}
	if storageAfterToggle != "false" {
		t.Errorf("Auto mode must persist the toggle: localStorage=%q, want \"false\"", storageAfterToggle)
	}
	if moduleLoaded != "true" {
		t.Error("sidebar module did not load: the group-toggle marker alone must demand-load it without a collapse button")
	}
	if grpClosedDisplay != "none" {
		t.Errorf("closed group panel computed display=%q before the click — hidden must translate to display:none", grpClosedDisplay)
	}
	if grpExpanded != "true" || grpHidden != "false" {
		t.Errorf("group toggle click did not expand: aria-expanded=%q hidden=%q (want true/false)", grpExpanded, grpHidden)
	}
	if grpLinkShown == "none" {
		t.Errorf("expanded group panel computed display=%q — open groups must be visible", grpLinkShown)
	}
}
