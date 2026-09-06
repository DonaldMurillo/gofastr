//go:build darwin && arm64

package desktop

import "testing"

// The style math as pure functions, unit level: the NSWindowStyleMask
// bits, the floating window level, and the top-left to bottom-left
// origin flip. The values are AppKit's (verified against NSWindow.h
// and CGWindowLevel.h in the SDK on this machine); the e2e step in
// shell_darwin_e2e_test.go reads them back off a live NSPanel.

func TestStyleMaskBits(t *testing.T) {
	fixed := false
	cases := []struct {
		name  string
		style WindowStyle
		want  uintptr
	}{
		// ChromeDefault keeps the PoC's titled|closable|resizable.
		{"default", WindowStyle{}, maskTitled | maskClosable | maskResizable},
		// HiddenTitle lets the page paint under the title bar.
		{"hidden title", WindowStyle{Chrome: ChromeHiddenTitle}, maskTitled | maskClosable | maskResizable | maskFullSizeContentView},
		// Borderless is 0: no title bar, no close button, no resize.
		{"none", WindowStyle{Chrome: ChromeNone}, 0},
		// A panel adds nonactivatingPanel to whatever chrome it has.
		{"panel default", WindowStyle{Panel: true}, maskTitled | maskClosable | maskResizable | maskNonactivatingPanel},
		{"panel none", WindowStyle{Chrome: ChromeNone, Panel: true}, maskNonactivatingPanel},
		// Resizable=false clears only the resizable bit.
		{"fixed default", WindowStyle{Resizable: &fixed}, maskTitled | maskClosable},
		// A fixed borderless window stays borderless (nothing to clear).
		{"fixed none", WindowStyle{Chrome: ChromeNone, Resizable: &fixed}, 0},
	}
	for _, tc := range cases {
		if got := windowStyleMask(tc.style); got != tc.want {
			t.Errorf("%s: mask = %#x, want %#x", tc.name, got, tc.want)
		}
	}
	notResizable := false
	if got := windowStyleMask(WindowStyle{Resizable: &notResizable}); got&maskResizable != 0 {
		t.Errorf("resizable=false kept the resizable bit: %#x", got)
	}
}

func TestStyleWindowLevel(t *testing.T) {
	if got := windowLevel(WindowStyle{}); got != normalWindowLevel {
		t.Errorf("default level = %d, want %d", got, normalWindowLevel)
	}
	if got := windowLevel(WindowStyle{Float: true}); got != floatingWindowLevel {
		t.Errorf("float level = %d, want %d", got, floatingWindowLevel)
	}
	// Panel implies Float: the widget stays above normal windows even
	// though the caller never said Float.
	if got := windowLevel(WindowStyle{Panel: true}); got != floatingWindowLevel {
		t.Errorf("panel level = %d, want %d (panel implies float)", got, floatingWindowLevel)
	}
}

func TestStyleFrameOrigin(t *testing.T) {
	x, y := 40, 100
	// AppKit's bottom-left origin from a top-left based Y on a screen
	// 800 points tall with a 200-point window.
	cases := []struct {
		name   string
		style  WindowStyle
		wantX  float64
		wantY  float64
		wantOK bool
	}{
		{"top left corner", WindowStyle{X: &x, Y: &y}, 40, 500, true},
		{"flush top", WindowStyle{X: &x, Y: new(int)}, 40, 600, true},
	}
	for _, tc := range cases {
		gotX, gotY, ok := frameOrigin(tc.style, 200, 800)
		if ok != tc.wantOK || gotX != tc.wantX || gotY != tc.wantY {
			t.Errorf("%s: origin = (%v, %v, ok=%v), want (%v, %v, ok=%v)",
				tc.name, gotX, gotY, ok, tc.wantX, tc.wantY, tc.wantOK)
		}
	}
	// No coordinates means centered (the shell's default), not (0,0).
	if _, _, ok := frameOrigin(WindowStyle{}, 200, 800); ok {
		t.Error("frameOrigin reported an origin for a style with no X/Y, want ok=false (centered)")
	}
	// One coordinate alone is not a position: honored only as a pair.
	if _, _, ok := frameOrigin(WindowStyle{X: &x}, 200, 800); ok {
		t.Error("frameOrigin honored X without Y, want ok=false")
	}
	if _, _, ok := frameOrigin(WindowStyle{Y: &y}, 200, 800); ok {
		t.Error("frameOrigin honored Y without X, want ok=false")
	}
}
