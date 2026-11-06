package macos

import "github.com/DonaldMurillo/gofastr/battery/desktop"

func errWindowClosed() *desktop.Error {
	return desktop.ErrWindowClosed
}
