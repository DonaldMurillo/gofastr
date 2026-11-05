//go:build darwin && arm64 && !desktop_e2e

package macos

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

func TestStyleOriginUsesPrimary(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^$")
	command.Env = append(os.Environ(), "GOFASTR_TEST_STYLE_ORIGIN=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("style coordinate check: %v\n%s", err, output)
	}
}

func styleOriginCheck() int {
	objc.OpenFrameworks()
	keyScreen := objc.ID(objc.Send(objc.Class("NSScreen"), objc.Sel("mainScreen")))
	keyHeight := 0.0
	if keyScreen != 0 {
		keyHeight = objc.SendRectRet(keyScreen, objc.Sel("frame")).H
	}
	screens := twoScreens()
	screens[0].frame.H = keyHeight + 1000
	left, top, height := 1600, 100, 250
	style := desktop.WindowStyle{X: &left, Y: &top}
	want := frameToRect(desktop.Frame{X: left, Y: top, Height: height}, screens)
	actualX, actualY, ok := styleFrameOrigin(style, height, screens)
	if !ok || actualX != want.X || actualY != want.Y {
		fmt.Printf("style origin (%v, %v, %v), want primary-screen (%v, %v); key-screen height %v\n", actualX, actualY, ok, want.X, want.Y, keyHeight)
		return 1
	}
	return 0
}
