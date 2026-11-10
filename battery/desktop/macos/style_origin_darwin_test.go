//go:build darwin && (arm64 || amd64) && !desktop_e2e

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
	actualX, actualY, calls, reads := 0.0, 0.0, 0, 0
	window := objc.ID(123)
	apply := func(current desktop.WindowStyle) {
		applyWindowStyle(window, 0, current, height, windowStylePlacement{screens: func() []screenInfo {
			reads++
			return screens
		}, setOrigin: func(target objc.ID, originX, originY float64) {
			if target != window {
				panic("style origin targeted a different window")
			}
			actualX, actualY = originX, originY
			calls++
		}})
	}
	apply(style)
	if calls != 1 || reads != 1 || actualX != want.X || actualY != want.Y {
		fmt.Printf("style caller origin (%v, %v), setter calls %d, screen reads %d; want primary-screen (%v, %v); key-screen height %v\n", actualX, actualY, calls, reads, want.X, want.Y, keyHeight)
		return 1
	}
	for _, incomplete := range []desktop.WindowStyle{{}, {X: &left}, {Y: &top}} {
		apply(incomplete)
		if calls != 1 {
			fmt.Println("style caller set an incomplete origin")
			return 1
		}
	}
	return 0
}
