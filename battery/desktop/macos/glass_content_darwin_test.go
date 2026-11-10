//go:build darwin && (arm64 || amd64) && !desktop_e2e

package macos

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

func TestGlassKeepsContentOnReduceToggle(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^$")
	command.Env = append(os.Environ(), "GOFASTR_TEST_GLASS_CONTENT=1")
	output, err := command.CombinedOutput()
	if failure, ok := err.(*exec.ExitError); ok && failure.ExitCode() == 77 {
		t.Skip("NSGlassEffectView requires macOS 26")
	}
	if err != nil {
		t.Fatalf("native glass check: %v\n%s", err, output)
	}
	t.Log(string(output))
}

func glassContentCheck() int {
	objc.OpenFrameworks()
	if !glassAvailable() {
		return 77
	}
	objc.Send(objc.Class("NSApplication"), objc.Sel("sharedApplication"))
	window := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("NSWindow"), objc.Sel("alloc"))),
		objc.Sel("initWithContentRect:styleMask:backing:defer:"), objc.Rect{W: 400, H: 300}, maskTitled, 2, 0))
	configuration := objc.Send(objc.ID(objc.Send(objc.Class("WKWebViewConfiguration"), objc.Sel("alloc"))), objc.Sel("init"))
	webView := objc.ID(objc.SendRect(objc.ID(objc.Send(objc.Class("WKWebView"), objc.Sel("alloc"))),
		objc.Sel("initWithFrame:configuration:"), objc.Rect{W: 400, H: 300}, configuration))
	objc.Send(window, objc.Sel("setContentView:"), uintptr(webView))
	chrome := applyWindowMaterial(window, webView, desktop.WindowStyle{Material: desktop.MaterialGlass}, 0, false, slog.Default())
	for cycle := range 3 {
		chrome.setReduced(window, webView, true)
		if objc.ID(objc.Send(window, objc.Sel("contentView"))) != webView {
			fmt.Printf("cycle %d: reduced window lost its web view\n", cycle)
			return 1
		}
		chrome.setReduced(window, webView, false)
		if objc.ID(objc.Send(window, objc.Sel("contentView"))) != chrome.glassView ||
			objc.Send(webView, objc.Sel("isDescendantOf:"), uintptr(chrome.glassView)) == 0 {
			fmt.Printf("cycle %d: restored glass has no web view descendant\n", cycle)
			return 1
		}
	}
	fmt.Println("three native glass/opaque/glass cycles retained the web view")
	return 0
}
