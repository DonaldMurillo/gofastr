//go:build windows && amd64

package windows

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

const baseDPI = 96

func windowDPI(hwnd uintptr) uint32 {
	dpi := win32.WindowDPI(hwnd)
	if dpi == 0 {
		return baseDPI
	}
	return dpi
}

func pointsToPixels(points int, dpi uint32) int32 {
	if dpi == 0 {
		dpi = baseDPI
	}
	value := int64(points) * int64(dpi)
	if value < 0 {
		value -= baseDPI / 2
	} else {
		value += baseDPI / 2
	}
	return int32(value / baseDPI)
}

func pixelsToPoints(pixels int32, dpi uint32) int {
	if dpi == 0 {
		dpi = baseDPI
	}
	value := int64(pixels) * baseDPI
	if value < 0 {
		value -= int64(dpi) / 2
	} else {
		value += int64(dpi) / 2
	}
	return int(value / int64(dpi))
}

// frameFromWindowRect keeps screen origins in one virtual-desktop scale
// (system DPI) while expressing the window's size in its monitor's
// device-independent points. A per-window scale cannot be applied to an
// absolute virtual-desktop origin on mixed-DPI monitors.
func frameFromWindowRect(hwnd uintptr, left, top, right, bottom int32) desktop.Frame {
	return desktop.Frame{
		X:      pixelsToPoints(left, win32.SystemDPI()),
		Y:      pixelsToPoints(top, win32.SystemDPI()),
		Width:  pixelsToPoints(right-left, windowDPI(hwnd)),
		Height: pixelsToPoints(bottom-top, windowDPI(hwnd)),
	}
}
