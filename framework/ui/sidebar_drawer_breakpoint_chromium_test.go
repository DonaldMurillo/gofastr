//go:build chromium

package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// DrawerBreakpoint lg moves the sidebar's collapse switch from below md
// (48rem) to below lg (64rem). Hard rule 9: which form is on screen at a
// given width is layout, invisible to a stylesheet string check — the
// scoping classes could ride the markup while a media query still keys
// on the old width. This renders both postures in Chrome and measures
// which of the inline column and the hamburger trigger is visible.
func TestDrawerBreakpointLGMovesTheSwitch(t *testing.T) {
	cfgFor := func(bp StackBreakpoint) SidebarConfig {
		return SidebarConfig{
			NavLabel:         "Projects",
			DrawerBreakpoint: bp,
			Items:            []SidebarItem{{Label: "Project", Href: "/workspace"}},
		}
	}
	rendered := map[StackBreakpoint]string{}
	for _, bp := range []StackBreakpoint{StackBelowMD, StackBelowLG} {
		html, err := component.SafeRenderCtx(context.Background(), Sidebar(cfgFor(bp)))
		if err != nil {
			t.Fatalf("sidebar render: %v", err)
		}
		rendered[bp] = string(html)
	}
	css := sidebarStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bp := StackBreakpoint(r.URL.Query().Get("bp"))
		body := rendered[StackBelowMD]
		if bp == StackBelowLG {
			body = rendered[StackBelowLG]
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><head><style>" + css + "</style></head><body>" + body + "</body></html>"))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 90*time.Second)
	defer cancelTimeout()

	// posture returns the computed display of the inline column and the
	// drawer trigger at the emulated width, on the named page.
	posture := func(bp string, width int64) (inline, trigger string) {
		if err := chromedp.Run(ctx,
			chromedp.EmulateViewport(width, 800),
			chromedp.Navigate(srv.URL+"/?bp="+bp),
			chromedp.Poll(`!!document.querySelector('.fui-sidebar__inline a')`, nil,
				chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-sidebar__inline')).display`, &inline),
			chromedp.Evaluate(`getComputedStyle(document.querySelector('.fui-sidebar__hamburger')).display`, &trigger),
		); err != nil {
			t.Fatalf("chromedp (%s at %d): %v", bp, width, err)
		}
		return inline, trigger
	}

	// 900px sits between md (48rem = 768px) and lg (64rem = 1024px):
	// the default keeps the column, the lg option has the drawer.
	if inline, trigger := posture("", 900); inline == "none" || trigger != "none" {
		t.Errorf("default at 900px: inline column should show and trigger hide, got inline=%q trigger=%q", inline, trigger)
	}
	if inline, trigger := posture("lg", 900); inline != "none" || trigger == "none" {
		t.Errorf("lg at 900px: trigger should show and inline column hide, got inline=%q trigger=%q", inline, trigger)
	}
	// 1280px is at/above both breakpoints: every posture shows the column.
	for _, bp := range []string{"", "lg"} {
		if inline, trigger := posture(bp, 1280); inline == "none" || trigger != "none" {
			t.Errorf("bp=%q at 1280px: inline column should show and trigger hide, got inline=%q trigger=%q", bp, inline, trigger)
		}
	}
	// Below md both postures collapse: the drawer serves navigation.
	for _, bp := range []string{"", "lg"} {
		if inline, trigger := posture(bp, 600); inline != "none" || trigger == "none" {
			t.Errorf("bp=%q at 600px: trigger should show and inline column hide, got inline=%q trigger=%q", bp, inline, trigger)
		}
	}
}
