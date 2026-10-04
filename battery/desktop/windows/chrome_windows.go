//go:build windows && amd64

package windows

import (
	"golang.org/x/sys/windows/registry"

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
	// DWM accepts the inverse of AppsUseLightTheme: zero follows light mode,
	// one follows dark mode. Leave the OS default untouched if the preference
	// cannot be read.
	if light, ok := appsUseLightTheme(); ok {
		_ = win32.DwmSetWindowAttribute(hwnd, dwmaUseImmersiveDarkMode, immersiveDarkModeValue(light))
	}
	_ = win32.DwmSetWindowAttribute(hwnd, dwmaWindowCornerPreference, dwmCornerRound)

	custom := style.Chrome == desktop.ChromeHiddenTitle || style.Chrome == desktop.ChromeUnified
	_ = win32.DwmExtendFrameIntoClientArea(hwnd, customCaptionMargins(style, win32.Menu(hwnd) != 0, windowDPI(hwnd)))
	if !custom {
		return false
	}
	// Keep the standard system caption buttons and clear only the caption
	// fill. With a transparent WebView background, Mica/Acrylic continues
	// cleanly from the title bar into the app surface.
	return win32.DwmSetWindowAttribute(hwnd, dwmaCaptionColor, dwmColorNone) == nil
}

func appsUseLightTheme() (bool, bool) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false, false
	}
	defer key.Close()
	value, _, err := key.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false, false
	}
	return value != 0, true
}

func immersiveDarkModeValue(appsUseLightTheme bool) uint32 {
	if appsUseLightTheme {
		return 0
	}
	return 1
}

func customCaptionMargins(style desktop.WindowStyle, hasMenu bool, dpi uint32) win32.Margins {
	if hasMenu || (style.Chrome != desktop.ChromeHiddenTitle && style.Chrome != desktop.ChromeUnified) {
		return win32.Margins{}
	}
	return win32.Margins{Top: customCaptionHeight(dpi)}
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
