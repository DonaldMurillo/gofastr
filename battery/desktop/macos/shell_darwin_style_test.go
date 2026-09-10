//go:build darwin && arm64

package macos

import "github.com/DonaldMurillo/gofastr/battery/desktop"

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
		style desktop.WindowStyle
		want  uintptr
	}{
		// ChromeDefault keeps the PoC's titled window with all three
		// standard buttons: the miniaturize bit was missing once and
		// the yellow light rendered disabled in every active window.
		{"default", desktop.WindowStyle{}, maskTitled | maskClosable | maskMiniaturizable | maskResizable},
		// HiddenTitle lets the page paint under the title bar.
		{"hidden title", desktop.WindowStyle{Chrome: desktop.ChromeHiddenTitle}, maskTitled | maskClosable | maskMiniaturizable | maskResizable | maskFullSizeContentView},
		// Borderless is 0: no title bar, no close button, no resize.
		{"none", desktop.WindowStyle{Chrome: desktop.ChromeNone}, 0},
		// A panel adds nonactivatingPanel to whatever chrome it has.
		{"panel default", desktop.WindowStyle{Panel: true}, maskTitled | maskClosable | maskMiniaturizable | maskResizable | maskNonactivatingPanel},
		{"panel none", desktop.WindowStyle{Chrome: desktop.ChromeNone, Panel: true}, maskNonactivatingPanel},
		// Resizable=false clears only the resizable bit.
		{"fixed default", desktop.WindowStyle{Resizable: &fixed}, maskTitled | maskClosable | maskMiniaturizable},
		// A fixed borderless window stays borderless (nothing to clear).
		{"fixed none", desktop.WindowStyle{Chrome: desktop.ChromeNone, Resizable: &fixed}, 0},
	}
	for _, tc := range cases {
		if got := windowStyleMask(tc.style); got != tc.want {
			t.Errorf("%s: mask = %#x, want %#x", tc.name, got, tc.want)
		}
	}
	notResizable := false
	if got := windowStyleMask(desktop.WindowStyle{Resizable: &notResizable}); got&maskResizable != 0 {
		t.Errorf("resizable=false kept the resizable bit: %#x", got)
	}
}

func TestStyleWindowLevel(t *testing.T) {
	if got := windowLevel(desktop.WindowStyle{}); got != normalWindowLevel {
		t.Errorf("default level = %d, want %d", got, normalWindowLevel)
	}
	if got := windowLevel(desktop.WindowStyle{Float: true}); got != floatingWindowLevel {
		t.Errorf("float level = %d, want %d", got, floatingWindowLevel)
	}
	// Panel implies Float: the widget stays above normal windows even
	// though the caller never said Float.
	if got := windowLevel(desktop.WindowStyle{Panel: true}); got != floatingWindowLevel {
		t.Errorf("panel level = %d, want %d (panel implies float)", got, floatingWindowLevel)
	}
}

func TestStyleFrameOrigin(t *testing.T) {
	x, y := 40, 100
	// AppKit's bottom-left origin from a top-left based Y on a screen
	// 800 points tall with a 200-point window.
	cases := []struct {
		name   string
		style  desktop.WindowStyle
		wantX  float64
		wantY  float64
		wantOK bool
	}{
		{"top left corner", desktop.WindowStyle{X: &x, Y: &y}, 40, 500, true},
		{"flush top", desktop.WindowStyle{X: &x, Y: new(int)}, 40, 600, true},
	}
	for _, tc := range cases {
		gotX, gotY, ok := frameOrigin(tc.style, 200, 800)
		if ok != tc.wantOK || gotX != tc.wantX || gotY != tc.wantY {
			t.Errorf("%s: origin = (%v, %v, ok=%v), want (%v, %v, ok=%v)",
				tc.name, gotX, gotY, ok, tc.wantX, tc.wantY, tc.wantOK)
		}
	}
	// No coordinates means centered (the shell's default), not (0,0).
	if _, _, ok := frameOrigin(desktop.WindowStyle{}, 200, 800); ok {
		t.Error("frameOrigin reported an origin for a style with no X/Y, want ok=false (centered)")
	}
	// One coordinate alone is not a position: honored only as a pair.
	if _, _, ok := frameOrigin(desktop.WindowStyle{X: &x}, 200, 800); ok {
		t.Error("frameOrigin honored X without Y, want ok=false")
	}
	if _, _, ok := frameOrigin(desktop.WindowStyle{Y: &y}, 200, 800); ok {
		t.Error("frameOrigin honored Y without X, want ok=false")
	}
}

