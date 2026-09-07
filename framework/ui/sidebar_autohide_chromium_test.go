//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: whether the auto-hide rail actually widens on reveal is
// layout, invisible to a stylesheet string check. The reveal rule shipped
// for a week with its selector list ending in a comma and no opening
// brace; the browser dropped the whole rule, so hover and keyboard focus
// left the column at 64px while the base rules brought the labels and
// footer back inside it. Every selector-text test stayed green. This
// renders the variant in Chrome, focuses a link, and measures.
func TestAutoHideRailWidensOnFocus(t *testing.T) {
	page := component.RenderComponent(Sidebar(SidebarConfig{
		Title:   "App",
		Variant: SidebarAutoHide,
		Items: []SidebarItem{
			{Label: "Overview", Href: "/"},
			{Label: "Customers", Href: "/customers"},
		},
		Footer: "<a href=\"/logout\">Sign out</a>",
	}))
	css := sidebarStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>body{margin:0;display:flex;min-height:100vh}%s
%s</style>%s<main>content</main>`, theme.Default().CSSCustomProperties(), css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	// Computed width is the content box (64px / 220px); the border box
	// adds the rail padding and would tie the test to a spacing token.
	const width = `getComputedStyle(document.querySelector('.ui-sidebar__inline')).width`
	var rest, revealed string
	// The malformed rule also swallowed the rest-state rule that follows
	// it, so the title and footer leaked into the 64px rail; pin both.
	const titleDisplay = `getComputedStyle(document.querySelector('.ui-sidebar__title')).display`
	var titleRest, titleRevealed string
	var restShot, revealedShot []byte
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		// >= md so the inline column is the one on screen, not the drawer.
		chromedp.EmulateViewport(1280, 800),
		// Poll on presence: chromedp.WaitVisible never settled on this
		// node (display:contents parent) even with the box painted.
		chromedp.Poll(`!!document.querySelector('.ui-sidebar__inline a')`, nil,
			chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(width, &rest),
		chromedp.Evaluate(titleDisplay, &titleRest),
		chromedp.CaptureScreenshot(&restShot),
		// Keyboard focus is the deterministic reveal path (:focus-within);
		// it is also the one that matters for a11y, hover being optional.
		chromedp.Evaluate(`document.querySelector('.ui-sidebar__inline a').focus()`, nil),
		// The width transitions over --duration-fast (150ms); wait it out.
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(width, &revealed),
		chromedp.Evaluate(titleDisplay, &titleRevealed),
		chromedp.CaptureScreenshot(&revealedShot),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if dir := os.Getenv("GOFASTR_VISUAL_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "sidebar-autohide-rest.png"), restShot, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "sidebar-autohide-revealed.png"), revealedShot, 0o600)
	}
	if rest != "64px" {
		t.Errorf("auto-hide rail at rest should be 64px wide, got %v", rest)
	}
	if revealed != "220px" {
		t.Errorf("auto-hide rail should widen to 220px on focus-within, got %v (rest %v)", revealed, rest)
	}
	if titleRest != "none" {
		t.Errorf("auto-hide rail at rest should hide the title, got display %q", titleRest)
	}
	if titleRevealed == "none" {
		t.Errorf("auto-hide reveal should show the title again, got display %q", titleRevealed)
	}
}
