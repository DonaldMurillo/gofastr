package windows

import "github.com/DonaldMurillo/gofastr/battery/desktop"

func withWindowHandle(hwnd uintptr, closed bool, apply func(uintptr) error) error {
	if closed || hwnd == 0 {
		return errWindowClosed()
	}
	return apply(hwnd)
}

func errWindowClosed() error {
	return &desktop.Error{Code: desktop.CodeUnsupported, Message: "window is not open"}
}
