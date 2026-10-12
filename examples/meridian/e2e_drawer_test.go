package main

// Phone checks for the MountSidebar drawer: below md the drawer is the
// app shell's only navigation, so it must carry everything the desktop
// sidebar column does for the same signed-in session.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

const drawerSel = `[data-cui-widget="ui-sidebar-drawer"]`

// openPhoneDrawer signs in at desktop width (the login flow waits on the
// inline sidebar), drops to a 375px phone viewport, loads path and opens
// the sidebar drawer from its hamburger. It returns the drawer's text and
// saves a screenshot of the open drawer under the test's artifact dir.
func openPhoneDrawer(t *testing.T, path string, extra ...chromedp.Action) string {
	t.Helper()
	base := e2eBootApp(t)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800))
	e2eLogin(t, ctx, base)
	var text string
	var shot []byte
	actions := []chromedp.Action{
		chromedp.EmulateViewport(375, 812),
		chromedp.Navigate(base + path),
		chromedp.WaitVisible(`.fui-sidebar__hamburger`, chromedp.ByQuery),
		chromedp.Click(`.fui-sidebar__hamburger`, chromedp.ByQuery),
		chromedp.WaitVisible(drawerSel+` .fui-sidebar__nav`, chromedp.ByQuery),
		// Let the slide-in transition settle before reading and capturing.
		chromedp.Sleep(600 * time.Millisecond),
	}
	actions = append(actions, extra...)
	actions = append(actions,
		chromedp.Text(drawerSel, &text, chromedp.ByQuery),
		chromedp.CaptureScreenshot(&shot),
	)
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("open phone drawer on %s: %v", path, err)
	}
	png := filepath.Join(t.ArtifactDir(), "drawer.png")
	if err := os.WriteFile(png, shot, 0o600); err != nil {
		t.Fatalf("write screenshot: %v", err)
	}
	t.Logf("drawer screenshot: %s", png)
	return text
}

// A signed-in phone user must find Sign out in the drawer: the desktop
// column renders it from the request's session, and the drawer used to
// render the footer of a config built once at startup, anonymous forever.
func TestE2E_PhoneDrawerOffersSignOut(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	text := openPhoneDrawer(t, "/app/customers")
	if !strings.Contains(text, "Sign out") {
		t.Errorf("signed-in phone drawer offers no Sign out; drawer text:\n%s", text)
	}
}

// The drawer chrome is fetched after load and rendered without a
// current path, so only the runtime's active-link sweep can mark the
// entry for the page the drawer opened over, as the desktop column is.
func TestE2E_PhoneDrawerMarksCurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	var current string
	openPhoneDrawer(t, "/app/customers", chromedp.Evaluate(`(() => {
		const a = document.querySelector('`+drawerSel+` a[aria-current="page"]');
		return a ? a.getAttribute('href') : '';
	})()`, &current))
	if current != "/app/customers" {
		t.Errorf("phone drawer marks %q as the current entry on /app/customers, want /app/customers", current)
	}
}

// A <button> takes the UA's font, not the page's, unless its rule says
// font: inherit; the drawer's close button and the footer's theme
// toggle rendered in Arial beside rows set in the app's font.
func TestE2E_PhoneDrawerButtonsUseAppFont(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	var fonts map[string]string
	openPhoneDrawer(t, "/app/customers", chromedp.Evaluate(`(() => {
		const f = (s) => { const el = document.querySelector('`+drawerSel+` ' + s); return el ? getComputedStyle(el).fontFamily : 'missing'; };
		return {row: f('.fui-sidebar__link'), close: f('.fui-sidebar__drawer-close'), toggle: f('.fui-theme-toggle')};
	})()`, &fonts))
	for _, part := range []string{"close", "toggle"} {
		if fonts[part] != fonts["row"] {
			t.Errorf("drawer %s button font is %q, the rows' %q", part, fonts[part], fonts["row"])
		}
	}
}
