//go:build darwin && arm64

package macos

import (
	"math"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/objc"
)

// The window-frame math as pure functions, unit level: the top-left to
// bottom-left flip and back, and the "screen that is gone" check. The
// screen geometry comes from readScreens at runtime; here it is
// hand-built so a second monitor and an unplugged one are testable on
// any machine. The native e2e step (native_e2e_test.go) proves the same
// math against the live NSScreen list.

// twoScreens builds a primary screen 1440x900 at the origin (visible
// area losing a 25-point menu bar at the top) and a 1920x1080
// secondary to the right whose bottom sits at the same line.
func twoScreens() []screenInfo {
	return []screenInfo{
		{frame: objc.Rect{X: 0, Y: 0, W: 1440, H: 900}, visible: objc.Rect{X: 0, Y: 0, W: 1440, H: 875}},
		{frame: objc.Rect{X: 1440, Y: 0, W: 1920, H: 1000}, visible: objc.Rect{X: 1440, Y: 0, W: 1920, H: 975}},
	}
}

func TestFrameFlipRoundTrips(t *testing.T) {
	screens := twoScreens()
	cases := []desktop.Frame{
		{X: 0, Y: 0, Width: 800, Height: 600},
		{X: 100, Y: 850, Width: 200, Height: 40},   // bottom edge of the primary
		{X: 1500, Y: 400, Width: 640, Height: 480}, // on the secondary
		{X: 1400, Y: 890, Width: 100, Height: 100}, // straddling the seam
	}
	for _, f := range cases {
		rect := frameToRect(f, screens)
		// The bottom-left Y of a frame whose top-left Y is 0 on the
		// primary is base - Y - height.
		if want := 900.0 - float64(f.Y) - float64(f.Height); rect.Y != want {
			t.Errorf("frame %+v: rect.Y = %v, want %v", f, rect.Y, want)
		}
		got := rectToFrame(rect, screens)
		if got != f {
			t.Errorf("round trip: %+v -> rect %+v -> %+v", f, rect, got)
		}
	}
}

func TestFrameFlipUsesPrimaryNotMainScreen(t *testing.T) {
	screens := twoScreens()
	// A window on the SECONDARY screen still flips against the primary
	// (screens[0]): its global bottom-left Y is computed from the
	// primary's height, whatever screen holds the key window.
	f := desktop.Frame{X: 1600, Y: 300, Width: 400, Height: 250}
	rect := frameToRect(f, screens)
	if rect.Y != 900-300-250 {
		t.Fatalf("secondary-screen frame flipped against the wrong base: rect.Y = %v, want %v", rect.Y, 900-300-250)
	}
	// With no screens at all (headless) the base falls back to
	// mainScreenHeight; the math still produces a value, never a panic.
	_ = frameToRect(f, nil)
}

func TestFrameVisibleOnScreens(t *testing.T) {
	screens := twoScreens()
	visible := []desktop.Frame{
		{X: 10, Y: 30, Width: 800, Height: 600},    // well inside the primary
		{X: 1500, Y: 30, Width: 400, Height: 300},  // on the secondary
		{X: 1400, Y: 400, Width: 260, Height: 100}, // straddling the seam, enough on the secondary
	}
	for _, f := range visible {
		if !frameVisibleOnScreens(f, screens) {
			t.Errorf("frame %+v should be visible", f)
		}
	}
	gone := []desktop.Frame{
		{X: 4000, Y: 100, Width: 800, Height: 600},  // right of every screen
		{X: -2000, Y: 100, Width: 800, Height: 600}, // left of every screen
		{X: 1420, Y: 880, Width: 40, Height: 30},    // on a screen but under the 100x50 overlap
		{X: 1430, Y: 100, Width: 45, Height: 300},   // tall but too thin on both sides of the seam
		{X: 10, Y: 0, Width: 0, Height: 600},        // zero width
	}
	for _, f := range gone {
		if frameVisibleOnScreens(f, screens) {
			t.Errorf("frame %+v should be dropped", f)
		}
	}
	// The menu bar excludes the top 25 points: a frame pushed under it
	// keeps only 35 of its 60 points visible, under the 50-point
	// minimum.
	if frameVisibleOnScreens(desktop.Frame{X: 100, Y: 0, Width: 400, Height: 60}, screens) {
		t.Error("a frame with 35 visible points under the menu bar was kept")
	}
	// No screens: nothing is visible, everything centers.
	if frameVisibleOnScreens(desktop.Frame{X: 0, Y: 0, Width: 800, Height: 600}, nil) {
		t.Error("a frame with no screens to land on was kept")
	}
}
func TestScreenFlipBase(t *testing.T) {
	if got := screenFlipBase(twoScreens()); got != 900 {
		t.Fatalf("flip base = %v, want the primary screen's 900", got)
	}
	// No screens: zero, and no AppKit call (the unit suite runs before
	// any framework loads; mainScreenHeight would panic here).
	if got := screenFlipBase(nil); got != 0 {
		t.Fatalf("empty fallback = %v, want 0", got)
	}
}

// TestFrameFlipFloatsRound is the rounding contract: rectToFrame rounds
// to the nearest point so a frame read back after a fractional
// setFrame (a scaled display reports integral points, but the contract
// should not depend on it) still compares equal.
func TestFrameFlipFloatsRound(t *testing.T) {
	screens := twoScreens()
	rect := objc.Rect{X: 10.4, Y: 100.6, W: 800.5, H: 600.49}
	f := rectToFrame(rect, screens)
	if f.X != 10 || f.Y != 199 || f.Width != 801 || f.Height != 600 {
		t.Fatalf("rectToFrame rounded to %+v", f)
	}
	if math.Abs(frameToRect(f, screens).X-10.4) > 1 {
		t.Fatal("the rounded frame drifted from the original rect")
	}
}
