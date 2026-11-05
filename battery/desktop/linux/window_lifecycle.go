package linux

import "github.com/DonaldMurillo/gofastr/battery/desktop"

func errClosed() error {
	return desktop.ErrWindowClosed
}

func closeNativeWindow(isClosed func() bool, dispatch func(func()) error, closeWindow func()) error {
	if isClosed() {
		return nil
	}
	return dispatch(func() {
		if !isClosed() {
			closeWindow()
		}
	})
}
