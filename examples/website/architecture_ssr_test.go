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

// archStartServer brings up the live website backed by setupServer()
// — exactly the binary served at :8082. Returns the base URL.
func archStartServer(t *testing.T) string {
	t.Helper()
	fwApp, _ := setupServer()
	srv := httptest.NewServer(fwApp.Router)
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

	if strings.Contains(page, `data-fui-widget="components-confirm"`) {
		t.Error("contract violation: hidden widget 'components-confirm' must NOT be inlined into the page response; runtime fetches chrome lazily")
	}
	if strings.Contains(page, "Delete this thing?") {
		t.Error("contract violation: hidden widget body text bled into the page; the SSR layer is inlining hidden widgets")
	}
}

// CONTRACT: A deep-link-matched widget IS inlined into the page
// response with its content visible — search engines and screen
// readers see the modal as part of the document, no JS required to
// reveal it. Hydration on this element is a no-op DOM-wise.
func TestArchSSR_DeepLinkWidgetInlined(t *testing.T) {
	base := archStartServer(t)
	page := archFetch(t, base, "/components/modal?modal=user-edit&user_id=42")

	if !strings.Contains(page, `data-fui-widget="components-user-edit"`) {
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
	// site-toasts is registered without .Hidden() in component_demos.go
	// so it should appear on every page.
	for _, path := range []string{"/", "/about", "/components/", "/components/modal"} {
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

	// Find the user-edit widget's outermost tag and assert no `hidden`.
	idx := strings.Index(page, `data-fui-widget="components-user-edit"`)
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
// has NO `[data-fui-widget="components-confirm"]` element. The hidden
// widget is fetched lazily — apps must rely on the chrome endpoint
// rather than assume the element exists at page-load time.
func TestArchSSR_BareURLHasNoHiddenWidgets(t *testing.T) {
	base := archStartServer(t)
	page := archFetch(t, base, "/components/modal")

	for _, hidden := range []string{
		`data-fui-widget="components-confirm"`,
		`data-fui-widget="components-drawer"`,
		`data-fui-widget="components-filter-drawer"`,
		`data-fui-widget="components-user-edit"`,
		`data-fui-widget="ui-sidebar-drawer"`,
	} {
		if strings.Contains(page, hidden) {
			t.Errorf("contract violation: hidden widget %q was inlined into bare /components/modal", hidden)
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

	widgetIdx := strings.Index(page, `data-fui-widget="components-user-edit"`)
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
