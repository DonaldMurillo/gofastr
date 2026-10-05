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
