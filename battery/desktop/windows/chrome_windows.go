//go:build windows && amd64

package windows

import (
	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

const (
	dwmaUseImmersiveDarkMode   = 20
	dwmaWindowCornerPreference = 33
	dwmaCaptionColor           = 35
	dwmaSystemBackdropType     = 38

	dwmColorNone       = 0xFFFFFFFE
	dwmCornerRound     = 2
	dwmBackdropMica    = 2
	dwmBackdropAcrylic = 3
	dwmBackdropMicaAlt = 4
)

func applyWindowChrome(hwnd uintptr, style desktop.WindowStyle) (captionTransparent bool) {
	// Let the system keep the non-client frame in step with the user's
	// light/dark setting, and request the Windows 11 corner treatment.
	_ = win32.DwmSetWindowAttribute(hwnd, dwmaUseImmersiveDarkMode, 1)
	_ = win32.DwmSetWindowAttribute(hwnd, dwmaWindowCornerPreference, dwmCornerRound)

	if style.Chrome != desktop.ChromeHiddenTitle && style.Chrome != desktop.ChromeUnified {
		return false
	}
	// Keep the standard system caption buttons and clear only the caption
	// fill. With a transparent WebView background, Mica/Acrylic continues
	// cleanly from the title bar into the app surface.
	return win32.DwmSetWindowAttribute(hwnd, dwmaCaptionColor, dwmColorNone) == nil
}

func applyWindowMaterial(hwnd uintptr, material desktop.WindowMaterial) string {
	backdrop := uint32(0)
	name := "none"
	switch material {
	case desktop.MaterialWindow:
		backdrop, name = dwmBackdropMica, "mica"
	case desktop.MaterialSidebar:
		backdrop, name = dwmBackdropMicaAlt, "mica-alt"
	case desktop.MaterialGlass:
		backdrop, name = dwmBackdropAcrylic, "acrylic"
	default:
		return "none"
	}
	if err := win32.DwmSetWindowAttribute(hwnd, dwmaSystemBackdropType, backdrop); err != nil {
		return "none"
	}
	return name
}

func setTopMost(hwnd uintptr) { win32.SetTopMost(hwnd) }
