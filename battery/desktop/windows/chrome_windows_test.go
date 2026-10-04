//go:build windows && amd64

package windows

import (
	"syscall"
	"testing"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/win32"
)

func TestImmersiveDarkModeFollowsAppsTheme(t *testing.T) {
	if got := immersiveDarkModeValue(true); got != 0 {
		t.Fatalf("light theme DWM value = %d, want 0", got)
	}
	if got := immersiveDarkModeValue(false); got != 1 {
		t.Fatalf("dark theme DWM value = %d, want 1", got)
	}
}

func TestCustomCaptionMarginsReserveMenuAndCaption(t *testing.T) {
	style := desktop.WindowStyle{Chrome: desktop.ChromeUnified}
	got := customCaptionMargins(style, false, 96)
	if got.Top != customCaptionHeight(96) || got.Top <= 0 {
		t.Fatalf("menu-less caption margin = %+v, want top %d", got, customCaptionHeight(96))
	}
	if got := customCaptionMargins(style, true, 96); got != (win32.Margins{}) {
		t.Fatalf("caption margin with a menu = %+v, want zero", got)
	}
	if got := customCaptionMargins(desktop.WindowStyle{}, false, 96); got != (win32.Margins{}) {
		t.Fatalf("standard-frame margin = %+v, want zero", got)
	}
}

func TestImmersiveColorSetMessage(t *testing.T) {
	setting, err := syscall.UTF16PtrFromString("ImmersiveColorSet")
	if err != nil {
		t.Fatal(err)
	}
	if !isImmersiveColorSetMessage(wmSettingChange, uintptr(unsafe.Pointer(setting))) {
		t.Fatal("WM_SETTINGCHANGE ImmersiveColorSet was not recognized")
	}
	if isImmersiveColorSetMessage(wmSettingChange, 0) || isImmersiveColorSetMessage(wmSettingChange+1, uintptr(unsafe.Pointer(setting))) {
		t.Fatal("unrelated setting-change message was recognized")
	}
}
