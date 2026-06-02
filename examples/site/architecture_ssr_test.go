package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// SSR-layer architecture contract — these lock the "inline only when
// visible at first paint" rule that the uihost applies on every page
// response. Hidden click-to-open widgets must NOT appear in the
// rendered HTML; deep-link-matched widgets MUST appear unhidden;
// auto-mount (non-hidden) widgets MUST appear. Apps depending on
// search-engine indexing, screen-reader landmark navigation, or
// no-JS rendering rely on this contract holding.

// archStartServer brings up the live site backed by setupServer()
// — exactly the binary served at :8083. Returns the base URL.
func archStartServer(t *testing.T) string {
	t.Helper()
	fwApp := setupServer()
	srv := httptest.NewServer(fwApp.Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

func archFetch(t *testing.T, base, path string) string {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("fetch %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// CONTRACT: A hidden click-to-open widget is NOT inlined into the
// page response. The runtime fetches its chrome lazily on first
// data-fui-open click. Saves bytes per page; the trigger must do
// the work the user expects without a guaranteed-loaded chrome.
func TestArchSSR_HiddenWidgetNotInlined(t *testing.T) {
	base := archStartServer(t)
	page := archFetch(t, base, "/components/modal")

	// site-demo-modal is registered Hidden — must not appear in the bare page.
	if strings.Contains(page, `data-fui-widget="site-demo-modal"`) {
		t.Error("contract violation: hidden widget 'site-demo-modal' must NOT be inlined into the page response; runtime fetches chrome lazily")
	}
	if strings.Contains(page, "site-demo-modal-heading") {
		t.Error("contract violation: hidden widget heading id bled into the page; the SSR layer is inlining hidden widgets")
	}
}

// CONTRACT: A deep-link-matched widget IS inlined into the page
// response with its content visible — search engines and screen
// readers see the modal as part of the document, no JS required to
// reveal it. Hydration on this element is a no-op DOM-wise.
func TestArchSSR_DeepLinkWidgetInlined(t *testing.T) {
	base := archStartServer(t)
	// site-demo-modal is configured DeepLink("modal","user-edit") + DeepLinkParam("user_id").
	page := archFetch(t, base, "/components/modal?modal=user-edit&user_id=42")

	if !strings.Contains(page, `data-fui-widget="site-demo-modal"`) {
		t.Error("contract violation: deep-link-matched widget must be SSR-inlined; was absent from the page response")
	}
	if !strings.Contains(page, `role="dialog"`) {
		t.Error("contract violation: SSR-inlined dialog widget should carry role='dialog'")
	}
	if !strings.Contains(page, `aria-modal="true"`) {
		t.Error("contract violation: SSR-inlined modal widget should carry aria-modal='true'")
	}
}

// CONTRACT: A non-hidden auto-mount widget (toast stack, banner,
// persistent panel) IS inlined on every page so it's present
// immediately on first paint — no flicker, no per-page chrome fetch.
func TestArchSSR_AutoMountWidgetInlined(t *testing.T) {
	base := archStartServer(t)
	// site-toasts is registered without .Hidden() in main.go so it
	// should appear on every page.
	for _, path := range []string{"/", "/get-started", "/components/", "/components/modal"} {
		page := archFetch(t, base, path)
		if !strings.Contains(page, `data-fui-widget="site-toasts"`) {
			t.Errorf("contract violation: auto-mount toast stack must be inlined on %s", path)
		}
	}
}

// CONTRACT: The deep-link-matched widget is open (no `hidden` attr),
// not just present. Closed-but-inlined would be a confusing state
// — view-source would show structure not visible in the browser.
func TestArchSSR_DeepLinkWidgetIsOpen(t *testing.T) {
	base := archStartServer(t)
	page := archFetch(t, base, "/components/modal?modal=user-edit&user_id=42")

	// Find the site-demo-modal widget's outermost tag and assert no `hidden`.
	idx := strings.Index(page, `data-fui-widget="site-demo-modal"`)
	if idx < 0 {
		t.Fatal("widget not in page; can't check hidden state")
	}
	// The tag opens at the nearest preceding `<` and closes at the
	// next `>`. The `hidden` attribute, if present, lives in between.
	open := strings.LastIndex(page[:idx], "<")
	close := strings.Index(page[idx:], ">") + idx
	tag := page[open : close+1]
	if strings.Contains(tag, " hidden") {
		t.Errorf("contract violation: deep-link widget should be OPEN (no `hidden` attr); got tag=%s", tag)
	}
}

// CONTRACT: The bare `/components/modal` URL (no deep-link match)
// has no hidden overlay widgets inlined. They are fetched lazily —
// apps must rely on the chrome endpoint rather than assume the
// elements exist at page-load time.
func TestArchSSR_BareURLHasNoHiddenWidgets(t *testing.T) {
	base := archStartServer(t)
	page := archFetch(t, base, "/components/modal")

	for _, hidden := range []string{
		`data-fui-widget="site-demo-modal"`,
		`data-fui-widget="site-demo-drawer"`,
		`data-fui-widget="site-demo-bottomsheet"`,
	} {
		if strings.Contains(page, hidden) {
			t.Errorf("contract violation: hidden widget %q was inlined into bare /components/modal", hidden)
		}
	}
}

// CONTRACT: Per-page scoping flows through the SSR-inline path AND
// the catalog endpoint. Page-scoped widgets (.Pages / .PagesPrefix)
// don't appear in unrelated pages' HTML or catalog. site-toasts
// stays global; demo widgets are scoped to their specific pages.
func TestArchSSR_ScopedWidgetsAbsentFromOtherPages(t *testing.T) {
	base := archStartServer(t)

	// site-demo-modal is scoped to /components/modal.
	// On /components/drawer it should be absent from BOTH the page
	// HTML and the catalog endpoint.
	drawerPage := archFetch(t, base, "/components/drawer")
	if strings.Contains(drawerPage, `data-fui-widget="site-demo-modal"`) {
		t.Error("scoped widget site-demo-modal leaked onto /components/drawer SSR")
	}
	catalog := archFetch(t, base, "/__gofastr/widgets?page=/components/drawer")
	if strings.Contains(catalog, `"name":"site-demo-modal"`) {
		t.Error("scoped widget site-demo-modal leaked into /components/drawer catalog")
	}

	// site-demo-drawer is scoped to /components/drawer. Absent on
	// /components/modal.
	modalPage := archFetch(t, base, "/components/modal")
	if strings.Contains(modalPage, `data-fui-widget="site-demo-drawer"`) {
		t.Error("scoped widget site-demo-drawer leaked onto /components/modal SSR")
	}

	// site-toasts is global — appears on every page.
	for _, page := range []string{"/", "/get-started", "/components/modal", "/components/drawer"} {
		body := archFetch(t, base, page)
		if !strings.Contains(body, `data-fui-widget="site-toasts"`) {
			t.Errorf("global widget site-toasts missing from %s", page)
		}
	}
}

// CONTRACT: Widgets are inlined just before </body>, not anywhere
// else. Apps' layout/CSS expects the widget chrome to live at body
// level (position: fixed surfaces); putting them mid-page would
// break stacking-context isolation.
func TestArchSSR_InlineLocationIsBeforeBodyClose(t *testing.T) {
	base := archStartServer(t)
	page := archFetch(t, base, "/components/modal?modal=user-edit&user_id=42")

	widgetIdx := strings.Index(page, `data-fui-widget="site-demo-modal"`)
	bodyClose := strings.LastIndex(page, "</body>")
	if widgetIdx < 0 || bodyClose < 0 {
		t.Fatal("either widget or </body> missing — page malformed")
	}
	if widgetIdx > bodyClose {
		t.Error("contract violation: widget inlined AFTER </body>")
	}
	// The widget should be in the last 10% of the body — close to
	// </body> means "between </main> and </body>", not mid-content.
	if widgetIdx < bodyClose-len(page)/10 {
		// Loose heuristic; primarily a sanity check that we're not
		// emitting the widget inside <main> or earlier sections.
		t.Logf("note: widget at byte %d, body close at %d (page %d) — verify the inline point is at end-of-body",
			widgetIdx, bodyClose, len(page))
	}
}
