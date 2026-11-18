//go:build darwin && (arm64 || amd64) && !desktop_e2e

package macos

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

func TestSidebarCanStartAtZero(t *testing.T) {
	nativeCheck(t, "GOFASTR_TEST_SIDEBAR_ZONE", "native sidebar")
}

func sidebarZoneCheck() int {
	objc.OpenFrameworks()
	objc.Send(objc.Class("NSApplication"), objc.Sel("sharedApplication"))
	window := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("NSWindow"), objc.Sel("alloc"))),
		objc.Sel("initWithContentRect:styleMask:backing:defer:"), objc.Rect{W: 400, H: 300}, maskTitled, 2, 0))
	configuration := objc.Send(objc.ID(objc.Send(objc.Class("WKWebViewConfiguration"), objc.Sel("alloc"))), objc.Sel("init"))
	webView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("WKWebView"), objc.Sel("alloc"))),
		objc.Sel("initWithFrame:configuration:"), objc.Rect{W: 400, H: 300}, configuration))
	objc.Send(window, objc.Sel("setContentView:"), uintptr(webView))
	chrome := applyWindowMaterial(window, webView, desktop.WindowStyle{Material: desktop.MaterialSidebar}, 0, false, slog.Default())
	for _, width := range []int{240, 0, 160} {
		chrome.setSidebarWidth(window, width)
		if chrome.zoneView == 0 || objc.SendRectRet(chrome.zoneView, objc.Sel("frame")).W != float64(width) {
			fmt.Printf("sidebar width %d: no matching native zone\n", width)
			return 1
		}
		if objc.ID(objc.Send(chrome.zoneView, objc.Sel("superview"))) == 0 {
			fmt.Println("sidebar zone is not installed in the window")
			return 1
		}
	}
	return 0
}