func TestStyleMaskUnified(t *testing.T) {
	// ChromeUnified is HiddenTitle's mask: the page paints under the
	// title bar, the toolbar strip merges into it.
	got := windowStyleMask(desktop.WindowStyle{Chrome: desktop.ChromeUnified})
	want := maskTitled | maskClosable | maskMiniaturizable | maskResizable | maskFullSizeContentView
	if got != want {
		t.Fatalf("unified mask = %#x, want %#x", got, want)
	}
	notResizable := false
	if got := windowStyleMask(desktop.WindowStyle{Chrome: desktop.ChromeUnified, Resizable: &notResizable}); got&maskResizable != 0 {
		t.Fatalf("unified fixed mask kept resizable: %#x", got)
	}
}

func TestResolveMaterial(t *testing.T) {
	cases := []struct {
		name  string
		style desktop.WindowMaterial
		glass bool
		want  desktop.WindowMaterial
	}{
		// none is none everywhere.
		{"none on 26", desktop.MaterialNone, true, desktop.MaterialNone},
		{"none below 26", desktop.MaterialNone, false, desktop.MaterialNone},
		// The sidebar zone has one shape: the zone vibrancy view.
		{"sidebar on 26", desktop.MaterialSidebar, true, desktop.MaterialSidebar},
		{"sidebar below 26", desktop.MaterialSidebar, false, desktop.MaterialSidebar},
		// Window and glass wrap in NSGlassEffectView on 26, vibrancy
		// below it; glass degrades to the window material.
		{"window on 26", desktop.MaterialWindow, true, desktop.MaterialGlass},
		{"window below 26", desktop.MaterialWindow, false, desktop.MaterialWindow},
		{"glass on 26", desktop.MaterialGlass, true, desktop.MaterialGlass},
		{"glass below 26", desktop.MaterialGlass, false, desktop.MaterialWindow},
		// The battery refuses unknown materials at New; the shell's
		// answer for one anyway is the opaque window.
		{"unknown", desktop.WindowMaterial("frosted"), true, desktop.MaterialNone},
	}
	for _, tc := range cases {
		if got := resolveMaterial(tc.style, tc.glass); got != tc.want {
			t.Errorf("%s: resolveMaterial = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestToolbarStyleName(t *testing.T) {
	// NSWindowToolbarStyle's enum order, from NSWindow.h in the SDK.
	for v, want := range map[uintptr]string{
		0: "automatic", 1: "expanded", 2: "preference", 3: "unified", 4: "unifiedCompact",
	} {
		if got := toolbarStyleName(v); got != want {
			t.Errorf("toolbarStyleName(%d) = %q, want %q", v, got, want)
		}
	}
	if got := toolbarStyleName(9); got != "" {
		t.Errorf("toolbarStyleName(9) = %q, want empty", got)
	}
}

func TestTitleVisibilityName(t *testing.T) {
	// NSWindowTitleVisibility, from NSWindow.h in the SDK: visible 0,
	// hidden 1.
	for v, want := range map[uintptr]string{0: "visible", 1: "hidden"} {
		if got := titleVisibilityName(v); got != want {
			t.Errorf("titleVisibilityName(%d) = %q, want %q", v, got, want)
		}
	}
	if got := titleVisibilityName(7); got != "" {
		t.Errorf("titleVisibilityName(7) = %q, want empty", got)
	}
}
